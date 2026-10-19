//go:build ignore
// +build ignore

// Copyright 2025 The go-zond Authors
// This file implements wallet-related CLI commands

package main

import (
	"bufio"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
	"github.com/theQRL/go-qrl/cmd/utils"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/crypto/pqcrypto"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/zond/qrlclient"
	"github.com/urfave/cli/v2"
)

var (
	walletCommand = &cli.Command{
		Name:     "wallet",
		Usage:    "Manage wallet operations",
		Category: "WALLET",
		Description: `
The wallet command provides operations for managing QRL wallets including
checking balances, sending transactions, and receiving funds across multiple
QRL networks (Classic and Zond).

Examples:
  gzond wallet balance --address Z20... --network zond-kurtosis
  gzond wallet send --from Z20... --to Z20... --amount 1.5 --network zond-testnet
  gzond wallet receive --address Z20...
		`,
		Subcommands: []*cli.Command{
			{
				Name:   "balance",
				Usage:  "Check wallet balance",
				Action: walletBalance,
				Flags: []cli.Flag{
					utils.NetworkFlag,
					utils.ZeusProxyFlag,
					utils.RPCURLFlag,
					&cli.StringFlag{
						Name:     "address",
						Aliases:  []string{"addr", "a"},
						Usage:    "QRL address to check balance (Z for Zond, Q for Classic)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "unit",
						Aliases: []string{"u"},
						Usage:   "Display unit: quanta or shor",
						Value:   "quanta",
					},
				},
				Description: `
Check the balance of a QRL address on any supported network.

The address prefix determines the address type:
  - Z prefix: Zond address (requires --network zond-*)
  - Q prefix: Classic QRL address (requires --network classic-*)

Examples:
  # Check balance on Zond Kurtosis (local development)
  gzond wallet balance --address Z2026a881d9dF1C880f1c0e9335a383E8E7FC50Ef --network zond-kurtosis

  # Check balance on Zond Testnet
  gzond wallet balance --address Z2026a881d9dF1C880f1c0e9335a383E8E7FC50Ef --network zond-testnet

  # Check balance on Classic Testnet
  gzond wallet balance --address Q010300... --network classic-testnet

  # Display balance in Shor (smallest unit)
  gzond wallet balance --address Z20... --network zond-kurtosis --unit shor
				`,
			},
			{
				Name:   "status",
				Usage:  "Check node and network status",
				Action: walletStatus,
				Flags: []cli.Flag{
					utils.NetworkFlag,
					utils.ZeusProxyFlag,
					utils.RPCURLFlag,
					&cli.BoolFlag{
						Name:    "verbose",
						Aliases: []string{"v"},
						Usage:   "Show detailed status information",
						Value:   false,
					},
				},
				Description: `
Check the status of a QRL node and network.

Displays information including:
  - Node version and state
  - Current block height
  - Number of peer connections
  - Network synchronization status
  - Node uptime

Examples:
  # Check status of Zond Kurtosis network
  gzond wallet status --network zond-kurtosis

  # Check status of Classic Mainnet
  gzond wallet status --network classic-mainnet

  # Show verbose status information
  gzond wallet status --network zond-testnet --verbose
				`,
			},
			{
				Name:   "send",
				Usage:  "Send QRL transaction",
				Action: walletSend,
				Flags: []cli.Flag{
					utils.NetworkFlag,
					utils.ZeusProxyFlag,
					utils.RPCURLFlag,
					&cli.StringFlag{
						Name:     "from",
						Aliases:  []string{"f"},
						Usage:    "Sender address (Z for Zond, Q for Classic)",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "to",
						Aliases:  []string{"t"},
						Usage:    "Recipient address",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "amount",
						Aliases:  []string{"value", "v"},
						Usage:    "Amount to send in Quanta",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "keyfile",
						Aliases: []string{"key", "k"},
						Usage:   "Path to Dilithium private key file (hex seed)",
					},
					&cli.StringFlag{
						Name:    "gas",
						Aliases: []string{"g"},
						Usage:   "Gas limit (default: auto-estimate)",
					},
					&cli.StringFlag{
						Name:    "gasprice",
						Aliases: []string{"price", "p"},
						Usage:   "Gas price in Gwei (default: network suggested)",
					},
					&cli.StringFlag{
						Name:    "gasTipCap",
						Aliases: []string{"tip"},
						Usage:   "Max priority fee per gas in Gwei (default: 1.5 Gwei)",
					},
					&cli.StringFlag{
						Name:    "gasFeeCap",
						Aliases: []string{"maxfee"},
						Usage:   "Max fee per gas in Gwei (default: auto)",
					},
					&cli.Uint64Flag{
						Name:    "nonce",
						Aliases: []string{"n"},
						Usage:   "Transaction nonce (default: auto-retrieve from address state)",
					},
					&cli.BoolFlag{
						Name:    "yes",
						Aliases: []string{"y"},
						Usage:   "Skip confirmation prompt",
						Value:   false,
					},
				},
				Description: `
Send a transaction to another QRL address.

The transaction requires a private key to sign. You can provide the key via:
  - --keyfile: Path to hex seed file
  - Interactive prompt (if not provided)

Gas and fees:
  - If not specified, gas limit is estimated automatically
  - Gas price defaults to network suggested price
  - For EIP-1559 transactions, gasTipCap and gasFeeCap are used

Examples:
  # Send 1.5 Quanta on Zond Kurtosis
  gzond wallet send \
    --from Z20... \
    --to Z20... \
    --amount 1.5 \
    --keyfile ~/.qrl/wallet.key \
    --network zond-kurtosis

  # Send with custom gas settings
  gzond wallet send \
    --from Z20... \
    --to Z20... \
    --amount 0.5 \
    --gas 21000 \
    --gasprice 2 \
    --keyfile ~/.qrl/wallet.key \
    --network zond-testnet

  # Send without confirmation prompt
  gzond wallet send \
    --from Z20... \
    --to Z20... \
    --amount 1.0 \
    --keyfile ~/.qrl/wallet.key \
    --network zond-kurtosis \
    --yes
				`,
			},
		},
	}
)

