# Chain State Export - January 29, 2026

## Current State (Block 739)

### Accounts: 41 total
- Only genesis account has balance: 0x52b7d2dcc80cd2e4000000 (100M Quanta)
- All other 40 accounts have 0 balance

### Transactions: 0
- No transactions have been sent on this chain
- Genesis account nonce: 0

### Contracts: 0
- No contracts deployed
- Precompiles (0x20, 0x21) are native, not deployed

### Keystore Files
All 41 account keystore files preserved in ./keystore/

## What This Means
The only valuable data is:
1. The 41 keystore files (private keys)
2. Genesis account balance (set in genesis.json)

Since no transactions occurred, restoring from Jan 3 backup
and re-adding the keystore files will preserve everything.
