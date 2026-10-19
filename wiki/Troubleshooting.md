# Troubleshooting

Full matrix at
[docs/operations#troubleshooting](https://theqrl.github.io/qrl-popup-nodes/operations.html#troubleshooting).

## Chain halted (head_slot not climbing)

Three common causes:

| Symptom (`docker logs validator`)              | Cause                            | Fix |
|------------------------------------------------|----------------------------------|-----|
| `genesis_validators_root ... does not match`   | validator DB from an earlier chain | `rm -f docker/data/validators/validator.db` + restart validator |
| `could not connect: dial tcp ... beacon-node1` | validator can't reach beacon     | restart beacon-node1 |
| `el_offline: true` in /qrl/v1/node/syncing     | beacon lost Engine API to gqrl   | restart gqrl-node1 + beacon-node1, check JWT secret |

## Beacon crash-loops

| Error                                                              | Fix |
|--------------------------------------------------------------------|-----|
| `Forkchoice requested unknown head`                                | genesis.ssz mismatches gqrl chaindata — run `./docker/configure.sh --regen-genesis` |
| `failed to unmarshal state, detected fork=zond: xx`                | beacon image predates current genesis — `docker compose build --no-cache beacon-node{1,2,3,4}` |
| `genesis_validators_root ... does not match validator db`          | not a beacon issue — see validator fix above |

## gqrl crash-loops

| Error                                                 | Fix |
|-------------------------------------------------------|-----|
| `datadir already used by another process`             | stale LOCK file — `docker rm -f` the container, `rm -f docker/data/node{N}/gqrl/gqrl/LOCK`, re-up |
| `Fatal: need genesis.json file as the only argument`  | missing `--entrypoint /opt/qrl/bin/gqrl` in init command |

## Dashboard

| Symptom                               | Fix |
|---------------------------------------|-----|
| crash-loop with `ENOTSUP` on bind     | VirtioFS Unix-socket limit — rebuild with commit `13a36e4d` or newer |
| OTHER STACKS panel stuck "loading…"   | daemon slow — rebuild with commit `18e82c03` or newer (30s timeout) |
| identity chip reports `LEAD` on L2    | missing `QRL_STACK_RANK` env on l2-rescue dashboard — set in overlay |
| HTTP 000 on :8080                     | container restarting — `docker logs dashboard` |

## Build

| Error                                                   | Fix |
|---------------------------------------------------------|-----|
| `TLS handshake timeout` fetching go-qrllib              | transient proxy.golang.org blip — retry; or use `--network=host` buildx flag |
| `target stage "dashboard" could not be found`           | don't pass `--target dashboard` to `docker buildx` directly; let compose drive the build |
| `No such image: l2-moscow-bootstrap-fetcher:latest`     | tag the shared bootstrap-fetcher: `docker tag l2-rescue-bootstrap-fetcher:latest l2-moscow-bootstrap-fetcher:latest` |

## Disk

| Symptom                                     | Fix |
|---------------------------------------------|-----|
| `configure.sh refuses to start, <10 GB free`| `docker system prune -af` or move to a bigger disk |
| 💾 badge amber on dashboard                 | plan to free disk soon; three stacks consume ~45 GB/day combined |
| 💾 badge red                                | act now; compose will crash on next chain write |

## Resource

| Symptom                            | Fix |
|------------------------------------|-----|
| Docker Desktop OOM (macOS)         | Preferences → Resources → Memory → 12 GB for three stacks |
| CPU pegged at 100% on validator    | normal during attestation; verify by `docker stats validator` across epoch boundaries |

## Reset

If everything is wedged beyond diagnosis:

```bash
./docker/full-reset.sh --snapshot-first   # per stack
```

Or the dashboard **COMPOSE RESET** button (same thing, UI-driven).
Two-step prompt in both cases.
