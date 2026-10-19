# Operations

Full write-up at
[docs/operations](https://theqrl.github.io/qrl-popup-nodes/operations.html).

## Chain snapshots

Compose-aware helper:

```bash
./docker/chain-snapshot.sh                            # default: qrl-popup
QRL_SNAPSHOT_PROJECT=l2-rescue ./docker/chain-snapshot.sh
./docker/chain-snapshot.sh --restore <tarball>
```

Snapshots include gqrl chaindata, beaconchain.db, keystores, and
config for the chosen project.

## Full reset

Dashboard surface: **COMPOSE RESET** button, Node Control tab,
two-step prompt. CLI:

```bash
./docker/full-reset.sh --project qrl-popup --snapshot-first
```

Always snapshot before reset.

## Keystore management

Dashboard **WALLETS** tab. Password routes through Argon2id (WASM) /
shadow-DOM overlay / OS-native prompt (tier 1/2/3). Keystores land in
`docker/data/dashboard-home/<walletId>.json` as Argon2id-wrapped
AES-256-GCM.

## Release pipeline

Five host-side scripts drive a signed, byte-reproducible release:

- `docker/build-images.sh` — produce `.oci.tar` per variant.
- `docker/sign-images.sh` — ML-DSA-87 detached sign SHA256SUMS.
- `docker/verify-images.sh` — step-1 sig verify, step-2 per-tar hash.
- `docker/release.sh` — orchestrator.
- `docker/load-release.sh` — consumer-side verify + docker-load.

Dashboard surface: **RELEASE** tab in the main tab bar.

## Monitoring probes

```bash
# chain alive?
curl -s http://127.0.0.1:3600/qrl/v1/node/syncing | jq .data.head_slot

# gqrl alive?
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
  http://127.0.0.1:9545 | jq -r .result

# containers healthy?
curl -s http://127.0.0.1:8080/api/compose/status | jq .summary
```

A `head_slot` that doesn't climb for >2 slots (120s) = chain halted.
See [Troubleshooting](Troubleshooting.html) for the fault tree.

## Soak tests

Seccomp soak harness at `docker/soak/`:

```bash
./docker/soak/run-seccomp-soak.sh --duration 14400
```

Exercises long-running edge paths (chain-snapshot, release-load,
reset, token-index-rebuild, PQ-key-regen, epoch-boundary, ancient-
prune) in a loop and fails fast on the first seccomp block.
