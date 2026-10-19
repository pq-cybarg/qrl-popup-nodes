# ML-DSA-87 Upgrade (FIPS 204)

## Overview

This document describes the upgrade from the pre-standard Dilithium signature scheme to the FIPS 204 ML-DSA-87 standard in go-zond.

## What Changed

**Signature Scheme**: Dilithium -> ML-DSA-87

The pre-standardization Dilithium implementation has been replaced with the finalized NIST FIPS 204 ML-DSA-87 standard. Both schemes use the same underlying cryptographic parameters (K=8, L=7) but ML-DSA-87 is the official standardized version published in August 2024.

## Why the Change

1. **NIST Standardization**: ML-DSA-87 is the finalized FIPS 204 standard, providing regulatory clarity and long-term stability
2. **API Improvements**: FIPS 204 introduces context parameters for domain separation
3. **Future Compatibility**: Using the standardized version ensures compatibility with other implementations

## Technical Details

### API Changes

The primary API difference is the addition of a context parameter to signing operations:

```go
// Old Dilithium API
signature, err := d.Sign(message)
dilithium.Verify(message, signature, &pk)

// New ML-DSA-87 API (FIPS 204)
signature, err := d.Sign(nil, message)  // nil context used
ml_dsa_87.Verify(nil, message, signature, &pk)
```

### Context Parameter Decision

We use `nil` (empty context) for the context parameter because:
- Provides closest equivalent behavior to the old Dilithium implementation
- Simplifies migration
- Transaction signatures already include chain ID, nonce, and other uniqueness guarantees
- Domain-specific context can be added later if needed

### Key Parameters (unchanged)

| Parameter | Value |
|-----------|-------|
| Security Level | NIST Level 5 (equivalent to AES-256) |
| Seed Size | 32 bytes |
| Public Key Size | 2,592 bytes |
| Secret Key Size | 4,896 bytes |
| Signature Size | 4,627 bytes |
| Address Size | 20 bytes |

### Address Derivation

Address derivation remains the same:
```
address = SHAKE256(descriptor || public_key)[:20]
descriptor = [wallet_type_byte, 0x00, 0x00]
```

Where `wallet_type_byte` is `0x01` for ML-DSA-87.

## Impact

### Breaking Changes

1. **All existing keys are invalid**: The upgrade changes the signature scheme, making previously generated keys incompatible
2. **Chain data is incompatible**: Any existing chain data signed with Dilithium will not validate

### Required Actions

After this upgrade, you must:

1. **Reset the chain**: Run `reset-chain.sh` to regenerate genesis and accounts
2. **Generate new keys**: All validator and user keys must be regenerated
3. **Restart the network**: All nodes must restart with the new genesis

## Files Modified

### Core Crypto
- `crypto/pqcrypto/crypto.go` - Key generation and loading functions
- `crypto/pqcrypto/signature.go` - Signing wrapper with context parameter
- `crypto/crypto.go` - Updated GenerateMLDSA87Key function

### Keystore
- `accounts/keystore/key.go` - Key struct uses MLDSA87 type
- `accounts/keystore/keystore.go` - Updated imports
- `accounts/keystore/passphrase_pqc.go` - Key encryption/decryption

### Transaction Signing
- `core/types/transaction_signing.go` - SignTx with context parameter

### CLI Tools
- `cmd/zondkey/generate.go` - Key generation
- `cmd/zondkey/message.go` - Message signing/verification
- `cmd/gzond/walletcmd.go` - Wallet operations
- `cmd/zvm/internal/t8ntool/transition.go` - State transition tool

### Test Files
Multiple test files updated with new imports and type references.

## go-qrllib Changes

The `GetAddress()` method was added to `ml_dsa_87.MLDSA87` in go-qrllib to maintain API compatibility with the old Dilithium implementation. This allows code to continue using `key.GetAddress()` directly.

## Date

December 2024
