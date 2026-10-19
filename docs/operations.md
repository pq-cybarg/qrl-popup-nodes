---
title: Operations
nav_order: 8
---

# Operations

Day-two: snapshots, releases, resets, troubleshooting.

## Chain snapshots

`docker/chain-snapshot.sh` captures the full state of a running compose
project — every gqrl chaindata, every beaconchain.db, every keystore,
every config file — into a timestamped tarball in `docker/snapshots/`.

```bash
# default: snapshot the qrl-popup stack
./docker/chain-snapshot.sh

# a specific stack
QRL_SNAPSHOT_PROJECT=l2-rescue ./docker/chain-snapshot.sh

# a specific output dir
./docker/chain-snapshot.sh --out ~/snapshots/
```

The script is **compose-aware**: it reads the project's labels off
the daemon and only includes containers in the named project. Running
it from the LEAD stack doesn't snapshot L2 / MC.

Restore: `./docker/chain-snapshot.sh --restore <tarball>` stops the
stack, extracts over the data dirs, re-brings up. Idempotent.

## Release pipeline

Five host-side scripts drive a signed, byte-reproducible release:

| Script                     | Purpose                                        |
|----------------------------|------------------------------------------------|
| `docker/build-images.sh`   | docker buildx of every variant into `docker/out/*.oci.tar` |
| `docker/sign-images.sh`    | ML-DSA-87 detached sign over `SHA256SUMS.txt` |
| `docker/verify-images.sh`  | step-1 sig verify, step-2 per-tarball hash   |
| `docker/release.sh`        | orchestrator — invokes build + sign + bundle |
| `docker/load-release.sh`   | consumer-side — verify + `docker load`       |

Dashboard surface for the above: **RELEASE** tab in the main tab bar.
Build / Sign / Verify / Load — each has a streaming log pane; the
artifacts table shows SBOM (SPDX 2.3) + provenance (SLSA v0.2) badges
per tarball.

## Full reset

The nuclear option — stops every container in the project, wipes the
bind-mounted chain state, re-brings up. Dashboard surface:
**COMPOSE RESET** button in the Node Control tab (two-step prompt).

```bash
# CLI equivalent
./docker/full-reset.sh --project qrl-popup --snapshot-first
```

**Always snapshot before reset.** `full-reset.sh --snapshot-first` is
the one-liner that does the right thing.

The dashboard's own container is intentionally skipped so the handler
doesn't self-terminate mid-flight. The dashboard re-connects on its own
once the stack is back.

## Keystore management

The dashboard's **WALLETS** tab exposes `/api/wallet/create` and
`/api/wallet/unlock`. Password prompts are routed through:

- Tier 1 — in-browser Argon2id (WASM) — the standard pathway.
- Tier 2 — shadow-DOM overlay for passwords that must never cross a
  framework boundary.
- Tier 3 — OS-native prompt (osascript on macOS, zenity on Linux)
  for bare-metal operation where the dashboard is on the host.

Keys land in `docker/data/dashboard-home/<walletId>.json` as
Argon2id-wrapped AES-256-GCM blobs. **The wallet-level password is
never sent to the server after unlock** — the server derives the KEK
and holds a session-scoped unlocked ref.

## Monitoring

The dashboard's own panels are the primary monitoring surface. For
scripted monitoring:

```bash
# chain alive?
curl -s http://127.0.0.1:3600/qrl/v1/node/syncing | jq .data.head_slot
# gqrl alive?
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
  http://127.0.0.1:9545 | jq -r .result
# all containers healthy?
curl -s http://127.0.0.1:8080/api/compose/status | jq .summary
```

A `head_slot` that doesn't climb for > 2 slots (120 s) means the
chain is halted. Common causes:

- Validator can't connect to beacon (`genesis_validators_root` mismatch
  — see Troubleshooting below).
- Beacon can't connect to gqrl (`el_offline: true` — JWT mismatch or
  gqrl crashed).
- Fork choice halted (minority view — restart all beacons).

## Troubleshooting

### "Forkchoice requested unknown head"

The beacon's genesis block root doesn't match gqrl's. Rare but
pernicious — usually hits after a `go-qrl` upstream bump broke the
hash alignment.

Fix:

1. Confirm qrysm's `go.mod` has `replace github.com/theQRL/go-qrl =>
   ../go-qrl`.
2. `cd qrl-2.0/upstream/qrysm && go mod tidy`.
3. Full `--no-cache` rebuild of every beacon service.
4. Re-run `docker/configure.sh --regen-genesis`.

### "failed to unmarshal state, detected fork=zond: xx"

Beacon image is older than the current genesis.ssz. Classic post-upgrade
symptom.

Fix: `docker compose -p <project> build --no-cache beacon-node1
beacon-node2 beacon-node3 beacon-node4 validator`.

### Validator loops "genesis_validators_root ... does not match"

Validator DB carries a root from an earlier chain. Reset the DB:

```bash
docker compose -p <project> stop validator
rm -f docker/data<-...>/validators/validator.db
docker compose -p <project> up -d validator
```

### gqrl: "datadir already used by another process"

Stale LOCK file inside `data/node<N>/gqrl/gqrl/LOCK`. Most commonly
from a crashed init container that didn't release the flock.

Fix:

```bash
docker rm -f <project>-gqrl-node<N>
rm -f docker/data<-...>/node<N>/gqrl/gqrl/LOCK
# optional: nested LOCKs
rm -f docker/data<-...>/node<N>/gqrl/gqrl/{nodes,chaindata}/LOCK
docker compose -p <project> up -d gqrl-node<N>
```

If the error persists, hard-recreate: `docker rm -f` then `up -d`.
Occasional VirtioFS lock-release delay on macOS Docker Desktop makes
this a reliable last-resort fix.

### Dashboard crash-loops with `ENOTSUP`

VirtioFS can't bind a Unix socket on a bind-mounted path.
`status-server.js` already handles this via an async `'error'` handler
on `http.createServer` — if your dashboard is older than commit
`13a36e4d`, rebuild it.

### Dashboard "OTHER STACKS: loading..." forever

The `/api/compose/projects` dockerd call is slow (30+ containers +
VirtioFS). Confirm you're on commit `18e82c03` or newer which bumps
the timeout to 30 s. Hard-reload the tab.

### Three stacks fighting over disk

The default compose data roots consume ~15 GB/day each. If disk is
tight:

```bash
# the dashboard surface
curl -s http://127.0.0.1:8080/api/system/disk | jq .
# force a prune
docker system prune -af
```

The disk-space preflight (`docker/configure.sh`) refuses to start if
less than 10 GB is free. The dashboard's **💾 disk badge** turns amber
below 20 GB, red below 10 GB.

### A seccomp block fires in production

See `docker/soak/README.md` for how to find which syscall was blocked.
The soak-test harness is specifically for this failure mode — it
reproduces epoch-boundary / prune / snapshot / release-load code paths
in a loop and fails fast on the first `EPERM`.

Fix: add the syscall to `docker/seccomp-profile.json`, rebuild
dashboard, re-run soak.

### Need to debug inside a running container

```bash
# shell into gqrl-node1
docker exec -it qrl-popup-gqrl-node1 /bin/sh
# or if you want root (for debugging — kills hardening):
docker exec -u 0 -it qrl-popup-gqrl-node1 /bin/sh
```

Note: `-u 0` requires the hardened overlay NOT to be applied
(`no-new-privileges` blocks it). On a hardened stack you must bring
up a one-off debug container without the overlay.
