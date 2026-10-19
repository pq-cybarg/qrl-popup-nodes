---
title: Stacks
nav_order: 4
---

# Multi-stack deployment

Each popup stack is a self-contained compose project. The repo ships
three ready-to-run stack definitions out of the box, and adding a
fourth is a short overlay file + port offset away.

## Stack identity

Every stack is identified by **three** independent handles:

- **Compose project name** (`qrl-popup` / `l2-rescue` / `l2-moscow`).
  Threads through container names, network names, docker labels.
- **Dashboard rank** (`LEAD` / `L2` / `MC`). Set via `QRL_STACK_RANK`
  env on the dashboard container; drives the identity chip + theme
  accent.
- **Port offset** (`+0` / `+200` / `+400` for gqrl HTTP; `+0` / `+100`
  / `+400` for beacon REST; `+0` / `+100` / `+200` for dashboard).

The offsets are intentionally non-uniform — each service picks the
offset that doesn't collide with existing shoreline (3500-range for
Pyth, 4000-range for OpenZeppelin's local tooling, etc).

| Component          | qrl-popup       | l2-rescue       | l2-moscow       |
|--------------------|-----------------|-----------------|-----------------|
| gqrl HTTP RPC      | 9545–9548       | 9745–9748       | 9945–9948       |
| gqrl WS            | 9645–9648       | 9845–9848       | 10045–10048     |
| gqrl Engine API    | 9551–9554       | 9751–9754       | 9951–9954       |
| gqrl p2p           | 31303–31306     | 31503–31506     | 31703–31706     |
| beacon REST        | 3600            | 3700            | 4000            |
| beacon gRPC        | 4100            | 4200            | 4500            |
| beacon p2p         | 13000           | 13100           | 13300           |
| dashboard HTTP     | 8080            | 8180            | 8280            |
| rank chip          | LEAD            | L2              | MC              |
| theme              | cobalt (#3aa0ff)| rose (#ff5577)  | violet (#b070ff)|
| variant image      | `qrl-gqrl:standard` | `qrl-gqrl:rescue-edition` | `qrl-gqrl:moscow-chill` |

Port assignments live in `docker/.env`; override by exporting
`QRL_DASHBOARD_PORT=…` and friends before `docker compose up`.

## Isolation

Nothing is shared between stacks:

- **No shared network** — each project gets its own bridge.
- **No shared data** — each project's bind mounts root at a distinct
  path (`docker/data`, `docker/data-l2-rescue`, `docker/data-l2-moscow`).
- **No shared keystores** — each project's `validators/` is its own.
- **No shared genesis** — three distinct `genesis.ssz` files with
  three distinct generation times.
- **No shared chain** — the three chains are not peers; they fork at
  slot 0 and diverge forever.

The LEAD dashboard enumerates sibling stacks for navigation but can't
command them — each stack's own dashboard remains authoritative for
its containers. (The underlying guardrail is in
`status-server.js:verifyContainerInProject`: every container-ops
endpoint refuses to touch an id outside its own compose project.)

## Adding a fourth stack

Say you want a `qrl-eva` stack running yet another variant. The three
touchpoints are:

1. **A new compose overlay** (`docker/docker-compose.eva.yaml`) — model
   after `docker/docker-compose.moscow-chill.yaml`; swap:
   - `image: qrl-gqrl:eva` on every gqrl service.
   - `container_name: l2-eva-*` on every service.
   - Port offsets that don't collide with the three existing stacks.
   - `environment.QRL_STACK_RANK: EVA` and `COMPOSE_PROJECT_NAME: l2-eva`
     on the dashboard.
2. **A new data root** (`docker/data-l2-eva/`) — mkdir + seed config,
   same shape as `docker/data-l2-moscow/`.
3. **A theme entry** — add `l2-eva` to `pickTheme()` in
   `status-server.js` so the LEAD dashboard's OTHER STACKS panel
   can show its colour correctly.

Bring up:

```bash
QRL_DATA_DIR=./data-l2-eva qrlenv run -- docker compose -p l2-eva \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.eva.yaml \
    up -d
```

## Running three stacks on one laptop — resource budget

A single stack idles at:

- ~250 MB RAM per gqrl node × 4 = 1 GB
- ~400 MB RAM per beacon × 4 = 1.6 GB
- ~200 MB RAM validator + ~400 MB dashboard
- ~50 MB disk per node for a fresh chain, growing ~5 MB per epoch

Three stacks: ~10 GB RAM floor + ~15 GB disk per 24 h. Docker Desktop
default memory ceiling (8 GB on Mac) will swap under the load — bump
Preferences → Resources → Memory to 12 GB before running all three.

## Dashboard cross-stack navigation

Each dashboard's left-column **OTHER STACKS** panel enumerates every
compose project it can see on the daemon:

- `[THIS]` badge marks the dashboard you're looking at.
- Theme-coloured dot for identity at a glance.
- `N/M healthy` summary (M containers; N healthy).
- Clickable URL if a `dashboard` service maps a host port.

Clicking a URL opens that stack's own dashboard in a new tab. The
panel refreshes every 15 s. If the daemon is slow (30+ containers,
VirtioFS) the request can take several seconds — the underlying
`/api/compose/projects` endpoint carries a 30 s dockerd timeout
precisely for this reason.
