//go:build ignore
// +build ignore

// Copyright 2025 The QRL Authors. All rights reserved.
// This file provides tools for upgrading legacy keystores to PQC-secure keystores.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/theQRL/go-qrl/accounts/keystore"
	"github.com/theQRL/go-qrl/cmd/utils"
	"github.com/urfave/cli/v2"
)

var (
	pqcUpgradeCommand = &cli.Command{
		Name:      "pqc-upgrade",
		Usage:     "Upgrade keystores to post-quantum secure encryption",
		ArgsUsage: "",
		Description: `
Upgrades legacy keystores (scrypt + AES-128) to post-quantum secure keystores
using Argon2id + KMAC256 + AES-256.

This provides:
- Memory-hard key derivation (GPU/ASIC resistant)
- Quantum-resistant MAC (KMAC256 based on SHA-3)
- Stronger encryption (AES-256 vs AES-128)

The upgrade process:
1. Scans the keystore directory for legacy keystores
2. For each keystore, prompts for password
3. Decrypts with old format, re-encrypts with PQC format
4. Backs up original to .legacy extension
5. Saves new PQC keystore

Note: This requires knowing the password for each keystore.
`,
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.KeyStoreDirFlag,
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show what would be upgraded without actually upgrading",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Skip confirmation prompts",
			},
		},
		Action: upgradePQCKeystores,
	}

	pqcInfoCommand = &cli.Command{
		Name:      "pqc-info",
		Usage:     "Display security information about keystores",
		ArgsUsage: "[address]",
		Description: `
Displays detailed security information about keystores, including:
- Encryption type (legacy vs PQC)
- Key derivation function parameters
- Security level rating
- Quantum resistance status

If an address is provided, shows info for that specific keystore.
Otherwise, shows info for all keystores in the keystore directory.
`,
		Flags: []cli.Flag{
			utils.DataDirFlag,
			utils.KeyStoreDirFlag,
		},
		Action: showPQCInfo,
	}
)

func upgradePQCKeystores(ctx *cli.Context) error {
	// Get keystore directory
	keystoreDir := ctx.String(utils.KeyStoreDirFlag.Name)
	if keystoreDir == "" {
		datadir := ctx.String(utils.DataDirFlag.Name)
		if datadir == "" {
			return fmt.Errorf("must specify --datadir or --keystore")
		}
		keystoreDir = filepath.Join(datadir, "keystore")
	}

	// Check if directory exists
	if _, err := os.Stat(keystoreDir); os.IsNotExist(err) {
		return fmt.Errorf("keystore directory does not exist: %s", keystoreDir)
	}

	// Scan for keystore files
	files, err := ioutil.ReadDir(keystoreDir)
	if err != nil {
		return fmt.Errorf("failed to read keystore directory: %v", err)
	}

	dryRun := ctx.Bool("dry-run")
	force := ctx.Bool("force")

	var legacyFiles []string
	var pqcFiles []string

	// Identify legacy vs PQC keystores
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Skip backup files
		if strings.HasSuffix(file.Name(), ".legacy") {
			continue
		}

		// Skip non-keystore files
		if !strings.HasPrefix(file.Name(), "UTC--") {
			continue
		}

		fullPath := filepath.Join(keystoreDir, file.Name())
		data, err := ioutil.ReadFile(fullPath)
		if err != nil {
			fmt.Printf("Warning: Could not read %s: %v\n", file.Name(), err)
			continue
		}

		if keystore.IsPQCKeystore(data) {
			pqcFiles = append(pqcFiles, file.Name())
		} else {
			legacyFiles = append(legacyFiles, file.Name())
		}
	}

	// Display status
	fmt.Printf("\n=== Keystore Status ===\n")
	fmt.Printf("Directory: %s\n", keystoreDir)
	fmt.Printf("PQC Keystores: %d\n", len(pqcFiles))
	fmt.Printf("Legacy Keystores: %d\n", len(legacyFiles))
	fmt.Printf("\n")

	if len(legacyFiles) == 0 {
		fmt.Println("✓ All keystores are already using PQC encryption!")
		return nil
	}

	// Show which files would be upgraded
	fmt.Printf("The following keystores will be upgraded:\n")
	for _, file := range legacyFiles {
		fmt.Printf("  - %s\n", file)
	}
	fmt.Printf("\n")

	if dryRun {
		fmt.Println("Dry run mode - no changes will be made")
		return nil
	}

	// Confirm upgrade
	if !force {
		fmt.Print("Do you want to proceed with the upgrade? (yes/no): ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "yes" {
			fmt.Println("Upgrade cancelled")
			return nil
		}
	}

	// Upgrade each legacy keystore
	upgraded := 0
	failed := 0

	for _, file := range legacyFiles {
		fullPath := filepath.Join(keystoreDir, file)

		fmt.Printf("\nUpgrading: %s\n", file)
		password := utils.GetPassPhraseWithList("Enter password: ", false, 0, nil)

		// Read the legacy keystore
		legacyData, err := ioutil.ReadFile(fullPath)
		if err != nil {
			fmt.Printf("  ✗ Failed to read keystore: %v\n", err)
			failed++
			continue
		}

		// Decrypt with legacy format
		key, err := keystore.DecryptKey(legacyData, password)
		if err != nil {
			fmt.Printf("  ✗ Failed to decrypt (wrong password?): %v\n", err)
			failed++
			continue
		}

		// Re-encrypt with PQC format
		pqcData, err := keystore.EncryptKeyPQC(key, password)
		if err != nil {
			fmt.Printf("  ✗ Failed to encrypt with PQC: %v\n", err)
			failed++
			continue
		}

		// Backup original
		backupPath := fullPath + ".legacy"
		if err := os.Rename(fullPath, backupPath); err != nil {
			fmt.Printf("  ✗ Failed to backup original: %v\n", err)
			failed++
			continue
		}

		// Write new PQC keystore
		if err := ioutil.WriteFile(fullPath, pqcData, 0600); err != nil {
			// Try to restore backup
			os.Rename(backupPath, fullPath)
			fmt.Printf("  ✗ Failed to write PQC keystore: %v\n", err)
			failed++
			continue
		}

		fmt.Printf("  ✓ Upgraded successfully\n")
		fmt.Printf("    Backup saved to: %s.legacy\n", file)
		upgraded++
	}

	// Summary
	fmt.Printf("\n=== Upgrade Summary ===\n")
	fmt.Printf("Upgraded: %d\n", upgraded)
	fmt.Printf("Failed: %d\n", failed)

	if upgraded > 0 {
		fmt.Printf("\n✓ Your keystores are now post-quantum secure!\n")
		fmt.Printf("  - Argon2id for memory-hard key derivation\n")
		fmt.Printf("  - KMAC256 (SHA-3) for quantum-resistant authentication\n")
		fmt.Printf("  - AES-256 for stronger encryption\n")
	}

	return nil
}

