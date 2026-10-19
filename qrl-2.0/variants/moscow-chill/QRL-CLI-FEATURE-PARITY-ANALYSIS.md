# QRL-CLI Feature Parity Analysis for gzond

## Executive Summary

This document analyzes the feature parity between QRL's command-line tools (QRL-CLI and QRL Node CLI) and the current gzond implementation. It identifies missing features and provides a prioritized roadmap for implementation.

**Current Status**: gzond implements core node functionality but is missing most wallet management and transaction operations that QRL-CLI provides.

---

## Current gzond Commands

### Implemented Commands

1. **account** - Manage accounts
   - `list` - List existing accounts
   - `new` - Create new account (with security level support)
   - `update` - Update existing account
   - `import` - Import private key

2. **attach** - Connect to running node (JavaScript console)

3. **console** - Interactive JavaScript environment

4. **db** - Low-level database operations

5. **dump** - Dump specific block from storage

6. **dumpconfig** - Export configuration as TOML

7. **dumpgenesis** - Export genesis block JSON

8. **export** - Export blockchain to file

9. **import** - Import blockchain from file

10. **init** - Initialize genesis block

11. **license** - Display license information

12. **pqc-info** - Display keystore security information ✅ (QRL-specific)

13. **pqc-upgrade** - Upgrade keystores to PQC encryption ✅ (QRL-specific)

14. **removedb** - Remove blockchain and state databases

15. **snapshot** - Snapshot-based commands

16. **version** - Print version numbers

17. **version-check** - Check for security vulnerabilities

---

## QRL-CLI Commands (Missing from gzond)

### High Priority - Core Wallet & Transaction Features

#### 1. **balance** ❌ MISSING
**Purpose**: Query wallet balance from network
**Functionality**:
- Query balance by address or wallet file
- Support for mainnet/testnet
- Display in Quanta or Shor units
- Custom gRPC endpoints

**Implementation Requirements**:
- gRPC client for QRL network
- Address parsing and validation
- Unit conversion (Quanta ↔ Shor)
- Network selection (mainnet/testnet)

**Priority**: **CRITICAL** - Essential for basic wallet operations

---

#### 2. **send** / **tx_transfer** ❌ MISSING
**Purpose**: Transfer Quanta between addresses
**Functionality**:
- Send transactions with amount and recipient
- Optional message attachment (up to 80 bytes)
- Multi-output transactions
- Offline signing support
- Custom network endpoints
- OTS key index management

**Implementation Requirements**:
- Transaction builder
- Dilithium signature generation
- OTS key management
- gRPC transaction submission
- Fee calculation

**Priority**: **CRITICAL** - Essential for basic wallet operations

---

#### 3. **receive** ❌ MISSING
**Purpose**: Display receiving address with QR code
**Functionality**:
- Show QRL address in text format
- Generate QR code for mobile scanning
- Address validation

**Implementation Requirements**:
- QR code generation library
- Terminal-based QR rendering
- Address formatting

**Priority**: **HIGH** - Important for user experience

---

#### 4. **create-wallet** ⚠️ PARTIAL (as `account new`)
**Purpose**: Generate new QRL wallet
**Current Implementation**: `gzond account new`
**Missing Features**:
- ❌ Configurable tree height (4-18) for XMSS
- ❌ Hashing mechanism selection (SHA2-256, SHAKE-128, SHAKE-256)
- ✅ Password encryption (implemented)
- ✅ File output (implemented)
- ✅ Security level selection (implemented via `--security`)

**Implementation Requirements**:
- XMSS tree height configuration
- Multiple hash function support
- Dilithium parameter configuration

**Priority**: **MEDIUM** - Current implementation works, but missing QRL-specific options

---

### Medium Priority - Transaction Management

#### 5. **tx_message** / **send-message** ❌ MISSING
**Purpose**: Send message transactions
**Functionality**:
- Broadcast messages up to 80 bytes
- Message-only transactions (no value transfer)
- Recipient address optional

**Implementation Requirements**:
- Message transaction type
- Message encoding/decoding
- Transaction builder updates

**Priority**: **MEDIUM**

---

#### 6. **tx_inspect** ❌ MISSING
**Purpose**: Analyze transaction blobs before submission
**Functionality**:
- Decode transaction blob
- Display transaction details
- Verify signatures
- Validate structure

**Implementation Requirements**:
- Transaction decoder
- Pretty-printer for transaction details
- Signature verification

