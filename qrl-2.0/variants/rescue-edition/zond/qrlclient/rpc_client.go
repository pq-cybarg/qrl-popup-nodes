// Copyright 2025 The go-zond Authors
// This file implements a direct JSON-RPC client for Zond networks using zond_* RPC methods

package qrlclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// RPCClient is a direct JSON-RPC client for Zond networks
type RPCClient struct {
	rpcURL     string
	httpClient *http.Client
	network    NetworkType
}

// RPCConfig holds RPC client configuration
type RPCConfig struct {
	RPCURL  string        // Direct RPC URL (e.g., "http://localhost:8545")
	Network NetworkType   // Network identifier (for display purposes)
	Timeout time.Duration // Request timeout
}

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// NewRPCClient creates a new direct RPC client for Zond networks
func NewRPCClient(config *RPCConfig) (*RPCClient, error) {
	if config.RPCURL == "" {
		return nil, fmt.Errorf("RPC URL is required")
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Network == "" {
		config.Network = ZondLocal // Default to local
	}

	return &RPCClient{
		rpcURL: config.RPCURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		network: config.Network,
	}, nil
}

// call performs a JSON-RPC call
func (c *RPCClient) call(method string, params []interface{}) (*JSONRPCResponse, error) {
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(c.rpcURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s (data: %s)",
			rpcResp.Error.Code, rpcResp.Error.Message, rpcResp.Error.Data)
	}

	return &rpcResp, nil
}

// GetBalance retrieves the balance for an address via zond_getBalance
func (c *RPCClient) GetBalance(address string) (*big.Int, error) {
	// Call zond_getBalance with "latest" block parameter
	resp, err := c.call("zond_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return nil, fmt.Errorf("zond_getBalance failed: %w", err)
	}

	// Parse hex string result
	var balanceHex string
	if err := json.Unmarshal(resp.Result, &balanceHex); err != nil {
		return nil, fmt.Errorf("failed to parse balance: %w", err)
	}

	// Convert hex to big.Int
	balance := new(big.Int)
	balance.SetString(balanceHex[2:], 16) // Remove "0x" prefix
	return balance, nil
}

// GetTransactionCount retrieves the nonce for an address via zond_getTransactionCount
func (c *RPCClient) GetTransactionCount(address string) (uint64, error) {
	// Call zond_getTransactionCount with "latest" block parameter
	resp, err := c.call("zond_getTransactionCount", []interface{}{address, "latest"})
	if err != nil {
		return 0, fmt.Errorf("zond_getTransactionCount failed: %w", err)
	}

	// Parse hex string result
	var nonceHex string
	if err := json.Unmarshal(resp.Result, &nonceHex); err != nil {
		return 0, fmt.Errorf("failed to parse nonce: %w", err)
	}

	// Convert hex to uint64
	nonce := new(big.Int)
	nonce.SetString(nonceHex[2:], 16) // Remove "0x" prefix
	return nonce.Uint64(), nil
}

// SendRawTransaction broadcasts a signed transaction via zond_sendRawTransaction
func (c *RPCClient) SendRawTransaction(signedTx []byte) (string, error) {
	// Encode transaction as hex with "0x" prefix
	txHex := fmt.Sprintf("0x%x", signedTx)

	// Call zond_sendRawTransaction
	resp, err := c.call("zond_sendRawTransaction", []interface{}{txHex})
	if err != nil {
		return "", fmt.Errorf("zond_sendRawTransaction failed: %w", err)
	}

	// Parse transaction hash result
	var txHash string
	if err := json.Unmarshal(resp.Result, &txHash); err != nil {
		return "", fmt.Errorf("failed to parse transaction hash: %w", err)
	}

	return txHash, nil
}

// GetBlockNumber retrieves the current block number via zond_blockNumber
func (c *RPCClient) GetBlockNumber() (uint64, error) {
	resp, err := c.call("zond_blockNumber", []interface{}{})
	if err != nil {
		return 0, fmt.Errorf("zond_blockNumber failed: %w", err)
	}

	// Parse hex string result
	var blockHex string
	if err := json.Unmarshal(resp.Result, &blockHex); err != nil {
		return 0, fmt.Errorf("failed to parse block number: %w", err)
	}

	// Convert hex to uint64
	block := new(big.Int)
	block.SetString(blockHex[2:], 16) // Remove "0x" prefix
	return block.Uint64(), nil
}

// GetChainID retrieves the chain ID via zond_chainId
func (c *RPCClient) GetChainID() (*big.Int, error) {
	resp, err := c.call("zond_chainId", []interface{}{})
	if err != nil {
		return nil, fmt.Errorf("zond_chainId failed: %w", err)
	}

	// Parse hex string result
	var chainIDHex string
	if err := json.Unmarshal(resp.Result, &chainIDHex); err != nil {
		return nil, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	// Convert hex to big.Int
	chainID := new(big.Int)
	chainID.SetString(chainIDHex[2:], 16) // Remove "0x" prefix
	return chainID, nil
}

// GetTransactionReceipt retrieves the receipt for a transaction via zond_getTransactionReceipt
func (c *RPCClient) GetTransactionReceipt(txHash string) (map[string]interface{}, error) {
	resp, err := c.call("zond_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return nil, fmt.Errorf("zond_getTransactionReceipt failed: %w", err)
	}

	// Parse receipt object
	var receipt map[string]interface{}
	if err := json.Unmarshal(resp.Result, &receipt); err != nil {
		return nil, fmt.Errorf("failed to parse receipt: %w", err)
	}

	return receipt, nil
}

// GetGasPrice retrieves the current gas price via zond_gasPrice
func (c *RPCClient) GetGasPrice() (*big.Int, error) {
	resp, err := c.call("zond_gasPrice", []interface{}{})
	if err != nil {
		return nil, fmt.Errorf("zond_gasPrice failed: %w", err)
	}

	// Parse hex string result
	var gasPriceHex string
	if err := json.Unmarshal(resp.Result, &gasPriceHex); err != nil {
		return nil, fmt.Errorf("failed to parse gas price: %w", err)
	}

	// Convert hex to big.Int
	gasPrice := new(big.Int)
	gasPrice.SetString(gasPriceHex[2:], 16) // Remove "0x" prefix
	return gasPrice, nil
}

// SetNetwork changes the active network (for display purposes)
func (c *RPCClient) SetNetwork(network NetworkType) {
	c.network = network
}

// GetNetwork returns the current network
func (c *RPCClient) GetNetwork() NetworkType {
	return c.network
}

// IsConnected checks if the RPC endpoint is responding
func (c *RPCClient) IsConnected() bool {
	_, err := c.GetBlockNumber()
	return err == nil
}