func walletBalance(ctx *cli.Context) error {
	address := ctx.String("address")
	networkStr := ctx.String(utils.NetworkFlag.Name)
	zeusProxyURL := ctx.String(utils.ZeusProxyFlag.Name)
	rpcURL := ctx.String(utils.RPCURLFlag.Name)
	unit := ctx.String("unit")

	// Validate address
	if err := qrlclient.ValidateAddress(address); err != nil {
		utils.Fatalf("Invalid address: %v", err)
	}

	// Determine network
	var network qrlclient.NetworkType
	if networkStr != "" {
		network = qrlclient.NetworkType(networkStr)
	} else {
		// Auto-detect network from address
		detectedNetwork, err := qrlclient.DetectNetwork(address)
		if err != nil {
			utils.Fatalf("Could not detect network: %v\nPlease specify --network flag", err)
		}
		network = detectedNetwork

		// Show suggested networks
		suggested := qrlclient.SuggestNetwork(address)
		if len(suggested) > 1 {
			fmt.Printf("Auto-detected network: %s\n", qrlclient.NetworkDescription(network))
			fmt.Printf("Available networks for this address: ")
			for i, net := range suggested {
				if i > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("%s", net)
			}
			fmt.Printf("\nUse --network flag to specify a different network\n\n")
		}
	}

	fmt.Printf("Querying balance for %s on %s...\n", address, qrlclient.NetworkDescription(network))

	// Check if using direct RPC or Zeus Proxy
	if rpcURL != "" {
		// Use direct RPC client (for Zond networks)
		rpcClient, err := qrlclient.NewRPCClient(&qrlclient.RPCConfig{
			RPCURL:  rpcURL,
			Network: network,
		})
		if err != nil {
			utils.Fatalf("Failed to create RPC client: %v", err)
		}

		// Get balance via RPC
		balanceBig, err := rpcClient.GetBalance(address)
		if err != nil {
			utils.Fatalf("Failed to get balance: %v", err)
		}

		// Display balance
		fmt.Printf("\n")
		fmt.Printf("Address:  %s\n", address)
		fmt.Printf("Network:  %s\n", qrlclient.NetworkDescription(network))

		if unit == "shor" {
			fmt.Printf("Balance:  %s Shor\n", balanceBig.String())
		} else {
			fmt.Printf("Balance:  %s Quanta\n", qrlclient.FormatBalance(balanceBig))
			fmt.Printf("          (%s Shor)\n", balanceBig.String())
		}
	} else {
		// Use Zeus Proxy client (for Classic networks)
		client, err := qrlclient.NewClient(&qrlclient.Config{
			ZeusProxyURL: zeusProxyURL,
			Network:      network,
		})
		if err != nil {
			utils.Fatalf("Failed to create client: %v", err)
		}

		// Get balance via Zeus Proxy
		balance, err := client.GetBalance(address)
		if err != nil {
			utils.Fatalf("Failed to get balance: %v", err)
		}

		// Display balance
		fmt.Printf("\n")
		fmt.Printf("Address:  %s\n", balance.Address)
		fmt.Printf("Network:  %s\n", qrlclient.NetworkDescription(network))

		if unit == "shor" {
			fmt.Printf("Balance:  %s Shor\n", balance.Balance.String())
		} else {
			fmt.Printf("Balance:  %s Quanta\n", qrlclient.FormatBalance(balance.Balance))
			fmt.Printf("          (%s Shor)\n", balance.Balance.String())
		}
	}

	return nil
}

