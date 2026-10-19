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

`docker/gen-tls-cert.sh --hybrid-ecdsa-mldsa --cn localhost` emits a
cert whose primary signature is ECDSA (so every TLS stack accepts it)
with an **`altSignatureAlgorithm` extension** containing an ML-DSA-87
signature over the same TBS. Clients that understand the extension
verify the ML-DSA leg; clients that don't fall back to ECDSA.

## Long-term fix

Patch `crypto/x509` + `crypto/tls` in Go upstream. Prototype fork
exists at `qrl-2.0/upstream/go-qrllib/experimental/x509-pq/`. Needs a
2-day test-suite sweep before linking into production.

## Tracking

- #93 — Go proposal + `theQRL/qrysm#...` (TBD link)
- #94 — [theQRL/qrysm#78](https://github.com/theQRL/qrysm/issues/78)
