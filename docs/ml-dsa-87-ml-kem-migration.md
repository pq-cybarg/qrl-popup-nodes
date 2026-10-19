---
title: PQ TLS migration
nav_order: 11
---

# ML-DSA-87 cert signatures + ML-KEM upstream patch

Two related upstream tasks:

- **#93** — migrate the dashboard + beacon TLS cert *signature* from
  the current RSA/ECDSA/EdDSA selection to ML-DSA-87, closing the
  last classical primitive in the TLS stack.
- **#94** — upstream the ML-KEM-wrapped KEM already in use on the
  dashboard back into qrysm itself, so the beacon's gRPC gateway can
  terminate PQ-TLS without a sidecar.

Both are **upstream patches to third-party projects** — qrysm and the
Go `crypto/x509` toolchain. Nothing can be shipped from this repo
alone; the docs below are the full recipe for the operator or the
contributor who wants to drive one or both patches home.

## Where we are today

| Layer                | Current primitive              | Target        | Blocker                        |
|----------------------|--------------------------------|---------------|--------------------------------|
| TLS handshake (KEM)  | X25519 + ML-KEM-768 hybrid     | same          | — (already in place)           |
| TLS handshake (sig)  | ECDSA P-256 cert               | **ML-DSA-87** | #93 (no `crypto/x509` support) |
| TLS record (cipher)  | AES-256-GCM / ChaCha20-Poly1305 | same          | — (symmetric, PQ-safe)         |
| qrysm gRPC gateway   | plain TCP (dashboard sidecar wraps) | ML-KEM/ML-DSA | #94 (needs patch to qrysm)  |

The dashboard already terminates PQ-TLS correctly — a reverse proxy
sidecar (nginx with the openssl-oqs-provider) handles the hybrid KEM
and serves an ECDSA cert underneath. The cert signature is the one
classical primitive left in the hot path.

## Task #93 — ML-DSA-87 X.509 certs

### Why ECDSA stays today

Go's `crypto/x509` doesn't recognise ML-DSA-87 as a signature
algorithm. The OID (`2.16.840.1.101.3.4.3.17` for ML-DSA-87) is
registered by NIST, but:

- `pkix.AlgorithmIdentifier.Algorithm` map in `crypto/x509` doesn't
  include it.
- Even if we hand-crafted a cert, `tls.Config` would reject it on
  the handshake side.

Chrome / Firefox are in the same spot. Draft-ietf-lamps-dilithium
(the IETF spec for ML-DSA in X.509) is nearly finalised; browser
vendors are shipping prototype support behind feature flags.

### Short-term workaround — ML-DSA-87 as "unknown extension"

A cert can carry an ML-DSA-87 signature in a non-critical extension
(`subjectAltPublicKeyInfo` + `altSignatureAlgorithm`), with the main
cert still ECDSA-signed. Clients that understand the extension verify
the ML-DSA leg; clients that don't fall back to ECDSA. Hybrid, not
pure PQ, but strictly better than ECDSA-only.

Our `docker/gen-tls-cert.sh` is written to pre-stage this:

```bash
./docker/gen-tls-cert.sh --hybrid-ecdsa-mldsa --cn localhost
# produces cert.pem (ECDSA sign + ML-DSA ext) and key.pem (ECDSA)
# ML-DSA key stored at key-mldsa.pem (used by the sidecar)
```

### Long-term fix — upstream patch to Go `crypto/x509`

Steps:

1. Patch `crypto/x509/x509.go` to add ML-DSA-87 to:
   - `signatureAlgorithmDetails` list.
   - `getSignatureAlgorithmFromAI`.
   - `checkSignature`.
2. Patch `crypto/tls/handshake_messages.go` to add the ML-DSA-87
   signature scheme code point.
3. Submit via Go proposal process (`golang/go/issues/new`).

While waiting, we build a `goqrllib/x509-pq` fork of
`crypto/x509` + `crypto/tls` with ML-DSA-87 bolted on, and link it
into the dashboard + beacon via `go.mod` replace.

Status: fork prototyped at
`qrl-2.0/upstream/go-qrllib/experimental/x509-pq/`. Needs a 2-day
test-suite sweep before linking into production.

## Task #94 — Native PQ-TLS in qrysm gRPC gateway

### Why the sidecar exists

qrysm's gRPC gateway (`qrysm/beacon-chain/gateway/`) uses
`grpc-ecosystem/grpc-gateway/v2` + `net/http`'s stock TLS, which
chains to the same `crypto/tls` → `crypto/x509` surface we're
blocked on above. The dashboard sidecar (nginx + openssl-oqs-provider)
terminates PQ-TLS externally and proxies plaintext gRPC to the
gateway over the compose network.

**Trade-off**: PQ terminates one hop short of the gRPC handler. For
localhost-only deployments this is fine (the one hop is a bridge
network). For LAN-exposed deployments the sidecar is on the same
host so still fine. For cross-datacenter deployments, operators
should pin the gRPC gateway + sidecar on the same VM.

### Upstream patch path

The clean fix is a qrysm PR:

1. Add `--pq-tls-cert` + `--pq-tls-key` + `--pq-tls-kem` flags to
   the beacon-chain CLI.
2. Replace `net/http`'s `http.Server.TLSConfig` with a wrapper that
   accepts ML-KEM-wrapped KEM shares + ML-DSA-87 cert signatures.
   Needs the `crypto/tls` fork from #93 above (so #94 blocks on #93).
3. Default to classical TLS when the flags are not set — no
   regression for existing operators.
4. Add e2e tests in `qrysm/testing/endtoend` that bring up two
   beacons, one with `--pq-tls-*`, verify handshake succeeds via a
   client linked against the same fork.

The qrysm maintainers have signalled receptiveness
([issue theQRL/qrysm#78](https://github.com/theQRL/qrysm/issues/78)
— tracked). The patch is ~1200 lines, mostly test coverage; the
production surface is ~200 lines of new code.

## Operator-facing today

Even without #93 / #94 upstream, this stack already gives you:

- **ML-KEM-wrapped KEM** on every control-plane TLS session (dashboard
  + beacon gateway via sidecar).
- **ML-DSA-87 release signatures** — every signed OCI tarball.
- **ML-DSA-87 validator signatures** — every attestation / block
  signed by `qrysm validator`.
- **ML-DSA-87 wallet signatures** — every tx signed via the
  dashboard's keystore.

The classical ECDSA cert signature is the *only* remaining classical
primitive, and only in a control-plane path with no record-and-decrypt
value since the data behind TLS is immediately public (chain state).
A nation-state adversary with a quantum capability intercepting the
dashboard HTTPS stream in 2035 gains nothing because the content is
on-chain in cleartext from epoch 0.

The hybrid cert extension in `gen-tls-cert.sh --hybrid-ecdsa-mldsa`
closes even that gap for operators who want full PQ in the
handshake today, at the cost of browsers that don't understand the
extension falling back to ECDSA.

## Tracking

- #93 — [theQRL/qrysm#...](link TBD) + Go proposal.
- #94 — [theQRL/qrysm#78](https://github.com/theQRL/qrysm/issues/78).
- Hybrid-cert sidecar — shipped (see
  `docker/popup-node/nginx-beacon-pqtls.conf`).
