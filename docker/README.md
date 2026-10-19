# QRL popup nodes — rootless container packaging

**Status:** Phase 1 substrate + 4-node convergence proven end-to-end. All 4 Dockerfiles build, all 10 containers boot rootless (USER 1000, no CAPs, read-only rootfs, RLIMIT_CORE=0), the leader beacon produces blocks continuously, **followers dial the leader via QNR bootstrap and import blocks** (verified: all 4 gqrl reach the same block height, all 4 beacons show `activePeers=3`).

Follower P2P works via a `bootstrap-fetcher` init container that curls beacon-node1's `/qrl/v1/node/identity` for its QNR and writes it to a shared named volume; a `beacon-entrypoint.sh` wrapper on each follower reads the QNR at start and injects it as `--bootstrap-node`. The same wrapper also auto-detects the container's bridge IP and injects it as `--p2p-host-ip` so the QNR the *leader* advertises is dialable across the Docker network (bare-metal used loopback, containers need the bridge IP). Both concerns are per-container and rootless. Bare-metal parity of the QNR-fetch step lives in `qrl-2.0/configs/localnet/start-follower-beacons.sh`.

Track under nodes task #68 (parent) + #70 (bring-up, this pass) + #74 (follower QNR bootstrap, completed).

## What this is

A `docker compose`-orchestrated 4-node zond-private localnet that runs entirely as UID 1000 with:

- No root user in any image
- No `--privileged`, no `cap_add`, no host-network mode
- Read-only rootfs, tmpfs for `/tmp`, writable only under `/opt/qrl/data`
- `no-new-privileges` set, seccomp default policy applied by runtime
- `RLIMIT_CORE=0` (no coredumps to disk)
- All ports >1024, bound to `127.0.0.1` unless the operator explicitly opts a port to `0.0.0.0`
- Outbound HTTPS only via a pinned CA bundle

The intended runtime is **Podman rootless** (native, no daemon). **Docker rootless** works as a fallback.

## Target adversary

Nation-state. Rootless containers alone are phase 1 of a multi-layer defense. Full threat model, phased maturity roadmap, and per-attack-class mitigations in `docs/POPUP-NODES-ROOTLESS.md`.

## Quick start

Under **docker** (fallback):

```bash
./configure.sh                                  # interactive wizard
qrlenv run -- docker compose up                 # -d for detached
open http://127.0.0.1:8080/                     # dashboard
```

Under **rootless podman** (intended default for security-focused users):

```bash
./configure.sh
qrlenv run -- podman compose \
    -f docker-compose.yaml \
    -f docker-compose.podman.yaml \
    up
# or, with the third-party podman-compose CLI:
# qrlenv run -- podman-compose \
#     -f docker-compose.yaml -f docker-compose.podman.yaml up
```

The `docker-compose.podman.yaml` overlay adds `userns_mode: keep-id:uid=1000,gid=1000`
to every service. Under rootless podman, this remaps the operator's host UID
into container-UID 1000, so the `qrl` user inside the container matches
the ownership of the `docker/data` bind-mount without any `chown`
gymnastics. Docker doesn't need it (Docker Desktop's VM handles the
mapping, and Docker rootless has its own userns setup).

Everything else — `read_only`, `security_opt: no-new-privileges`,
`cap_drop: ALL`, `RLIMIT_CORE=0`, named volume ownership, loopback-only
port publish, `service_completed_successfully` gating for the bootstrap
fetcher — is portable across both engines and needs no overlay.

`qrlenv` handles secret injection: values marked `qrl-env:...` in `.env` are
decrypted with the OS-keychain master key and passed to compose as
environment variables. Plaintext lines pass through unchanged. It's
engine-agnostic — invoke it the same way for docker or podman.

See "Config secrets — no non-PQ crypto anywhere" below for why we use
qrlenv instead of dotenvx.

Currently, individual images can be built by hand:

```bash
# From the repo root:
docker build \
  --build-arg QRL_VARIANT_PATH=qrl-2.0/upstream/go-qrl \
  -f docker/popup-node/Dockerfile.gqrl \
  -t qrl-gqrl:standard .
```

## Layout