**Priority**: **MEDIUM** - Important for security and debugging

---

#### 7. **tx_push** ❌ MISSING
**Purpose**: Submit signed transaction blob to node
**Functionality**:
- Accept pre-signed transaction
- Validate transaction
- Broadcast to network
- Return transaction hash

**Implementation Requirements**:
- Transaction blob parser
- gRPC submission
- Response handling

**Priority**: **MEDIUM**

---

#### 8. **sign-tx-offline** ❌ MISSING
**Purpose**: Offline transaction signing
**Functionality**:
- Sign transactions without network connection
- Support for air-gapped systems
- Export signed transaction blob

**Implementation Requirements**:
- Offline transaction builder
- Transaction serialization
- Signature generation without node

**Priority**: **MEDIUM** - Important for security-conscious users

---

### Medium Priority - Wallet Management

#### 9. **wallet_secret** ❌ MISSING
**Purpose**: Display mnemonic and hexseed
**Functionality**:
- Show mnemonic phrase for recovery
- Display hexseed
- Secure display (masked by default)
- Require authentication

**Implementation Requirements**:
- Mnemonic generation (if not stored)
- Hexseed extraction from keystore
- Secure output handling

**Priority**: **MEDIUM**

---

#### 10. **wallet_recover** ❌ MISSING
**Purpose**: Restore wallet from mnemonic or hexseed
**Functionality**:
- Accept mnemonic phrase
- Accept hexseed
- Reconstruct Dilithium keys
- Save to keystore

**Implementation Requirements**:
- Mnemonic parser
- Hexseed parser
- Key derivation from seed
- Keystore integration

**Priority**: **MEDIUM**

---

#### 11. **wallet_encrypt** / **wallet_decrypt** ⚠️ PARTIAL
**Current Implementation**: Keystores are encrypted by default
**Missing Features**:
- ❌ Toggle encryption on existing keystores
- ❌ Change encryption without re-creating account

**Implementation Requirements**:
- In-place keystore encryption toggle
- Password change without full re-keying

**Priority**: **LOW** - Current encryption is sufficient

---

#### 12. **wallet_rm** ❌ MISSING
**Purpose**: Remove address from wallet
**Functionality**:
- Delete keystore file by address
- Require confirmation
- Backup warning

**Implementation Requirements**:
- Safe file deletion
- Confirmation prompts
- Backup recommendations

**Priority**: **LOW**

---

### High Priority - Network Operations

#### 13. **status** / **state** ❌ MISSING
**Purpose**: Query network status
**Functionality**:
- Node state (version, sync status)
- Block height
- Peer connections
- Network identification (mainnet/testnet)

**Implementation Requirements**:
- gRPC GetNodeState call
- Pretty-printed output
- Real-time status updates (optional)

**Priority**: **HIGH** - Essential for node operators

---

#### 14. **search** ❌ MISSING
**Purpose**: Look up transactions, blocks, addresses
**Functionality**:
- Search by transaction hash
- Search by block number/hash
- Search by address
- Display results in human-readable format

**Implementation Requirements**:
- gRPC API integration for queries
- Multi-type search parser
- Result formatter

**Priority**: **HIGH** - Essential for blockchain exploration

---

#### 15. **validate** ❌ MISSING
**Purpose**: Validate QRL addresses
**Functionality**:
- Check address format
- Verify checksums
- Display address information
- Address type detection

**Implementation Requirements**:
- Address parser
- Checksum verification
- Address metadata extraction

**Priority**: **MEDIUM**

---

### Medium Priority - Advanced Cryptography

#### 16. **generate-lattice-keys** ❌ MISSING
**Purpose**: Create Kyber and Dilithium lattice key pairs
**Functionality**:
- Generate Kyber KEM keypairs
- Generate Dilithium signature keypairs
- Include ECDSA keys (hybrid)
- Optional network broadcasting
- File encryption

**Implementation Requirements**:
- ML-KEM-768 key generation (already have library)
- Dilithium key generation (already implemented)
- ECDSA key generation (if needed)
- Lattice key transaction type

**Priority**: **MEDIUM** - Already have crypto primitives

---

#### 17. **generate-shared-keys** ❌ MISSING
**Purpose**: Create shared keys from lattice keys
**Functionality**:
- Key encapsulation using ML-KEM
- Shared secret generation
- File encryption for shared keys

