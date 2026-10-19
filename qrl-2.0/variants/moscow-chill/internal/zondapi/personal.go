// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package zondapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/theQRL/go-qrl/accounts"
	"github.com/theQRL/go-qrl/accounts/keystore"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/crypto"
	"github.com/theQRL/go-qrl/crypto/pqcrypto"
	"github.com/theQRL/go-qrl/log"
)

// PersonalAccountAPI provides an API for managing accounts via the personal namespace.
// It offers methods to create, list, lock, and unlock accounts.
type PersonalAccountAPI struct {
	am        *accounts.Manager
	nonceLock *AddrLocker
	b         Backend
}

// NewPersonalAccountAPI creates a new PersonalAccountAPI.
func NewPersonalAccountAPI(b Backend, nonceLock *AddrLocker) *PersonalAccountAPI {
	return &PersonalAccountAPI{
		am:        b.AccountManager(),
		nonceLock: nonceLock,
		b:         b,
	}
}

// ListAccounts returns all account addresses managed by this node.
func (s *PersonalAccountAPI) ListAccounts() []common.Address {
	return s.am.Accounts()
}

// NewAccount creates a new account with the given password and returns the address.
func (s *PersonalAccountAPI) NewAccount(password string) (common.Address, error) {
	ks := s.getKeyStore()
	if ks == nil {
		return common.Address{}, errors.New("no keystore backend available")
	}
	acc, err := ks.NewAccount(password)
	if err != nil {
		return common.Address{}, err
	}
	return acc.Address, nil
}

// UnlockAccount unlocks the account with the given address and password for the
// specified duration. If duration is 0, the account is unlocked indefinitely.
// Returns true if the account was successfully unlocked.
func (s *PersonalAccountAPI) UnlockAccount(ctx context.Context, addr common.Address, password string, duration *uint64) (bool, error) {
	// Check if insecure unlock is allowed when accessed via external RPC
	if s.b.ExtRPCEnabled() && !s.am.Config().InsecureUnlockAllowed {
		return false, errors.New("account unlock with HTTP access is forbidden")
	}

	ks := s.getKeyStore()
	if ks == nil {
		return false, errors.New("no keystore backend available")
	}

	// Find the account
	acc := accounts.Account{Address: addr}
	acc, err := ks.Find(acc)
	if err != nil {
		return false, err
	}

	// Determine timeout duration
	var d time.Duration
	if duration == nil {
		d = 300 * time.Second // Default 5 minutes
	} else if *duration == 0 {
		d = 0 // Unlock indefinitely
	} else {
		d = time.Duration(*duration) * time.Second
	}

	// Perform the unlock
	err = ks.TimedUnlock(acc, password, d)
	if err != nil {
		log.Warn("Failed to unlock account", "address", addr, "err", err)
		return false, err
	}

	log.Info("Account unlocked", "address", addr, "duration", d)
	return true, nil
}

// LockAccount locks the account with the given address.
// Returns true if the account was successfully locked.
func (s *PersonalAccountAPI) LockAccount(addr common.Address) bool {
	ks := s.getKeyStore()
	if ks == nil {
		return false
	}

	if err := ks.Lock(addr); err != nil {
		log.Warn("Failed to lock account", "address", addr, "err", err)
		return false
	}

	log.Info("Account locked", "address", addr)
	return true
}

// SendTransaction creates a transaction from the given arguments and signs it
// with the account's private key. The password is used to decrypt the private key.
func (s *PersonalAccountAPI) SendTransaction(ctx context.Context, args TransactionArgs, passwd string) (common.Hash, error) {
	// Check if we have from address
	if args.From == nil {
		return common.Hash{}, errors.New("from address not specified")
	}

	// Look up the wallet containing the requested signer
	ks := s.getKeyStore()
	if ks == nil {
		return common.Hash{}, errors.New("no keystore backend available")
	}

	// Find the account
	acc := accounts.Account{Address: *args.From}
	acc, err := ks.Find(acc)
	if err != nil {
		return common.Hash{}, err
	}

	// Set defaults
	if args.Nonce == nil {
		s.nonceLock.LockAddr(*args.From)
		defer s.nonceLock.UnlockAddr(*args.From)
	}

	if err := args.setDefaults(ctx, s.b); err != nil {
		return common.Hash{}, err
	}

	// Create the transaction
	tx := args.toTransaction()

	// Sign the transaction with passphrase
	chainID := s.b.ChainConfig().ChainID
	signedTx, err := ks.SignTxWithPassphrase(acc, passwd, tx, chainID)
	if err != nil {
		return common.Hash{}, err
	}

	// Submit the transaction
	if err := s.b.SendTx(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}

	return signedTx.Hash(), nil
}

