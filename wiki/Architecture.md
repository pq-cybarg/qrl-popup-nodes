# Architecture

Full write-up at
[docs/architecture](https://theqrl.github.io/qrl-popup-nodes/architecture.html).
Three key points:

## 1. Two-layer consensus split

Like Ethereum post-merge: **consensus layer** (`qrysm` beacon,
proof-of-stake, finality, fork choice) + **execution layer**
(`gqrl`, state, mempool, QRVM). Engine API on port 9551
(authrpc, JWT-authenticated) is the only interface between them.

## 2. 1:1 node pairing

Each gqrl-node[1-4] pairs with beacon-node[1-4] by shared
`jwtsecret-nodeN`. All four nodes peer on an isolated bridge.
One validator holds all four validator keystores — interop-style
single-tenant chain. SLOTS_PER_EPOCH=128, SECONDS_PER_SLOT=60.

## 3. Variants only differ in `core/vm/contracts.go`

The three variant images (`qrl-gqrl:standard`,
`qrl-gqrl:rescue-edition`, `qrl-gqrl:moscow-chill`) all build from
the same `qrl-2.0/upstream/go-qrl` tree. Only the precompile table at
`core/vm/contracts.go` differs. See
[Variants](Variants.html) for the precompile tables.

## go-qrl unification gotcha

Both gqrl and qrysm depend on `github.com/theQRL/go-qrl`. If they
build from different revisions, their genesis block root hashes
disagree and the beacon rejects every block as "Forkchoice requested
unknown head". Fix: `qrysm`'s `go.mod` carries a
`replace github.com/theQRL/go-qrl => ../go-qrl` so both build against
the same local tree.

## Compose project isolation

Three stacks run concurrently via `docker compose -p <project>`.
Each gets its own bridge network, container prefix, data root,
genesis.ssz, and port range. **No cross-project traffic by design**
— these are three independent chains, not peers of a single chain.
