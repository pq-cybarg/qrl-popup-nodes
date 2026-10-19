# Tier 3d — FIPS 140-3 / CNSA 2.0 build scoping

Status target: full-stack FIPS 140-3 Level 1 for the wallet-creation and key-management paths, with an upgrade path to Level 2 and CNSA 2.0 alignment.

Author's note: this is a scoping document, not a compliance claim. Reaching a validated build requires a **CMVP-listed cryptographic module** per component and a signed **Security Policy** — neither exists in the current tree. Below is the concrete gap analysis and the shortest path from "hardened but unvalidated" (where we are after Tier 3a-c) to "FIPS-validated".

## 1. What is in scope

The wallet lifecycle we harden:

| Step | Component | File |
| --- | --- | --- |
| Password → KEK derivation | Argon2id (browser via `hash-wasm`; Node via `crypto.argon2Sync`; CLI via same Node primitive) | `web/argon2-worker.js`, `lib/keystore.js`, `bin/qrl-wallet-cli.js` |
| Symmetric seal of private key | AES-256-GCM (Node `crypto.createCipheriv`) | `lib/keystore.js` |
| Random salt / IV / walletId | Node `crypto.randomBytes` / `crypto.randomUUID` | same |
| Signing keypair generation (Zond) | ML-DSA-87 via `github.com/theQRL/go-qrllib` | `qrl-2.0/tools/mldsa87keygen/main.go` |
| Signing keypair generation (Classic) | XMSS (SHAKE-128, height 10) via QRL Python CLI | `qrl-1.0/upstream/QRL/venv/bin/qrl` |
| Transport | Loopback TCP + Unix domain socket (mode 0600) | `status-server.js` |
| Storage at rest | JSON keystore (mode 0600) under `$HOME/.qrl-dashboard/keystores/` | `lib/wallet_store.js` |
| Session KEK cache | macOS Keychain / Linux Secret Service / Windows DPAPI | `bin/qrl-wallet-cli.js` |

## 2. Per-primitive validation status (as of 2026-08)

### AES-256-GCM

* **Approved.** NIST SP 800-38D. On the CNSA 2.0 list.
* **Node.js:** default OpenSSL build is *not* FIPS-validated. Node can be linked against the OpenSSL 3.x FIPS module (`fips.dylib` / `fips.so`); this makes the AES-256-GCM path a validated call. See action item #1.
* **Go (used by keygen tools):** `GOEXPERIMENT=boringcrypto` gives BoringSSL FIPS 140-2 module. Go 1.24 added a native FIPS 140-3 module option (`GOFIPS=1`) that is currently in CMVP queue.

### SHA-256 / SHA-512 (used internally by GCM / AAD hashing)

* **Approved.** FIPS 180-4.
* Same validation path as AES: linked against a validated crypto module.

### Argon2id

* **Not FIPS-validated.** Argon2 is not on NIST's approved-algorithm list. NIST SP 800-63B *permits* memory-hard KDFs but does not endorse a specific one. NIST's own recommended KDF for FIPS-validated modules is PBKDF2 with ≥ 10,000 iterations of SHA-256.
* **Impact:** if the deployment requires a strict FIPS-only stack, Argon2id in `hash-wasm` and `crypto.argon2Sync` must be replaced with PBKDF2. This is a downgrade in brute-force resistance (Argon2id is memory-hard; PBKDF2 is not).
* **Mitigation options:**
  1. **Hybrid mode** — run PBKDF2-SHA-256 first (for FIPS coverage), then Argon2id on the PBKDF2 output (for GPU/ASIC resistance). Argon2id is treated as a defense-in-depth wrapper around a validated primitive, not the primary KDF.
  2. **Pure PBKDF2 mode** — accept the security downgrade in exchange for validation. Bump iteration count to ≥ 600,000 (OWASP 2023 guidance for SHA-256).
  3. **Wait for NIST** — the Password-Based KDF Task Force may standardize Argon2 (or a successor); track NIST IR 8009 for updates.

### ML-DSA-87

* **Approved algorithm.** NIST FIPS 204 (published August 2024).
* **No CMVP-validated module exists** for ML-DSA-87 in general use as of early 2026. The `go-qrllib` implementation is a reference implementation, not a validated module.
* **Impact:** ML-DSA-87 signature *generation and verification* are FIPS-approved algorithms, but the specific software running them is not a validated module. This is a common state during the "algorithm approved, modules in queue" gap.
* **Mitigation:** track NIST CMVP queue for ML-DSA validation. In the meantime, document that keygen is via reference implementation and rely on Node's validated crypto module for the *storage* pathway.

### XMSS (QRL Classic)

* **Related but not equivalent.** FIPS 205 (SLH-DSA) is a *hash-based* signature scheme based on SPHINCS+, which shares design ancestry with XMSS. The QRL Classic XMSS variant (RFC 8391 XMSS with SHAKE-128) is **not** the same parameter set as FIPS 205, and is not FIPS-approved.
* **Impact:** if a deployment requires FIPS coverage for Classic signatures, QRL Classic support must be marked out-of-scope and disabled at the module level.
* **Mitigation:** rely on the fact that this repo scopes Classic support to `classic-private` (local development only). Production FIPS deployments would use Zond networks + ML-DSA-87 exclusively.

### `crypto.randomBytes` / `crypto.randomUUID`

* Backed by OpenSSL's DRBG. FIPS-validated in OpenSSL 3.x FIPS module (SP 800-90A CTR_DRBG).
* Same OpenSSL-FIPS build requirement as AES-256-GCM.

## 3. Concrete action items

### A. Node.js FIPS build (highest priority)

1. Build OpenSSL 3.x with the FIPS provider:

    ```
    ./Configure enable-fips
    make && make install_fips
    ```

