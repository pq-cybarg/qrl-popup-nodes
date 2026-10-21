# qrysm PQ-TLS patch (task #94)

A drop-in extension that adds native post-quantum TLS termination to
the qrysm beacon-chain gRPC gateway, closing the one-hop plaintext
gap between the dashboard sidecar and the gateway handler.

## What it adds

Three new CLI flags on `beacon-chain`:

| flag              | purpose                                        |
|-------------------|------------------------------------------------|
| `--pq-tls-cert`   | Path to the TLS cert (PEM, X.509)              |
| `--pq-tls-key`    | Path to the TLS key (PEM)                      |
| `--pq-tls-kem`    | KEM to offer: `x25519mlkem768` (default) or `mlkem768` |
| `--pq-tls-mldsa-sig` | Path to the ML-DSA-87 detached signature over the cert DER (hybrid mode) |
| `--pq-tls-mldsa-pub` | Path to the ML-DSA-87 pinned public key for the hybrid signature |

When all four flags are set, the gRPC gateway terminates PQ-TLS natively;
otherwise it falls back to classical TLS (if the stock `--tls-cert` /
`--tls-key` are present) or plain TCP. No regression on upstream behavior.

## Files

- `pq_tls.go` — The TLS config wrapper. Imports the openssl-oqs-provider
  via cgo in a build-tagged file; falls back to Go's crypto/tls when the
  provider is absent.
- `pq_tls_integration_test.go` — End-to-end test: fire up two gateways,
  connect from a client linked against the same openssl, verify the
  cipher is X25519MLKEM768 and the cert validates (both ECDSA and
  ML-DSA-87 if the sidecar flags are set).
- `cmd_patch.diff` — Unified diff adding the four flags to
  `qrysm/cmd/beacon-chain/beacon.go` and wiring them into the gateway
  construction in `qrysm/beacon-chain/gateway/gateway.go`.

## Dependencies

- Go 1.22+ (same as qrysm).
- openssl 3.2+ with `openssl-oqs-provider` built against the same ABI.
  On a Debian/Ubuntu host: `apt install libssl3t64 oqs-provider` once
  the oqs-provider is in main repos (currently in `experimental`;
  self-build until then).
- `go-qrllib` (already a qrysm dep for ML-DSA-87 wallet ops).

## Applying

```sh
# 1. Clone qrysm at the pinned commit (see scripts/bootstrap.sh)
cd qrl-2.0/upstream/qrysm
git checkout 2c8cf1b6e37bebc63dd355debe182b81391bdcbb

# 2. Copy the extension file into place
cp /path/to/patches/qrysm-pq-tls/pq_tls.go beacon-chain/gateway/

# 3. Apply the CLI + wire-up diff
git apply /path/to/patches/qrysm-pq-tls/cmd_patch.diff

# 4. Build + run
go build -tags oqs -o beacon-chain ./cmd/beacon-chain
./beacon-chain \
    --pq-tls-cert /etc/qrl/tls/cert.pem \
    --pq-tls-key  /etc/qrl/tls/key.pem \
    --pq-tls-kem  x25519mlkem768 \
    --pq-tls-mldsa-sig /etc/qrl/tls/cert.pem.mldsa.sig \
    --pq-tls-mldsa-pub /etc/qrl/tls/mldsa.pub \
    ... (other beacon flags)
```

## Testing

```sh
# gRPC gateway now serves on HTTPS with the PQ handshake
curl -sk --tls-max 1.3 \
    --curves X25519MLKEM768 \
    https://127.0.0.1:3600/qrl/v1/node/syncing

# or via s_client to see the negotiated params:
openssl s_client \
    -connect 127.0.0.1:3600 \
    -groups X25519MLKEM768 \
    -tls1_3 </dev/null 2>&1 | grep -E 'Server Temp Key|Cipher is'
```

Expected: `Server Temp Key: X25519MLKEM768` and `Cipher is TLS_AES_256_GCM_SHA384`.

## Why a patch and not shipped in-tree

The repo keeps qrysm as a pinned upstream dependency (fetched via
`scripts/bootstrap.sh` at a known commit). Shipping the patch here
rather than vendoring a forked qrysm lets operators:

1. Verify the exact delta against upstream.
2. Submit the patch upstream (<https://github.com/theQRL/qrysm/pulls>)
   with no "but you forked qrysm" friction.
3. Pick it up on the next `scripts/bootstrap.sh` once accepted upstream.

## Upstream tracking

See [docs/ml-dsa-87-ml-kem-migration.md](../../docs/ml-dsa-87-ml-kem-migration.md)
for the broader PQ-TLS story and the parallel task #93 (ML-DSA-87 X.509
cert signature algorithm).

The ML-DSA-87 cert signature in #93 is composable with this patch:
when the Go `crypto/x509` upstream patch (#93) lands, this file picks
it up via `crypto/x509.MLDSA87WithSHA512` without further changes.
Until then, the hybrid detached-signature mode (above) covers the gap.
