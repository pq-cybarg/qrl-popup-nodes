# QRL Popup Nodes — Wiki

Welcome. This wiki is the operator-facing documentation set for the
QRL popup-node stack. The GitHub Pages site at
<https://pq-cybarg.github.io/qrl-popup-nodes/> is the public-facing
version; this wiki is the shorter, link-forward handbook.

> **Third-party build.** Maintained at
> [pq-cybarg/qrl-popup-nodes](https://github.com/pq-cybarg/qrl-popup-nodes);
> **not affiliated with or endorsed by The QRL Foundation**. Vendors
> public theQRL artefacts (go-qrl, qrysm, go-qrllib) at pinned
> commits. Target precompile spec: [QIP #46](https://github.com/theQRL/qips/pull/46) (draft).

## What is a popup node?

A local, isolated QRL 2.0 chain running on your laptop under Docker
or Podman. Four `gqrl` execution nodes + four `qrysm` beacon nodes +
one validator + a dashboard. Three variants (standard, rescue-edition,
moscow-chill) can run side-by-side.

**Never a mainnet participant.** The chain IDs, keys, and
fee-recipients are development-only — see
[Security](Security.html#operational-posture) for the full "do not
deploy to mainnet" checklist.

## Pages

- [Running locally](Running-Locally.html) — bringup in 60s + three
  stacks at once.
- [Architecture](Architecture.html) — single-operator chain
  topology, Engine API, go-qrl unification.
- [Variants](Variants.html) — standard vs rescue-edition vs
  moscow-chill, precompile tables.
- [API reference](API-Reference.html) — JSON-RPC, beacon REST,
  dashboard HTTP.
- [Security](Security.html) — hardening posture, PQ primitives,
  threat model.
- [Operations](Operations.html) — snapshots, releases, resets,
  monitoring.
- [Troubleshooting](Troubleshooting.html) — the common bringup gotchas
  + their fixes.
- [Reproducibility](Reproducibility.html) — two-builder byte-identical
  proof (#80).
- [TPM attestation](TPM-Attestation.html) — measured boot (#81).
- [PQ TLS migration](PQ-TLS-Migration.html) — ML-DSA-87 X.509 + ML-KEM
  (#93 / #94).
- [Podman proof](Podman-Proof.html) — rootless Podman parity (#75).

## Status chips

![LEAD](https://img.shields.io/badge/qrl--popup-LEAD-3aa0ff) 
![L2](https://img.shields.io/badge/l2--rescue-L2-ff5577) 
![MC](https://img.shields.io/badge/l2--moscow-MC-b070ff)

![PQ-safe](https://img.shields.io/badge/crypto-PQ_safe-3fcf3f) 
![rootless](https://img.shields.io/badge/runtime-rootless-3fcf3f) 
![read--only](https://img.shields.io/badge/rootfs-read_only-3fcf3f) 
![seccomp](https://img.shields.io/badge/seccomp-262_syscalls-3fcf3f) 
![AppArmor](https://img.shields.io/badge/AppArmor-enforce-3fcf3f) 
![SELinux](https://img.shields.io/badge/SELinux-qrl_popup_t-3fcf3f) 
![reproducible](https://img.shields.io/badge/build-two_builder_identical-3fcf3f) 
![ML--DSA--87](https://img.shields.io/badge/signatures-ML_DSA_87-3fcf3f) 
![ML--KEM--768](https://img.shields.io/badge/TLS_KEM-ML_KEM_768-3fcf3f)

## Links

- Source: <https://github.com/theQRL/qrl-popup-nodes>
- Issues: <https://github.com/theQRL/qrl-popup-nodes/issues>
- Pages site: <https://theqrl.github.io/qrl-popup-nodes/>
- QRL main site: <https://theqrl.org/>
- QRL 2.0 whitepaper: <https://theqrl.org/qrl-2.0.pdf> (external)
