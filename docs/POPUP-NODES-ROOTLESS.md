# Popup Nodes — Rootless Container Packaging

**Status:** Scaffolding. First-pass Dockerfiles + compose landed; end-to-end bring-up + block-production proof still owed. Track under nodes task #68.

**Origin:** Discord conversation with Mist, 2026-08-31. Mist observed that many blockchain "run a node in Docker" flows ship with `USER root` inside the container and no CAP restrictions, which turns any process-escape into host root. Cybarg committed to shipping the QRL popup nodes rootless from the start rather than bolting security on later. Follow-up: user should also be able to configure everything (variant, ports, node count, chain-id, etc.) — no hard-coded defaults baked into the image.

## Threat model

**Target adversary: nation-state.** This is the bar cybarg set 2026-08-31. Rootless containers alone are nowhere near enough; they're phase 1 of a multi-layer defense. Every mitigation below is either shipped in the first pass, planned for a labeled phase, or explicitly out of scope with reasoning.

The specific attack classes we must defeat at maturity, and the primary mitigation each requires:

| Attack class | Primary mitigation | Phase |
| --- | --- | --- |
| RCE in gqrl / beacon-chain via crafted P2P message | Rootless container + seccomp default deny + AppArmor/SELinux profile | 1 |
| runc / containerd escape CVE | Podman rootless (no root daemon) + gVisor or Kata for HW-backed isolation | 2 |
| Supply-chain compromise of a base image or Go dep | Digest-pinned bases + reproducible builds + Sigstore/cosign signatures + full SBOM published per release | 2 |
| Malicious compiler artifact | Reproducible builds (byte-for-byte across two independent builders) + published diverse builds | 3 |
| Malicious kernel / firmware | TPM-attested boot + measured launch (DRTM: Intel TXT / AMD SKINIT) + expected PCR values published | 3 |
| Coredump exfiltration after a crash | RLIMIT_CORE=0 + `/proc/sys/kernel/core_pattern=\|/bin/false` + no writable /tmp | 1 |
| Side channels (Spectre/Meltdown class, TAA, MDS) | CPU mitigations enforced (`kernel.unprivileged_bpf_disabled=1`, MDS_MITIGATION=full) + validator on dedicated cores, isolated from other tenants | 2 |
| Compromised RNG | Prefer hardware RNG (`/dev/hwrng`) where available, mix with `/dev/urandom`, refuse to start if only `/dev/random` is present and blocks | 2 |
| Passive network observation | Pin certificates for any outbound HTTPS + refuse plaintext HTTP for anything but loopback | 1 |
| MITM on P2P handshake | Bootstrap peers pinned by libp2p peer ID (already), refuse untrusted peers | 1 |
| DNS poisoning of bootstrap enodes | Bootstrap by QNR/enode (IP + peer ID, no DNS) — QRL already does this | 1 |
| Persistence after intrusion | Read-only rootfs, tmpfs for `/tmp`, no writable app dirs outside `/opt/qrl/data` which the operator sees | 1 |
| Physical DMA / cold-boot | Full-disk encryption on host + IOMMU enforced + secure boot + wipe on tamper (host-level, not container-level) | Out |
| Local user escalation from same UID | Container is single-purpose; malicious `docker exec` from the operator equals operator compromising themselves | Out |

**Explicit non-goals (with reasoning, not blind waivers):**

* **Same-UID local escalation.** If an attacker already has the operator's uid, they can `docker exec` and read the keystore directory the container mounts. Defense: keystore encryption (Tier 1-3 wallet stack already in place, Argon2id + AES-256-GCM). The container adds nothing beyond that; document it, don't pretend otherwise.
* **Host kernel 0-day.** No container runtime protects against a kernel 0-day the attacker knows and we don't. Mitigation is defense-in-depth (gVisor moves the syscall surface out of the host kernel; Kata puts a VM boundary in the way). Both planned in phase 2.
* **Physical access.** Host-level concern (LUKS, secure boot, TPM). Popup nodes are a workload, not a hardening framework for the host. Document what a hardened host looks like; don't ship it inside the image.
* **Air-gapped operation.** Blockchain nodes need network by definition. If the operator wants air-gap, they run signing/keystore ops on a separate machine — covered by the Tier 3 CLI stack that already talks over a Unix socket and OS keychain.