// SignTransaction creates a transaction from the given arguments and signs it
// with the account's private key. It doesn't broadcast the transaction.
func (s *PersonalAccountAPI) SignTransaction(ctx context.Context, args TransactionArgs, passwd string) (*SignTransactionResult, error) {
	// Check if we have from address
	if args.From == nil {
		return nil, errors.New("from address not specified")
	}

	// Look up the wallet containing the requested signer
	ks := s.getKeyStore()
	if ks == nil {
		return nil, errors.New("no keystore backend available")
	}

	// Find the account
	acc := accounts.Account{Address: *args.From}
	acc, err := ks.Find(acc)
	if err != nil {
		return nil, err
	}

	// Set defaults (including nonce)
	if err := args.setDefaults(ctx, s.b); err != nil {
		return nil, err
	}

	// Create the transaction
	tx := args.toTransaction()

	// Sign the transaction with passphrase
	chainID := s.b.ChainConfig().ChainID
	signedTx, err := ks.SignTxWithPassphrase(acc, passwd, tx, chainID)
	if err != nil {
		return nil, err
	}

	// Encode the transaction
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return &SignTransactionResult{data, signedTx}, nil
}

// Sign signs the given data with the private key of the specified account.
// The password is used to decrypt the private key.
func (s *PersonalAccountAPI) Sign(ctx context.Context, data hexutil.Bytes, addr common.Address, passwd string) (hexutil.Bytes, error) {
	ks := s.getKeyStore()
	if ks == nil {
		return nil, errors.New("no keystore backend available")
	}

	// Find the account
	acc := accounts.Account{Address: addr}
	acc, err := ks.Find(acc)
	if err != nil {
		return nil, err
	}

	// Sign the data with passphrase using ML-DSA-87
	sig, err := ks.SignHashWithPassphrase(acc, passwd, crypto.Keccak256(data))
	if err != nil {
		return nil, err
	}

	return sig, nil
}

// ImportRawKey imports the given unencrypted private key (hex encoded) into
// the keystore and encrypts it with the given password.
func (s *PersonalAccountAPI) ImportRawKey(privkey string, password string) (common.Address, error) {
	ks := s.getKeyStore()
	if ks == nil {
		return common.Address{}, errors.New("no keystore backend available")
	}

	// Decode hex string to bytes
	keyBytes, err := hexutil.Decode(privkey)
	if err != nil {
		return common.Address{}, fmt.Errorf("invalid hex key: %v", err)
	}

	// For ML-DSA-87, we need to import the full key from seed
	d := pqcrypto.ToMLDSA87Unsafe(keyBytes)
	if d == nil {
		return common.Address{}, fmt.Errorf("invalid ML-DSA-87 seed")
	}

	acc, err := ks.ImportMLDSA87(d, password)
	if err != nil {
		return common.Address{}, err
	}

	return acc.Address, nil
}

// ListWallets returns the wallets this node manages.
func (s *PersonalAccountAPI) ListWallets() []rawWallet {
	wallets := s.am.Wallets()
	result := make([]rawWallet, len(wallets))
	for i, wallet := range wallets {
		status, err := wallet.Status()
		result[i] = rawWallet{
			URL:      wallet.URL().String(),
			Status:   status,
			Accounts: wallet.Accounts(),
		}
		if err != nil {
			result[i].Failure = err.Error()
		}
	}
	return result
}

// rawWallet is a JSON representation of an accounts.Wallet
type rawWallet struct {
	URL      string             `json:"url"`
	Status   string             `json:"status"`
	Failure  string             `json:"failure,omitempty"`
	Accounts []accounts.Account `json:"accounts,omitempty"`
}

// getKeyStore returns the keystore backend if available.
func (s *PersonalAccountAPI) getKeyStore() *keystore.KeyStore {
	backends := s.am.Backends(keystore.KeyStoreType)
	if len(backends) == 0 {
		return nil
	}
	return backends[0].(*keystore.KeyStore)
}

// DeriveAccount requests a HD wallet to derive a new account, optionally pinning
// it for later reuse.
func (s *PersonalAccountAPI) DeriveAccount(url string, path string, pin *bool) (accounts.Account, error) {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return accounts.Account{}, err
	}
	derivPath, err := accounts.ParseDerivationPath(path)
	if err != nil {
		return accounts.Account{}, err
	}
	if pin == nil {
		pin = new(bool)
	}
	return wallet.Derive(derivPath, *pin)
}

// OpenWallet initiates a hardware wallet opening procedure, establishing a USB
// connection and attempting to authenticate via the provided passphrase.
func (s *PersonalAccountAPI) OpenWallet(url string, passphrase *string) error {
	wallet, err := s.am.Wallet(url)
	if err != nil {
		return err
	}
	pass := ""
	if passphrase != nil {
		pass = *passphrase
	}
	return wallet.Open(pass)
}
