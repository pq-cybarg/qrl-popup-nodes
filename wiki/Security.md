# Security

Full write-up at
[docs/security](https://theqrl.github.io/qrl-popup-nodes/security.html).

## Threat model

A well-funded adversary with container-escape exploits and
side-channel capability. **Not** an opportunist with a plain shell;
**not** a petty criminal.

## Three invariants

1. No crypto in the critical path circumventable by a quantum-capable
   adversary (ML-DSA-87, ML-KEM-768, SHAKE-256).
2. No container runs as root; `cap_drop: [ALL]`, `no-new-privileges`,
   UID 1000.
3. Every LSM layer rejects the obvious escape routes (seccomp,
   AppArmor OR SELinux, optional gVisor / Kata).

## Hardening overlay stack

```
docker-compose.yaml                   ← base: UID 1000, cap_drop:ALL, no-new-privs
└── docker-compose.hardened.yaml      ← read_only rootfs
    └── docker-compose.seccomp.yaml   ← 262-syscall allowlist
        └── docker-compose.apparmor.yaml  ← AppArmor (Ubuntu/SUSE)
            — OR —
            docker-compose.selinux.yaml   ← SELinux (RHEL/Fedora)
            └── docker-compose.qemu.yaml  ← optional Kata/qemu runtime
                └── docker-compose.tls.yaml  ← optional PQ-TLS everywhere
```

Each layer is additive. Run with none for dev, with the first two for
default production, with all seven for regulated / FIPS deployments.

## PQ primitives

| Function                                | Primitive                       |
|-----------------------------------------|---------------------------------|
| Validator signatures                    | ML-DSA-87 (FIPS 204)            |
| Address derivation                      | SHAKE-256(pub, 64)              |
| Keystore KDF                            | Argon2id                        |
| Keystore encryption                     | AES-256-GCM                     |
| TLS KEM                                 | X25519 + ML-KEM-768 (hybrid)    |
| Release signatures                      | ML-DSA-87 detached              |
| X.509 cert sig                          | ML-DSA-87 (planned — #93)       |

See [PQ TLS Migration](PQ-TLS-Migration.html) for the ML-DSA-87 X.509
+ ML-KEM-upstream-to-qrysm roadmap.

## Operational posture

**NEVER expose a popup node to the public internet.** No firewall
rules, no port forwarding, no public ingress. These are **local**,
**air-gapped** developer tools. Genesis accounts have well-known
passwords.

**NEVER reuse popup-node accounts on mainnet.** The genesis keypair
is publicly in `qrl-2.0/configs/localnet/`.

**NEVER skip signature verification on a release load.** The
`load-release.sh` command refuses to proceed on signature mismatch,
and no supported flag changes that behaviour.

## Audit trail

Every config-changing endpoint logs to the host stderr. qrlenv emits
on every key access. `docker/release.sh` writes a tamper-evident log
of each release cut. Seccomp blocks fire audit events on Linux hosts
with auditd — see [docs/soak](https://github.com/theQRL/qrl-popup-nodes/tree/main/docker/soak)
for how to collect them.
