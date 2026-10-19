# QRL popup-node release integrity

This document defines the trust story for a QRL popup-node release —
who signs, how you verify, what breaks if a signer's key is
compromised, and how to rotate.

## Threat model

**Target adversary:** nation-state. See `docs/POPUP-NODES-ROOTLESS.md`
for the full popup-node threat model. Release-integrity subset:

| Attack | Mitigation |
| --- | --- |
| Malicious image published to a mirror under our name | Consumers verify `SHA256SUMS.txt.qrlsig` against a pinned trusted key before running any container. Mismatch = don't run. |
| Compromised release-signer host silently substitutes a malicious binary before signing | Reproducible builds (`build-images.sh` pins `SOURCE_DATE_EPOCH`, base-image digests, uses `-trimpath`) — an independent second builder from the same source will produce byte-identical tarballs. Divergence is detectable. Two-builder proof is task #80 (still owed). |
| Compromised registry proxy returns a modified tarball to consumers | Consumer's SHA-256 hash-check (`shasum -c SHA256SUMS.txt`) detects tampering even if the signature is valid over the manifest. |
| Attacker forges a signature with a stolen private key | The signing key is stored under the wallet stack's KEK-encrypted keystore (Argon2id + AES-256-GCM). Physical theft of the encrypted keystore is not enough; you also need the operator's password OR their OS keychain entry (Tier 3). Rotation procedure below assumes the worst-case scenario. |
| Quantum computer breaks the signature algorithm | ML-DSA-87 (FIPS 204) is a post-quantum lattice-based signature scheme. Classical-quantum hybrid attacks that break ML-DSA would also break every ECDSA/Ed25519/RSA release-signing system in production — this is a strictly better position than industry standard (cosign uses ECDSA-P256, quantum-broken by Shor's). |
| Adversary at rest on the release-signer host reads the seed during signing | The signing key material never persists in cleartext outside the KEK-wrapped keystore. `mldsa87keygen` receives the seed on `argv`, wipes its local copy on exit. Longer-term hardening: HSM/YubiKey with an ML-DSA firmware (nascent as of 2026-09). |

## Signature algorithm

**ML-DSA-87** (NIST FIPS 204). Post-quantum lattice signature scheme.
Same primitive the QRL 2.0 wallet stack uses for on-chain
transactions (see `qrl-2.0/tools/mldsa87keygen`, wrapping `go-qrllib`).

Deliberate choice:

* **PQ-safe today.** ECDSA/Ed25519/RSA fall to a sufficiently large
  quantum computer via Shor's algorithm. ML-DSA-87 doesn't.
* **No side-installed toolchain.** Cosign/Sigstore are excellent
  ecosystems but their signing primitive is ECDSA-P256 (as of
  2026-09; Sigstore's ML-DSA support is roadmap). Adopting them
  would embed a quantum-broken primitive in the release-integrity
  chain, which violates this project's "no crypto can circumvent
  PQC" rule.
* **Same infra as the wallet.** The signing key IS a QRL wallet.
  Rotation, backup, keystore-encryption, and OS-keychain integration
  all reuse the wallet stack tested for user funds.

Cosign/Sigstore layering when they ship ML-DSA is future work — no
principled conflict, just not yet.

## What each release contains

A release tarball (`qrl-popup-release-<version>.tar`) unpacks to:

```
qrl-popup-release-<version>/
├── qrl-gqrl-standard.oci.tar          OCI-format image tarballs
├── qrl-gqrl-rescue-edition.oci.tar    (one per role × variant)
├── qrl-gqrl-moscow-chill.oci.tar
├── qrl-beacon-latest.oci.tar
├── qrl-validator-latest.oci.tar
├── qrl-dashboard-latest.oci.tar
├── SHA256SUMS.txt                     manifest — one line per tarball
├── SHA256SUMS.txt.qrlsig              JSON: algorithm, address,
│                                      publicKey, signature
├── RELEASE.md                         this file, for the consumer
├── RELEASE.json                       machine-readable release metadata
└── verify/
    ├── verify-release.sh              two-step verification script
    └── mldsa87keygen                  Go binary — no repo checkout needed
```

Each `.oci.tar` also carries its own SBOM (SPDX 2.3) + provenance
(SLSA v0.2) as in-toto attestations. Inspect with
`docker/inspect-image.sh <tarball> --sbom` etc.

## Consumer verification

```bash
tar -xf qrl-popup-release-v0.4.0.tar
cd qrl-popup-release-v0.4.0
./verify/verify-release.sh
```

The script does **two independent checks** — both must pass:

**Step 1** — ML-DSA-87 signature over `SHA-256(SHA256SUMS.txt)` via
`./verify/mldsa87keygen -verify`. Proves the manifest was signed by
someone in possession of the signing wallet's private material. It
does NOT prove the signer was authorized — for that, see step 3.

**Step 2** — `shasum -a 256 -c SHA256SUMS.txt`. Recomputes SHA-256 for
every listed tarball and compares against the manifest. Proves the
tarballs haven't been swapped or corrupted after signing.

**Step 3 (manual)** — cross-reference the printed signer public key
against a pinned trusted key published by the QRL release team.
`verify-release.sh` prints `signer pubkey: <first-32-hex>...<last-32-hex>`
and warns you to do this. If the pubkey isn't the one you trust,
STOP — the signature might be from an attacker who compromised a
different signing key.

**Trusted public keys** are published:

* On the QRL GitHub release page for each release, in the release
  notes.
* On the project website `theqrl.org` under `/releases/keys` (planned).
* On a dedicated `SIGNING_KEYS.txt` in the repo root, itself
  signed by an older long-term key (planned — key hierarchy design
  is task #79 tail).

## Signer procedure (project side)

### One-time setup

Generate a signing wallet using the wallet-cli. The signing key IS a
QRL wallet — same keystore format, same Argon2id KDF, same AES-256-GCM
cipher. This means you can put it on hardware (Tier 3 keychain path)
without any custom firmware.

```bash
./bin/qrl-wallet-cli.js create --network zond-private
# Note the walletId printed; store the password in a safe place.
```

For repeated use, unlock it once via OS keychain (Tier 3):

```bash
./bin/qrl-wallet-cli.js keychain-store --walletId <id> --os-prompt
# Subsequent signing operations pull the KEK from the OS keychain
# without prompting for a password.
```

**Publish the wallet's public key** in every documented pinning
location BEFORE you sign the first release. Consumers verify against
the pinned key; if you publish the key IN a release, you're
bootstrapping trust from the release itself, which defeats it.

### Cutting a release

```bash
VERSION=v0.4.0 QRL_WALLET_ID=<id> ./docker/release.sh
```

The `release.sh` orchestrator runs: refresh-base-digests → build-images
→ sign-images → bundle. Output is `docker/out/qrl-popup-release-v0.4.0.tar`.

Distribute the tarball via any channel — GitHub release, S3 bucket,
Bittorrent. Consumers verify against pinned keys published elsewhere;
the distribution channel doesn't need to be trusted.

### Rotation (compromise or scheduled)

**Case A — scheduled rotation** (e.g. every 12 months):

1. Generate a new signing wallet (same procedure as one-time setup).
2. Sign a `KEY_TRANSITION_<date>.txt` file with BOTH the old key
   AND the new key, containing:
     ```
     effective_date: 2027-01-01
     old_pubkey: <full hex>
     new_pubkey: <full hex>
     policy: releases after effective_date are signed by new_pubkey;
             older releases remain valid under old_pubkey.
     ```
3. Publish `KEY_TRANSITION_<date>.txt` in every documented pinning
   location alongside the old and new public keys.
4. Retire the old key material (delete keystore, revoke OS keychain
   entry). Do NOT keep it "just in case" — an unused key is a liability.

**Case B — compromised key** (emergency):

1. Do NOT sign anything with the compromised key.
2. If you're not sure whether the key was compromised, assume yes.
   Rotation is cheap; a compromised trusted key is very expensive.
3. Publish an urgent `KEY_REVOCATION_<date>.txt` signed by an
   off-line long-term ROOT key (if you don't have one, this is the
   moment you regret it; ship an out-of-band signed statement via
   any high-assurance channel).
4. All releases signed AFTER the compromise date are untrusted; ask
   consumers to re-verify releases they've already installed against
   older cached signatures (from before the compromise).
5. Generate a new signing key and publish it.

For scheduled rotations you can plan the ROOT-key layer above the
release-signing key: root signs annual signer keys, signer keys sign
releases. This is common in Debian/Fedora/Alpine and gives you a
graceful compromise-recovery path. Design of a QRL-native root-key
hierarchy is task #79 tail.

## Reproducibility guarantee

`build-images.sh` combines:

* Digest-pinned base images (see `docker/BASE-IMAGE-DIGESTS.md`)
* `SOURCE_DATE_EPOCH` pinned to `git log -1 --pretty=%ct`
* Go's `-buildvcs=false -trimpath -ldflags="-s -w"` (already in every
  popup-node Dockerfile)
* `BUILDKIT_INLINE_CACHE=1` for deterministic layer digests

Given the same source tree at the same commit, two independent
builders should produce byte-identical `.oci.tar` files. Verify by:

```bash
sha256sum docker/out/qrl-*.oci.tar     # on builder 1
# same command, second builder, compare hashes
```

Any divergence localizes the reproducibility bug — the failure mode is
loud, not silent. Full two-builder proof is task #80.

## What this doesn't cover

* **Runtime compromise.** Signed release binaries can still be
  compromised at runtime by a malicious operator, a kernel exploit,
  or a supply-chain attack against the language runtime (Go stdlib,
  Node modules). Runtime hardening is phase 1/2 territory — see
  `docs/POPUP-NODES-ROOTLESS.md`.
* **Attestation of runtime state.** The release proves the images are
  what we shipped. It doesn't prove your host is running those exact
  images. TPM/measured-boot attestation is task #81.
* **Confidentiality of the release.** Releases are meant to be public.
  If you need private distribution, encrypt with ML-KEM (FIPS 203)
  AFTER signing.
