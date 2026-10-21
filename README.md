# QRL Popup Nodes

Post-quantum QRL 2.0 blockchain stack — `gqrl` execution + `qrysm`
beacon + validator — as **rootless, hardened Docker/Podman
containers** with a batteries-included operator dashboard. Three
variants (standard, rescue-edition, moscow-chill) run side-by-side.

> **Third-party build.** This repo is maintained at
> [pq-cybarg/qrl-popup-nodes](https://github.com/pq-cybarg/qrl-popup-nodes)
> and is **not affiliated with or endorsed by The QRL Foundation**.
> It vendors public artefacts from [theQRL](https://github.com/theQRL)
> (go-qrl, qrysm, go-qrllib) at pinned commits for a local-only
> popup-node packaging. The precompile spec it targets is
> [QIP #46](https://github.com/theQRL/qips/pull/46) (draft; see
> [variants](docs/variants.md) for the source-code drift note).

[![PQ-safe](https://img.shields.io/badge/crypto-PQ_safe-3fcf3f)](docs/security.md)
[![rootless](https://img.shields.io/badge/runtime-rootless-3fcf3f)](docs/security.md#defense-in-depth)
[![read--only](https://img.shields.io/badge/rootfs-read_only-3fcf3f)](docs/security.md#defense-in-depth)
[![seccomp](https://img.shields.io/badge/seccomp-262_syscalls-3fcf3f)](docker/soak/README.md)
[![AppArmor](https://img.shields.io/badge/AppArmor-enforce-3fcf3f)](docker/apparmor/README.md)
[![SELinux](https://img.shields.io/badge/SELinux-qrl_popup_t-3fcf3f)](docker/selinux/README.md)
[![reproducible](https://img.shields.io/badge/build-two_builder_identical-3fcf3f)](docs/reproducibility.md)
[![ML--DSA--87](https://img.shields.io/badge/signatures-ML_DSA_87-3fcf3f)](docs/security.md#cryptographic-primitives--line-by-line)
[![ML--KEM--768](https://img.shields.io/badge/TLS_KEM-ML_KEM_768-3fcf3f)](docs/security.md#cryptographic-primitives--line-by-line)

## 60-second bringup

```bash
git clone https://github.com/theQRL/qrl-popup-nodes.git
cd qrl-popup-nodes
./docker/configure.sh
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    up -d
open http://127.0.0.1:8080/
```

Within ~90 seconds the chain is producing blocks and the dashboard is
live. See [docs/getting-started](docs/getting-started.md) for three
stacks at once, signed releases, and hardening overlays.

## What's inside

| Dir / file                               | Role                                          |
|------------------------------------------|-----------------------------------------------|
| `docker/`                                | compose files, Dockerfiles, release pipeline  |
| `docker/popup-node/`                     | per-service Dockerfile + entrypoint           |
| `docker/apparmor/`, `docker/selinux/`    | LSM profiles (task #77)                       |
| `docker/soak/`                           | seccomp soak-test harness (task #84)          |
| `docker/reproducibility-check.sh`        | two-builder byte-identical proof (task #80)   |
| `qrl-2.0/upstream/`                      | pinned source trees — gqrl, qrysm, go-qrllib  |
| `qrl-2.0/variants/`                      | per-variant precompile overlays               |
| `qrl-2.0/configs/localnet/`              | genesis template, chain config                |
| `status-server.js` + `dashboard-v2.html` | operator dashboard                            |
| `qrlenv/`                                | PQ-safe .env encryption tool                  |
| `docs/`                                  | GitHub Pages site                             |
| `wiki/`                                  | GitHub Wiki source                            |

## Three stacks, three variants

| Stack        | Variant         | Dashboard                | Rank |
|--------------|-----------------|---------------------------|------|
| `qrl-popup`  | standard        | <http://127.0.0.1:8080/>  | LEAD |
| `l2-rescue`  | rescue-edition  | <http://127.0.0.1:8180/>  | L2   |
| `l2-moscow`  | moscow-chill    | <http://127.0.0.1:8280/>  | MC   |

Each stack is a self-contained compose project — isolated bridge
network, distinct container names, distinct data root, distinct
genesis. Three independent chains, not peers. See
[docs/stacks](docs/stacks.md).

## Documentation

- **Pages site** — <https://theqrl.github.io/qrl-popup-nodes/>
- **Wiki** — <https://github.com/theQRL/qrl-popup-nodes/wiki>
- **In-repo** — [docs/](docs/)

Key reads:

- [Getting started](docs/getting-started.md)
- [Architecture](docs/architecture.md)
- [Variants](docs/variants.md)
- [Security](docs/security.md)
- [API reference](docs/api.md)
- [Operations](docs/operations.md)

## Zond Private legacy quickstart

The repo also carries the older Zond-Private four-node bare-metal
reset flow for operators who haven't moved to the docker-compose
packaging yet:

```bash
cd zond-private
./reset-chain.sh
```

See `zond-private/CLAUDE.md` for the full doc.

## Status

- **Build**: three gqrl variants build byte-reproducibly from a
  single `qrl-2.0/upstream/go-qrl` tree with per-variant precompile
  overlay.
- **Chain**: all three stacks produce blocks every ~60 s under
  default `SECONDS_PER_SLOT=60`, `SLOTS_PER_EPOCH=128`.
- **Security**: `cap_drop:[ALL]`, `read_only:true`, `no-new-privileges`,
  262-syscall seccomp allowlist, AppArmor / SELinux overlays,
  optional Kata / gVisor runtime, ML-KEM-768 TLS, ML-DSA-87 release
  signatures.
- **Reproducibility**: two-builder byte-identical proof script with
  CI integration recipe.
- **Attestation**: TPM2 PCR-extend recipe for measured boot.
- **Observability**: dashboard surfaces compose health, hardening,
  disk, qrlenv state, release pipeline, PQ TLS cert gen, full reset.

## Not a mainnet participant

**These are development nodes.** Chain IDs, genesis accounts, and
keystores are development-only and **never** go on a public QRL
network. The hardening posture is designed for a local workbench;
public internet exposure is explicitly out of scope. See
[docs/security — operational posture](docs/security.md#operational-posture)
for the full "don't deploy to mainnet" checklist.

## License

See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) if present; otherwise open an
issue or PR against <https://github.com/theQRL/qrl-popup-nodes>.
