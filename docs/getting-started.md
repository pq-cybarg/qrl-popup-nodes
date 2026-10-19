---
title: Getting started
nav_order: 3
---

# Getting started

## Prerequisites

- **Docker Desktop** on macOS/Windows, or **Docker Engine** / **Podman** 4+
  on Linux. The stack runs under either runtime.
- **GNU Make**, **Bash**, **Python 3.9+** (for genesis templating).
- A **16 GB RAM / 40 GB free disk** laptop will do fine for one stack;
  double that if you run all three variants concurrently.
- Go 1.22+ is only needed if you're going to rebuild the gqrl /
  qrysm binaries from source. The release pipeline publishes
  pre-built, signed OCI tarballs you can `load-release.sh` instead.

## 60-second bringup — the standard stack

```bash
git clone https://github.com/theQRL/qrl-popup-nodes.git
cd qrl-popup-nodes

# One-time: scaffold .env + build the gqrl + beacon + validator + dashboard images,
# init per-node chaindata, lay down genesis. Non-interactive if you accept defaults.
./docker/configure.sh

# Bring the stack up.
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    up -d

# Open the dashboard.
open http://127.0.0.1:8080/
```

Within ~90 seconds the beacon reports `head_slot: 1` and climbs by one
every minute. Blocks become visible on the gqrl side a few slots later.

## Bringing up a second variant alongside it

The three variants coexist cleanly. To add rescue-edition:

```bash
# One-off: build the variant image if you haven't yet
docker/build-images.sh rescue-edition

QRL_DATA_DIR=./data-l2-rescue qrlenv run -- docker compose -p l2-rescue \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.rescue.yaml \
    up -d

open http://127.0.0.1:8180/     # rose theme, L2 rank chip
```

Add `l2-moscow`:

```bash
docker/build-images.sh moscow-chill

QRL_DATA_DIR=./data-l2-moscow qrlenv run -- docker compose -p l2-moscow \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.moscow-chill.yaml \
    up -d

open http://127.0.0.1:8280/     # violet theme, MC rank chip
```

The LEAD dashboard's **OTHER STACKS** panel will auto-populate with
clickable links to the two sibling stacks after a hard reload.

## Running from a signed release instead of a source build

If you don't want to compile Go, grab a signed release:

```bash
# Download a release tarball (one HTTP GET; sig lives alongside)
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar.sha256sums
curl -LO https://releases.example.org/qrl-popup-v1.0.0.tar.sha256sums.qrlsig

# Verify the detached ML-DSA-87 signature against a pinned public key
./docker/verify-images.sh \
    --sums qrl-popup-v1.0.0.tar.sha256sums \
    --sig qrl-popup-v1.0.0.tar.sha256sums.qrlsig \
    --pinned-pubkey keys/release.pub

# Load into the local docker daemon
./docker/load-release.sh qrl-popup-v1.0.0.tar

# Bring up as usual
qrlenv run -- docker compose -f docker/docker-compose.yaml up -d
```

The verify step **must** pass before load; `load-release.sh` refuses
a tarball whose SHA256 doesn't match the signed manifest.

## Hardening on top (Linux)

The default bringup already ships with `read_only: true`, `cap_drop: [ALL]`,
`no-new-privileges`, and the 262-syscall seccomp allowlist. On a Linux
host you can stack AppArmor or SELinux too:

```bash
# Load profiles once (sudo required)
sudo install -m 0644 docker/apparmor/*.aa /etc/apparmor.d/
for p in qrl-gqrl qrl-beacon qrl-validator qrl-dashboard; do
  sudo apparmor_parser -r -W /etc/apparmor.d/$p
done

# Attach via overlay
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.hardened.yaml \
    -f docker/docker-compose.seccomp.yaml \
    -f docker/docker-compose.apparmor.yaml \
    up -d
```

See [security](security.html#defense-in-depth) for the full stack
of overlays.

## Taking it down

```bash
# One stack
docker compose -p qrl-popup -f docker/docker-compose.yaml down

# All stacks
docker compose -p qrl-popup -f docker/docker-compose.yaml down
docker compose -p l2-rescue -f docker/docker-compose.yaml -f docker/docker-compose.rescue.yaml down
docker compose -p l2-moscow -f docker/docker-compose.yaml -f docker/docker-compose.moscow-chill.yaml down
```

Chain data lives in `docker/data{,-l2-rescue,-l2-moscow}` on the host.
`down` keeps it; `down -v` wipes volumes but **not** bind-mounted
chain state. To restart from an empty genesis, delete those dirs or
use the dashboard's **COMPOSE RESET** (which snapshots first).

## Common bringup gotchas

- **"Forkchoice requested unknown head"** — mismatched genesis.ssz vs
  gqrl chaindata. Fix: re-run `docker/configure.sh --regen-genesis`
  which rebuilds both from `genesis-template.json`.
- **"failed to unmarshal state, detected fork=zond: xx"** — stale
  beacon image predates the current qrysm. Rebuild `--no-cache`
  for every `beacon-node*` service.
- **Dashboard crash-loops with `ENOTSUP`** — macOS Docker Desktop /
  VirtioFS can't bind a Unix socket on a bind-mounted path. The
  dashboard already handles this by falling back to TCP-only; make
  sure you're on a build that includes commit `13a36e4d` or newer.
- **"datadir already used by another process"** on gqrl init — stale
  LOCK file from a prior crashed init. Fix: `docker rm -f` the
  container, then re-bring up via `compose up -d`.

For the whole troubleshooting matrix see
[operations](operations.html#troubleshooting).