func showPQCInfo(ctx *cli.Context) error {
	// Get keystore directory
	keystoreDir := ctx.String(utils.KeyStoreDirFlag.Name)
	if keystoreDir == "" {
		datadir := ctx.String(utils.DataDirFlag.Name)
		if datadir == "" {
			return fmt.Errorf("must specify --datadir or --keystore")
		}
		keystoreDir = filepath.Join(datadir, "keystore")
	}

	// Check if directory exists
	if _, err := os.Stat(keystoreDir); os.IsNotExist(err) {
		return fmt.Errorf("keystore directory does not exist: %s", keystoreDir)
	}

	// Get target address if specified
	targetAddr := ""
	if ctx.Args().Len() > 0 {
		targetAddr = strings.ToLower(ctx.Args().Get(0))
	}

	// Scan keystore files
	files, err := ioutil.ReadDir(keystoreDir)
	if err != nil {
		return fmt.Errorf("failed to read keystore directory: %v", err)
	}

	fmt.Printf("\n=== Keystore Security Information ===\n")
	fmt.Printf("Directory: %s\n\n", keystoreDir)

	count := 0
	for _, file := range files {
		if file.IsDir() || strings.HasSuffix(file.Name(), ".legacy") {
			continue
		}

		if !strings.HasPrefix(file.Name(), "UTC--") {
			continue
		}

		fullPath := filepath.Join(keystoreDir, file.Name())
		data, err := ioutil.ReadFile(fullPath)
		if err != nil {
			continue
		}

		// Parse to get address
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}

		addr, ok := m["address"].(string)
		if !ok {
			continue
		}

		// Filter by address if specified
		if targetAddr != "" && !strings.Contains(strings.ToLower(addr), targetAddr) {
			continue
		}

		// Get security info
		info, err := keystore.GetKeystoreSecurityInfo(data)
		if err != nil {
			fmt.Printf("File: %s\n", file.Name())
			fmt.Printf("  Error: %v\n\n", err)
			continue
		}

		fmt.Printf("Address: %s\n", addr)
		fmt.Printf("File: %s\n", file.Name())
		fmt.Printf("Type: %s\n", info["type"])
		fmt.Printf("KDF: %s\n", info["kdf"])
		fmt.Printf("Cipher: %s\n", info["cipher"])
		fmt.Printf("MAC: %s\n", info["mac"])

		if pqc, ok := info["pqc_compliant"].(bool); ok && pqc {
			fmt.Printf("Memory: %s\n", info["memory"])
			fmt.Printf("Iterations: %v\n", info["iterations"])
			fmt.Printf("Security Level: %s\n", info["security_level"])
			fmt.Printf("Grover Security: %s\n", info["grover_security"])
			fmt.Printf("✓ Post-Quantum Secure: Yes\n")
		} else {
			fmt.Printf("Grover Security: %s\n", info["grover_security"])
			fmt.Printf("✗ Post-Quantum Secure: No\n")
			if rec, ok := info["recommendation"].(string); ok {
				fmt.Printf("⚠  %s\n", rec)
			}
		}

		fmt.Println()
		count++
	}

	if count == 0 {
		fmt.Println("No keystores found")
	}

	return nil
}
