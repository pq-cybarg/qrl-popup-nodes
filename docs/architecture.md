---
title: Architecture
nav_order: 2
---

# Architecture

A QRL popup stack is a one-host, four-container-per-tier topology:
four `gqrl` execution nodes, four `qrysm` beacon nodes paired 1:1
with them, a single `validator` holding all four keystores for an
interop-style self-contained chain, and a `dashboard` on the side
for operator visibility.

## Two-layer consensus split

Like Ethereum post-merge, QRL 2.0 is split into a **consensus layer**
(beacon, proof-of-stake, finality, fork choice) and an **execution
layer** (txs, state, mempool, EVM-equivalent). They communicate over
the **Engine API** on `authrpc` port 9551, authenticated by a
32-byte JWT shared via bind-mount at `/opt/qrl/config/jwtsecret-nodeN`.

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
                                        │  — geth fork        │
                                        │  — QRVM             │
                                        │  — p2p :31303…      │
                                        │  — JSON-RPC :9545   │
                                        └─────────────────────┘
```

The dashboard sits beside this stack and probes both layers over
their public RPCs, plus the local docker socket for compose health.

## Why four nodes

- **One** validator holds all four validator keystores (an interop
  pattern — real deployments split 1 key per host).
- **Four** `beacon-node[1-4]`: lets the chain survive a single beacon
  rotation (one restart or crash doesn't halt finality) while keeping
  the stack small enough to run on a laptop.
- **Four** `gqrl-node[1-4]` paired 1:1 with the beacons via a
  per-node JWT secret. All four peer on an isolated bridge network.
- A **bootstrap-fetcher** short-lived container warms up every
  node's QNR (QRL node record) so a cold start doesn't wait on
  discovery.

`SLOTS_PER_EPOCH=128`, `SECONDS_PER_SLOT=60` → one epoch is ~2h8m.
Finality lands two epochs behind the head.

## Variant mechanics

```
qrl-2.0/
├── upstream/go-qrl/       # single source of truth
└── variants/
    ├── rescue-edition/
    │   └── core/vm/contracts.go   # overrides PrecompiledContractsZond
    └── moscow-chill/
        └── core/vm/contracts.go   # different override
```

Each variant builds against the **same** `upstream/go-qrl` tree —
only `core/vm/contracts.go` (and whatever helpers it drags in) is
replaced at build time. The result is an image tag:

- `qrl-gqrl:standard`
- `qrl-gqrl:rescue-edition`
- `qrl-gqrl:moscow-chill`

The compose overlay files (`docker-compose.rescue.yaml`,
`docker-compose.moscow-chill.yaml`) swap the `image:` for each
`gqrl-node*` service. The beacon / validator / dashboard are
**unchanged across variants** — they're variant-agnostic.

## Isolated compose projects

Three stacks run concurrently via Docker Compose's project-isolation
mechanism (`-p <project>`):

| Compose project | `-f` overlays                                | ports (gqrl / beacon / dash) |
|-----------------|----------------------------------------------|------------------------------|
| `qrl-popup`     | base + docker-sock                           | 9545-8 / 3600 / 8080         |
| `l2-rescue`     | base + docker-sock + rescue                  | 9745-8 / 3700 / 8180         |
| `l2-moscow`     | base + docker-sock + moscow-chill            | 9945-8 / 4000 / 8280         |

Each project gets its own bridge network (`qrl-popup_qrl-popup`,
`l2-rescue_qrl-popup`, `l2-moscow_qrl-popup`), its own container-name
prefix (`qrl-popup-gqrl-node1`, `l2-rescue-gqrl-node1`, `l2-moscow-gqrl-node1`),
and its own data root (`docker/data/`, `docker/data-l2-rescue/`,
`docker/data-l2-moscow/`). No cross-project traffic by design — these
are **independent chains**, not peers.

The compose `!override` tag (Compose 2.24+) is used in the overlays
to **replace** the port list rather than concatenate it, so a variant
can shift ports without the standard ports hanging around to collide.

## go-qrl unification

A subtle gotcha: both the execution layer (gqrl) and the consensus layer
(qrysm) depend on `github.com/theQRL/go-qrl` — the shared type/SSZ
library. If they build from **different revisions**, their genesis
block root hashes disagree, and the beacon rejects every block as
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

`status-server.js` is a plain Node.js HTTP server bound to port 8080
(default), 8180 (l2-rescue), 8280 (l2-moscow). No framework — just
`http.createServer` + a hand-written router — so the audit surface
stays small.

Panels (left column of the UI):

- **COMPOSE STACK** — per-container row with health chip, CPU/mem
  gauges, logs modal, lifecycle buttons (restart / pause / unpause).
  Hardening row shows read-only / no-new-privs / cap-drop:ALL /
  seccomp / AppArmor-or-SELinux counts.
- **QRLENV** — master-key presence + plaintext-vs-encrypted count on
  `docker/.env`.
- **OTHER STACKS** — enumerates every compose project on the daemon.
  LEAD stack links to L2 / MC. ThemeAccent shown as a dot.
- **VARIANT & SNAPSHOTS** — gqrl variant + go-qrl commit + snapshot
  list (compose-aware; see `docker/chain-snapshot.sh`).

The dashboard runs with no persistent state of its own beyond
browser `localStorage` for per-viewer conveniences. Every answer it
gives is a live probe of the compose stack and the chain RPCs.
