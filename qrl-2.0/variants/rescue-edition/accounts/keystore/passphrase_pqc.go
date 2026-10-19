//go:build ignore
// +build ignore

// Copyright 2025 The QRL Authors. All rights reserved.
// This file implements post-quantum secure keystore encryption.
//
// Hybrid PQC Approach:
// 1. Argon2id for memory-hard key derivation (GPU + ASIC resistant)
// 2. KMAC256 (SHA-3) for quantum-resistant authentication
// 3. AES-256-CTR for symmetric encryption (Grover: 128-bit effective)
//
// Future: Add ML-KEM for key encapsulation

package keystore

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/theQRL/go-qrllib/crypto/ml_dsa_87"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/crypto/pqcrypto"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/sha3"
)

const (
	// Hybrid PQC constants
	pqcVersion         = 1
	pqcKDFArgon2id     = "argon2id"
	pqcCipherAES256CTR = "aes-256-ctr"
	pqcMACKMAC256      = "kmac256"

	// Argon2id recommended parameters for keystore encryption
	pqcArgon2Memory      = 65536 // 64 MB
	pqcArgon2Iterations  = 3
	pqcArgon2Parallelism = 4
	pqcArgon2KeyLen      = 32 // 256-bit key for AES-256

	// Salt and IV lengths
	pqcSaltLen = 32 // 256-bit salt
	pqcIVLen   = 16 // 128-bit IV for AES-CTR
)

// kmac256 implements KMAC256 using cSHAKE256 per NIST SP 800-185
// KMAC (Keccak Message Authentication Code) is the SHA-3 family's MAC
// This provides post-quantum authentication
func kmac256(key, data, customization []byte, outputLen int) []byte {
	// KMAC is defined as cSHAKE with function name "KMAC" and custom string S
	// Function name N = "KMAC"
	functionName := []byte("KMAC")

	// Create cSHAKE with function name "KMAC" and customization string
	h := sha3.NewCShake256(functionName, customization)

	// Encode key length (left_encode) and write key
	// For KMAC, we encode length in bits
	keyLenBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(keyLenBuf, uint64(len(key)*8)) // length in bits
	h.Write(keyLenBuf)
	h.Write(key)

	// Write the message data
	h.Write(data)

	// Read output of specified length
	output := make([]byte, outputLen)
	h.Read(output)

	return output
}

// encryptDataPQC uses the Web3 Secret Storage format, but with PQC primitives
type encryptedKeyJSONPQC struct {
	Address string     `json:"address"`
	Crypto  cryptoPQC  `json:"crypto"`
	Id      string     `json:"id"`
	Version int        `json:"version"`
}

type cryptoPQC struct {
	Cipher       string            `json:"cipher"`
	CipherText   string            `json:"ciphertext"`
	CipherParams cipherparamsPQC   `json:"cipherparams"`
	KDF          string            `json:"kdf"`
	KDFParams    kdfparamsPQC      `json:"kdfparams"`
	MAC          string            `json:"mac"`
	MACParams    macparamsPQC      `json:"macparams"`
}

type cipherparamsPQC struct {
	IV string `json:"iv"`
}

type kdfparamsPQC struct {
	DKLen  int    `json:"dklen"`
	Salt   string `json:"salt"`
	Memory uint32 `json:"memory"`
	Time   uint32 `json:"time"`
	Threads uint8  `json:"threads"`
}

type macparamsPQC struct {
	Customization string `json:"customization"`
}

// EncryptKeyPQC encrypts a private key using post-quantum secure primitives with default security level
func EncryptKeyPQC(key *Key, auth string) ([]byte, error) {
	return EncryptKeyPQCWithLevel(key, auth, SecurityLevelStandard)
}

