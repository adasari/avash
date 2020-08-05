/*
Copyright © 2019 AVA Labs <collin@avalabs.org>
*/

package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ava-labs/gecko/snow"

	"github.com/ava-labs/avash/cfg"
	"github.com/ava-labs/avash/node"
	pmgr "github.com/ava-labs/avash/processmgr"
	dagwallet "github.com/ava-labs/avash/wallets/dags"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/spf13/cobra"

	"github.com/ava-labs/gecko/utils/crypto"

	"github.com/ybbus/jsonrpc"
)

// AVAXWalletCmd represents the avawallet command
var AVAXWalletCmd = &cobra.Command{
	Use:   "avaxwallet",
	Short: "Tools for interacting with AVAX Payments over the network.",
	Long: `Tools for interacting with AVAX Payments over the network. Using this 
	command you can create, send, and get the status of a transaction.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// AVAXWalletCreateCmd creates a new named wallet
var AVAXWalletCreateCmd = &cobra.Command{
	Use:   "create [wallet name] [networkID] [blockchainID] [txFee]",
	Short: "Creates a wallet.",
	Long:  `Creates a wallet persistent for this session.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 4 {
			log := cfg.Config.Log
			defer func() {
				if r := recover(); r != nil {
					log.Error("invalid blockchainID: %s", args[2])
				} else {
					log.Info("wallet created: %s", args[0])
				}
			}()
			networkID, _ := strconv.ParseUint(args[1], 10, 0)
			blockchainID, _ := ids.ShortFromString(args[2])
			txfee, _ := strconv.ParseUint(args[3], 10, 0)
			dagwallet.Wallets[args[0]] = dagwallet.NewWallet(uint32(networkID), blockchainID.LongID(), uint64(txfee))
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletNewKeyCmd creates a new private key
var AVAXWalletNewKeyCmd = &cobra.Command{
	Use:   "newkey",
	Short: "Creates a random private key.",
	Long:  `Creates a random private key.`,
	Run: func(cmd *cobra.Command, args []string) {
		log := cfg.Config.Log
		factory := crypto.FactorySECP256K1R{}
		if skGen, err := factory.NewPrivateKey(); err == nil {
			sk := skGen.(*crypto.PrivateKeySECP256K1R)
			fb := formatting.CB58{}
			fb.Bytes = sk.Bytes()
			log.Info("Pk:%s", fb.String())
		} else {
			log.Error("could not create private key")
		}
	},
}

// AVAXWalletAddKeyCmd adds a private key to a wallet
var AVAXWalletAddKeyCmd = &cobra.Command{
	Use:   "addkey [wallet name] [private key]",
	Short: "Adds a private key to a wallet.",
	Long:  `Adds a private key to a wallet from a b58 string and returns its address. Reminder: refresh the UTXOs after keys are imported.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 2 {
			log := cfg.Config.Log
			if w, ok := dagwallet.Wallets[args[0]]; ok {
				factory := crypto.FactorySECP256K1R{}
				fb := formatting.CB58{}
				fb.FromString(args[1])
				if skGen, err := factory.ToPrivateKey(fb.Bytes); err == nil {
					sk := skGen.(*crypto.PrivateKeySECP256K1R)
					w.ImportKey(sk)
					log.Info("Addr:%s", skGen.PublicKey().Address().String())
				} else {
					log.Error("unable to add key %s: %s", args[1], err.Error())
				}
			} else {
				log.Error("wallet not found: %s", args[0])
			}
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletMakeTxCmd will create a transaction and return its signed string
var AVAXWalletMakeTxCmd = &cobra.Command{
	Use:   "maketx [wallet name] [destination address] [amount]",
	Short: "Creates a signed transaction.",
	Long:  `Creates a signed transaction for an amount to an address. Returns the a string of the transaction.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 3 {
			log := cfg.Config.Log
			w, ok := dagwallet.Wallets[args[0]]
			if !ok {
				log.Error("wallet not found: %s", args[0])
				return
			}
			amount, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				log.Error("amount %s cannot convert to uint64", args[2])
				return
			}
			fb := formatting.CB58{}
			addr := strings.Split(args[1], "-")
			if len(addr) < 2 {
				log.Error("invalid prefixed address: %s", args[1])
				return
			}
			fb.FromString(strings.Split(args[1], "-")[1])
			toAddr, err := ids.ToShortID(fb.Bytes)
			if err != nil {
				log.Error(err.Error())
				return
			}
			signedTx, err := w.CreateTx(amount, 0, 1, []ids.ShortID{toAddr})
			if err != nil {
				log.Error("unable to create tx, check UTXO set")
				return
			}
			ctx := snow.DefaultContextTest()
			ctx.NetworkID = w.GetNetworkID()
			ctx.ChainID = w.GetSubnetID()
			if err := signedTx.Verify(ctx, 0); err != nil {
				log.Error("signedTx cannot verify")
				return
			}
			fb.Bytes = signedTx.Bytes()
			log.Info("Tx:%s", fb.String())
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletRemoveCmd will remove a transaction from the UTXO set
var AVAXWalletRemoveCmd = &cobra.Command{
	Use:   "remove [wallet name] [tx string]",
	Short: "Removes a transaction from a wallet's UTXO set.",
	Long:  `Removes a transaction from a wallet's UTXO set.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			cmd.Help()
			return
		}

		log := cfg.Config.Log
		w, ok := dagwallet.Wallets[args[0]]
		if !ok {
			log.Error("wallet not found: %s", args[0])
			return
		}

		fb := formatting.CB58{}
		fb.FromString(args[1])
		txBytes := fb.Bytes
		codec := spdagvm.Codec{}
		tx, err := codec.UnmarshalTx(txBytes)
		if err != nil {
			log.Error("cannot unmarshal tx: %s", args[1])
			return
		}

		for _, in := range tx.Ins() {
			utxoID := in.InputID()
			w.RemoveUtxo(utxoID)
		}

		log.Info("transaction removed: %s", args[1])
	},
}

// AVAXWalletSpendCmd will spend (update inputs and outputs) a transaction from the UTXO set
var AVAXWalletSpendCmd = &cobra.Command{
	Use:   "spend [wallet name] [tx string]",
	Short: "Spends a transaction from a wallet's UTXO set.",
	Long:  `Spends a transaction from a wallet's UTXO set.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			cmd.Help()
			return
		}

		log := cfg.Config.Log
		w, ok := dagwallet.Wallets[args[0]]
		if !ok {
			log.Error("wallet not found: %s", args[0])
			return
		}

		fb := formatting.CB58{}
		fb.FromString(args[1])
		txBytes := fb.Bytes
		codec := spdagvm.Codec{}
		tx, err := codec.UnmarshalTx(txBytes)
		if err != nil {
			log.Error("cannot unmarshal tx: %s", args[1])
			return
		}

		w.SpendTx(tx)
		log.Info("transaction spent: %s", args[1])
	},
}

// AVAXWalletSendCmd will send a transaction through a node
var AVAXWalletSendCmd = &cobra.Command{
	Use:   "send [node name] [tx string]",
	Short: "Sends a transaction to a node.",
	Long:  `Sends a transaction to a node.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 2 {
			log := cfg.Config.Log
			if meta, err := pmgr.ProcManager.Metadata(args[0]); err == nil {
				var md node.Metadata
				metaBytes := []byte(meta)
				if err := json.Unmarshal(metaBytes, &md); err == nil {
					jrpcloc := fmt.Sprintf("http://%s:%s/ext/bc/avm", md.Serverhost, md.HTTPport)
					rpcClient := jsonrpc.NewClient(jrpcloc)
					response, err := rpcClient.Call("avm.issueTx", struct {
						Tx string
					}{
						Tx: args[1],
					})
					if err != nil {
						log.Error("error sent tx: %s", args[1])
						log.Error("rpcClient returned error: %s", err.Error())
					} else if response.Error != nil {
						log.Error("error sent tx: %s", args[1])
						log.Error("rpcClient returned error: %d, %s", response.Error.Code, response.Error.Message)
					} else {
						var s struct {
							TxID string
						}
						err = response.GetObject(&s)
						if err != nil {
							log.Error("error on parsing response: %s", err.Error())
						} else {
							log.Info("TxID:%s", s.TxID)
						}
					}
				} else {
					log.Error("unable to unmarshal metadata for node %s: %s", args[0], err.Error())
				}
			} else {
				log.Error("node not found: %s", args[0])
			}
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletStatusCmd will get the status of a transaction for a particular node
var AVAXWalletStatusCmd = &cobra.Command{
	Use:   "status [node name] [tx id]",
	Short: "Checks the status of a transaction on a node.",
	Long:  `Checks the status of a transaction on a node.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 2 {
			log := cfg.Config.Log
			if meta, err := pmgr.ProcManager.Metadata(args[0]); err == nil {
				var md node.Metadata
				metaBytes := []byte(meta)
				if err := json.Unmarshal(metaBytes, &md); err == nil {
					jrpcloc := fmt.Sprintf("http://%s:%s/ext/bc/avm", md.Serverhost, md.HTTPport)
					rpcClient := jsonrpc.NewClient(jrpcloc)
					response, err := rpcClient.Call("avm.getTxStatus", struct {
						TxID string
					}{
						TxID: args[1],
					})
					if err != nil {
						log.Error("error sent txid: %s", args[1])
						log.Error("rpcClient returned error: %s", err.Error())
					} else if response.Error != nil {
						log.Error("error sent txid: %s", args[1])
						log.Error("rpcClient returned error: %d, %s", response.Error.Code, response.Error.Message)
					} else {
						var s struct {
							Status string
						}
						err = response.GetObject(&s)
						if err != nil {
							log.Error("error on parsing response: %s", err.Error())
						} else {
							log.Info("Status:%s", s.Status)
						}
					}
				} else {
					log.Error("unable to unmarshal metadata for node %s: %s", args[0], err.Error())
				}
			} else {
				log.Error("node not found: %s", args[0])
			}
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletGetBalanceCmd will get the balance of an address from a node
var AVAXWalletGetBalanceCmd = &cobra.Command{
	Use:   "balance [node name] [address]",
	Short: "Checks the balance of an address from a node.",
	Long:  `Checks the balance of an address from a node.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) >= 2 {
			log := cfg.Config.Log
			if meta, err := pmgr.ProcManager.Metadata(args[0]); err == nil {
				var md node.Metadata
				metaBytes := []byte(meta)
				if err := json.Unmarshal(metaBytes, &md); err == nil {
					jrpcloc := fmt.Sprintf("http://%s:%s/ext/bc/avm", md.Serverhost, md.HTTPport)
					rpcClient := jsonrpc.NewClient(jrpcloc)
					response, err := rpcClient.Call("avm.getBalance", struct {
						Address string
						AssetID string
					}{
						Address: args[1],
						AssetID: "AVAX",
					})
					if err != nil {
						log.Error("error sent address: %s", args[1])
						log.Error("rpcClient returned error: %s", err.Error())
					} else if response.Error != nil {
						log.Error("error sent address: %s", args[1])
						log.Error("rpcClient returned error: %d, %s", response.Error.Code, response.Error.Message)
					} else {
						var s struct {
							Balance string
						}
						err = response.GetObject(&s)
						if err != nil {
							log.Error("error on parsing response: %s", err.Error())
						} else {
							log.Info("Balance: %s", s.Balance)
						}
					}
				} else {
					log.Error("unable to unmarshal metadata for node %s: %s", args[0], err.Error())
				}
			} else {
				log.Error("node not found: %s", args[0])
			}
		} else {
			cmd.Help()
		}
	},
}

// AVAXWalletRefreshCmd will send a transaction through a node
var AVAXWalletRefreshCmd = &cobra.Command{
	Use:   "refresh [node name] [wallet name]",
	Short: "Refreshes UTXO set from node.",
	Long:  `Refreshes UTXO set from node.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			cmd.Help()
			return
		}

		log := cfg.Config.Log
		w, ok := dagwallet.Wallets[args[1]]
		if !ok {
			log.Error("wallet not found: %s", args[1])
			return
		}

		meta, err := pmgr.ProcManager.Metadata(args[0])
		if err != nil {
			log.Error("node not found: %s", args[0])
			return
		}

		var md node.Metadata
		metaBytes := []byte(meta)
		err = json.Unmarshal(metaBytes, &md)
		if err != nil {
			log.Error("unable to unmarshal metadata for node %s: %s", args[0], err.Error())
			return
		}

		jrpcloc := fmt.Sprintf("http://%s:%s/ext/bc/avm", md.Serverhost, md.HTTPport)
		rpcClient := jsonrpc.NewClient(jrpcloc)

		response, err := rpcClient.Call("avm.getUTXOs", struct {
			Addresses []string
		}{
			Addresses: w.Addresses(),
		})

		if err != nil {
			log.Error("rpcClient returned error: %s", err.Error())
		} else if response.Error != nil {
			log.Error("rpcClient returned error: %d, %s", response.Error.Code, response.Error.Message)
		} else {
			var s struct {
				UTXOs []string
			}
			err = response.GetObject(&s)
			if err != nil {
				log.Error("error on parsing response: %s", err.Error())
			} else {
				fb := formatting.CB58{}
				acodec := spdagvm.Codec{}
				for _, aUTXO := range s.UTXOs {
					fb.FromString(aUTXO)
					if utxo, err := acodec.UnmarshalUTXO(fb.Bytes); err == nil {
						w.AddUtxo(utxo)
					} else {
						log.Error("unable to add UTXO: %s", aUTXO)
					}
				}
				//fmt.Printf("[%s]", strings.Join(s.UTXOs, ","))
				log.Info("utxo set refreshed on wallet %s from node %s", args[1], args[0])
			}
		}
	},
}

// AVAXWalletWriteUTXOCmd writes the UTXOs of a wallet to the filename specified in the stash
var AVAXWalletWriteUTXOCmd = &cobra.Command{
	Use:   "writeutxo [wallet name A] [filename]",
	Short: "Writes the UTXO set to a file.",
	Long:  `Writes the UTXO set to a file.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			cmd.Help()
			return
		}

		log := cfg.Config.Log
		wallet, ok := dagwallet.Wallets[args[0]]
		if !ok {
			log.Error("wallet not found: %s", args[0])
			return
		}

		stashdir := cfg.Config.DataDir
		basename := filepath.Base(args[1])
		basedir := filepath.Dir(stashdir + "/" + args[1])

		os.MkdirAll(basedir, os.ModePerm)
		outputfile := basedir + "/" + basename
		utxoset := wallet.GetUtxos()

		if marshalled, err := utxoset.JSON(); err == nil {
			if err := ioutil.WriteFile(outputfile, marshalled, 0755); err != nil {
				log.Error("unable to write file: %s - %s", string(outputfile), err.Error())
			} else {
				log.Info("UTXO Set written to: %s", outputfile)
			}
		} else {
			log.Error("unable to marshal: %s", err.Error())
		}
	},
}

// AVAXWalletCompareCmd compares the UTXO set between two wallets, stores difference in a variable
var AVAXWalletCompareCmd = &cobra.Command{
	Use:   "compare [wallet name A] [wallet name B] [var scope] [var name]",
	Short: "Compares the UTXO set between two wallets.",
	Long:  `Compares the UTXO set between two wallets.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 4 {
			cmd.Help()
			return
		}

		log := cfg.Config.Log
		w1, ok := dagwallet.Wallets[args[0]]
		if !ok {
			log.Error("wallet not found: %s", args[0])
			return
		}

		w2, ok := dagwallet.Wallets[args[1]]
		if !ok {
			log.Error("wallet not found: %s", args[1])
			return
		}

		store, err := AvashVars.Get(args[2])
		if err != nil {
			log.Error("store not found: " + args[2])
			return
		}

		us1 := w1.GetUtxos()
		us2 := w2.GetUtxos()
		diff := us1.SetDiff(us2)
		diffByte, err := json.MarshalIndent(diff, "", "    ")
		if err != nil {
			log.Error("unable to marshal: %s", err.Error())
		} else {
			store.Set(args[3], string(diffByte))
		}
	},
}

/*
avaxwallet
	create [wallet name] -> "wallet created: " + [wallet name]
	addkey [wallet name] [private key] -> address
	balance [node name] [address] -> uint
	status [node name] [tx string] -> [status]
	maketx [wallet name] [destination address] [amount] -> txString
	refresh [node name] [wallet name] -> "wallet refreshed: " + [wallet name]
	remove [wallet name] [tx string] -> "transaction removed: " + [tx string]
	send [node name] [tx string] -> "sent tx: " [tx string]
	newkey -> privateKey
*/

func init() {
	AVAXWalletCmd.AddCommand(AVAXWalletCreateCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletNewKeyCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletAddKeyCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletGetBalanceCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletMakeTxCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletRemoveCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletSpendCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletSendCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletRefreshCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletCompareCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletStatusCmd)
	AVAXWalletCmd.AddCommand(AVAXWalletWriteUTXOCmd)
}
