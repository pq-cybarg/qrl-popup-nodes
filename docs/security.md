---
title: Security
nav_order: 6
---

# Security posture

The threat model is **a well-funded adversary** with container-escape
exploits and side-channel capability. Not an opportunist with a
plain shell, not a petty criminal, but a team that will spend hours
on each node. The stack is designed under three invariants:

1. **No crypto in the critical path can be circumvented by a
   quantum-capable adversary.** Every signature verification, every
   KEM, every X.509 cert signature uses an ML-* (post-quantum)
   primitive or a hybrid that remains secure as long as *one* leg
   does.
2. **No container runs as root; no container has even a single Linux
   capability.** `cap_drop: [ALL]`, `no-new-privileges: true`, UID 1000.
3. **Every layer in the LSM stack rejects the obvious escape routes.**
   seccomp, AppArmor (or SELinux), optional gVisor or Kata. One leg
   failing doesn't open the door.

## Cryptographic primitives — line by line

| Function                                | Primitive                              | PQ-safe |
|-----------------------------------------|----------------------------------------|---------|
| Validator signatures                    | ML-DSA-87 (FIPS 204, SLH-DSA family)   | ✅       |
| Beacon aggregated attestation signature | ML-DSA-87 bulk-verify                  | ✅       |
| Block hash                              | SHAKE-256                              | ✅       |
| Address derivation                      | SHAKE-256(pub, 64)                     | ✅       |
| Keystore KDF                            | Argon2id                               | ✅       |
| Keystore encryption                     | AES-256-GCM (symmetric)                | ✅       |
| TLS KEM (dashboard, beacon gateway)     | ML-KEM-768 hybrid with X25519          | ✅       |
| Release signatures                      | ML-DSA-87 detached                     | ✅       |
| X.509 cert signature                    | ML-DSA-87 (planned, task #93)          | 🚧       |
| qrysm gRPC gateway TLS                  | ML-KEM wired via sidecar (task #94)    | 🚧       |
| QRC-20 contract-level signatures        | application choice (recommend ML-DSA-87)| N/A    |

The remaining 🚧 items are documented recipes with a known upstream
patch required. See [the ML-DSA-87 / ML-KEM migration doc](ml-dsa-87-ml-kem-migration.html).

## Defense in depth

Overlays stack:

```
docker-compose.yaml                 ← base: UID 1000, cap_drop:ALL, no-new-privs
└── docker-compose.hardened.yaml    ← read_only rootfs, tmpfs for writable paths
    └── docker-compose.seccomp.yaml ← 262-syscall allowlist
        └── docker-compose.apparmor.yaml  ← AppArmor profiles (Ubuntu/SUSE)
        └── docker-compose.selinux.yaml   ← OR SELinux qrl_popup_t (RHEL/Fedora)
            └── docker-compose.qemu.yaml  ← OPTIONAL: Kata / qemu runtime
            └── docker-compose.tls.yaml   ← OPTIONAL: PQ-TLS on dashboard
```

Each layer is additive — you can run with none (dev), with the first
two (default production), or with all seven (regulated / FIPS deploys).

### What each layer stops

- **UID 1000 + cap_drop:ALL**: a privilege-escalation vuln in a service
  can only touch files writable by uid 1000 inside the container. No
  CAP_SYS_* to re-mount, raise RLIMITs, or set uid.
- **read_only rootfs**: a shell drop in a service can't persist a
  stage-two binary. Writable paths (/tmp, datadir) are explicit.
- **seccomp allowlist**: 262 of the ~380 Linux syscalls allowed. The
  excluded ones include `ptrace`, `module_load`, `kexec_*`, `fanotify_init`,
  `userfaultfd`, `kcmp`, `move_mount`, and the whole eBPF family.
- **AppArmor / SELinux**: enforces a path-based policy in parallel
  with the Discretionary ACL — a compromise that gains root inside
  the container still can't `mount`, `ptrace`, write to `/proc/sys/`,
  or load a kernel module.
- **Kata / gVisor runtime**: the host kernel is only reachable via a
  stub (gVisor's user-space syscall emulator or Kata's lightweight VM).
  Container-escape vulns in runc or crun stop at the stub.
- **PQ-TLS on control plane**: a nation-state record-now-decrypt-later
  attack on the dashboard or beacon gateway stays unreadable after
  the quantum break.

## Reproducible builds

Task #80 — a two-builder diff proof script that builds every image
twice (on two different hosts, same SOURCE_DATE_EPOCH), compares each
OCI layer by digest, and reports the first mismatch. The image recipe
is **expected to be byte-identical**; any divergence is a build
non-determinism to track down.

Mechanisms used:

- `SOURCE_DATE_EPOCH` passed as a build-arg and consumed by every
  stage that touches a timestamp (`cp -p`, `tar --mtime`, `go build
  -trimpath`).
- Debian base images pinned by digest (sha256).
- `go build -buildvcs=false -trimpath -ldflags='-s -w -buildid='`.
- No `apt-get upgrade` in Dockerfile — install the exact packages.

Run the proof:

```bash
./docker/reproducibility-check.sh --out /tmp/repro-report.md
```

## Operational posture

**NEVER expose a popup node to the public internet.** Nothing in the
stack is designed to resist a credentialed MITM, DDoS at line rate,
or a bootstrap-fetcher poisoning. These are **local, air-gapped**
developer tools.

**NEVER reuse popup-node accounts on mainnet.** The genesis account
in `qrl-2.0/configs/localnet/genesis-template.json` has a
publicly-documented password (`QrlClassic`) — anyone who clones this
repo has the private key. Keep the chainId at 1 only for local
development; a real mainnet participant must configure a distinct
chainId that doesn't collide with any production QRL network.

**NEVER skip signature verification on a release load.** `load-release.sh`
refuses to proceed if the ML-DSA-87 detached signature on
`SHA256SUMS.txt` doesn't verify against the pinned pubkey. If you
override that check (there's no supported flag — don't add one in a
fork either), you're loading an untrusted image.

See `./CLAUDE.md`
Critical Security Notes section for the operational checklist used by
the maintainers.

## Side-channel scope

Not in scope:

- CPU microarchitectural attacks (Spectre, Meltdown, Zenbleed).
  Mitigation is at the host kernel + microcode level; the stack
  does not try to double-cover.
- Row Hammer on DRAM. Same — host firmware / ECC concern.
- Physical access. The stack is a developer tool.
- Supply-chain compromise of the Debian base image. Mitigated by
  digest pinning + a two-builder reproducibility proof — if an
  adversary swaps a package, two builders disagree.

In scope and mitigated:

- Container escape via runc / crun / docker-engine CVE —
  seccomp + AppArmor/SELinux + optional Kata.
- Credential exfiltration via docker socket — mounted RO by default;
  on-demand RW overlay only when the compose/release endpoints need
  it, with host-side `verifyContainerInProject` gating.
- Side-channel between popup stacks — containers in different compose
  projects run on different bridges with no cross-traffic; no shared
  tmpfs; no shared config directories; separate rootfs.

## Audit trail

- All config-touching endpoints log to the host stderr (visible via
  `docker compose logs -f dashboard`).
- qrlenv emits a line on every key rotation and key access.
- `docker/release.sh` writes a tamper-evident log of each release
  cut to `docker/out/release-log.txt` with ML-DSA-87 signatures.
- A seccomp block fires an audit event (`type=1326`) on Linux hosts
  with auditd — see `docker/soak/README.md` for how to collect them.

If something looks wrong — a container restarting unexpectedly, an
RPC call failing with EPERM — check the host audit log first. The
default is **noisy** by design.
