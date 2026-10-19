# API reference

Full matrix at
[docs/api](https://theqrl.github.io/qrl-popup-nodes/api.html).

Each popup stack exposes four API surfaces:

| Surface            | Protocol         | qrl-popup   | l2-rescue | l2-moscow |
|--------------------|------------------|-------------|-----------|-----------|
| gqrl JSON-RPC      | JSON-RPC 2.0     | :9545-9548  | :9745-9748| :9945-9948|
| gqrl WebSocket     | JSON-RPC over WS | :9645-9648  | :9845-9848| :10045-10048|
| beacon REST        | REST + JSON      | :3600       | :3700     | :4000     |
| beacon gRPC gateway| gRPC             | :4100       | :4200     | :4500     |
| dashboard HTTP     | REST + JSON      | :8080       | :8180     | :8280     |

## JSON-RPC methods — prefix is `qrl_` not `eth_`

Methods are intentionally **not** aliased to `eth_*`. Portability
without noticing the quantum-safe semantics (64-byte Q-addresses,
ML-DSA-87 signatures) is a footgun; the different prefix catches it.

Common methods:

- `qrl_blockNumber`
- `qrl_chainId`
- `qrl_getBalance`
- `qrl_getBlockByNumber`
- `qrl_getTransactionByHash`
- `qrl_getTransactionReceipt`
- `qrl_call`
- `qrl_estimateGas`
- `qrl_sendRawTransaction`
- `qrl_getLogs`
- `qrl_variant` ← returns `"standard"` / `"rescue-edition"` / `"moscow-chill"`

## Beacon REST

- `GET /qrl/v1/node/syncing` → head_slot, sync_distance, is_syncing, el_offline
- `GET /qrl/v1/node/identity` → ENR, p2p multiaddr
- `GET /qrl/v1/beacon/headers/head` → head block header + root
- `GET /qrl/v1/beacon/states/head/validators` → validator set + status
- `GET /qrl/v1/beacon/states/head/finality_checkpoints` → finality info

## Dashboard API (not stable across releases)

- `GET /api/dashboard/identity` → rank, project, theme, variant
- `GET /api/compose/status` → per-container health
- `GET /api/compose/projects` → every compose project on daemon (LEAD-only)
- `GET /api/compose/security` → aggregated hardening posture
- `POST /api/compose/reset` → destructive full reset
- `GET /api/system/disk` → disk-space badge
- `GET /api/system/qrlenv` → master-key presence
- `POST /api/wallet/create` → PQ keygen
- `POST /api/wallet/unlock` → unlock keystore
- `POST /api/release/{build,sign,verify,load,bundle}` → release pipeline
- `POST /api/tls/gen-cert` → generate PQ TLS cert

## Addresses

- **Q-prefix 64-byte** (current QRL 2.0): `Q` + 128 hex chars.
- **Z-prefix 20-byte** (legacy Zond): `Z` + 40 hex chars. Accepted on
  input, normalised to Q on output.
- Everything out of the chain is Q-64. See [Security](Security.html)
  for the migration story.
