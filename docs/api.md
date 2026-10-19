---
title: API reference
nav_order: 7
---

# API reference

Three API surfaces are exposed by every popup stack:

- **gqrl JSON-RPC** on port 9545 (`qrl-popup`), 9745 (`l2-rescue`), 9945 (`l2-moscow`).
- **beacon REST** on port 3600 / 3700 / 4000.
- **beacon gRPC-gateway** on port 4100 / 4200 / 4500.

Plus a fourth, operator-facing surface:

- **dashboard HTTP** on port 8080 / 8180 / 8280.

## gqrl JSON-RPC

The gqrl client exposes the `qrl_*` method namespace — the QRL
equivalent of Ethereum's `eth_*`. **Prefix every method with `qrl_`**
not `eth_`; the eth-named methods are intentionally not aliased so
users don't accidentally port an Ethereum client without noticing the
quantum-safe semantics (64-byte Q-addresses, ML-DSA-87 signatures).

Core methods used by the dashboard:

| Method                 | Params                  | Returns              |
|------------------------|-------------------------|----------------------|
| `qrl_blockNumber`      | `[]`                    | `0x…` hex block num  |
| `qrl_chainId`          | `[]`                    | `0x…` hex chainId    |
| `qrl_getBalance`       | `[address, blockTag]`   | `0x…` hex wei        |
| `qrl_getTransactionByHash` | `[txHash]`          | tx object            |
| `qrl_getBlockByNumber` | `[blockTag, fullTxs]`   | block object         |
| `qrl_getBlockByHash`   | `[blockHash, fullTxs]`  | block object         |
| `qrl_getTransactionReceipt` | `[txHash]`         | receipt or null      |
| `qrl_call`             | `[txObject, blockTag]`  | `0x…` return data    |
| `qrl_estimateGas`      | `[txObject]`            | `0x…` hex gas        |
| `qrl_sendRawTransaction` | `[signedTxHex]`       | `0x…` txHash         |
| `qrl_getLogs`          | `[filter]`              | log array            |
| `qrl_variant`          | `[]`                    | `"standard"` / `"rescue-edition"` / `"moscow-chill"` |

Addresses are **64-byte Q-prefix** hex strings — 130 characters total
including the `Q` and 128 hex chars. Example:

```
Q00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003
```

Legacy 20-byte Z-prefix addresses (from the pre-migration Zond chain)
are **accepted on input** by the dashboard and the deposit contract,
but **never emitted on output**. Everything out of the chain is 64-byte Q.

### Example — getBlockByNumber

```bash
curl -sS -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"qrl_getBlockByNumber","params":["latest",false],"id":1}' \
  http://127.0.0.1:9545
```

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "number": "0x1c8",
    "hash": "0x…",
    "parentHash": "0x…",
    "miner": "Q0000…0001",
    "gasLimit": "0x1312d00",
    "baseFeePerGas": "0x3b9aca00",
    "timestamp": "0x6ad…",
    "transactions": []
  }
}
```

## Beacon REST

The beacon exposes the standard beacon-API REST surface at
`http://127.0.0.1:3600/qrl/v1/…`. Core endpoints:

| Path                              | Returns                           |
|-----------------------------------|-----------------------------------|
| `/qrl/v1/node/syncing`            | `head_slot`, `sync_distance`, `is_syncing`, `el_offline` |
| `/qrl/v1/node/identity`           | ENR, p2p multiaddr                |
| `/qrl/v1/beacon/headers/head`     | head block header + root          |
| `/qrl/v1/beacon/states/head/validators` | validator set + status      |
| `/qrl/v1/beacon/states/head/finality_checkpoints` | current / previous justified + finalized epoch |
| `/qrl/v1/beacon/blocks/{slot}`    | block at slot                     |

### Example — chain head

```bash
curl -s http://127.0.0.1:3600/qrl/v1/node/syncing
```

```json
{
  "data": {
    "head_slot": "2640",
    "sync_distance": "0",
    "is_syncing": false,
    "is_optimistic": false,
    "el_offline": false
  }
}
```

