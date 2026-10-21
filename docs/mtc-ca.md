---
title: MTC CA (self-hosted)
nav_order: 13
---

# qrl-mtc-ca — self-hosted Merkle Tree Certificate authority

A minimum-viable implementation of
[`draft-ietf-plants-merkle-tree-certs-07`](https://datatracker.ietf.org/doc/draft-ietf-plants-merkle-tree-certs/)
that ships in this repo at
[`qrl-2.0/tools/qrl-mtc-ca/`](https://github.com/pq-cybarg/qrl-popup-nodes/tree/main/qrl-2.0/tools/qrl-mtc-ca).

**Operators who want MTC-authenticated PQ-TLS today don't need to
wait for Cloudflare / Let's Encrypt Q1 2027.** Run your own CA.

## Why MTC

Chrome's PQ web-PKI choice. Rather than put ML-DSA-87 directly in
X.509 (which inflates handshakes from ~2 KB to ~15-50 KB), the CA
batches many certs into a Merkle tree, PQ-signs **one** tree head
per batch, and servers send ~736-byte inclusion proofs on each
handshake. Full story in
[the migration doc](ml-dsa-87-ml-kem-migration.html).

## Semantics shipped here

- **Merkle tree** — RFC 6962 §2.1 construction with SHA-256; lone
  nodes promoted at odd levels (not paired-with-self Bitcoin-style).
- **Head signature** — ML-DSA-87 over a deterministic big-endian
  binary payload (`uint16 issuer_id_len || issuer_id || uint64
  batch_number || 32-byte tree_hash || uint64 leaf_count || uint64
  signed_at`). Matches MTC draft §5.2.
- **Inclusion proof** — ~log₂(batch_size) × 32 bytes. Verifier walks
  siblings with (leaf_index, tree_size) state so promoted lone
  nodes don't skew the walk.
- **Pubkey trust** — pin-by-pubkey (SSH `known_hosts` model). No CA
  root chain, so no Chrome-root program needed for operator-to-
  operator or consortium deployments.

Full RFC 6962 edge cases covered in `tree_test.go` (11 tree sizes
including 1, 2, 3, 7, 11, 100 for lone-node promotion coverage).

## Not Chrome-compatible out of the box

The IETF draft specifies a binary TLS extension codec; this tool
keeps the on-disk format as JSON for operator ergonomics. The signing
input (`canonicalBatchHeadBytes`) is already the draft-shape binary
payload, so a codec bridge is additive — the crypto doesn't change.

Browser-side verification is a follow-on. For operator-to-operator +
API-client trust, JSON works fine today.

## Lifecycle

```bash
cd qrl-2.0/tools/qrl-mtc-ca
go build -o qrl-mtc-ca .

# 1. Scaffold a CA
./qrl-mtc-ca init --state ./ca-state

# 2. Queue a leaf (one per cert) — subject_pubkey is an ML-DSA-87 pubkey
#    (e.g. from `mldsa87keygen` or from docker/gen-tls-cert.sh --mldsa).
cat > alice.leaf.json <<EOF
{
  "subject_type": 1,
  "subject_pubkey": "<2592-byte ML-DSA-87 pubkey, 5184 hex chars>",
  "san": ["alice.popup.local", "127.0.0.1"],
  "not_before": 1792000000,
  "not_after":  1823536000
}
EOF
./qrl-mtc-ca issue --state ./ca-state --leaf alice.leaf.json

# 3. Seal the batch — builds Merkle tree, signs head with ML-DSA-87,
#    emits per-leaf inclusion proofs.
./qrl-mtc-ca batch --state ./ca-state
# → ca-state/batches/1/head.json
# → ca-state/batches/1/leaves/leaf-000000.proof.json
# → ca-state/batches/latest -> 1

# 4. Serve to clients
./qrl-mtc-ca serve --state ./ca-state --listen 127.0.0.1:4600

# 5. Verify (client-side)
./qrl-mtc-ca verify \
    --head  ca-state/batches/1/head.json \
    --proof ca-state/batches/1/leaves/leaf-000000.proof.json \
    --ca-pub ca-state/ca.pub
# verify: VALID ✓
```

## End-to-end verification

The repo's own smoke test on the author's dev box:

```
[init] CA state at /tmp/mtc-smoke
[issue] queued (4 leaves)
[batch] batch 1 sealed under /tmp/mtc-smoke/batches/1/
         head.json          + ML-DSA-87 signature
         leaves/leaf-*.proof.json  — one per cert
[verify] verify: VALID ✓
   batch:      1  (issuer qrl-popup-mtc-v1)
   tree_hash:  e08c84614542b19dcd86c01698eb5ddd584acc28084f508427eda48f8c26d8cd
   leaf_idx:   0 / 4
   SAN:        [alice.popup.local 127.0.0.1]
```

`go test ./...` passes 7/7 — build / proof round-trip (across 11
tree sizes) / tampered-leaf / wrong-index / canonical-head bytes
determinism.

## Composition with the rest of the stack

- **go-qrllib** does the ML-DSA-87 crypto; the CA imports
  `wallet/ml_dsa_87` for keygen/sign/verify and the shared
  `descriptor` + `wallettype` packages.
- **mldsa87keygen** (`qrl-2.0/tools/mldsa87keygen/`) emits ML-DSA-87
  keypairs; the subject_pubkey field of a Leaf JSON is directly
  copy-pasteable from its JSON output.
- **gen-tls-cert.sh --mldsa** generates an ML-DSA-87 keypair and a
  detached signature over the ECDSA cert's DER. The same ML-DSA-87
  pubkey can be used as a Leaf's subject_pubkey.
- **patches/qrysm-pq-tls/pq_tls.go** already shells
  `mldsa87keygen -verify` on startup; swap the shell-out for a call
  to `verifyBatch` + a tree-head cache fetch and the qrysm beacon
  gRPC gateway speaks MTC-authenticated PQ-TLS natively.

## Dashboard integration (follow-up)

The dashboard's existing `/api/tls/gen-cert` can delegate to the
MTC CA with a small shim:

1. Operator starts a CA via `./qrl-mtc-ca init --state ...` once.
2. Dashboard config: `QRL_MTC_CA_URL=http://127.0.0.1:4600`,
   `QRL_MTC_CA_PUB=<pinned ca.pub>`.
3. Dashboard's cert-gen panel posts a Leaf JSON to the CA, waits for
   the next batch, pulls back the inclusion proof + head, exposes
   both to the server for use at TLS handshake time.

Not shipped yet — it's a dashboard refactor, not a CA refactor. The
CA is complete.

## Trust model and threat posture

- An attacker who steals `ca.seed` can forge batch heads. Treat it
  like a TLS private key: chmod 600, offline backup, Shamir split
  for multi-operator CAs.
- Tampered leaf JSON after batching fails the leaf-hash check — the
  inclusion proof pins exact canonical bytes.
- Serving a stale head alongside a fresh proof fails the signature
  check (new head's batch_number differs; old head's signature
  doesn't cover the new tree_hash).
- No revocation yet — short expiries + fast re-issue beat a CRL for
  the popup-node operator-to-operator use case. (MTC draft §9
  discusses a shorter batch interval + offline logged revocation if
  that becomes a hard requirement.)

## References

- [draft-ietf-plants-merkle-tree-certs-07](https://datatracker.ietf.org/doc/draft-ietf-plants-merkle-tree-certs/)
- [Cloudflare — Building a post-quantum CA with MTC](https://blog.cloudflare.com/pq-ca-with-mtcs/)
- [Let's Encrypt — PQ Future](https://letsencrypt.org/2026/06/03/pq-certs)
- Source tree: [`qrl-2.0/tools/qrl-mtc-ca/`](https://github.com/pq-cybarg/qrl-popup-nodes/tree/main/qrl-2.0/tools/qrl-mtc-ca)