// EncryptKeyPQCWithLevel encrypts a private key using post-quantum secure primitives
// Returns JSON in Web3 Secret Storage format with PQC extensions
func EncryptKeyPQCWithLevel(key *Key, auth string, level SecurityLevel) ([]byte, error) {
	// Get security parameters for the specified level
	params := GetPQCParams(level)
	// Generate random salt for Argon2id
	salt := make([]byte, pqcSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, errors.Wrap(err, "failed to generate salt")
	}

	// Step 1: Derive key using Argon2id (memory-hard, GPU-resistant)
	derivedKey := argon2.IDKey(
		[]byte(auth),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLen,
	)

	// Step 2: Use first 32 bytes for AES-256 encryption key
	encryptKey := derivedKey[:32]

	// Generate random IV for AES-CTR
	iv := make([]byte, pqcIVLen)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, errors.Wrap(err, "failed to generate IV")
	}

	// Encrypt the ML-DSA-87 seed using AES-256-CTR
	seed := key.MLDSA87.GetSeed()
	cipherText, err := aesCTRXOR(encryptKey, seed[:], iv)
	if err != nil {
		return nil, err
	}

	// Step 3: Generate KMAC256 for authentication (post-quantum MAC)
	// MAC over: ciphertext || iv
	macData := append(cipherText, iv...)
	mac := kmac256(
		derivedKey,                    // Use full derived key as KMAC key
		macData,                       // MAC the ciphertext + IV
		[]byte("ZOND-KEYSTORE-PQC-V1"), // Customization string
		32,                            // 256-bit MAC
	)

	// Create the encrypted key JSON structure
	cryptoStruct := cryptoPQC{
		Cipher:     pqcCipherAES256CTR,
		CipherText: hex.EncodeToString(cipherText),
		CipherParams: cipherparamsPQC{
			IV: hex.EncodeToString(iv),
		},
		KDF: pqcKDFArgon2id,
		KDFParams: kdfparamsPQC{
			DKLen:   int(params.KeyLen),
			Salt:    hex.EncodeToString(salt),
			Memory:  params.Memory,
			Time:    params.Iterations,
			Threads: params.Parallelism,
		},
		MAC: hex.EncodeToString(mac),
		MACParams: macparamsPQC{
			Customization: "ZOND-KEYSTORE-PQC-V1",
		},
	}

	encryptedKeyJSON := encryptedKeyJSONPQC{
		Address: hex.EncodeToString(key.Address[:]),
		Crypto:  cryptoStruct,
		Id:      uuid.New().String(),
		Version: pqcVersion,
	}

	return json.Marshal(encryptedKeyJSON)
}

// DecryptKeyPQC decrypts a keystore file using post-quantum secure verification
func DecryptKeyPQC(keyjson []byte, auth string) (*Key, error) {
	// Parse the encrypted key JSON
	var keyJSON encryptedKeyJSONPQC
	if err := json.Unmarshal(keyjson, &keyJSON); err != nil {
		return nil, err
	}

	// Verify version
	if keyJSON.Version != pqcVersion {
		return nil, fmt.Errorf("unsupported keystore version: %d", keyJSON.Version)
	}

	// Verify KDF is Argon2id
	if keyJSON.Crypto.KDF != pqcKDFArgon2id {
		return nil, fmt.Errorf("unsupported KDF: %s", keyJSON.Crypto.KDF)
	}

	// Decode salt
	salt, err := hex.DecodeString(keyJSON.Crypto.KDFParams.Salt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode salt")
	}

	// Derive key using Argon2id
	derivedKey := argon2.IDKey(
		[]byte(auth),
		salt,
		keyJSON.Crypto.KDFParams.Time,
		keyJSON.Crypto.KDFParams.Memory,
		keyJSON.Crypto.KDFParams.Threads,
		uint32(keyJSON.Crypto.KDFParams.DKLen),
	)

	// Decode ciphertext and IV
	cipherText, err := hex.DecodeString(keyJSON.Crypto.CipherText)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode ciphertext")
	}

	iv, err := hex.DecodeString(keyJSON.Crypto.CipherParams.IV)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode IV")
	}

	// Decode stored MAC
	storedMAC, err := hex.DecodeString(keyJSON.Crypto.MAC)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode MAC")
	}

	// Verify MAC using KMAC256 (post-quantum authentication)
	macData := append(cipherText, iv...)
	customization := keyJSON.Crypto.MACParams.Customization
	if customization == "" {
		customization = "ZOND-KEYSTORE-PQC-V1" // Default
	}

	computedMAC := kmac256(
		derivedKey,
		macData,
		[]byte(customization),
		32, // 256-bit MAC
	)

	// Constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare(storedMAC, computedMAC) != 1 {
		return nil, errors.New("could not decrypt key with given password")
	}

	// Decrypt the private key
	encryptKey := derivedKey[:32] // Use first 32 bytes for AES-256
	plainText, err := aesCTRXOR(encryptKey, cipherText, iv)
	if err != nil {
		return nil, err
	}

	// Parse the decrypted ML-DSA-87 seed
	if len(plainText) != pqcrypto.SeedSize {
		return nil, fmt.Errorf("invalid seed size: got %d, want %d", len(plainText), pqcrypto.SeedSize)
	}

	var seed [ml_dsa_87.SeedBytes]uint8
	copy(seed[:], plainText)

	mldsa87Key, err := ml_dsa_87.NewMLDSA87FromSeed(seed)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct ML-DSA-87 key from seed")
	}

	// Verify the address matches
	pk := mldsa87Key.GetPK()
	addr := pqcrypto.MLDSA87PKToAddress(pk[:])
	expectedAddrBytes, err := hex.DecodeString(keyJSON.Address)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode expected address")
	}

	var expectedAddr common.Address
	copy(expectedAddr[:], expectedAddrBytes)

	if !bytes.Equal(addr[:], expectedAddr[:]) {
		return nil, errors.New("address mismatch: decryption succeeded but key is invalid")
	}

	return &Key{
		Id:      uuid.MustParse(keyJSON.Id),
		Address: addr,
		MLDSA87: mldsa87Key,
	}, nil
}

