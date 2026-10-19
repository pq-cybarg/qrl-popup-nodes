// Copyright 2025 The QRL Authors. All rights reserved.
// This file defines configurable security levels for keystore encryption.

package keystore

import "fmt"

// SecurityLevel represents the security vs performance tradeoff for keystore encryption
type SecurityLevel int

const (
	// SecurityLevelFast provides fast unlock times (~0.5s) with moderate security
	// Suitable for: Development, testing, frequently-accessed wallets
	// Security: NIST Level 2 (Medium) - 64-bit quantum security
	SecurityLevelFast SecurityLevel = iota

	// SecurityLevelStandard provides balanced security and performance (~2s)
	// Suitable for: Most users, hot wallets, daily transactions
	// Security: NIST Level 5 (Very High) - 128-bit quantum security
	SecurityLevelStandard

	// SecurityLevelHigh provides maximum security with slower unlock (~4s)
	// Suitable for: Cold storage, validator keys, high-value accounts
	// Security: NIST Level 5+ (Extreme) - 128-bit quantum security with extreme memory-hardness
	SecurityLevelHigh

	// SecurityLevelParanoid provides extreme security with very slow unlock (~8s)
	// Suitable for: Long-term storage, multi-million dollar accounts, institutional custody
	// Security: NIST Level 5++ (Paranoid) - 128-bit quantum security with maximum memory-hardness
	SecurityLevelParanoid
)

// String returns the string representation of the security level
func (sl SecurityLevel) String() string {
	switch sl {
	case SecurityLevelFast:
		return "fast"
	case SecurityLevelStandard:
		return "standard"
	case SecurityLevelHigh:
		return "high"
	case SecurityLevelParanoid:
		return "paranoid"
	default:
		return "unknown"
	}
}

// ParseSecurityLevel parses a string into a SecurityLevel
func ParseSecurityLevel(s string) (SecurityLevel, error) {
	switch s {
	case "fast", "f", "1":
		return SecurityLevelFast, nil
	case "standard", "std", "s", "2", "":
		return SecurityLevelStandard, nil
	case "high", "h", "3":
		return SecurityLevelHigh, nil
	case "paranoid", "p", "4":
		return SecurityLevelParanoid, nil
	default:
		return SecurityLevelStandard, fmt.Errorf("invalid security level: %s (valid: fast, standard, high, paranoid)", s)
	}
}

// MLKEMParams contains ML-KEM keystore encryption parameters
type MLKEMParams struct {
	// Argon2id parameters
	Memory      uint32 // Memory in KB
	Iterations  uint32 // Time parameter
	Parallelism uint8  // Number of threads
	KeyLen      uint32 // Derived key length

	// Description
	UnlockTime string // Approximate unlock time
	NISTLevel  string // NIST security level
	MemoryMB   string // Memory usage in MB
}

// GetMLKEMParams returns the ML-KEM encryption parameters for a given security level
func GetMLKEMParams(level SecurityLevel) MLKEMParams {
	switch level {
	case SecurityLevelFast:
		// Fast: 32 MB, 2 iterations, ~0.5s unlock
		// Suitable for development and testing
		return MLKEMParams{
			Memory:      32768, // 32 MB
			Iterations:  2,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~0.5s",
			NISTLevel:   "Level 2 (Medium)",
			MemoryMB:    "32 MB",
		}

	case SecurityLevelStandard:
		// Standard: 128 MB, 4 iterations, ~2s unlock
		// Default for most users - good balance
		return MLKEMParams{
			Memory:      131072, // 128 MB
			Iterations:  4,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~2s",
			NISTLevel:   "Level 5 (Very High)",
			MemoryMB:    "128 MB",
		}

	case SecurityLevelHigh:
		// High: 256 MB, 5 iterations, ~4s unlock
		// For cold storage and validator keys
		return MLKEMParams{
			Memory:      262144, // 256 MB
			Iterations:  5,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~4s",
			NISTLevel:   "Level 5+ (Extreme)",
			MemoryMB:    "256 MB",
		}

	case SecurityLevelParanoid:
		// Paranoid: 512 MB, 6 iterations, ~8s unlock
		// Maximum security for long-term storage
		return MLKEMParams{
			Memory:      524288, // 512 MB
			Iterations:  6,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~8s",
			NISTLevel:   "Level 5++ (Paranoid)",
			MemoryMB:    "512 MB",
		}

	default:
		return GetMLKEMParams(SecurityLevelStandard)
	}
}

// PQCParams contains Hybrid PQC keystore encryption parameters
type PQCParams struct {
	// Argon2id parameters
	Memory      uint32 // Memory in KB
	Iterations  uint32 // Time parameter
	Parallelism uint8  // Number of threads
	KeyLen      uint32 // Derived key length

	// Description
	UnlockTime string // Approximate unlock time
	NISTLevel  string // NIST security level
	MemoryMB   string // Memory usage in MB
}

