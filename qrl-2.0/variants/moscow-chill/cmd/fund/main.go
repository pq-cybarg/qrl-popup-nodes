// Package main - fund is a dev tool for signing and submitting value transfers
// against a QRL 2.0 node, using a local keystore file (offline signing).
//
// This replaces the removed `--unlock`/`personal_unlockAccount` flow with a
// modern offline-signing pattern: node stays account-less, the signer is here.
//
// Usage:
//
//	fund -keystore <file> -password <file> -to <addr> -value <wei> [-rpc url] [-chainid n]
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"

	"github.com/theQRL/go-qrl/accounts/keystore"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/core/types"
)

func fail(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "fund: "+msg+"\n", args...)
	os.Exit(1)
}

type rpcReq struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcResp struct {
	Result string          `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func rpcCall(url, method string, params []interface{}) (string, error) {
	body, _ := json.Marshal(rpcReq{Jsonrpc: "2.0", Method: method, Params: params, ID: 1})
	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r rpcResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("bad response: %s", raw)
	}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		return "", fmt.Errorf("rpc error: %s", r.Error)
	}
	return r.Result, nil
}

func hexToBigInt(s string) *big.Int {
	if len(s) > 2 && s[:2] == "0x" {
		s = s[2:]
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}

func main() {
	var (
		keystoreFile = flag.String("keystore", "", "path to keystore JSON file (UTC--...)")
		passwordFile = flag.String("password", "", "path to file containing the keystore password")
		toStr        = flag.String("to", "", "recipient Q-address (64 bytes hex)")
		valueStr     = flag.String("value", "", "value in wei (decimal or 0x-hex)")
		rpcURL       = flag.String("rpc", "http://localhost:9545", "JSON-RPC endpoint")
		chainIDArg   = flag.Int64("chainid", 0, "chain ID (0 = ask the node)")
		gasLimit     = flag.Uint64("gas", 21000, "gas limit")
		maxFee       = flag.Int64("maxfee", 2_000_000_000, "max fee per gas (wei)")
		tipCap       = flag.Int64("tip", 1_000_000_000, "max priority fee (wei)")
	)
	flag.Parse()

	if *keystoreFile == "" || *passwordFile == "" || *toStr == "" || *valueStr == "" {
		flag.Usage()
		os.Exit(2)
	}

	// Load + decrypt keystore
	ksBytes, err := os.ReadFile(*keystoreFile)
	if err != nil {
		fail("read keystore: %v", err)
	}
	pwBytes, err := os.ReadFile(*passwordFile)
	if err != nil {
		fail("read password file: %v", err)
	}
	password := string(bytes.TrimSpace(pwBytes))

	key, err := keystore.DecryptKey(ksBytes, password)
	if err != nil {
		fail("decrypt keystore: %v", err)
	}
	fmt.Printf("signer address: Q%x\n", key.Address)

	// Parse to-address
	toHex := *toStr
	if len(toHex) > 0 && (toHex[0] == 'Q' || toHex[0] == 'q') {
		toHex = toHex[1:]
	}
	toBytes, _ := hex.DecodeString(toHex)
	if len(toBytes) != common.AddressLength {
		fail("to address must be Q + %d hex chars (%d bytes)", 2*common.AddressLength, common.AddressLength)
	}
	var toAddr common.Address
	copy(toAddr[:], toBytes)

	// Parse value
	value := new(big.Int)
	if _, ok := value.SetString(*valueStr, 0); !ok {
		fail("bad value: %s", *valueStr)
	}

	// Discover chain ID + nonce from node
	chainID := big.NewInt(*chainIDArg)
	if chainID.Sign() == 0 {
		cidHex, err := rpcCall(*rpcURL, "qrl_chainId", nil)
		if err != nil {
			fail("qrl_chainId: %v", err)
		}
		chainID = hexToBigInt(cidHex)
	}

	fromHex := fmt.Sprintf("Q%x", key.Address)
	nonceHex, err := rpcCall(*rpcURL, "qrl_getTransactionCount", []interface{}{fromHex, "pending"})
	if err != nil {
		fail("qrl_getTransactionCount: %v", err)
	}
	nonce := hexToBigInt(nonceHex).Uint64()

	// Build + sign tx
	txData := &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(*tipCap),
		GasFeeCap: big.NewInt(*maxFee),
		Gas:       *gasLimit,
		To:        &toAddr,
		Value:     value,
		Data:      nil,
	}
	tx := types.NewTx(txData)
	signer := types.LatestSignerForChainID(chainID)
	signed, err := types.SignTx(tx, signer, key.Wallet)
	if err != nil {
		fail("sign: %v", err)
	}
	rawBytes, err := signed.MarshalBinary()
	if err != nil {
		fail("marshal: %v", err)
	}
	rawHex := "0x" + hex.EncodeToString(rawBytes)

	// Submit
	hashOut, err := rpcCall(*rpcURL, "qrl_sendRawTransaction", []interface{}{rawHex})
	if err != nil {
		fail("qrl_sendRawTransaction: %v", err)
	}
	fmt.Printf("tx hash:        %s\n", hashOut)
	fmt.Printf("nonce:          %d\n", nonce)
	fmt.Printf("chain ID:       %s\n", chainID.String())
	fmt.Printf("from:           %s\n", fromHex)
	fmt.Printf("to:             Q%x\n", toAddr)
	fmt.Printf("value (wei):    %s\n", value.String())
}