**Implementation Requirements**:
- ML-KEM encapsulation (already have)
- Key storage format
- Encryption layer

**Priority**: **MEDIUM**

---

#### 18. **get-keys** ❌ MISSING
**Purpose**: Retrieve lattice keys from network
**Functionality**:
- Query by address
- Query by transaction hash
- Display public keys
- Verify on-chain registration

**Implementation Requirements**:
- gRPC query for lattice keys
- Key parser and formatter
- Transaction decoder for key broadcasts

**Priority**: **MEDIUM**

---

### Low Priority - Token Operations

#### 19. **tx_token** ❌ MISSING
**Purpose**: Create new tokens on network
**Functionality**:
- Token creation transaction
- Configure token parameters (name, symbol, decimals, supply)
- Token ownership management

**Implementation Requirements**:
- Token transaction type
- Token metadata encoding
- Token registry integration

**Priority**: **LOW** - Advanced feature

---

#### 20. **tx_transfertoken** ❌ MISSING
**Purpose**: Transfer tokens
**Functionality**:
- Token transfer transactions
- Token ID selection
- Amount specification

**Implementation Requirements**:
- Token transfer transaction type
- Token balance queries
- Token ownership verification

**Priority**: **LOW** - Advanced feature

---

#### 21. **token_list** ❌ MISSING
**Purpose**: List tokens owned by address
**Functionality**:
- Query tokens by address
- Display token balances
- Show token metadata

**Implementation Requirements**:
- gRPC token query
- Token balance aggregation
- Token metadata retrieval

**Priority**: **LOW** - Advanced feature

---

### Low Priority - Multi-Signature

#### 22. **tx_multi_sig_create** ❌ MISSING
**Purpose**: Create multi-signature addresses
**Functionality**:
- Define signing threshold
- Add multiple signers
- Create multi-sig address

**Implementation Requirements**:
- Multi-sig address format
- Multi-sig transaction type
- Signer management

**Priority**: **LOW** - Advanced feature

---

#### 23. **tx_multi_sig_spend** ❌ MISSING
**Purpose**: Execute multi-sig transactions
**Functionality**:
- Collect signatures from required signers
- Validate threshold
- Submit multi-sig transaction

**Implementation Requirements**:
- Multi-sig signature collection
- Threshold validation
- Multi-sig transaction builder

**Priority**: **LOW** - Advanced feature

---

### Low Priority - Notarization

#### 24. **notarize** ❌ MISSING
**Purpose**: Anchor document hashes to blockchain
**Functionality**:
- Hash document (SHA-256)
- Create notarization transaction
- Optional metadata message
- Timestamp on blockchain

**Implementation Requirements**:
- Document hash transaction type
- Hash verification
- Metadata encoding

**Priority**: **LOW** - Specialized use case

---

### Low Priority - OTS Management

#### 25. **ots** ❌ MISSING
**Purpose**: Display next unused OTS key index
**Functionality**:
- Query address state
- Calculate next OTS index
- Warn about OTS exhaustion
- Display remaining signatures

**Implementation Requirements**:
- OTS index tracking
- Address state queries
- OTS exhaustion warnings

**Priority**: **LOW** - Already managed internally for Dilithium

---

### Low Priority - Slave Transactions

#### 26. **slave_tx_generate** ❌ MISSING
**Purpose**: Generate slave transactions
**Functionality**:
- Create slave OTS trees
- Delegate signing authority
- Manage slave addresses

**Implementation Requirements**:
- Slave transaction type
- OTS tree management
- Delegation logic

**Priority**: **LOW** - QRL-specific advanced feature

---

## Implementation Priority Matrix

### Critical (Must Have)
1. **balance** - Cannot function as wallet without balance queries
2. **send** / **tx_transfer** - Core transaction functionality
3. **status** / **state** - Essential for node operators

### High Priority (Should Have)
4. **receive** - Important UX for receiving funds
5. **search** - Blockchain exploration
6. **validate** - Address verification

### Medium Priority (Nice to Have)
7. **tx_message** - Message transactions
8. **tx_inspect** - Security and debugging
9. **tx_push** - Pre-signed transaction support
10. **sign-tx-offline** - Air-gapped signing
11. **wallet_secret** - Recovery information
12. **wallet_recover** - Wallet restoration
13. **generate-lattice-keys** - Advanced crypto (have primitives)
14. **generate-shared-keys** - Key encapsulation
15. **get-keys** - Query lattice keys