## Phased maturity — no phase promises what a later phase delivers

**Phase 1 — Rootless baseline (first-pass, in this PR):**
- USER 1000, no CAPs, no --privileged, all ports >1024
- Distroless/Alpine minimal base, no shell in runtime image
- Read-only rootfs (`read_only: true` in compose), writable only `/opt/qrl/data` (mounted)
- RLIMIT_CORE=0, `security_opt: [no-new-privileges]`
- Seccomp default (`docker`/`podman` default policy) applied by the runtime
- Ports bound to loopback only unless the operator explicitly opts a port out to `0.0.0.0`
- All outbound HTTPS via pinned CA bundle; refuse plaintext HTTP outbound
- **`.env`-configurable** per the config surface table below

**Phase 2 — Hardened isolation:**
- Bundled AppArmor and SELinux profiles (audited allowlist, not the runtime default)
- Custom seccomp profile deriving from the default with unused syscalls (ptrace, kexec_load, etc.) explicitly blocked
- Alternative runtime path: gVisor (`runsc`) and Kata (`kata-runtime`) documented + tested; compose file supports `runtime:` selection via `.env`
- All images pinned by digest, not tag
- Sigstore/cosign signatures on every image + verification instructions in the README
- Full SBOM (SPDX) published per release
- CPU-mitigation preflight check in `configure.sh` (refuse to bring up on a host with known-vulnerable mitigations disabled)

**Phase 3 — Attestation + reproducibility:**
- Reproducible builds: two independent builders (repo CI + a mirrored fork) produce byte-identical images; mismatch fails release
- TPM-attested boot recipe: expected PCR values documented, tooling to check on bring-up
- Optional: measured launch via Intel TXT / AMD SKINIT for the container host itself (host-level, but documented as a supported topology)
- Post-quantum TLS on control-plane surfaces where the underlying library supports it (tracking OpenSSL 3.5+ ML-KEM ship date; see docs/TIER3D-FIPS-SCOPING.md)

Every phase must be independently verifiable. If phase 1 ships but the operator can't verify it, phase 1 isn't done.

## Config surface (user-owned knobs)

**Secrets are encrypted in the `.env` — but not with dotenvx.**

Plaintext `.env` files are one of the most common secret-leak vectors (grepped from repos, checked into CI logs, shoulder-surfed). The obvious answer is dotenvx (`https://dotenvx.com`), which encrypts per-value using public-key crypto. **We do not use dotenvx as-is.** Its default primitive is X25519 + XChaCha20-Poly1305: X25519 is a classical elliptic-curve KEM, broken by a sufficiently large quantum computer. That fails the "no crypto in the functionality can circumvent PQC" rule this project holds itself to.

Instead we ship a QRL-native `qrlenv` tool with the same UX (`qrlenv set NAME value`, `qrlenv run -- <cmd>`) built on PQ-safe primitives already in the wallet stack:

- **Symmetric cipher:** AES-256-GCM. 256-bit key gives effective 128-bit security under Grover's algorithm — CNSA 2.0 approved. Same primitive `lib/keystore.js` already uses for wallet keystores.
- **KDF:** Argon2id (64 MiB × 3 passes, matching wallet defaults). Memory-hard, PQ-neutral.
- **Master key storage:** OS keychain (macOS Keychain / Linux Secret Service / Windows DPAPI) — the same mechanism the Tier 3 wallet stack uses for KEK storage.
- **Per-value nonce:** 12 random bytes per `qrlenv set` invocation, stored inline with the ciphertext.
- **File format:** `.env` still, but secret lines look like `QRL_GENESIS_PASSWORD=qrl-env:<12B nonce hex>:<GCM ct+tag hex>`. Plaintext values look normal.