2. Compile Node.js against that OpenSSL:

    ```
    ./configure --shared-openssl --shared-openssl-includes=... \
                --shared-openssl-libpath=...
    make
    ```

3. Enable FIPS mode at runtime:

    ```
    node --enable-fips --openssl-config=/path/to/fipsmodule.cnf status-server.js
    ```

4. Add a startup check in `status-server.js` that calls `crypto.getFips()` and refuses to run in a non-FIPS build when `QRL_REQUIRE_FIPS=1` is set. Log the OpenSSL module version + CMVP certificate number.

Estimated effort: 1-2 engineer-days for the build + runtime gate; validation cycle time separate.

### B. KDF replacement decision

Pick one of:

* **B1 (recommended for defense-in-depth):** hybrid PBKDF2-then-Argon2id. Store both parameter sets in the keystore's `kdf` block:

    ```json
    "kdf": {
      "name": "hybrid-pbkdf2-argon2id",
      "pbkdf2": { "hash": "sha256", "iterations": 600000, "saltHex": "..." },
      "argon2id": { "memoryKiB": 65536, "passes": 3, "parallelism": 8, "saltHex": "..." }
    }
    ```

  Then: `kek = argon2id(pbkdf2(password, salt1), salt2)`. Both salts are freshly generated. PBKDF2 is the FIPS-validated part; Argon2id is defense-in-depth against ASIC brute force.

* **B2 (pure FIPS):** PBKDF2-only. Simpler; loses memory-hardness. Only pick if a strict validator refuses the hybrid.

Either choice is contained to `lib/keystore.js` — the API surface (`encryptWithKek`, `decryptWithKek`) does not change. Client (browser + CLI) picks up the new derivation from the `kdf.name` field in the keystore, so keystores from the two modes coexist.

### C. Go binary FIPS mode

For `qrl-2.0/tools/mldsa87keygen/main.go`:

1. Add a build variant using `GOFIPS=1` (Go 1.24+) or `GOEXPERIMENT=boringcrypto` (older Go). The go-qrllib ML-DSA path uses `crypto/sha3` for keccak; that call gets routed through the validated module automatically when GOFIPS is on.
2. Emit the FIPS status in the JSON output so the calling Node process can record which mode produced the keypair.

### D. Vendored dependencies

* `vendor/hash-wasm/argon2.umd.min.js` — pin the version and checksum (already done in `status-server.js`). Under a strict FIPS build, disable this path entirely: the server-side `/api/wallet/create/prepare` refuses to issue sessions and the frontend falls back to Tier 1 (server-side, which by then uses PBKDF2 via the validated OpenSSL module).

### E. Transport hardening

Loopback TCP is not encrypted. Under CNSA 2.0 the loopback path is arguably out of the classified boundary (same host, same user), but for auditor-friendliness:

* Make Unix socket the primary transport, TCP secondary and disabled behind an env var `QRL_ALLOW_TCP=1`.
* Or terminate TLS on the loopback socket using a self-signed cert + CNSA 2.0 cipher suite (`TLS_AES_256_GCM_SHA384`).

Decision deferred to the deployment-time reviewer — the transport is a policy choice, not a code question.

### F. Keystore-file signature

Currently the keystore JSON has no integrity protection at the file-system level (only the GCM tag on the ciphertext). Add:

1. HMAC-SHA-256 over the full JSON, keyed with a machine-scoped key stored in `/etc/qrl-dashboard/hmac.key` (mode 0400, root-only).
2. Refuse to load a keystore whose HMAC doesn't verify.
3. This closes the "attacker rewrites keystore metadata to a KEK they can derive" attack surface at the file-system level (the AAD already closes it at the crypto level, but defense-in-depth).

## 4. What Tier 3d does not solve

* **Hardware-backed KEK storage.** Full CNSA 2.0 posture requires a HSM (YubiHSM, Nitrokey HSM, or cloud KMS with FIPS 140-3 module). This is a Tier 4 concern.
* **Boot-time attestation.** TPM-measured launch of the status-server so a tampered binary can't run. Out of scope.
* **Signing key rotation policy.** ML-DSA-87 is stateless so rotation is a policy decision, not a technical one. XMSS is stateful and requires index tracking — already handled in the QRL Classic keystore payload.
* **Post-quantum TLS.** CNSA 2.0 requires ML-KEM key exchange in TLS. Node's OpenSSL FIPS module does not yet ship ML-KEM. Track OpenSSL 3.5 release notes.

## 5. Sequencing

1. **Now (Tier 3d as-is):** this document + a `QRL_REQUIRE_FIPS` gate that refuses to run in a non-FIPS build. See `status-server.js` action item #A.4.
2. **Next sprint:** switch to hybrid PBKDF2+Argon2id in `lib/keystore.js`. Backward-compatible via `kdf.name`.
3. **Blocked on CMVP:** ML-DSA-87 validated module. Track NIST CMVP queue.
4. **Deployment-time:** OpenSSL FIPS build + Node linked against it.

The code changes in items 1 and 2 are self-contained and can land without waiting for external validation cycles. Everything else is validation-workflow work, not code work.

## 6. Related files

* `lib/keystore.js` — encrypt/decrypt paths; PBKDF2 switch lands here.
* `status-server.js` — startup check + `QRL_REQUIRE_FIPS` gate; vendor integrity checks.
* `web/argon2-worker.js` — WASM Argon2id; gated off in strict-FIPS mode.
* `bin/qrl-wallet-cli.js` — CLI derivation; must match server's KDF choice.
* `qrl-2.0/tools/mldsa87keygen/` — Go tool that would be rebuilt with GOFIPS.