// GetPQCParams returns the Hybrid PQC encryption parameters for a given security level
func GetPQCParams(level SecurityLevel) PQCParams {
	switch level {
	case SecurityLevelFast:
		// Fast: 16 MB, 2 iterations, ~0.3s unlock
		return PQCParams{
			Memory:      16384, // 16 MB
			Iterations:  2,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~0.3s",
			NISTLevel:   "Level 2 (Medium)",
			MemoryMB:    "16 MB",
		}

	case SecurityLevelStandard:
		// Standard: 64 MB, 3 iterations, ~1s unlock
		// Original default
		return PQCParams{
			Memory:      65536, // 64 MB
			Iterations:  3,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~1s",
			NISTLevel:   "Level 3 (High)",
			MemoryMB:    "64 MB",
		}

	case SecurityLevelHigh:
		// High: 128 MB, 4 iterations, ~2s unlock
		return PQCParams{
			Memory:      131072, // 128 MB
			Iterations:  4,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~2s",
			NISTLevel:   "Level 5 (Very High)",
			MemoryMB:    "128 MB",
		}

	case SecurityLevelParanoid:
		// Paranoid: 256 MB, 5 iterations, ~4s unlock
		return PQCParams{
			Memory:      262144, // 256 MB
			Iterations:  5,
			Parallelism: 4,
			KeyLen:      32,
			UnlockTime:  "~4s",
			NISTLevel:   "Level 5+ (Extreme)",
			MemoryMB:    "256 MB",
		}

	default:
		return GetPQCParams(SecurityLevelStandard)
	}
}

// GetSecurityLevelInfo returns human-readable information about a security level
func GetSecurityLevelInfo(level SecurityLevel) map[string]interface{} {
	mlkem := GetMLKEMParams(level)
	pqc := GetPQCParams(level)

	var useCases []string
	var attackCost string
	var recommendation string

	switch level {
	case SecurityLevelFast:
		useCases = []string{
			"Development and testing",
			"Frequently-accessed hot wallets",
			"Low-value accounts (<$1,000)",
			"Temporary accounts",
		}
		attackCost = "~$10K GPU cluster"
		recommendation = "Not recommended for production use"

	case SecurityLevelStandard:
		useCases = []string{
			"Most users and applications",
			"Daily transaction accounts",
			"Medium-value accounts ($1K-$100K)",
			"Hot wallets for regular use",
		}
		attackCost = "~$2M GPU cluster"
		recommendation = "Recommended default for most users"

	case SecurityLevelHigh:
		useCases = []string{
			"Cold storage",
			"Validator keys",
			"High-value accounts ($100K-$1M)",
			"Long-term storage (5+ years)",
		}
		attackCost = "~$10M GPU cluster"
		recommendation = "Recommended for high-value assets"

	case SecurityLevelParanoid:
		useCases = []string{
			"Multi-million dollar accounts",
			"Institutional custody",
			"Long-term storage (10+ years)",
			"Maximum security requirements",
		}
		attackCost = "~$50M+ GPU cluster"
		recommendation = "Maximum security, accept slower unlock times"
	}

	return map[string]interface{}{
		"level":                level.String(),
		"mlkem_unlock_time":    mlkem.UnlockTime,
		"mlkem_memory":         mlkem.MemoryMB,
		"mlkem_nist_level":     mlkem.NISTLevel,
		"pqc_unlock_time":      pqc.UnlockTime,
		"pqc_memory":           pqc.MemoryMB,
		"pqc_nist_level":       pqc.NISTLevel,
		"use_cases":            useCases,
		"attack_cost_estimate": attackCost,
		"recommendation":       recommendation,
	}
}

// CompareSecurityLevels returns a comparison of all security levels
func CompareSecurityLevels() []map[string]interface{} {
	levels := []SecurityLevel{
		SecurityLevelFast,
		SecurityLevelStandard,
		SecurityLevelHigh,
		SecurityLevelParanoid,
	}

	comparison := make([]map[string]interface{}, len(levels))
	for i, level := range levels {
		comparison[i] = GetSecurityLevelInfo(level)
	}

	return comparison
}

// ValidateSecurityLevel checks if a security level is appropriate for a given use case
func ValidateSecurityLevel(level SecurityLevel, isProduction bool, accountValue float64) (bool, string) {
	// Development/testing: any level is acceptable
	if !isProduction {
		return true, "Any security level is acceptable for development"
	}

	// Production validation
	switch level {
	case SecurityLevelFast:
		if accountValue > 1000 {
			return false, "Fast security level not recommended for accounts > $1,000 in production"
		}
		return true, "Warning: Fast security level has lower resistance to GPU attacks"

	case SecurityLevelStandard:
		if accountValue > 100000 {
			return false, "Standard security level not recommended for accounts > $100,000"
		}
		return true, "Standard security level is appropriate for most production use"

	case SecurityLevelHigh:
		if accountValue > 1000000 {
			return false, "High security level may not be sufficient for accounts > $1,000,000 (consider Paranoid)"
		}
		return true, "High security level is appropriate for cold storage and validators"

	case SecurityLevelParanoid:
		return true, "Paranoid security level provides maximum protection"

	default:
		return false, "Unknown security level"
	}
}
