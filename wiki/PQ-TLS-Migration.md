# PQ TLS migration — #93 and #94

Full write-up at
[docs/ml-dsa-87-ml-kem-migration](https://theqrl.github.io/qrl-popup-nodes/ml-dsa-87-ml-kem-migration.html).

Two upstream patches gate the full PQ TLS story:

| Task | What                                                         | Blocker                                           |
|------|--------------------------------------------------------------|---------------------------------------------------|
| #93  | ML-DSA-87 as the X.509 cert signature algorithm              | Go `crypto/x509` doesn't know ML-DSA-87 yet       |
| #94  | ML-KEM / ML-DSA-87 natively in qrysm's gRPC gateway TLS      | Blocks on #93 (same crypto/tls dependency chain)  |

## Where we are today

| Layer                     | Current primitive                   | Target        | PQ-safe |
|---------------------------|-------------------------------------|---------------|---------|
| TLS handshake (KEM)       | X25519 + ML-KEM-768 hybrid          | same          | ✅       |
| TLS handshake (sig)       | ECDSA P-256 cert                    | **ML-DSA-87** | 🚧 (#93) |
| TLS record (cipher)       | AES-256-GCM / ChaCha20-Poly1305     | same          | ✅       |
| qrysm gRPC gateway TLS    | plain TCP + dashboard sidecar wraps | native in qrysm | 🚧 (#94) |

The dashboard already terminates PQ-TLS via nginx + openssl-oqs-provider
sidecar. The one remaining classical primitive is the ECDSA cert
signature — in a control-plane path serving public blockchain data,
so the record-and-decrypt threat is minimal.

## Short-term workaround

`docker/gen-tls-cert.sh --mldsa` emits an ECDSA-P256 cert plus a
detached ML-DSA-87 signature over the cert's DER, with a sidecar
`hybrid.manifest`. Operators pin BOTH out-of-band: TLS stacks
verify the ECDSA leg on handshake, `mldsa87keygen -verify` validates
the ML-DSA leg against the pinned pubkey.

## Why not direct ML-DSA in X.509

Chrome explicitly chose **not** to. ML-DSA-87 signatures are ~4.5 KB
each vs ~64 bytes for ECDSA. A handshake with ML-DSA cert chain
balloons from ~2 KB to ~15–50 KB — enough to double TLS RTT. The
Chrome / Google / Cloudflare answer is Merkle Tree Certificates (MTC).

## MTC (long-term, Q1 2027)

[draft-ietf-plants-merkle-tree-certs-07](https://datatracker.ietf.org/doc/draft-ietf-plants-merkle-tree-certs/).
A CA batches many certs into a Merkle tree, PQ-signs **one** tree
head per batch. Servers send leaf + ~736-byte inclusion proof —
not a ~4.5 KB per-cert ML-DSA signature.

Shipping:

- **Cloudflare** — public CA planned, production MTC issuance Q1 2027
  (announced [2026-09-29](https://blog.cloudflare.com/pq-ca-with-mtcs/)).
- **Let's Encrypt** — [plans production MTCs in 2027](https://letsencrypt.org/2026/06/03/pq-certs) (announced 2026-06-03).
- **Chrome** — ran a Chrome Beta live experiment Aug 2026 (billions
  of handshakes); declared MTC the preferred PQ web-PKI path.

For a public-facing popup-node dashboard, two paths now exist:

1. **Self-hosted MTC CA (shipped)** — `qrl-2.0/tools/qrl-mtc-ca/`
   batches certs, signs tree heads with ML-DSA-87, emits per-leaf
   inclusion proofs. Pin-by-pubkey trust (SSH-style). No waiting.
   See [docs/mtc-ca.html](https://pq-cybarg.github.io/qrl-popup-nodes/mtc-ca.html).
2. **Cloudflare / Let's Encrypt MTC in Q1 2027** if you need a
   browser-root-trusted CA; then plug an MTC-aware ACME client into
   `/api/tls/gen-cert`.

For localhost dev, the hybrid detached-sig flow
(`gen-tls-cert.sh --mldsa`) is already strictly stronger than
ECDSA-only — no MTC needed at all.

## Tracking

- #93 — superseded by MTC path (Cloudflare / Let's Encrypt Q1 2027)
- #94 — [patches/qrysm-pq-tls/](https://github.com/pq-cybarg/qrl-popup-nodes/tree/main/patches/qrysm-pq-tls) — drop-in patch ready, pending upstream submission