func walletStatus(ctx *cli.Context) error {
	networkStr := ctx.String(utils.NetworkFlag.Name)
	zeusProxyURL := ctx.String(utils.ZeusProxyFlag.Name)
	verbose := ctx.Bool("verbose")

	// Network is required for status command
	if networkStr == "" {
		utils.Fatalf("--network flag is required for status command\n" +
			"Available networks:\n" +
			"  Classic: classic-mainnet, classic-testnet, classic-kurtosis, classic-local\n" +
			"  Zond: zond-mainnet, zond-testnet, zond-kurtosis, zond-local")
	}

	network := qrlclient.NetworkType(networkStr)

	// Create client
	client, err := qrlclient.NewClient(&qrlclient.Config{
		ZeusProxyURL: zeusProxyURL,
		Network:      network,
	})
	if err != nil {
		utils.Fatalf("Failed to create client: %v", err)
	}

	// Get node state
	fmt.Printf("Querying node status for %s...\n\n", qrlclient.NetworkDescription(network))

	nodeState, err := client.GetNodeState()
	if err != nil {
		utils.Fatalf("Failed to get node state: %v", err)
	}

	// Display status information
	fmt.Printf("Network:         %s\n", qrlclient.NetworkDescription(network))
	fmt.Printf("Node Version:    %s\n", nodeState.Info.Version)
	fmt.Printf("State:           %s\n", nodeState.Info.State)
	fmt.Printf("Block Height:    %d\n", nodeState.Info.BlockHeight)
	fmt.Printf("Block Hash:      %s\n", nodeState.Info.BlockLastHash)
	fmt.Printf("Network ID:      %s\n", nodeState.Info.NetworkID)
	fmt.Printf("Connections:     %d peers\n", nodeState.Info.NumConnections)
	fmt.Printf("Known Peers:     %d\n", nodeState.Info.NumKnownPeers)

	// Format uptime
	uptime := nodeState.Info.Uptime
	hours := uptime / 3600
	minutes := (uptime % 3600) / 60
	seconds := uptime % 60
	fmt.Printf("Uptime:          %dh %dm %ds\n", hours, minutes, seconds)

	// Verbose output
	if verbose {
		fmt.Printf("\nDetailed Information:\n")
		fmt.Printf("---------------------\n")

		// Connection status
		if nodeState.Info.NumConnections > 0 {
			fmt.Printf("✓ Node is connected to %d peer(s)\n", nodeState.Info.NumConnections)
		} else {
			fmt.Printf("⚠ Node has no peer connections\n")
		}

		// Sync status
		if nodeState.Info.State == "SYNCED" {
			fmt.Printf("✓ Node is fully synchronized\n")
		} else {
			fmt.Printf("⚠ Node synchronization state: %s\n", nodeState.Info.State)
		}

		// Block height check
		if nodeState.Info.BlockHeight > 0 {
			fmt.Printf("✓ Node has blocks: %d\n", nodeState.Info.BlockHeight)
		} else {
			fmt.Printf("⚠ Node has no blocks yet\n")
		}

		// Network ID
		fmt.Printf("• Network ID: %s\n", nodeState.Info.NetworkID)
	}

	return nil
}

