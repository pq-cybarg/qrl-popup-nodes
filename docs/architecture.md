---
title: Architecture
nav_order: 2
---

# Architecture

A QRL popup stack is a **single-operator chain** running on one host:
four `gqrl` execution nodes, four `qrysm` beacon nodes paired 1:1
with them, a single `validator` that holds the keystores for all four
validator identities, and a `dashboard` beside the stack for
operator visibility.

The design serves two production use cases:

- A **sovereign operator** running their own chain under their own
  trust boundary — treasury, QRC-20 contract development, QNS, PQ
  wallet infrastructure, consortium coordination.
- A **PoC / staging environment** for an application that will later
  move to a public QRL 2.0 chain. The popup stack reproduces the full
  PoS consensus + QRVM execution semantics without the operational
  weight of a multi-host testnet.

## Two-layer consensus split

QRL 2.0 is split into a **consensus layer** (beacon, proof-of-stake,
finality, fork choice) and an **execution layer** (txs, state,
mempool, QRVM). They communicate over the **Engine API** on `authrpc`
port 9551, authenticated by a 32-byte JWT shared via bind-mount at
`/opt/qrl/config/jwtsecret-nodeN`.

```
┌───────────────────┐                   ┌─────────────────────┐
│   validator (one) │                   │  beacon-node[1-4]   │
│   — ML-DSA-87     │ ──gRPC :4100──── ►│  — qrysm (Go)       │
│   — slashing DB   │                   │  — SSZ state        │
└───────────────────┘                   │  — fork choice      │
                                        │  — p2p :13000/14000 │
                                        └──┬──────────────────┘
                                           │ Engine API
                                           │ (JWT auth, :9551)
                                           ▼
                                        ┌─────────────────────┐
                                        │  gqrl-node[1-4]     │
                                        │  — QRVM             │
                                        │  — p2p :31303…      │
                                        │  — JSON-RPC :9545   │
                                        └─────────────────────┘
```

The dashboard sits beside this stack and probes both layers over
their public RPCs, plus the local docker socket for compose health.

## Topology

- **One validator process** holds the four validator keystores,
  wrapped by `qrlenv` (Argon2id → AES-256-GCM) and gated by an
  operator password. A single-operator chain has a single trust
  root; co-locating the four validator shares in one hardened
  process is the direct consequence of that trust boundary. The
  slashing-protection database (`validator.db`) covers all four
  identities so a validator restart does not double-sign.
- **Four beacon nodes** form the consensus quorum. One beacon can
  restart without halting finality — the other three continue to
  finalize epochs. The four peer over a bridge network only
  reachable from inside the compose project. Note: this quorum
  property covers the *consensus layer*. The single validator
  process below is still a signing bottleneck — if the validator
  restarts, no new blocks are proposed by this operator during the
  restart window (finality continues to advance against already-
  gossiped blocks). Chain-tip motion depends on both layers being
  up.
- **Four execution nodes**, paired 1:1 with the beacons via a
  per-node JWT secret. Peering over an isolated bridge; no
  cross-project connectivity by design.
- **Bootstrap-fetcher**, a short-lived container that warms every
  node's QNR (QRL node record) at compose-up so a cold start does
  not wait on p2p discovery.

Chain parameters: `SLOTS_PER_EPOCH=128`, `SECONDS_PER_SLOT=60`.
One epoch = ~2h8m. Finality lands two epochs behind the head.

## Variants

```
qrl-2.0/
├── upstream/go-qrl/       # single source of truth (pinned commit)
└── variants/
    ├── moscow-chill/
    │   └── core/vm/contracts.go   # QIP #46 precompile set
    └── rescue-edition/
        └── core/vm/contracts.go   # moscow-chill + zRonin precompiles
```

Each variant builds against the **same** `upstream/go-qrl` tree —
only `core/vm/contracts.go` (and whatever helpers it drags in) is
replaced at build time. The result is an image tag:

- `qrl-gqrl:standard`         — upstream baseline
- `qrl-gqrl:moscow-chill`     — [QIP #46](https://github.com/theQRL/qips/pull/46) PQ precompiles
- `qrl-gqrl:rescue-edition`   — QIP #46 + zRonin Rescue-Prime / STARK verify

The compose overlay files (`docker-compose.rescue.yaml`,
`docker-compose.moscow-chill.yaml`) swap the `image:` for each
`gqrl-node*` service. Beacon, validator, and dashboard are
**variant-agnostic** — the precompile set lives entirely in the
execution image.

## Three stacks, three chains

A single operator can run all three variants concurrently via
Docker Compose's project-isolation mechanism (`-p <project>`):

| Compose project | `-f` overlays                                | ports (gqrl / beacon / dash) |
|-----------------|----------------------------------------------|------------------------------|
| `qrl-popup`     | base + docker-sock                           | 9545-8 / 3600 / 8080         |
| `l2-rescue`     | base + docker-sock + rescue                  | 9745-8 / 3700 / 8180         |
| `l2-moscow`     | base + docker-sock + moscow-chill            | 9945-8 / 4000 / 8280         |

Each project gets its own bridge network, container-name prefix, and
data root. **No cross-project traffic by design** — these are three
independent chains, not peers.

The compose `!override` tag (Compose 2.24+) is used in the overlays
to **replace** the port list rather than concatenate it, so a variant
can shift its published ports without the base ports hanging around
and colliding.

## go-qrl unification

Both the execution layer (gqrl) and the consensus layer (qrysm)
depend on `github.com/theQRL/go-qrl` — the shared type/SSZ library.
If they build from **different revisions**, their genesis block root
hashes disagree, and the beacon rejects every block as
`Forkchoice requested unknown head`.

To prevent this: `qrysm`'s `go.mod` carries a `replace` directive
pointing at the same local tree gqrl builds from:

```go
replace github.com/theQRL/go-qrl => ../go-qrl
```

A clean build (`go mod tidy` + `--no-cache` docker builds for every
beacon service) is required after bumping go-qrl. The dashboard's
VARIANT & SNAPSHOTS panel displays the go-qrl commit hash each
container has baked in so operators spot a mismatch early.

## Dashboard — the operator surface

`status-server.js` is a plain Node.js HTTP server. No framework —
just `http.createServer` + a hand-written router — so the audit
surface stays small. Per stack, it binds a distinct host port
(8080 / 8180 / 8280) and reads a distinct identity from
`QRL_STACK_RANK` (shown in the identity chip).

Panels (left column of the UI):

- **COMPOSE STACK** — per-container row with health chip, CPU/mem
  gauges, logs modal, lifecycle buttons (restart / pause / unpause).
  Hardening row shows read-only / no-new-privs / cap-drop:ALL /
  seccomp / AppArmor-or-SELinux counts.
- **QRLENV** — master-key presence + plaintext-vs-encrypted count on
  `docker/.env`.
- **OTHER STACKS** — enumerates every compose project on the daemon,
  cross-links to its own dashboard, colour-coded by theme accent.
- **VARIANT & SNAPSHOTS** — gqrl variant + pinned go-qrl commit +
  snapshot list (compose-aware; see `docker/chain-snapshot.sh`).

The dashboard runs with **no persistent state of its own** beyond
per-viewer browser `localStorage` for conveniences. Every answer
is a live probe of the compose stack and the chain RPCs.
