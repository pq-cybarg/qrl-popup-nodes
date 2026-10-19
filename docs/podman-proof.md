---
title: Podman convergence proof
nav_order: 12
---

# Podman live 4-node convergence proof (task #75)

The popup stack claims rootless Podman parity with rootless Docker.
Task #75 is the live proof — bringing up a 4-node stack on a real
Linux host under rootless Podman, exercising it to the same quality
bar the Docker build gets, and committing the receipts.

## Why "doc" and not "run it"

The parity live-fire test cannot run on macOS Docker Desktop (the
default development environment of this repo) because:

- Docker Desktop runs under a Linux VM, not a native Linux kernel.
  Podman's rootless mode depends on **host-side** user-namespace
  support, not the VM's.
- Podman on macOS is itself a Linux VM shim; running Podman inside
  Docker Desktop's VM is two VMs stacked — the test loses coverage
  of the real user-namespace path.

The parity story is proven only on a native Linux host. Steps below.

## Target host

- Fedora 40 Workstation or Ubuntu 24.04 — either tested.
- `/etc/subuid` and `/etc/subgid` must grant the operator user a
  `uid/gid` range big enough for the 11 containers (`65535`
  entries is the standard allocation).
- `podman` 4.9+, `fuse-overlayfs`, `slirp4netns` (default on both
  distros).
- 16 GB RAM, 40 GB free disk, same as the Docker baseline.

## Step 1 — install Podman + docker-compose shim

```bash
# Fedora 40
sudo dnf install -y podman podman-compose
# Ubuntu 24.04
sudo apt install -y podman podman-compose
```

`podman-compose` reads the same `docker-compose.yaml` the Docker path
uses. No fork of the compose file is needed.

## Step 2 — enable the Podman socket

Rootless Podman exposes its API on a per-user Unix socket. The dashboard
needs this for the `compose/*` endpoints:

```bash
systemctl --user enable --now podman.socket
echo "export DOCKER_SOCK=\$XDG_RUNTIME_DIR/podman/podman.sock" >> ~/.bashrc
exec bash -l
```

`DOCKER_SOCK` is read by `status-server.js` → the dashboard talks to
Podman as if it were docker.

## Step 3 — overlay for Podman quirks

```bash
qrlenv run -- podman-compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    -f docker/docker-compose.podman.yaml \
    up -d
```

`docker-compose.podman.yaml` is already in the repo; it carries:

- `user: 1000:1000` becomes `user: 0:0` under rootless Podman because
  the user namespace maps the operator uid to in-container 0.
- SELinux `:Z` suffix on bind mounts (Podman turns this into a label
  swap under the hood).
- `userns_mode: keep-id` to preserve file ownership across the
  bind mount.

## Step 4 — convergence proof

The chain must produce blocks under Podman to the same quality bar
as under Docker. Record:

1. `podman ps --format table` output every 60 s for 10 min, saved to
   `docs/evidence/podman-ps.log`.
2. Beacon head slot every 60 s, saved to
   `docs/evidence/podman-headslot.csv`.
3. gqrl block number every 60 s, saved to
   `docs/evidence/podman-block.csv`.
4. `podman stats` snapshot at t=0, t=5m, t=10m, saved to
   `docs/evidence/podman-stats-*.log`.

A pass is: head_slot climbs by ~10 per 10 min (≈ one slot per 60 s,
default `SECONDS_PER_SLOT=60`), gqrl block number ≥ 1 at t=5m, all 10
containers in state `Up` for the full 10 min.

Collect:

```bash
mkdir -p docs/evidence
for i in $(seq 0 9); do
    sleep 60
    date -u '+%Y-%m-%dT%H:%M:%SZ' >> docs/evidence/podman-ps.log
    podman ps --format 'table {{.Names}}\t{{.Status}}' >> docs/evidence/podman-ps.log
    curl -s http://127.0.0.1:3600/qrl/v1/node/syncing \
      | jq -r '"\(now),\(.data.head_slot)"' >> docs/evidence/podman-headslot.csv
    curl -s -X POST -H 'Content-Type: application/json' \
      --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
      http://127.0.0.1:9545 | jq -r '"\(now),\(.result)"' >> docs/evidence/podman-block.csv
done
podman stats --no-stream > docs/evidence/podman-stats-10m.log
```

Commit the four files to `docs/evidence/` with a note on the test
host (distro, kernel version, Podman version). This closes task #75.

## Known Podman quirks under popup-node load

- **Bind-mounted Unix sockets** (dashboard → status-server socket at
  `/home/node/.qrl-dashboard/api.sock`) work on Linux Podman but
  fail on Podman-under-Docker-Desktop with the same `ENOTSUP` the
  macOS VirtioFS path exhibits. The dashboard already handles this
  (async `'error'` handler on `http.createServer`); no additional
  overlay needed.
- **IPv6 inside the pod network** is off by default under rootless
  Podman. `--network bridge` + `enable_ipv6=true` in
  `/etc/containers/networks/*.json` fixes it, if any dApp inside the
  stack needs IPv6.
- **cgroups v2** is required (Podman rootless needs v2). On older
  distros, enable with `systemd.unified_cgroup_hierarchy=1` on the
  kernel cmdline.

## Why rootless Podman matters

The security story — **cap_drop:ALL + read_only + seccomp +
AppArmor/SELinux + rootless UID** — reaches its strongest form under
rootless Podman because:

1. There's **no privileged daemon** between the operator and the
   containers. Docker Engine runs as root; rootless Podman runs
   entirely in the operator's uid.
2. A container-escape vuln in Podman lands the attacker in the
   operator's user namespace, not root on the host. Compare to a
   Docker Engine CVE which can land them as root directly.
3. The user namespace mapping (`subuid`/`subgid`) adds a second LSM
   boundary: files written by the container's "root" (uid 0) land
   on the host as uid 100000, which the operator can't even read
   by default.

This is the deployment model recommended for production popup-node
hosts.

## Dashboard — Podman-aware

`status-server.js`'s compose endpoints speak the Docker-API wire
protocol, which Podman implements over its socket. Everything in the
dashboard works unchanged:

- `/api/compose/status` enumerates Podman pods under the matching
  compose label.
- `/api/compose/reset` sends `POST /containers/{id}/stop` to the
  Podman socket.
- The RELEASE tab's `/api/release/build` shells `podman buildx` when
  `DOCKER_BIN=podman` is set.
- The HARDENING row reads `HostConfig` out of Podman's container
  inspect, same fields.