A `head_slot` that climbs by one every ~60 s means the chain is
alive. `el_offline: true` means the beacon lost the Engine API
connection to gqrl — check `docker logs qrl-popup-gqrl-node1`.

## Beacon gRPC gateway

Same endpoints as the REST surface, available over gRPC on port 4100
for programmatic use. Protos ship with the qrysm repo. The gateway is
the one qrysm wires PQ-TLS onto; see [security](security.html).

## Dashboard HTTP

The dashboard's own API, scoped to its own compose project. **Only use
these from the dashboard UI**; they're not stable across releases.

| Endpoint                         | Method | Purpose                              |
|----------------------------------|--------|--------------------------------------|
| `/api/dashboard/identity`        | GET    | rank, project, theme, variant, hostname |
| `/api/compose/status`            | GET    | per-container health + resources     |
| `/api/compose/projects`          | GET    | every compose project on the daemon (LEAD only) |
| `/api/compose/security`          | GET    | aggregated hardening posture         |
| `/api/compose/reset`             | POST   | stop + wipe + re-up (destructive)    |
| `/api/compose/container/restart` | POST   | restart one container, same project only |
| `/api/system/disk`               | GET    | disk-space badge                     |
| `/api/system/qrlenv`             | GET    | master-key presence + plaintext count |
| `/api/wallet/create`             | POST   | PQ keypair generation                |
| `/api/wallet/unlock`             | POST   | password → in-memory private key     |
| `/api/wallet/list`               | GET    | known walletId list                  |
| `/api/release/build`             | POST   | invoke `build-images.sh`             |
| `/api/release/sign`              | POST   | invoke `sign-images.sh`              |
| `/api/release/verify`            | POST   | invoke `verify-images.sh`            |
| `/api/release/load`              | POST   | invoke `load-release.sh`             |
| `/api/release/artifacts`         | GET    | enumerate `docker/out/`              |
| `/api/release/bundle`            | POST   | invoke `release.sh`                  |
| `/api/tls/gen-cert`              | POST   | invoke `docker/gen-tls-cert.sh`      |

### Example — identity

```bash
curl -s http://127.0.0.1:8080/api/dashboard/identity | jq .
```

```json
{
  "success": true,
  "rank": "LEAD",
  "project": "qrl-popup",
  "inContainer": true,
  "hostname": "ba5c9787635a",
  "theme": {
    "name": "cobalt",
    "accent": "#3aa0ff",
    "label": "COBALT"
  },
  "variant": "standard"
}
```

## Address format reference

- **Q-prefix 64-byte**: `Q` + 128 hex chars = 129-char string.
  Current. Every QRL 2.0 chain emits only this.
- **Z-prefix 20-byte (legacy)**: `Z` + 40 hex chars = 41-char string.
  Pre-migration Zond. Still accepted on input where backwards-compat
  matters (deposit contract constructor, address input fields on the
  dashboard) but normalised to Q on display.
- **Q-prefix 20-byte**: not a thing. 20-byte Q addresses are rejected
  as malformed.
- **Classic Q (XMSS)**: `Q010…` + 70 hex chars. This is the format of
  the QRL Classic chain (XMSS post-quantum signatures, pre-ZOND). The
  popup stack is a QRL 2.0 target, not Classic; dashboard distinguishes
  by prefix pattern.

## Common client gotchas

- **"invalid chainId"** on a transaction: EIP-155 chain-id in the raw
  signed tx must match the chain's `qrl_chainId`. The popup stacks pin
  to `1` since they are not mainnet — a dApp that hardcodes mainnet
  chainId will reject every tx.
- **"gas estimation failed"**: the gqrl mempool is empty; your pending
  tx probably reverts. Switch to `qrl_call` with the same inputs and
  read the revert reason.
- **"beacon returning head_slot=0 forever"**: your validator db has a
  stale `genesis_validators_root` from a prior chain. Fix: stop
  validator, `rm -f docker/data/validators/validator.db`, restart.
