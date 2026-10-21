---
title: Home
layout: default
nav_order: 1
description: "Rootless, hardened popup QRL 2.0 blockchain stack — gqrl + qrysm + validator + dashboard, PQ-safe end to end, three variants side by side."
permalink: /
---

<div class="hero">
  <h1>QRL Popup Nodes</h1>
  <p class="tagline">
    A rootless, nation-state-resistant packaging of the QRL 2.0 blockchain
    stack — <code>gqrl</code> execution + <code>qrysm</code> beacon +
    validator — as hardened Docker / Podman containers with a
    batteries-included operator dashboard. Three variants (standard,
    rescue-edition, moscow-chill) run side-by-side on one host.
  </p>
  <p style="font-size: 0.85rem; opacity: 0.75; max-width: 44rem; margin: -0.5rem 0 1rem 0;">
    <strong>Third-party build.</strong> Maintained at
    <a href="https://github.com/pq-cybarg/qrl-popup-nodes">pq-cybarg/qrl-popup-nodes</a>;
    <strong>not affiliated with or endorsed by The QRL Foundation</strong>. Vendors public
    artefacts from <a href="https://github.com/theQRL">theQRL</a> (go-qrl, qrysm,
    go-qrllib) at pinned commits. Precompile spec target:
    <a href="https://github.com/theQRL/qips/pull/46">QIP #46 (draft)</a>.
  </p>
  <div class="badges">
    <img alt="PQ-safe" src="https://img.shields.io/badge/crypto-PQ_safe-3fcf3f">
    <img alt="rootless" src="https://img.shields.io/badge/runtime-rootless-3fcf3f">
    <img alt="read-only" src="https://img.shields.io/badge/rootfs-read_only-3fcf3f">
    <img alt="seccomp" src="https://img.shields.io/badge/seccomp-262_syscalls-3fcf3f">
    <img alt="AppArmor" src="https://img.shields.io/badge/AppArmor-enforce-3fcf3f">
    <img alt="SELinux" src="https://img.shields.io/badge/SELinux-qrl_popup_t-3fcf3f">
    <img alt="reproducible" src="https://img.shields.io/badge/build-byte_identical-3fcf3f">
    <img alt="ML-DSA-87" src="https://img.shields.io/badge/signatures-ML_DSA_87-3fcf3f">
    <img alt="ML-KEM-768" src="https://img.shields.io/badge/TLS_KEM-ML_KEM_768-3fcf3f">
  </div>
  <div class="hero-cta">
    <a class="primary" href="getting-started.html">Get started →</a>
    <a class="secondary" href="https://github.com/pq-cybarg/qrl-popup-nodes">View on GitHub ↗</a>
    <a class="secondary" href="architecture.html">Architecture</a>
  </div>
</div>

## 60-second bringup

```bash
git clone https://github.com/pq-cybarg/qrl-popup-nodes.git
cd qrl-popup-nodes
./scripts/bootstrap.sh                  # fetch theQRL upstream deps
./docker/configure.sh                   # scaffold .env + build images + init chaindata
qrlenv run -- docker compose \
    -f docker/docker-compose.yaml \
    -f docker/docker-compose.docker-sock.yaml \
    up -d
open http://127.0.0.1:8080/
```

Within ~90 seconds the chain is producing blocks and the dashboard
is live. See [getting started](getting-started.html) for three stacks
at once, signed releases, and hardening overlays.

## Three stacks, three variants

<div class="stack-grid">
  <div class="stack-card lead">
    <span class="rank-chip">LEAD</span>
    <h3>qrl-popup</h3>
    <p>Standard variant — the baseline QRL 2.0 precompile set. Closest to upstream for benchmarking + plain-QRVM dApps.</p>
    <div class="port-row">gqrl :9545 · beacon :3600 · dashboard :8080</div>
  </div>
  <div class="stack-card l2">
    <span class="rank-chip">L2</span>
    <h3>l2-rescue</h3>
    <p>Rescue-edition variant — moscow-chill plus Rescue-Prime (0x20), STARK verify (0x21), ML-DSA-87 verify-with-addr (0x22).</p>
    <div class="port-row">gqrl :9745 · beacon :3700 · dashboard :8180</div>
  </div>
  <div class="stack-card mc">
    <span class="rank-chip">MC</span>
    <h3>l2-moscow</h3>
    <p>Moscow-chill variant — <a href="https://github.com/theQRL/qips/pull/46">QIP #46</a>: ML-DSA-87 verify at 0x03 (64-byte frame), SHAKE256(x, 64) at 0x06. Canonical QRL 2.0 PQ precompiles.</p>
    <div class="port-row">gqrl :9945 · beacon :4000 · dashboard :8280</div>
  </div>
</div>

Each stack runs in its own compose project — isolated bridge network,
distinct container names, distinct data root, distinct genesis. Three
independent chains, not peers.

## What's in the stack

| Layer | Component | Role |
|-------|-----------|------|
| Consensus | `qrysm` beacon | Post-quantum PoS consensus (Prysm fork, 64-byte addr) |
| Consensus | `qrysm` validator | Signs blocks + attestations via go-qrllib ML-DSA-87 |
| Execution | `gqrl` | Geth fork — chain tip, state, mempool, Engine API |
| Visibility | `status-server` | Operator dashboard — compose health, chain state, keys |

## Nation-state hardening posture

The stack is designed to be useful against a well-funded adversary with
container-escape exploits and side-channel capability:

| Layer | Mechanism |
|-------|-----------|
| Image surface | distroless base, digest-pinned |
| Runtime (user) | rootless UID 1000 under rootless Docker / Podman |
| Runtime (filesystem) | `read_only: true` rootfs, writable paths explicit |
| Runtime (capabilities) | `cap_drop: [ALL]` on every service |
| Runtime (syscalls) | 262-syscall seccomp allowlist |
| Runtime (MAC) | AppArmor profiles (Ubuntu) OR SELinux `qrl_popup_t` |
| Runtime (gVisor) | optional via `runtime: runsc` |
| Secrets at rest | `qrlenv` — PQ-safe .env encryption |
| Release artefacts | ML-DSA-87 detached signatures over SHA256SUMS |
| TLS | ML-KEM-768-wrapped X25519 on dashboard + beacon gateway |
| Reproducibility | SOURCE_DATE_EPOCH-pinned, two-builder diff proof |
| Attestation | TPM2 PCR-extend recipe |

Every primitive in the signing / KEM / signature-verify path is
post-quantum. See [security](security.html) for the primitive-by-primitive
breakdown.

{: .warning }
**NEVER expose a popup node to the public internet.** Chain IDs, keys,
and fee-recipients are development-only. See [security — operational posture](security.html#operational-posture).

## Where to start

- **Running it** → [getting started](getting-started.html)
- **Understanding the design** → [architecture](architecture.html)
- **Picking a variant** → [variants](variants.html)
- **Operating a deployment** → [operations](operations.html)
- **Writing a client** → [API reference](api.html)
- **Security review** → [security](security.html)
- **Reproducing a build** → [reproducibility](reproducibility.html)
- **Measured boot** → [TPM attestation](tpm-attestation.html)
- **PQ TLS story** → [ML-DSA-87 / ML-KEM migration](ml-dsa-87-ml-kem-migration.html)
- **Rootless Podman** → [Podman convergence proof](podman-proof.html)
