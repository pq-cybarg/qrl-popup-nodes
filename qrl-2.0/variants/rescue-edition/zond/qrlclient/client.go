// Copyright 2025 The go-zond Authors
// This file implements a REST/gRPC client for QRL networks via Zeus Proxy

package qrlclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// NetworkType represents the type of QRL network
type NetworkType string

const (
	// Classic QRL networks
	ClassicMainnet NetworkType = "classic-mainnet"
	ClassicTestnet NetworkType = "classic-testnet"

	// Zond networks
	ZondMainnet  NetworkType = "zond-mainnet"
	ZondTestnet  NetworkType = "zond-testnet"
	ZondKurtosis NetworkType = "zond-kurtosis"
	ZondLocal    NetworkType = "zond-local"

	// Legacy compatibility
	Mainnet NetworkType = "mainnet"
	Testnet NetworkType = "testnet"
)

// Client is a REST/gRPC client for QRL networks via Zeus Proxy
type Client struct {
	baseURL    string
	httpClient *http.Client
	network    NetworkType
}

// Config holds client configuration
type Config struct {
	ZeusProxyURL string      // Zeus Proxy URL (e.g., "http://localhost:3000")
	Network      NetworkType // Network to connect to
	Timeout      time.Duration
}

// NewClient creates a new QRL client
func NewClient(config *Config) (*Client, error) {
	if config.ZeusProxyURL == "" {
		config.ZeusProxyURL = "http://localhost:3000"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Network == "" {
		config.Network = ZondKurtosis // Default to Kurtosis for development
	}

	return &Client{
		baseURL: config.ZeusProxyURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		network: config.Network,
	}, nil
}

// SetNetwork changes the active network
func (c *Client) SetNetwork(network NetworkType) {
	c.network = network
}

// GetNetwork returns the current network
func (c *Client) GetNetwork() NetworkType {
	return c.network
}

// get performs a GET request to Zeus Proxy
func (c *Client) get(endpoint string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/grpc/%s/%s", c.baseURL, c.network, endpoint)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return result, nil
}

// post performs a POST request to Zeus Proxy
func (c *Client) post(endpoint string, data interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/grpc/%s/%s", c.baseURL, c.network, endpoint)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("POST request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return result, nil
}

// Health checks the Zeus Proxy health status
func (c *Client) Health() (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return result, nil
}

// Networks returns information about all configured networks
func (c *Client) Networks() (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/networks", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("networks query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return result, nil
}