If a future flow needs "encrypt to another party's public key" (asymmetric encryption for `.env`), use ML-KEM (Kyber, FIPS 203) — never X25519, never ECDH. The primitive is already in `go-qrllib` alongside ML-DSA-87.

`qrlenv` lives at `bin/qrlenv` (or is added to `qrl-wallet-cli` as a subcommand — same crypto library). Compose invocation is `qrlenv run -- docker compose up`; entrypoints see decrypted values only inside the container's env, never on disk in plaintext.

Rules:
- Public / operational knobs (variant, ports, node count, chain-id) → plaintext in `.env` — no security concern, and grep-visible values help debugging.
- Secret knobs (`QRL_GENESIS_PASSWORD`, per-node JWT secrets, any future signing key) → `qrlenv set NAME value` — writes a `qrl-env:...` blob into `.env`.
- Any variable that carries authenticator material, key seeds, or password-equivalents defaults to encrypted. Opt-out (with justification), not opt-in.

Every knob below MUST be settable this way. MUST NOT have implicit defaults burned into the Dockerfile. Same variables also govern the bare-metal `configs/localnet/.env` today; the container path is a superset, not a fork.

| Variable | Purpose | Storage | Default |
| --- | --- | --- | --- |
| `QRL_VARIANT` | Which gqrl variant image tag to pull (`standard`, `rescue-edition`, `moscow-chill`) | plaintext | `standard` |
| `QRL_NUM_NODES` | Number of execution+beacon pairs to spin up (1..N) | plaintext | `4` |
| `QRL_NETWORK_ID` | Network ID (also chainID under localnet convention) | plaintext | `32333` |
| `QRL_CHAIN_ID` | Chain ID (usually same as network ID) | plaintext | `32333` |
| `QRL_DATA_DIR` | Host-side path to bind for all chain data | plaintext | `./data` |
| `QRL_GENESIS_PASSWORD` | Password for the genesis account | **dotenvx-encrypted** | `testnet123` (dev-only) |
| `QRL_GENESIS_BALANCE` | Genesis account balance in Quanta | plaintext | `100000000` |
| `QRL_EL_HTTP_PORT_BASE` | Base port for execution HTTP; node N uses base+N-1 | plaintext | `9545` |
| `QRL_EL_WS_PORT_BASE` | Base port for execution WebSocket | plaintext | `9645` |
| `QRL_EL_AUTH_PORT_BASE` | Base port for execution authRPC (engine API) | plaintext | `9551` |
| `QRL_EL_P2P_PORT_BASE` | Base port for execution P2P | plaintext | `31303` |
| `QRL_CL_GRPC_PORT_BASE` | Base port for beacon gRPC | plaintext | `4100` |
| `QRL_CL_REST_PORT_BASE` | Base port for beacon REST gateway | plaintext | `3600` |
| `QRL_CL_UDP_PORT_BASE` | Base port for beacon discv5 UDP | plaintext | `13000` |
| `QRL_CL_TCP_PORT_BASE` | Base port for beacon libp2p TCP | plaintext | `14000` |
| `QRL_DASHBOARD_PORT` | Status-server HTTP port | plaintext | `8080` |
| `QRL_JWT_SECRET_NODE1..N` | Engine-API JWT shared between EL and CL per node | **dotenvx-encrypted** | auto-generated by `configure.sh` |
| `QRL_DISABLE_STAKING_CONTRACT_CHECK` | Devnet flag; skips deposit contract validation | plaintext | `true` |
| `QRL_LOG_LEVEL` | `info` / `debug` for verbose troubleshooting | plaintext | `info` |
| `QRL_RUNTIME` | Container runtime: `default` / `runsc` (gVisor) / `kata-runtime` (Kata) | plaintext | `default` |

