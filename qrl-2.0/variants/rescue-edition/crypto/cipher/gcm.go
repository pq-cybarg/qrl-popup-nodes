package cipher

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

const GCMNonceSize = 12

// EncryptGCM encrypts plaintext using AES-GCM with the given key and nonce. The ciphertext is
// appended to dest, which must not overlap with plaintext.
func EncryptGCM(dest, key, nonce, plaintext, additionalData []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(fmt.Errorf("can't create block cipher: %v", err))
	}
	aesgcm, err := cipher.NewGCMWithNonceSize(block, GCMNonceSize)
	if err != nil {
		panic(fmt.Errorf("can't create GCM: %v", err))
	}
	return aesgcm.Seal(dest, nonce, plaintext, additionalData), nil
}

// DecryptGCM decrypts ciphertext using AES-GCM with the given key and nonce.
func DecryptGCM(key, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("can't create block cipher: %v", err)
	}
	if len(nonce) != GCMNonceSize {
		return nil, fmt.Errorf("invalid GCM nonce size: %d", len(nonce))
	}
	aesgcm, err := cipher.NewGCMWithNonceSize(block, GCMNonceSize)
	if err != nil {
		return nil, fmt.Errorf("can't create GCM: %v", err)
	}
	pt := make([]byte, 0, len(ciphertext))
	return aesgcm.Open(pt, nonce, ciphertext, additionalData)
}
