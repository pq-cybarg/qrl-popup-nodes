#!/bin/bash
# Sign every OCI tarball under docker/out/ with ML-DSA-87 (FIPS 204).
#
# PQ-safe release integrity — no ECDSA/Ed25519/RSA anywhere. Uses the
# same qrl-2.0/tools/mldsa87keygen the wallet stack uses, so the
# signature format is identical to what an on-chain address would sign.
#
# The signing target is a manifest — SHA256SUMS.txt — that lists each
# image tarball with its SHA-256 hash. One ML-DSA-87 signature covers
# every tarball in the release, plus the manifest is verifiable
# independently (grep the file, re-hash, compare — no crypto needed to
# spot-check).
#
# Cosign / Sigstore parity is a phase-3 tail (task #79 follow-up); when
# they ship ML-DSA-87 support we can layer on top.
#
# Usage:
#   ./docker/sign-images.sh                          # sign with wallet from qrl-wallet-cli
#   SEED_HEX=<96hex> ./docker/sign-images.sh         # sign directly with a raw seed
#                                                    (dev/test only)
#
# Prerequisites:
#   * OCI tarballs under docker/out/ (produced by build-images.sh)
#   * qrl-2.0/tools/mldsa87keygen/mldsa87keygen binary — auto-built if
#     missing
#
# Output (all under docker/out/):
#   SHA256SUMS.txt                — canonical manifest
#   SHA256SUMS.txt.qrlsig         — { address, publicKey, signature } as JSON
#   verify-images.sh companion    — reads both, verifies

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"
MLDSA_BIN="${QRL_MLDSA_BIN:-$REPO_ROOT/qrl-2.0/tools/mldsa87keygen/mldsa87keygen}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# ---------- pre-flight ----------

[ -d "$OUT_DIR" ] || die "no OCI tarballs at $OUT_DIR — run ./docker/build-images.sh first"

TARS=("$OUT_DIR"/*.oci.tar)
[ -e "${TARS[0]}" ] || die "no *.oci.tar files under $OUT_DIR"

if [ ! -x "$MLDSA_BIN" ]; then
    info "building mldsa87keygen..."
    (cd "$(dirname "$MLDSA_BIN")" && go build -buildvcs=false -o mldsa87keygen .) \
        || die "mldsa87keygen build failed"
fi

# ---------- manifest ----------

MANIFEST="$OUT_DIR/SHA256SUMS.txt"
info "generating $MANIFEST"
> "$MANIFEST"
for tar in "${TARS[@]}"; do
    name="$(basename "$tar")"
    hash=$(shasum -a 256 "$tar" | awk '{print $1}')
    printf '%s  %s\n' "$hash" "$name" >> "$MANIFEST"
    echo "  $hash  $name"
done
ok "manifest written: $(wc -l < "$MANIFEST" | tr -d ' ') entries"

# ---------- sign ----------

# ML-DSA-87 message = SHA-256 hash of the manifest bytes. Signing 32
# bytes is cheap; the manifest itself carries the file inventory.
MANIFEST_HASH_HEX=$(shasum -a 256 "$MANIFEST" | awk '{print $1}')
info "manifest hash (to be signed): $MANIFEST_HASH_HEX"

# Prefer explicit SEED_HEX from environment (CI / dev). Fall back to
# qrl-wallet-cli sign for interactive operator use.
if [ -n "${SEED_HEX:-}" ]; then
    warn "signing with SEED_HEX from environment (DEV/TEST ONLY)"
    SIG_JSON=$("$MLDSA_BIN" -sign -seed "$SEED_HEX" -message "$MANIFEST_HASH_HEX") \
        || die "mldsa87keygen -sign failed"
elif [ -n "${QRL_WALLET_ID:-}" ]; then
    info "signing via qrl-wallet-cli, walletId=$QRL_WALLET_ID"
    CLI="$REPO_ROOT/bin/qrl-wallet-cli.js"
    [ -x "$CLI" ] || die "qrl-wallet-cli.js not found at $CLI"
    # wallet-cli sign returns { address, publicKey, signature } (hex, no 0x).
    SIG_JSON=$("$CLI" sign --walletId "$QRL_WALLET_ID" --message "$MANIFEST_HASH_HEX") \
        || die "qrl-wallet-cli sign failed"
else
    die "provide QRL_WALLET_ID=<wallet-id> (via qrl-wallet-cli) or SEED_HEX=<96hex>"
fi

# ---------- write signature ----------

SIG_FILE="$OUT_DIR/SHA256SUMS.txt.qrlsig"
python3 -c "
import json, sys
sig = json.loads('''$SIG_JSON''')
out = {
    'algorithm': 'ML-DSA-87',
    'spec': 'FIPS 204',
    'signed_over': 'SHA-256(SHA256SUMS.txt)',
    'manifest_hash_hex': '$MANIFEST_HASH_HEX',
    'address': sig['address'],
    'publicKey': sig['publicKey'],
    'signature': sig['signature'],
}
print(json.dumps(out, indent=2))
" > "$SIG_FILE"
ok "signature written: $SIG_FILE"

# ---------- next steps ----------

echo
info "verify with:"
echo "    ./docker/verify-images.sh"
echo "  or:"
echo "    ./docker/verify-images.sh $OUT_DIR"