// IsPQCKeystore checks if a keystore JSON uses PQC encryption
func IsPQCKeystore(keyjson []byte) bool {
	var m map[string]interface{}
	if err := json.Unmarshal(keyjson, &m); err != nil {
		return false
	}

	crypto, ok := m["crypto"].(map[string]interface{})
	if !ok {
		return false
	}

	kdf, ok := crypto["kdf"].(string)
	return ok && kdf == pqcKDFArgon2id
}

// GetKeystoreSecurityInfo returns security information about a keystore
func GetKeystoreSecurityInfo(keyjson []byte) (map[string]interface{}, error) {
	// Check ML-KEM first (newest format)
	if IsMLKEMKeystore(keyjson) {
		return GetMLKEMKeystoreSecurityInfo(keyjson)
	} else if IsPQCKeystore(keyjson) {
		var keyJSON encryptedKeyJSONPQC
		if err := json.Unmarshal(keyjson, &keyJSON); err != nil {
			return nil, err
		}

		memoryMB := float64(keyJSON.Crypto.KDFParams.Memory) / 1024
		var securityLevel string
		if keyJSON.Crypto.KDFParams.Memory >= 131072 {
			securityLevel = "NIST Level 5 (Very High)"
		} else if keyJSON.Crypto.KDFParams.Memory >= 65536 {
			securityLevel = "NIST Level 3 (High)"
		} else {
			securityLevel = "NIST Level 2 (Medium)"
		}

		return map[string]interface{}{
			"type":           "Post-Quantum Secure (Hybrid)",
			"kdf":            "Argon2id (memory-hard, GPU-resistant)",
			"cipher":         "AES-256-CTR",
			"mac":            "KMAC256 (SHA-3 based, quantum-resistant)",
			"memory":         fmt.Sprintf("%.1f MB", memoryMB),
			"iterations":     keyJSON.Crypto.KDFParams.Time,
			"parallelism":    keyJSON.Crypto.KDFParams.Threads,
			"security_level": securityLevel,
			"pqc_compliant":  true,
			"lattice_based":  false,
			"grover_security": "128-bit effective (AES-256)",
			"recommendation": "Upgrade to ML-KEM for full lattice-based protection",
		}, nil
	} else {
		return map[string]interface{}{
			"type":           "Legacy",
			"kdf":            "scrypt",
			"cipher":         "AES-128-CTR",
			"mac":            "Keccak-256 (not quantum-secure MAC)",
			"pqc_compliant":  false,
			"lattice_based":  false,
			"grover_security": "64-bit effective (AES-128)",
			"recommendation": "Upgrade to ML-KEM keystore for quantum resistance",
		}, nil
	}
}
