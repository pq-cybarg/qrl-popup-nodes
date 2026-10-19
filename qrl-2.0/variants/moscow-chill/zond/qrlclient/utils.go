// Copyright 2025 The go-zond Authors
// This file implements utility functions for QRL client

package qrlclient

import (
	"fmt"
	"math/big"
	"strings"
)

const (
	// Unit conversions
	ShorPerQuanta = 1_000_000_000 // 1 Quanta = 1 billion Shor

	// Address prefixes
	ZondAddressPrefix    = "Z"
	ClassicAddressPrefix = "Q"
)

// QuantaToShor converts Quanta to Shor
func QuantaToShor(quanta float64) *big.Int {
	// Convert float to big.Float for precision
	quantaBig := new(big.Float).SetFloat64(quanta)

	// Multiply by 10^9
	shorPerQuanta := new(big.Float).SetInt64(ShorPerQuanta)
	shorBig := new(big.Float).Mul(quantaBig, shorPerQuanta)

	// Convert to big.Int
	shor, _ := shorBig.Int(nil)
	return shor
}

// ShorToQuanta converts Shor to Quanta
func ShorToQuanta(shor *big.Int) float64 {
	// Convert to big.Float
	shorBig := new(big.Float).SetInt(shor)

	// Divide by 10^9
	shorPerQuanta := new(big.Float).SetInt64(ShorPerQuanta)
	quantaBig := new(big.Float).Quo(shorBig, shorPerQuanta)

	// Convert to float64
	quanta, _ := quantaBig.Float64()
	return quanta
}

// FormatBalance formats a balance in Shor to a human-readable string in Quanta
func FormatBalance(shor *big.Int) string {
	quanta := ShorToQuanta(shor)
	return fmt.Sprintf("%.9f", quanta)
}

// ParseAmount parses an amount string (in Quanta) to Shor
func ParseAmount(amount string) (*big.Int, error) {
	// Try parsing as float
	var quanta float64
	_, err := fmt.Sscanf(amount, "%f", &quanta)
	if err != nil {
		return nil, fmt.Errorf("invalid amount format: %w", err)
	}

	if quanta < 0 {
		return nil, fmt.Errorf("amount cannot be negative")
	}

	return QuantaToShor(quanta), nil
}

// ValidateAddress validates a QRL address format
func ValidateAddress(address string) error {
	if len(address) == 0 {
		return fmt.Errorf("address cannot be empty")
	}

	// Check prefix
	prefix := address[0:1]
	if prefix != ZondAddressPrefix && prefix != ClassicAddressPrefix {
		return fmt.Errorf("invalid address prefix: must start with '%s' (Zond) or '%s' (Classic)", ZondAddressPrefix, ClassicAddressPrefix)
	}

	// Check length (Zond: Z + 40 hex chars = 41, Classic: Q + variable)
	if prefix == ZondAddressPrefix {
		if len(address) != 41 {
			return fmt.Errorf("invalid Zond address length: expected 41 characters, got %d", len(address))
		}

		// Validate hex characters after prefix
		hexPart := address[1:]
		for _, c := range hexPart {
			if !isHexChar(c) {
				return fmt.Errorf("invalid character in address: %c (expected hex)", c)
			}
		}
	} else if prefix == ClassicAddressPrefix {
		// Classic QRL addresses have variable length (typically 79 chars)
		if len(address) < 20 {
			return fmt.Errorf("invalid Classic address length: too short")
		}
	}

	return nil
}

// isHexChar checks if a character is a valid hexadecimal digit
func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// GetAddressPrefix returns the prefix of an address
func GetAddressPrefix(address string) string {
	if len(address) == 0 {
		return ""
	}
	return address[0:1]
}

// IsZondAddress checks if an address is a Zond address
func IsZondAddress(address string) bool {
	return strings.HasPrefix(address, ZondAddressPrefix)
}

// IsClassicAddress checks if an address is a Classic QRL address
func IsClassicAddress(address string) bool {
	return strings.HasPrefix(address, ClassicAddressPrefix)
}

// DetectNetwork detects the network type based on an address
func DetectNetwork(address string) (NetworkType, error) {
	if err := ValidateAddress(address); err != nil {
		return "", fmt.Errorf("invalid address: %w", err)
	}

	if IsZondAddress(address) {
		// Default to Zond Kurtosis for development
		// User can override with --network flag
		return ZondKurtosis, nil
	} else if IsClassicAddress(address) {
		// Default to Classic Testnet for safety
		return ClassicTestnet, nil
	}

	return "", fmt.Errorf("unable to detect network from address")
}

// SuggestNetwork suggests appropriate networks based on address
func SuggestNetwork(address string) []NetworkType {
	if IsZondAddress(address) {
		return []NetworkType{
			ZondKurtosis,
			ZondLocal,
			ZondTestnet,
			ZondMainnet,
		}
	} else if IsClassicAddress(address) {
		return []NetworkType{
			ClassicTestnet,
			ClassicMainnet,
		}
	}

	return []NetworkType{}
}

// NetworkDescription returns a human-readable description of a network
func NetworkDescription(network NetworkType) string {
	descriptions := map[NetworkType]string{
		ClassicMainnet: "QRL Classic Mainnet (Production)",
		ClassicTestnet: "QRL Classic Testnet (Testing)",
		ZondMainnet:    "Zond Mainnet (Production)",
		ZondTestnet:    "Zond Testnet (Testing)",
		ZondKurtosis:   "Zond Kurtosis (Local Development)",
		ZondLocal:      "Zond Local Node (Local Development)",
		Mainnet:        "Mainnet (Legacy)",
		Testnet:        "Testnet (Legacy)",
	}

	if desc, ok := descriptions[network]; ok {
		return desc
	}

	return string(network)
}