### Low Priority (Future Enhancements)
16. **tx_token** - Token creation
17. **tx_transfertoken** - Token transfers
18. **token_list** - Token queries
19. **tx_multi_sig_create** - Multi-sig creation
20. **tx_multi_sig_spend** - Multi-sig execution
21. **notarize** - Document notarization
22. **ots** - OTS management
23. **slave_tx_generate** - Slave transactions
24. **wallet_rm** - Wallet deletion

---

## Technical Requirements

### Core Dependencies

1. **gRPC Client Integration**
   - QRL Public API Service client
   - GetNodeState, GetBalance, GetTransaction, etc.
   - Zeus Proxy compatibility (REST → gRPC)

2. **Transaction Builder**
   - Transaction serialization (protobuf?)
   - Signature generation (Dilithium)
   - Fee calculation
   - OTS key management (if applicable)

3. **Address Management**
   - QRL address format parsing
   - Checksum verification
   - Address type detection (P2PKH, multi-sig, etc.)

4. **Network Configuration**
   - Mainnet/testnet selection
   - Custom RPC endpoints
   - Zeus Proxy integration

5. **Unit Conversion**
   - Quanta ↔ Shor conversion
   - Display formatting

---

## Recommended Implementation Phases

### Phase 1: Essential Wallet Operations (Critical)
**Timeline**: 2-3 weeks
- Implement gRPC client for QRL network
- Add `balance` command
- Add `send` command (basic transfer)
- Add `status` command
- Test with mainnet/testnet

**Deliverables**:
- Users can check balances
- Users can send transactions
- Users can query node status

---

### Phase 2: Enhanced UX (High Priority)
**Timeline**: 1-2 weeks
- Add `receive` command with QR codes
- Add `search` command for blockchain exploration
- Add `validate` command for address verification
- Improve error messages and output formatting

**Deliverables**:
- Better user experience
- Blockchain exploration capabilities
- Address validation

---

### Phase 3: Advanced Transaction Features (Medium Priority)
**Timeline**: 2-3 weeks
- Add `tx_message` for message transactions
- Add `tx_inspect` for transaction analysis
- Add `tx_push` for pre-signed transactions
- Add `sign-tx-offline` for air-gapped signing
- Add `wallet_secret` and `wallet_recover`

**Deliverables**:
- Message transactions
- Offline signing support
- Wallet recovery options

---

### Phase 4: Lattice Cryptography (Medium Priority)
**Timeline**: 2-3 weeks
- Add `generate-lattice-keys` command
- Add `generate-shared-keys` command
- Add `get-keys` command
- Implement lattice key transaction types

**Deliverables**:
- Full post-quantum key management
- Shared key generation
- On-chain key registry

---

### Phase 5: Token & Multi-Sig (Low Priority)
**Timeline**: 3-4 weeks
- Add token creation (`tx_token`)
- Add token transfer (`tx_transfertoken`)
- Add token queries (`token_list`)
- Add multi-sig support (`tx_multi_sig_create`, `tx_multi_sig_spend`)

**Deliverables**:
- Token functionality
- Multi-signature wallets

---

### Phase 6: Specialized Features (Low Priority)
**Timeline**: 2-3 weeks
- Add `notarize` command
- Add `ots` management
- Add `slave_tx_generate`
- Add `wallet_rm`

**Deliverables**:
- Notarization service
- Advanced OTS management
- Wallet cleanup tools

---

## Architecture Recommendations

### 1. Create `cmd/gzond/wallcmd.go`
New file for wallet operations:
- `balance`
- `send`
- `receive`
- `wallet_secret`
- `wallet_recover`

### 2. Create `cmd/gzond/txcmd.go`
New file for transaction operations:
- `tx_message`
- `tx_inspect`
- `tx_push`
- `tx_token`
- `tx_transfertoken`
- `tx_multi_sig_*`

### 3. Create `cmd/gzond/latticecmd.go`
New file for lattice cryptography:
- `generate-lattice-keys`
- `generate-shared-keys`
- `get-keys`

### 4. Create `cmd/gzond/networkcmd.go`
New file for network operations:
- `status`
- `search`
- `validate`

### 5. Create `zond/qrlclient/` package
gRPC client for QRL network:
- Connection management
- API wrappers
- Error handling
- Zeus Proxy integration

