# Podman convergence proof — #75

Full recipe at
[docs/podman-proof](https://theqrl.github.io/qrl-popup-nodes/podman-proof.html).

The parity live-fire test cannot run on macOS Docker Desktop (Podman
inside the Linux-VM shim loses coverage of the real user-namespace
path). Run on a Fedora 40 or Ubuntu 24.04 host.

## Four steps

1. Install Podman + `podman-compose`.
2. Enable the per-user Podman socket:
   ```bash
   systemctl --user enable --now podman.socket
   export DOCKER_SOCK=$XDG_RUNTIME_DIR/podman/podman.sock
   ```
3. Bring up with the Podman overlay:
   ```bash
   qrlenv run -- podman-compose \
       -f docker/docker-compose.yaml \
       -f docker/docker-compose.docker-sock.yaml \
       -f docker/docker-compose.podman.yaml \
       up -d
   ```
4. Record 10-minute convergence proof — ps snapshots, head_slot CSV,
   block number CSV, stats log — into `docs/evidence/podman-*`.

## Why Podman matters

Rootless Podman gives the strongest form of the hardening story:

- No privileged daemon (compare Docker Engine as root).
- Container escape vulns in Podman land in the operator's user
  namespace, not root on the host.
- user-namespace mapping (subuid/subgid) adds a second LSM boundary.

## Dashboard — unchanged

`status-server.js`'s compose endpoints speak the Docker API wire
protocol, which Podman implements. Everything in the dashboard works
without a fork — set `DOCKER_SOCK` to the Podman socket path and go.
