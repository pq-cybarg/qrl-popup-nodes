//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"

	"github.com/theQRL/go-qrl/crypto/pqcrypto"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: derive-address <keyfile>")
		os.Exit(1)
	}

	keyfile := os.Args[1]

	d, err := pqcrypto.LoadDilithium(keyfile)
	if err != nil {
		fmt.Printf("Error loading key: %v\n", err)
		os.Exit(1)
	}

	pk := d.GetPK()
	addr := pqcrypto.DilithiumPKToAddress(pk[:])

	fmt.Printf("Address: %s\n", addr.Hex())
}