### 6. Create `crypto/lattice/` package
Lattice key management:
- Key generation
- Key storage
- Key retrieval
- Shared key derivation

---

## Zeus Proxy Integration

Zeus Proxy provides REST → gRPC translation. Implementation options:

1. **Direct gRPC** (Recommended)
   - Use QRL's gRPC APIs directly
   - More efficient
   - Better type safety

2. **Via Zeus Proxy**
   - Use REST endpoints if gRPC is unavailable
   - Good for development/testing
   - Falls back to Zeus Proxy if needed

### Example Zeus Proxy Endpoints:
- `GET /grpc/mainnet/GetNodeState` - Node status
- `GET /grpc/mainnet/GetBalance?address=<addr>` - Balance query
- `POST /grpc/mainnet/PushTransaction` - Submit transaction

---

## Testing Strategy

### Unit Tests
- Address parsing and validation
- Transaction building and signing
- Key generation and management
- Unit conversion (Quanta ↔ Shor)

### Integration Tests
- Connect to testnet
- Create wallet
- Send transactions
- Query balances
- Search blockchain

### End-to-End Tests
- Full wallet lifecycle
- Multi-signature workflows
- Token creation and transfer
- Lattice key management

---

## Documentation Requirements

### User Documentation
- Command reference for each new command
- Tutorial: Setting up a wallet
- Tutorial: Sending your first transaction
- Tutorial: Offline signing
- Tutorial: Lattice key management

### Developer Documentation
- gRPC client usage
- Transaction builder API
- Adding new transaction types
- Zeus Proxy integration guide

---

## Security Considerations

1. **Private Key Storage**
   - Continue using ML-KEM encrypted keystores ✅
   - Never expose private keys in logs
   - Secure memory handling

2. **Transaction Signing**
   - Verify all transaction parameters before signing
   - Warn about large amounts
   - Require confirmation for destructive operations

3. **OTS Key Management**
   - Track OTS usage to prevent reuse
   - Warn about OTS exhaustion
   - Atomic OTS increment

4. **Network Security**
   - Verify node TLS certificates
   - Detect man-in-the-middle attacks
   - Secure RPC connections

---

## Compatibility Matrix

| Feature | QRL-CLI | QRL Node CLI | gzond Current | gzond Target |
|---------|---------|--------------|---------------|--------------|
| Create Wallet | ✅ | ✅ | ✅ (partial) | ✅ |
| Balance Query | ✅ | ❌ | ❌ | ✅ |
| Send Transaction | ✅ | ✅ | ❌ | ✅ |
| Receive (QR) | ✅ | ❌ | ❌ | ✅ |
| Message TX | ✅ | ✅ | ❌ | ✅ |
| Network Status | ✅ | ✅ | ❌ | ✅ |
| Search | ✅ | ❌ | ❌ | ✅ |
| Validate Address | ✅ | ❌ | ❌ | ✅ |
| Lattice Keys | ✅ | ❌ | ❌ | ✅ |
| Token Create | ❌ | ✅ | ❌ | ✅ |
| Token Transfer | ❌ | ✅ | ❌ | ✅ |
| Multi-Sig | ❌ | ✅ | ❌ | ✅ |
| Offline Signing | ✅ | ❌ | ❌ | ✅ |
| Notarize | ✅ | ❌ | ❌ | ✅ |
| OTS Management | ✅ | ❌ | ❌ | ✅ |
| Slave TX | ❌ | ✅ | ❌ | ✅ |
| PQC Keystore | ❌ | ❌ | ✅ | ✅ |
| Security Levels | ❌ | ❌ | ✅ | ✅ |

---

## Conclusion

**Current State**: gzond is a functional node implementation with excellent keystore security (ML-KEM, security levels) but lacks wallet and transaction management features.

**Gap Analysis**:
- Missing 19 major command groups
- No gRPC client integration
- No transaction builder
- No balance/send functionality

**Recommended Approach**:
Implement in phases, starting with critical wallet operations (balance, send, status) to achieve basic wallet functionality within 2-3 weeks.

**Estimated Total Timeline**:
- Phase 1-3 (Essential + UX + Advanced TX): ~6-8 weeks
- Phase 4-6 (Full Feature Parity): ~13-18 weeks total

**Next Steps**:
1. Set up gRPC client infrastructure
2. Implement Phase 1 (balance, send, status)
3. Test with testnet
4. Iterate based on user feedback
