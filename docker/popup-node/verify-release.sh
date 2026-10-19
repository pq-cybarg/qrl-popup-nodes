#!/bin/bash
# Verify a signed QRL popup-node release bundle.
#
# This script ships INSIDE the release tarball at verify/verify-release.sh.
# It has no repo dependency — the mldsa87keygen binary lives next to
# it under verify/, and SHA256SUMS.txt + .qrlsig are in the bundle root.
#
# Usage:  ./verify/verify-release.sh
#
# Runs from the bundle root (or anywhere — the script figures out its
# own location). Two-step verification:
#
#   Step 1: ML-DSA-87 signature over SHA-256(SHA256SUMS.txt) via the
#           bundled mldsa87keygen binary. Proves the manifest is
#           authentic to the signer's key.
#   Step 2: shasum -a 256 -c SHA256SUMS.txt over every listed image
#           tarball. Proves the artifacts weren't tampered with.
#
# Consumer MUST cross-reference the printed signer pubkey against a
# pinned trusted key published by the QRL release team. See RELEASE.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MLDSA_BIN="$SCRIPT_DIR/mldsa87keygen"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
bad()  { echo -e "${RED}✗${NC} $*"; }

MANIFEST="$BUNDLE_DIR/SHA256SUMS.txt"
SIG_FILE="$BUNDLE_DIR/SHA256SUMS.txt.qrlsig"

[ -f "$MANIFEST" ] || die "manifest missing at $MANIFEST"
[ -f "$SIG_FILE" ] || die "signature missing at $SIG_FILE"
[ -x "$MLDSA_BIN" ] || die "mldsa87keygen missing at $MLDSA_BIN (bundle corrupt?)"

# ---------- step 1: signature ----------

info "step 1: verifying ML-DSA-87 signature (FIPS 204) over $MANIFEST"

read -r ALGO ADDRESS PUBKEY SIGNATURE < <(python3 -c "
import json
d = json.load(open('$SIG_FILE'))
assert d.get('algorithm') == 'ML-DSA-87', 'unexpected algorithm: ' + str(d.get('algorithm'))
print(d['algorithm'], d['address'], d['publicKey'], d['signature'])
")

info "  algorithm:      $ALGO (FIPS 204)"
info "  signer address: $ADDRESS"
info "  signer pubkey:  ${PUBKEY:0:32}...${PUBKEY: -32}"
warn "  ↑ operator MUST cross-reference this pubkey against a pinned"
warn "    trusted key published by the QRL release team. See RELEASE.md."

MANIFEST_HASH_HEX=$(shasum -a 256 "$MANIFEST" | awk '{print $1}')
info "  manifest hash:  $MANIFEST_HASH_HEX"

if "$MLDSA_BIN" -verify -pubkey "$PUBKEY" -message "$MANIFEST_HASH_HEX" -signature "$SIGNATURE" 2>&1; then
    ok "  signature valid — manifest hasn't been tampered with"
else
    bad "  signature INVALID — manifest was tampered with, or wrong pubkey"
    exit 1
fi

# ---------- step 2: hash-inventory ----------

info "step 2: verifying every listed OCI tarball's SHA-256"

cd "$BUNDLE_DIR"
if shasum -a 256 -c SHA256SUMS.txt >/dev/null 2>&1; then
    ok "  all listed tarballs match their pinned SHA-256"
else
    bad "  hash mismatch — one or more tarballs are corrupted or replaced:"
    shasum -a 256 -c SHA256SUMS.txt 2>&1 | grep -E "FAIL|WARN" | sed 's/^/      /'
    exit 1
fi

echo
ok "release VERIFIED — signature valid + all image hashes match"
echo
info "trusted signer: $ADDRESS"
