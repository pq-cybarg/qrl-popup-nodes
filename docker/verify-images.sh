#!/bin/bash
# Verify a signed release manifest under docker/out/.
#
# Two-step verification, matching how a release consumer would check:
#
#   1. Signature verify — ML-DSA-87 signature over SHA-256(SHA256SUMS.txt)
#      via qrl-2.0/tools/mldsa87keygen -verify. Public key comes from
#      the .qrlsig file (self-declared). Operator MUST cross-reference
#      the publicKey against a pinned trusted key (published on the
#      QRL release page / signed elsewhere) — this script prints the
#      pubkey so the operator can eyeball it against their pinned copy.
#
#   2. Hash verify — re-hash every .oci.tar and check against
#      SHA256SUMS.txt. `shasum -c` does this natively.
#
# Both must pass for the release to be trustworthy. This script prints
# clearly which step passed/failed.
#
# Usage:
#   ./docker/verify-images.sh              # verifies docker/out/
#   ./docker/verify-images.sh <dir>        # verifies specified dir

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${1:-$SCRIPT_DIR/out}"
MLDSA_BIN="${QRL_MLDSA_BIN:-$REPO_ROOT/qrl-2.0/tools/mldsa87keygen/mldsa87keygen}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
bad()  { echo -e "${RED}✗${NC} $*"; }

# ---------- pre-flight ----------

MANIFEST="$OUT_DIR/SHA256SUMS.txt"
SIG_FILE="$OUT_DIR/SHA256SUMS.txt.qrlsig"

[ -f "$MANIFEST" ] || die "manifest missing at $MANIFEST"
[ -f "$SIG_FILE" ] || die "signature missing at $SIG_FILE"

if [ ! -x "$MLDSA_BIN" ]; then
    info "building mldsa87keygen..."
    (cd "$(dirname "$MLDSA_BIN")" && go build -buildvcs=false -o mldsa87keygen .) \
        || die "mldsa87keygen build failed"
fi

# ---------- step 1: signature ----------

info "step 1: verifying ML-DSA-87 signature over $MANIFEST"

# Extract signer's declared pubkey + signature from the .qrlsig JSON.
read -r ALGO ADDRESS PUBKEY SIGNATURE < <(python3 -c "
import json
d = json.load(open('$SIG_FILE'))
assert d.get('algorithm') == 'ML-DSA-87', 'unexpected algorithm: ' + str(d.get('algorithm'))
print(d['algorithm'], d['address'], d['publicKey'], d['signature'])
")

info "  algorithm:  $ALGO (FIPS 204)"
info "  signer address: $ADDRESS"
info "  signer pubkey: ${PUBKEY:0:32}...${PUBKEY: -32}"
warn "  ↑ operator MUST cross-reference this pubkey against a pinned"
warn "    trusted key published by the QRL release team (else you're"
warn "    trusting the .qrlsig file at face value, which defeats the point)"

# Recompute the manifest hash and verify the signature covers it.
MANIFEST_HASH_HEX=$(shasum -a 256 "$MANIFEST" | awk '{print $1}')
info "  manifest hash: $MANIFEST_HASH_HEX"

if "$MLDSA_BIN" -verify -pubkey "$PUBKEY" -message "$MANIFEST_HASH_HEX" -signature "$SIGNATURE" 2>&1; then
    ok "  signature valid — manifest hasn't been tampered with"
else
    bad "  signature INVALID — manifest was tampered with, or wrong pubkey"
    exit 1
fi

# ---------- step 2: hash-inventory ----------

info "step 2: verifying every listed OCI tarball's SHA-256"

cd "$OUT_DIR"
if shasum -a 256 -c SHA256SUMS.txt >/dev/null 2>&1; then
    ok "  all listed tarballs match their pinned SHA-256"
else
    bad "  hash mismatch — one or more tarballs are corrupted or replaced:"
    shasum -a 256 -c SHA256SUMS.txt 2>&1 | grep -E "FAIL|WARN" | sed 's/^/      /'
    exit 1
fi

# ---------- done ----------

echo
ok "release VERIFIED — signature valid + all image hashes match"
echo
info "trusted signer: $ADDRESS"