func walletSend(ctx *cli.Context) error {
	// Get parameters
	fromAddr := ctx.String("from")
	toAddr := ctx.String("to")
	amountStr := ctx.String("amount")
	keyfile := ctx.String("keyfile")
	networkStr := ctx.String(utils.NetworkFlag.Name)
	zeusProxyURL := ctx.String(utils.ZeusProxyFlag.Name)
	rpcURL := ctx.String(utils.RPCURLFlag.Name)
	skipConfirm := ctx.Bool("yes")

	// Validate addresses
	if err := qrlclient.ValidateAddress(fromAddr); err != nil {
		utils.Fatalf("Invalid from address: %v", err)
	}
	if err := qrlclient.ValidateAddress(toAddr); err != nil {
		utils.Fatalf("Invalid to address: %v", err)
	}

	// Parse amount
	amount, err := qrlclient.ParseAmount(amountStr)
	if err != nil {
		utils.Fatalf("Invalid amount: %v", err)
	}

	// Determine network
	var network qrlclient.NetworkType
	if networkStr != "" {
		network = qrlclient.NetworkType(networkStr)
	} else {
		// Auto-detect network from from address
		detectedNetwork, err := qrlclient.DetectNetwork(fromAddr)
		if err != nil {
			utils.Fatalf("Could not detect network: %v\nPlease specify --network flag", err)
		}
		network = detectedNetwork
		fmt.Printf("Auto-detected network: %s\n", qrlclient.NetworkDescription(network))
	}

	// Load private key
	var mldsa87Key *ml_dsa_87.MLDSA87
	if keyfile != "" {
		fmt.Printf("Loading private key from %s...\n", keyfile)
		mldsa87Key, err = pqcrypto.LoadMLDSA87(keyfile)
		if err != nil {
			utils.Fatalf("Failed to load private key: %v", err)
		}
	} else {
		// Prompt for hex seed
		fmt.Printf("Enter ML-DSA-87 hex seed (64 characters): ")
		reader := bufio.NewReader(os.Stdin)
		hexSeed, err := reader.ReadString('\n')
		if err != nil {
			utils.Fatalf("Failed to read hex seed: %v", err)
		}
		hexSeed = strings.TrimSpace(hexSeed)

		mldsa87Key, err = pqcrypto.HexToMLDSA87(hexSeed)
		if err != nil {
			utils.Fatalf("Failed to parse hex seed: %v", err)
		}
	}

	// Verify the from address matches the private key
	pk := mldsa87Key.GetPK()
	derivedAddr := pqcrypto.MLDSA87PKToAddress(pk[:])
	expectedAddr, err := common.NewAddressFromString(fromAddr)
	if err != nil {
		utils.Fatalf("Failed to parse from address: %v", err)
	}
	if derivedAddr != expectedAddr {
		utils.Fatalf("Private key does not match from address\nDerived: %s\nExpected: %s",
			derivedAddr.Hex(), expectedAddr.Hex())
	}

	fmt.Printf("\nPreparing transaction...\n")
	fmt.Printf("Network:  %s\n", qrlclient.NetworkDescription(network))
	fmt.Printf("From:     %s\n", fromAddr)
	fmt.Printf("To:       %s\n", toAddr)
	fmt.Printf("Amount:   %s Quanta\n", qrlclient.FormatBalance(amount))

	// Get nonce (using RPC or Zeus Proxy)
	var nonce uint64
	if ctx.IsSet("nonce") {
		nonce = ctx.Uint64("nonce")
		fmt.Printf("Nonce:    %d (manual)\n", nonce)
	} else {
		if rpcURL != "" {
			// Use RPC client to get nonce
			rpcClient, err := qrlclient.NewRPCClient(&qrlclient.RPCConfig{
				RPCURL:  rpcURL,
				Network: network,
			})
			if err != nil {
				utils.Fatalf("Failed to create RPC client: %v", err)
			}
			nonce, err = rpcClient.GetTransactionCount(fromAddr)
			if err != nil {
				utils.Fatalf("Failed to get nonce: %v", err)
			}
		} else {
			// Use Zeus Proxy client to get nonce
			client, err := qrlclient.NewClient(&qrlclient.Config{
				ZeusProxyURL: zeusProxyURL,
				Network:      network,
			})
			if err != nil {
				utils.Fatalf("Failed to create client: %v", err)
			}
			nonce, err = client.GetNonce(fromAddr)
			if err != nil {
				utils.Fatalf("Failed to get nonce: %v", err)
			}
		}
		fmt.Printf("Nonce:    %d (auto)\n", nonce)
	}

	// Parse gas parameters
	var gasLimit uint64 = 21000 // Default gas limit for simple transfer
	if ctx.IsSet("gas") {
		gasStr := ctx.String("gas")
		gasBig := new(big.Int)
		if _, ok := gasBig.SetString(gasStr, 10); !ok {
			utils.Fatalf("Invalid gas limit: %s", gasStr)
		}
		gasLimit = gasBig.Uint64()
	}
	fmt.Printf("Gas:      %d\n", gasLimit)

	// Parse gas price parameters (EIP-1559 style)
	gasTipCap := big.NewInt(1500000000) // Default 1.5 Gwei
	if ctx.IsSet("gasTipCap") {
		tipStr := ctx.String("gasTipCap")
		tipGwei := new(big.Float)
		if _, ok := tipGwei.SetString(tipStr); !ok {
			utils.Fatalf("Invalid gas tip cap: %s", tipStr)
		}
		// Convert Gwei to Wei
		gweiToWei := new(big.Float).SetInt(big.NewInt(1000000000))
		tipWei := new(big.Float).Mul(tipGwei, gweiToWei)
		gasTipCap, _ = tipWei.Int(nil)
	}

	gasFeeCap := new(big.Int).Mul(gasTipCap, big.NewInt(2)) // Default 2x tip
	if ctx.IsSet("gasFeeCap") {
		feeStr := ctx.String("gasFeeCap")
		feeGwei := new(big.Float)
		if _, ok := feeGwei.SetString(feeStr); !ok {
			utils.Fatalf("Invalid gas fee cap: %s", feeStr)
		}
		// Convert Gwei to Wei
		gweiToWei := new(big.Float).SetInt(big.NewInt(1000000000))
		feeWei := new(big.Float).Mul(feeGwei, gweiToWei)
		gasFeeCap, _ = feeWei.Int(nil)
	}

	// Calculate total cost
	maxGasCost := new(big.Int).Mul(gasFeeCap, big.NewInt(int64(gasLimit)))
	totalCost := new(big.Int).Add(amount, maxGasCost)
	fmt.Printf("Max Fee:  %s Quanta\n", qrlclient.FormatBalance(maxGasCost))
	fmt.Printf("Total:    %s Quanta (amount + max fee)\n\n", qrlclient.FormatBalance(totalCost))

	// Confirmation prompt
	if !skipConfirm {
		fmt.Printf("Send this transaction? (yes/no): ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			utils.Fatalf("Failed to read confirmation: %v", err)
		}
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "yes" && response != "y" {
			fmt.Println("Transaction cancelled.")
			return nil
		}
	}

	// Build transaction
	fmt.Println("\nBuilding transaction...")

	toAddress, err := common.NewAddressFromString(toAddr)
	if err != nil {
		utils.Fatalf("Failed to parse to address: %v", err)
	}
	chainID := getChainID(network)

	txData := &types.DynamicFeeTx{
		ChainID:    chainID,
		Nonce:      nonce,
		GasTipCap:  gasTipCap,
		GasFeeCap:  gasFeeCap,
		Gas:        gasLimit,
		To:         &toAddress,
		Value:      amount,
		Data:       nil,
		AccessList: types.AccessList{},
	}

	// Sign transaction
	fmt.Println("Signing transaction...")
	signer := types.NewShanghaiSigner(chainID)
	signedTx, err := types.SignNewTx(mldsa87Key, signer, txData)
	if err != nil {
		utils.Fatalf("Failed to sign transaction: %v", err)
	}

	// Encode transaction
	txBytes, err := signedTx.MarshalBinary()
	if err != nil {
		utils.Fatalf("Failed to encode transaction: %v", err)
	}

	// Broadcast transaction (using RPC or Zeus Proxy)
	fmt.Println("Broadcasting transaction...")

	var txHash string
	if rpcURL != "" {
		// Use RPC client to broadcast
		rpcClient, err := qrlclient.NewRPCClient(&qrlclient.RPCConfig{
			RPCURL:  rpcURL,
			Network: network,
		})
		if err != nil {
			utils.Fatalf("Failed to create RPC client: %v", err)
		}
		txHash, err = rpcClient.SendRawTransaction(txBytes)
		if err != nil {
			utils.Fatalf("Failed to broadcast transaction: %v", err)
		}
	} else {
		// Use Zeus Proxy client to broadcast
		client, err := qrlclient.NewClient(&qrlclient.Config{
			ZeusProxyURL: zeusProxyURL,
			Network:      network,
		})
		if err != nil {
			utils.Fatalf("Failed to create client: %v", err)
		}
		result, err := client.PushTransaction(txBytes)
		if err != nil {
			utils.Fatalf("Failed to broadcast transaction: %v", err)
		}

		// Extract transaction hash from result
		if hash, ok := result["tx_hash"].(string); ok {
			txHash = hash
		} else if hash, ok := result["txHash"].(string); ok {
			txHash = hash
		} else {
			txHash = signedTx.Hash().Hex()
		}
	}

	// Display result
	fmt.Printf("\n✓ Transaction sent successfully!\n")
	fmt.Printf("Transaction Hash: %s\n", txHash)

	fmt.Printf("\nYou can check the transaction status with:\n")
	fmt.Printf("  gzond wallet status --network %s\n", network)

	return nil
}

// getChainID returns the chain ID for a given network
func getChainID(network qrlclient.NetworkType) *big.Int {
	switch network {
	case qrlclient.ZondMainnet:
		return params.MainnetChainConfig.ChainID
	case qrlclient.ZondTestnet:
		return params.TestnetChainConfig.ChainID
	case qrlclient.ZondKurtosis:
		return big.NewInt(32382) // Kurtosis chain ID
	case qrlclient.ZondLocal:
		return big.NewInt(32383) // Local chain ID
	case qrlclient.ClassicMainnet:
		return params.MainnetChainConfig.ChainID
	case qrlclient.ClassicTestnet:
		return params.TestnetChainConfig.ChainID
	default:
		return params.MainnetChainConfig.ChainID
	}
}