Any variable added later that carries authenticator material, key seeds, or password-equivalents defaults to **dotenvx-encrypted** storage. The presumption is opt-out (with explicit justification) rather than opt-in.

All ports above are user-space (>1024). No container needs `CAP_NET_BIND_SERVICE`.

For per-variant runtime selection, the compose file uses `image: ghcr.io/theqrl/gqrl-${QRL_VARIANT}:${QRL_VERSION}`. Users switching variants only touch `.env`.

## Rootless rules (Dockerfile checklist)

Every image in `docker/popup-node/` must satisfy:

- [x] Ends with `USER 1000:1000` (or `USER qrl` where `qrl` is `uid=1000`).
- [x] No `RUN chmod +s` or setuid binaries.
- [x] No `--privileged` in compose.
- [x] No `cap_add:` in compose.
- [x] All EXPOSE'd ports are >1024.
- [x] Data volumes are declared with `chown 1000:1000` recipe in the entrypoint (so bind-mounts from a fresh host directory get correct ownership on first run).
- [x] Binary lives in `/opt/qrl/bin/` — not `/usr/local/bin/` (avoids conflicts with a host-mounted root fs, keeps our binaries in one owned tree).
- [x] Uses a distroless-static or Alpine-nonroot base — no shell in the runtime image unless a build actually needs it.

Compose file must:

- [x] Bind mounts owned by UID 1000 on the host (`configure.sh` prompts for a data dir and `chown -R 1000:1000` it, or falls back to a named volume).
- [x] Publish only the ports the user opted into via `.env` — not everything by default.
- [x] Health-checks that don't require `curl` inside the container (use the binary's own status command when possible, else expose an in-container `/health` endpoint from status-server).

## Layout

```
docker/
├── README.md                    quick-start + troubleshooting
├── popup-node/
│   ├── Dockerfile.gqrl          builder→minimal, USER 1000
│   ├── Dockerfile.beacon        builder→minimal, USER 1000
│   ├── Dockerfile.validator     builder→minimal, USER 1000
│   └── Dockerfile.status-server node:alpine → USER 1000
├── docker-compose.yaml          orchestration, .env-driven
├── configure.sh                 first-time wizard: prompts, writes .env
└── .env.example                 template with every knob + comments
```

Kept out of the top-level tree so it doesn't collide with in-repo bare-metal scripts. Compose runs from `docker/`.

## Rollout plan

1. **Scaffold** (done, first-pass this PR): design doc, `docker/popup-node/Dockerfile.gqrl`, `docker/.env.example`.
2. **Complete Dockerfiles**: beacon, validator, status-server. Same rootless rules.
3. **Compose**: full 4-node localnet up. Named volume per node (or bind if the user provided a data dir). Depends-on ordering so beacon waits for gqrl.
4. **configure.sh**: prompts for the essentials (variant, num nodes, data dir, genesis password), writes `.env`, does the initial `chown` on the data dir.
5. **Bring-up proof**: `docker compose up`, wait for block 5+ on all nodes, snapshot. Fold the snapshot into the existing snapshot flow (`chain-snapshot.sh` currently reads from `chains/localnet/` — either mount the compose data dir there, or extend the snapshot tool to also read the compose volume path).
6. **Podman parity**: verify the same compose works under `podman-compose` (Podman rootless is the intended default for security-focused users; Docker rootless is the fallback).
7. **CI**: add a GitHub Actions workflow that builds each image, runs the compose, waits for convergence, teardown. Same test the pre-compaction snapshot flow proved for the bare-metal path.

## Non-goals for the first pass

* Multi-host / swarm. Popup nodes are single-host by definition.
* Kubernetes helm chart. Kurtosis package (already exists at `qrl-classic-kurtosis-package/`) covers the "orchestrate for testing" use case; rootless containers here cover the "run one on your laptop" case.
* Auto-updates. Users pick a tag and stay on it until they intentionally pull a newer one.
