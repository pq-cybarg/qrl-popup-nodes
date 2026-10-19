// Copyright 2025 The go-zond Authors
// This file implements QRL API methods

package qrlclient

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// NodeInfo represents detailed information about a QRL node
type NodeInfo struct {
	Version         string `json:"version"`
	State           string `json:"state"`
	NumConnections  uint32 `json:"num_connections"`
	NumKnownPeers   uint32 `json:"num_known_peers"`
	BlockHeight     uint64 `json:"block_height"`
	BlockLastHash   string `json:"block_last_hash"`
	NetworkID       string `json:"network_id"`
	Uptime          uint64 `json:"uptime"`
}

// NodeState represents the state of a QRL node
type NodeState struct {
	Info NodeInfo `json:"info"`
}

// Balance represents an address balance
type Balance struct {
	Address string   `json:"address"`
	Balance *big.Int `json:"balance"`
}

// Transaction represents a QRL transaction
type Transaction struct {
	TxHash      string   `json:"tx_hash"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Amount      *big.Int `json:"amount"`
	Fee         *big.Int `json:"fee"`
	Nonce       uint64   `json:"nonce"`
	BlockNumber uint64   `json:"block_number"`
	Timestamp   uint64   `json:"timestamp"`
}

// GetNodeState retrieves the current node state
func (c *Client) GetNodeState() (*NodeState, error) {
	result, err := c.get("GetNodeState")
	if err != nil {
		return nil, fmt.Errorf("GetNodeState failed: %w", err)
	}

	// Extract node state from response
	// The exact structure depends on QRL's gRPC response format
	state := &NodeState{}

	if info, ok := result["info"].(map[string]interface{}); ok {
		if version, ok := info["version"].(string); ok {
			state.Info.Version = version
		}
		if stateStr, ok := info["state"].(string); ok {
			state.Info.State = stateStr
		}
		if numConnections, ok := info["num_connections"].(float64); ok {
			state.Info.NumConnections = uint32(numConnections)
		}
		if numKnownPeers, ok := info["num_known_peers"].(float64); ok {
			state.Info.NumKnownPeers = uint32(numKnownPeers)
		}
		if blockHeight, ok := info["block_height"].(float64); ok {
			state.Info.BlockHeight = uint64(blockHeight)
		}
		if blockLastHash, ok := info["block_last_hash"].(string); ok {
			state.Info.BlockLastHash = blockLastHash
		}
		if networkID, ok := info["network_id"].(string); ok {
			state.Info.NetworkID = networkID
		}
		if uptime, ok := info["uptime"].(float64); ok {
			state.Info.Uptime = uint64(uptime)
		}
	}

	return state, nil
}

// GetBalance retrieves the balance for a QRL address
func (c *Client) GetBalance(address string) (*Balance, error) {
	// Zeus proxy handles address prefix conversion automatically
	result, err := c.post("GetBalance", map[string]interface{}{
		"address": address,
	})
	if err != nil {
		return nil, fmt.Errorf("GetBalance failed: %w", err)
	}

	balance := &Balance{
		Address: address,
	}

	// Parse balance from response
	if balanceStr, ok := result["balance"].(string); ok {
		balanceBig := new(big.Int)
		if _, ok := balanceBig.SetString(balanceStr, 10); ok {
			balance.Balance = balanceBig
		}
	} else if balanceFloat, ok := result["balance"].(float64); ok {
		balance.Balance = big.NewInt(int64(balanceFloat))
	}

	if balance.Balance == nil {
		balance.Balance = big.NewInt(0)
	}

	return balance, nil
}

// GetAddressState retrieves detailed address state information
func (c *Client) GetAddressState(address string) (map[string]interface{}, error) {
	result, err := c.post("GetAddressState", map[string]interface{}{
		"address": address,
	})
	if err != nil {
		return nil, fmt.Errorf("GetAddressState failed: %w", err)
	}

	return result, nil
}

// GetTransaction retrieves a transaction by hash
func (c *Client) GetTransaction(txHash string) (map[string]interface{}, error) {
	result, err := c.post("GetTransaction", map[string]interface{}{
		"query": txHash,
	})
	if err != nil {
		return nil, fmt.Errorf("GetTransaction failed: %w", err)
	}

	return result, nil
}

// GetBlock retrieves a block by number or hash
func (c *Client) GetBlock(query interface{}) (map[string]interface{}, error) {
	result, err := c.post("GetBlock", map[string]interface{}{
		"query": query,
	})
	if err != nil {
		return nil, fmt.Errorf("GetBlock failed: %w", err)
	}

	return result, nil
}

// GetLatestData retrieves latest blocks, transactions, or unconfirmed transactions
func (c *Client) GetLatestData(filter string, offset, quantity uint32) (map[string]interface{}, error) {
	result, err := c.post("GetLatestData", map[string]interface{}{
		"filter":   filter,
		"offset":   offset,
		"quantity": quantity,
	})
	if err != nil {
		return nil, fmt.Errorf("GetLatestData failed: %w", err)
	}

	return result, nil
}

// GetOTS retrieves the next unused OTS key index for an address
func (c *Client) GetOTS(address string) (map[string]interface{}, error) {
	result, err := c.post("GetOTS", map[string]interface{}{
		"address": address,
	})
	if err != nil {
		return nil, fmt.Errorf("GetOTS failed: %w", err)
	}

	return result, nil
}

// PushTransaction submits a signed transaction to the network
func (c *Client) PushTransaction(transactionSigned []byte) (map[string]interface{}, error) {
	result, err := c.post("PushTransaction", map[string]interface{}{
		"transaction_signed": transactionSigned,
	})
	if err != nil {
		return nil, fmt.Errorf("PushTransaction failed: %w", err)
	}

	return result, nil
}

// Search searches for blocks, transactions, or addresses
func (c *Client) Search(query string) (map[string]interface{}, error) {
	result, err := c.post("GetObject", map[string]interface{}{
		"query": query,
	})
	if err != nil {
		return nil, fmt.Errorf("Search failed: %w", err)
	}

	return result, nil
}

// GetUnconfirmedTransactionCount returns the number of unconfirmed transactions
func (c *Client) GetUnconfirmedTransactionCount() (uint32, error) {
	url := fmt.Sprintf("%s/util/%s/unconfirmedTxCount", c.baseURL, c.network)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("GetUnconfirmedTransactionCount failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		UnconfirmedTransactionCount uint32 `json:"unconfirmedTransactionCount"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.UnconfirmedTransactionCount, nil
}

// GetNonce retrieves the current nonce for an address
func (c *Client) GetNonce(address string) (uint64, error) {
	// Get address state which includes nonce
	result, err := c.GetAddressState(address)
	if err != nil {
		return 0, fmt.Errorf("GetNonce failed: %w", err)
	}

	// Try to extract nonce from state
	if state, ok := result["state"].(map[string]interface{}); ok {
		if nonceFloat, ok := state["nonce"].(float64); ok {
			return uint64(nonceFloat), nil
		}
		if nonceStr, ok := state["nonce"].(string); ok {
			nonceBig := new(big.Int)
			if _, ok := nonceBig.SetString(nonceStr, 10); ok {
				return nonceBig.Uint64(), nil
			}
		}
	}

	// If nonce not found, return 0 (indicates new address)
	return 0, nil
}
