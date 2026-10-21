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
| TLS handshake (sig)  | ECDSA P-256 cert               | **ML-DSA-87 via MTC** | #93 — Chrome's chosen path is MTC, not direct ML-DSA X.509 |
| TLS record (cipher)  | AES-256-GCM / ChaCha20-Poly1305 | same          | — (symmetric, PQ-safe)         |
| qrysm gRPC gateway   | plain TCP (dashboard sidecar wraps) | ML-KEM/ML-DSA | #94 (needs patch to qrysm)  |

The dashboard already terminates PQ-TLS correctly — a reverse proxy
sidecar (nginx with the openssl-oqs-provider) handles the hybrid KEM
and serves an ECDSA cert underneath. The cert signature is the one
classical primitive left in the hot path.

## Why not just put ML-DSA in X.509 certs

Short version: **Google's Chrome team explicitly chose not to.**
Benjamin et al. observed that an ML-DSA-87 signature is ~4.5 KB vs
~64 bytes for ECDSA P-256. A typical TLS 1.3 handshake sees two or
three cert signatures (leaf + intermediates + CT SCTs). Direct ML-DSA
X.509 pushes handshake size from ~2 KB today to ~15–50 KB —
enough to blow QUIC's initial window and double TLS RTT. Chrome's
answer is **Merkle Tree Certificates (MTC)**.

## Merkle Tree Certificates (MTC)

MTC changes **how certs are delivered**, not what signs them:

- A CA batches many certs (billions per day) into a Merkle tree.
- The CA publishes **one PQ-signed tree head** per batch — the only
  ML-DSA signature anyone transmits.
- A server sends the leaf cert + a **short inclusion proof** — ~736
  bytes to prove membership in a 4.4 million-cert tree ([Encryption
  Consulting analysis][ec]). No per-cert ML-DSA signature on the wire.
- The client has already synced recent tree heads out-of-band (like
  CRL/CT today) and verifies the inclusion proof against a trusted
  head.

Net effect: handshake size stays Ethernet-frame-sized while **every
signature in the chain is post-quantum**.

### Who's shipping

- **IETF**: `draft-ietf-plants-merkle-tree-certs-07` (PLANTS working
  group, last updated 2026-10-07). Still evolving — not yet an RFC.
- **Google / Chrome**: ran a live Chrome Beta experiment Aug 2026
  (half of Beta users got MTC-signed test cert chains, billions of
  handshakes). Chrome has publicly declared MTC the preferred path
  for PQ on the public web.
- **Cloudflare**: announced a public CA on 2026-09-29 — applying to
  Chrome / Apple / Microsoft / Mozilla root programs, acquiring a
  GlobalSign root. **Production MTC issuance targeted Q1 2027.**
- **Let's Encrypt**: announced MTC support on 2026-06-03 — staging
  environment late 2026, production during 2027.

### Impact on this stack

For the **popup-node use case** (localhost dev, no public CA
dependency, no WAN latency to worry about), MTC is unnecessary. The
hybrid detached-signature mode already shipped in
[`docker/gen-tls-cert.sh --mldsa`](https://github.com/pq-cybarg/qrl-popup-nodes/blob/main/docker/gen-tls-cert.sh)
closes the one classical primitive in the handshake without any
delivery-mechanism cleverness.

For a **public-facing dashboard** (operator exposing their node to
the LAN, publishing a QNS record over PQ-TLS, etc.), two paths now
exist:

1. **Self-hosted MTC CA (shipped — see [MTC CA](mtc-ca.html)).** The
   repo includes [`qrl-2.0/tools/qrl-mtc-ca/`](https://github.com/pq-cybarg/qrl-popup-nodes/tree/main/qrl-2.0/tools/qrl-mtc-ca)
   — a minimum-viable MTC CA that batches certs, signs tree heads
   with ML-DSA-87, and emits per-leaf inclusion proofs. Trust model
   is pin-by-pubkey (SSH `known_hosts`-style), which is the right
   fit for operator-to-operator and consortium deployments. **No
   waiting.**
2. **Wait for Cloudflare / Let's Encrypt in Q1 2027** if you need a
   browser-root-trusted CA. Then plug an MTC-aware ACME client into
   the dashboard's cert-gen hook. The dashboard's existing `/api/tls/gen-cert` endpoint (and the
`openssl-oqs-provider` sidecar) will plug into an MTC CA's ACME
flow unchanged — the server-side TLS stack just carries whatever
the CA signed into.

### Composition with #93

MTC and the "ML-DSA-87 X.509 signature algorithm" story (#93 as
originally scoped) are **not alternatives — they compose**:

- The MTC CA still uses ML-DSA-87 to sign the tree head.
- The leaf certs delivered via MTC can still carry an ML-DSA-87
  subject public key (just not an ML-DSA signature inside the leaf,
  since the Merkle inclusion proof is what authenticates the leaf).
- The Go `crypto/x509` upstream patch (#93) is still useful for
  parsing MTC-delivered leafs that happen to have ML-DSA subject
  public key info.

### Operator checklist for Q1 2027

When Cloudflare / Let's Encrypt open production MTC issuance:

1. Rebuild the dashboard image against an MTC-aware TLS stack —
   `openssl-oqs-provider` with the MTC verifier added, or a Go stack
   with the `draft-ietf-plants` tree-head verifier.
2. Point `/api/tls/gen-cert` at an MTC-aware ACME client (Caddy,
   lego, or whichever ships support first).
3. Pre-cache tree heads via the CA's monitor endpoint (CRL-equivalent)
   on startup so a cold start doesn't block on a WAN fetch.
4. Operators behind air-gaps keep using
   `docker/gen-tls-cert.sh --mldsa` — the self-signed hybrid is the
   right answer where no WebPKI dependency exists.

[ec]: https://www.encryptionconsulting.com/about-merkle-tree-certificates/

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

- #93 — [MTC](mtc-ca.html) is the Chrome-chosen successor to direct ML-DSA-in-X.509; the shipped `qrl-2.0/tools/qrl-mtc-ca/` is the self-hosted CA.
- #94 — [theQRL/qrysm#78](https://github.com/theQRL/qrysm/issues/78).
- Hybrid-cert sidecar — shipped (see
  `docker/popup-node/nginx-beacon-pqtls.conf`).
