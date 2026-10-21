# Architecture

Full write-up at
[docs/architecture](https://pq-cybarg.github.io/qrl-popup-nodes/architecture.html).
Three key points:

## 1. Single-operator chain

A QRL popup stack is a **single-operator chain** — one trust root,
one host, one validator process holding all four validator keystores
wrapped by `qrlenv` (Argon2id → AES-256-GCM). Used for sovereign
operators (treasury / QRC-20 dev / QNS / consortium coordination)
and for staging environments ahead of public-chain deployment.

## 2. Two-layer consensus split

**Consensus layer** (`qrysm` beacon, proof-of-stake, finality,
fork choice) + **execution layer** (`gqrl`, state, mempool, QRVM).
Engine API on port 9551 (authrpc, JWT-authenticated) is the only
interface between them. SLOTS_PER_EPOCH=128, SECONDS_PER_SLOT=60.

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