```
docker/
├── README.md                    (this file)
├── .env.example                 config template — all knobs, documented
├── popup-node/
│   ├── Dockerfile.gqrl          rootless execution client (SHIPPED)
│   ├── Dockerfile.beacon        rootless consensus client + qrysmctl (SHIPPED)
│   ├── Dockerfile.validator     rootless validator (SHIPPED)
│   └── Dockerfile.status-server rootless dashboard (Node.js) (SHIPPED)
├── docker-compose.yaml          4x gqrl + 4x beacon + validator + dashboard
│                                + bootstrap-fetcher init container
│                                — read_only, no-new-privs, cap_drop:ALL,
│                                RLIMIT_CORE=0, loopback-only publish (SHIPPED)
├── docker-compose.podman.yaml   overlay for rootless podman — adds
│                                userns_mode:keep-id per service (SHIPPED)
├── docker-compose.hardened.yaml overlay for phase 2 — QRL_RUNTIME opt-in
│                                (gVisor/Kata) per service (SHIPPED)
├── docker-compose.seccomp.yaml  overlay for phase 2 — applies custom
│                                QRL seccomp profile per service (SHIPPED)
├── docker-compose.tls.yaml      overlay for PQ TLS on dashboard +
│                                beacon-chain REST gateway
│                                (X25519MLKEM768, FIPS 203) (SHIPPED)
├── gen-tls-cert.sh              generate self-signed cert for PQ TLS
├── popup-node/nginx-beacon-pqtls.conf
│                                nginx PQ-TLS sidecar config in front
│                                of beacon-node1's REST gateway
├── popup-node/SYSCALL-AUDIT.md  empirical syscall audit basis for seccomp
├── popup-node/seccomp-qrl.json  262-syscall allowlist (default-deny) —
│                                verified: 4-node convergence under it
├── BASE-IMAGE-DIGESTS.md        why + how base images are digest-pinned
├── refresh-base-digests.sh      regenerate every pin from `docker pull`
│                                (idempotent, safe to re-run monthly)
├── build-images.sh              reproducible build for every image with
│                                SBOM (SPDX 2.3) + SLSA v0.2 provenance
│                                attached (phase 3 groundwork)
├── inspect-image.sh             extract SBOM/provenance from an OCI
│                                tarball, no cosign/oras dependency
├── sign-images.sh               PQ-safe release signing (ML-DSA-87,
│                                FIPS 204) — writes SHA256SUMS.txt +
│                                .qrlsig
├── verify-images.sh             two-step verify: signature over
│                                manifest + hash of every listed tarball
├── release.sh                   end-to-end orchestrator:
│                                refresh → build → sign → bundle
├── load-release.sh              consumer-side: extract → verify →
│                                docker/podman load
├── reset-popup.sh               full-reset of local compose state:
│                                stack + data + secrets + master key
├── RELEASE.md                   threat model + key mgmt + rotation
│                                procedures for release signing
└── configure.sh                 interactive .env wizard — inits qrlenv
                                 master key, generates per-node JWT secrets
                                 encrypted via qrlenv (SHIPPED)

bin/
└── qrlenv                       PQ-safe dotenvx-shape CLI (AES-256-GCM
                                 + OS keychain, no elliptic curve). Ships
                                 with init/set/get/unset/keys/rotate/run
                                 subcommands. (SHIPPED — commit f4e03eb4)
```

Still owed:

* **Snapshot integration ✓ shipped.** `qrl-2.0/tools/chain-snapshot.sh` now takes `--layout compose` (or `QRL_LAYOUT=compose`) to point at `docker/data` instead of `qrl-2.0/chains/localnet`. Snapshot on-disk format is canonical (node1's beacon always lives under `beacon/`) so a snapshot saved from compose restores cleanly onto bare-metal and vice versa. Live-tested: 99MB snapshot saved from a compose bring-up at block 0x17, `source_layout` recorded in manifest.
* **Podman live proof.** The overlay is shipped and known-good on paper; a clean-host `podman compose ... up` under Podman 4.7+ still needs a live 4-node convergence run to close the loop. Task #75.
* **Phase 2 hardening (partial).** Shipped: `docker-compose.hardened.yaml` with `QRL_RUNTIME` opt-in for gVisor/Kata + CPU-mitigations preflight in `configure.sh` + digest-pinned base images across all 4 Dockerfiles (see `BASE-IMAGE-DIGESTS.md`). Still owed: custom seccomp profile (#76 — needs syscall audit against real gqrl/beacon workloads), AppArmor + SELinux profiles (#77 — need a Linux host with the LSM loaded). Task #72.
* **Phase 3 (partial).** Shipped: reproducible-build recipe (`build-images.sh`) with pinned SOURCE_DATE_EPOCH, `--sbom=true`, `--provenance=mode=max` producing OCI tarballs with SPDX 2.3 SBOM + SLSA v0.2 provenance attached; `inspect-image.sh` extracts + summarizes both without cosign dependency. Still owed: cosign signing pipeline + registry publish (task #79), TPM attestation, PQ TLS on control planes. Task #73.

## Reproducible builds + SBOM (phase 3, opt-in)

To rebuild every popup-node image with SBOM + provenance attached:

```bash
./docker/build-images.sh                 # all 6 images (3 gqrl variants + beacon + validator + dashboard)
./docker/build-images.sh dashboard       # single image
OUT_DIR=/tmp/qrl-oci ./docker/build-images.sh   # custom output dir
```

Each image lands as an OCI tarball under `docker/out/qrl-<role>-<variant>.oci.tar`
with:

* Full SPDX 2.3 SBOM attested via in-toto (`in-toto.io/predicate-type: https://spdx.dev/Document`)
* SLSA v0.2 provenance attested (materials chain: base image digest, Dockerfile, source path)
* `SOURCE_DATE_EPOCH` pinned to the last git commit's author time so two builders from the same source produce byte-identical tarballs (verify: `sha256sum` the resulting `.oci.tar`)

To inspect what's inside without cosign / oras:

```bash
./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar              # summary
./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar --sbom       # full SPDX JSON
./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar --provenance # full SLSA v0.2 JSON
./docker/inspect-image.sh docker/out/qrl-dashboard-latest.oci.tar --config     # image config
```

Once the OCI tarballs are built, sign them PQ-safely with ML-DSA-87
(FIPS 204) via the existing wallet infrastructure:

```bash
# Option A: sign via an unlocked wallet on the local machine
QRL_WALLET_ID=<walletId> ./docker/sign-images.sh

# Option B: sign with a raw seed (dev/test only)
SEED_HEX=$(python3 -c "print('a'*96)") ./docker/sign-images.sh
```

This writes:
* `docker/out/SHA256SUMS.txt` — canonical manifest, one SHA-256 per tarball
* `docker/out/SHA256SUMS.txt.qrlsig` — JSON with `algorithm`, `address`,
  `publicKey`, `signature` (all ML-DSA-87)

Verify with:

```bash
./docker/verify-images.sh
```

Or produce a full distributable release tarball (refresh digests → build →
sign → bundle → portable verify script + mldsa87keygen binary):

```bash
VERSION=v0.4.0 QRL_WALLET_ID=<id> ./docker/release.sh
# -> docker/out/qrl-popup-release-v0.4.0.tar (180MB, all 6 images + verifier)
```

Consumers extract the bundle and verify offline with no repo checkout:

```bash
tar -xf qrl-popup-release-v0.4.0.tar
cd qrl-popup-release-v0.4.0
./verify/verify-release.sh
```

Full trust story — threat model, key rotation, pinned-pubkey channels — in
`docker/RELEASE.md`.

Two-step: (1) ML-DSA-87 signature over the manifest hash — proves the
inventory hasn't been tampered with; (2) `shasum -a 256 -c` over each
listed tarball — proves the artifacts match. Both must pass. The signer's
public key is printed so the operator can cross-reference against a
pinned trusted key. **This is intentionally PQ-safe** — no ECDSA/Ed25519/
RSA anywhere. Cosign/Sigstore parity waits on their ML-DSA support (task
#79 tail).

## Hardened bring-up (phase 2, opt-in)

To run under gVisor (`runsc`) — kernel attack surface reduced by ~90%:

```bash
QRL_RUNTIME=runsc qrlenv run -- docker compose \
    -f docker-compose.yaml \
    -f docker-compose.hardened.yaml \
    up
```

To run under Kata (`kata-runtime`) — each container in its own microVM
(**this is our QEMU-backed isolation path** — Kata's default hypervisor
is QEMU; can be swapped to cloud-hypervisor or Firecracker in Kata's
config):

```bash
QRL_RUNTIME=kata-runtime qrlenv run -- docker compose \
    -f docker-compose.yaml \
    -f docker-compose.hardened.yaml \
    up
```

Both require the runtime to be installed on the host and registered with
the container engine. See
`docker/docker-compose.hardened.yaml` for links to install docs.

**Pure-QEMU (non-containerized)** — booting each popup-node process
inside a hand-managed QEMU VM with no OCI runtime wrapper — is a
separate substrate. Not supported directly by this compose file; track
under task #89.

## PQ TLS on the dashboard (opt-in)

The dashboard's status-server has a PQ TLS listener (X25519MLKEM768
hybrid, TLS 1.3, FIPS 203). Same hybrid PQ group Chrome + Firefox +
Cloudflare deploy at scale. Bring it up:

```bash
./docker/gen-tls-cert.sh                       # writes docker/tls/{cert,key}.pem
qrlenv run -- docker compose \
    -f docker-compose.yaml \
    -f docker-compose.tls.yaml \
    up
open https://127.0.0.1:8443/                   # PQ-TLS dashboard
```

Verify the handshake:

```bash
echo | openssl s_client -connect 127.0.0.1:8443 -groups X25519MLKEM768 -tls1_3 -trace 2>&1 \
    | grep "Negotiated TLS1.3 group"
# → Negotiated TLS1.3 group: X25519MLKEM768
```

The cert *signature* is ECDSA-P256 (self-signed); the KEM is
post-quantum. Cert-signature migration to ML-DSA-87 waits on X.509 /
CA / browser tooling — track under task #93.

**Beacon-chain REST gateway** — same overlay also brings up a
`beacon-pqtls` nginx sidecar (using `nginxinc/nginx-unprivileged` with
OpenSSL 3.5.7 built-in) in front of `beacon-node1:3600`. Prysm-fork's
Go crypto/tls doesn't yet expose ML-KEM in a release we control, so
the sidecar terminates PQ TLS externally and proxies plaintext over
the compose bridge internally. Verify:

```bash
curl -sk --tls-max 1.3 https://127.0.0.1:3643/qrl/v1/node/identity
# → real JSON with peer_id + QNR
echo | openssl s_client -connect 127.0.0.1:3643 -groups X25519MLKEM768 -tls1_3 -trace 2>&1 \
    | grep "Negotiated TLS1.3 group"
# → Negotiated TLS1.3 group: X25519MLKEM768
```

Direct upstream patch to qrysm's gRPC gateway TLS is task #91's tail —
would remove the sidecar hop.

To apply the custom QRL seccomp profile (`popup-node/seccomp-qrl.json`, 262
allow-listed syscalls, default-deny) on top:

```bash
qrlenv run -- docker compose \
    -f docker-compose.yaml \
    -f docker-compose.hardened.yaml \
    -f docker-compose.seccomp.yaml \
    up
```

The profile replaces Docker's default seccomp allowlist (~340 syscalls) with
a tighter one derived from an empirical 60s strace audit of running gqrl +
beacon-chain (steady-state 30 syscalls, expanded to cover startup + edge
paths). Verified: 4-node convergence at block 1 under the profile.
Startup + steady-state + graceful shutdown all pass. Long-running edge
paths (DB compaction, HTTP request storm, P2P reconnect loops) still need
soak-test coverage — track under nodes task #76 follow-ups.

See `docker/popup-node/SYSCALL-AUDIT.md` for the audit data + methodology.

`configure.sh` runs a CPU-mitigations preflight on Linux hosts (skipped on
macOS). If any `/sys/devices/system/cpu/vulnerabilities/*` file reports
`Vulnerable`, the wizard refuses to proceed unless `QRL_ACK_CPU_VULN=1` is
set. Bypass the whole check with `QRL_SKIP_CPU_PREFLIGHT=1` if you're
running under a hypervisor that opaques the mitigations state.

## Config secrets — no non-PQ crypto anywhere

Every crypto primitive in this project must be post-quantum safe. Dotenvx
uses X25519 (elliptic-curve KEM, quantum-broken) so we do NOT use it.
Instead we ship a QRL-native `qrlenv` that has the same UX (`qrlenv set`,
`qrlenv run`) but uses only PQ-safe primitives from our existing wallet
stack:

- **AES-256-GCM** for authenticated encryption (Grover-resistant at 128-bit
  effective security — CNSA 2.0 approved).
- **Argon2id** for the KDF (memory-hard, PQ-neutral).
- **OS keychain** for the master key (macOS Keychain / Linux Secret
  Service / Windows DPAPI — same mechanism as the Tier 3 wallet CLI).
- **ML-KEM (Kyber, FIPS 203)** if we ever need "encrypt to another party's
  public key" — never X25519 or ECDH.

The full threat model + phase roadmap is in `docs/POPUP-NODES-ROOTLESS.md`.

## Verification (once phase 2 lands)

Every release will ship with:

* **SBOM** (SPDX) listing every layer's contents.
* **Sigstore/cosign signature** for every image, verifiable against a public key committed to this repo.
* **Reproducible build recipe** — two independent builders (repo CI + a mirrored fork) produce byte-identical images; mismatch fails release.

Verify with:

```bash
cosign verify --key docker/cosign.pub ghcr.io/theqrl/gqrl-${QRL_VARIANT}:${QRL_VERSION}
cosign download sbom ghcr.io/theqrl/gqrl-${QRL_VARIANT}:${QRL_VERSION}
```

Neither of these commands works yet — the signing pipeline is phase 2.

## Why not Kubernetes / Kurtosis?

* **Kurtosis** already exists for orchestrated multi-node testing (`qrl-classic-kurtosis-package/`). That's the "spin up N nodes for a CI test" use case.
* **Kubernetes** is out of scope for "run one on your laptop." A helm chart or operator would come after phase 3 lands, if there's demand.

Rootless containers here fill the gap between "run bare-metal on your dev laptop" and "run in Kurtosis for a full CI test rig."
