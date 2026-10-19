#!/bin/bash
# End-to-end release orchestrator for popup-node images.
#
# Chains every phase into one operator-facing command:
#
#   1. refresh-base-digests.sh    (opt-out with SKIP_DIGEST_REFRESH=1)
#      Pin every base image to its current upstream digest so this
#      release is reproducible bit-for-bit from these exact inputs.
#
#   2. build-images.sh
#      Rebuild every popup-node image with SOURCE_DATE_EPOCH pinned,
#      SBOM (SPDX 2.3) + provenance (SLSA v0.2) attached, output as
#      OCI tarballs under docker/out/.
#
#   3. sign-images.sh
#      Hash every tarball, write SHA256SUMS.txt, sign the manifest hash
#      with ML-DSA-87 (FIPS 204), write .qrlsig.
#
#   4. Bundle
#      Assemble docker/out/qrl-popup-release-<version>.tar with:
#        - Every *.oci.tar
#        - SHA256SUMS.txt + SHA256SUMS.txt.qrlsig
#        - A COPY of verify-images.sh + mldsa87keygen binary (statically
#          built for the target platform) so the release consumer can
#          verify offline with no repo checkout
#        - RELEASE.md (copied from docker/) with the trust story
#
# Usage:
#   ./docker/release.sh                                    # dev/test with SEED_HEX
#   VERSION=v0.4.0 ./docker/release.sh
#   QRL_WALLET_ID=<id> ./docker/release.sh                 # sign via wallet
#   SEED_HEX=$(python3 -c "print('a'*96)") ./docker/release.sh
#   SKIP_DIGEST_REFRESH=1 ./docker/release.sh              # skip digest refresh
#
# Prerequisites:
#   * Docker with BuildKit (Docker 23+ or Docker Desktop 4.19+)
#   * Either SEED_HEX or QRL_WALLET_ID set for signing
#   * qrlenv master key + docker/.env populated (configure.sh) if using QRL_WALLET_ID

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --always --dirty 2>/dev/null || echo "dev")}"
MLDSA_BIN="${QRL_MLDSA_BIN:-$REPO_ROOT/qrl-2.0/tools/mldsa87keygen/mldsa87keygen}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# ---------- pre-flight ----------

if [ -z "${SEED_HEX:-}" ] && [ -z "${QRL_WALLET_ID:-}" ]; then
    die "provide SEED_HEX=<96hex> (dev) or QRL_WALLET_ID=<id> (prod) — signing needs a key"
fi

info "release version: $VERSION"
info "output dir: $OUT_DIR"

# ---------- disk-space preflight ----------
# 6 OCI tarballs + intermediate build cache can hit 5GB on disk.
# Refuse to start if we're clearly going to run out. Bypass with
# QRL_ACK_LOW_DISK=1 or QRL_SKIP_DISK_PREFLIGHT=1.
if [ "${QRL_SKIP_DISK_PREFLIGHT:-0}" != "1" ]; then
    mkdir -p "$OUT_DIR"
    AVAIL_KB=$(df -Pk "$OUT_DIR" 2>/dev/null | awk 'NR==2 {print $4}')
    if [ -n "$AVAIL_KB" ]; then
        AVAIL_GB=$((AVAIL_KB / 1024 / 1024))
        info "disk free at $OUT_DIR: ${AVAIL_GB}GB"
        if [ "$AVAIL_KB" -lt 5242880 ]; then       # < 5GB — release needs ~2-3GB tarballs + 2GB build cache
            warn "less than 5GB free — release build likely to fail out-of-space"
            [ "${QRL_ACK_LOW_DISK:-0}" = "1" ] || \
                die "aborting; set QRL_ACK_LOW_DISK=1 to override, or free some disk"
        fi
    fi
fi

# ---------- 1. refresh digest pins ----------

if [ "${SKIP_DIGEST_REFRESH:-0}" = "1" ]; then
    warn "SKIP_DIGEST_REFRESH=1 — leaving base image digests as-is"
    warn "release will use whatever pins are currently in the Dockerfiles"
else
    info "step 1/4: refresh base image digest pins"
    "$SCRIPT_DIR/refresh-base-digests.sh" 2>&1 | sed 's/^/    /'
    ok "digest pins refreshed"
fi

# ---------- 2. build ----------

info "step 2/4: build all images (reproducible + SBOM + provenance)"
"$SCRIPT_DIR/build-images.sh" 2>&1 | sed 's/^/    /'
ok "images built"

# Assert every image made it
BUILT=("$OUT_DIR"/*.oci.tar)
[ -e "${BUILT[0]}" ] || die "no *.oci.tar under $OUT_DIR — build failure"
info "  ${#BUILT[@]} image(s) built"

# ---------- 3. sign ----------

info "step 3/4: sign release with ML-DSA-87 (FIPS 204)"
"$SCRIPT_DIR/sign-images.sh" 2>&1 | sed 's/^/    /'

[ -f "$OUT_DIR/SHA256SUMS.txt" ]        || die "SHA256SUMS.txt missing — sign failed"
[ -f "$OUT_DIR/SHA256SUMS.txt.qrlsig" ] || die "SHA256SUMS.txt.qrlsig missing — sign failed"
ok "release signed"

# ---------- 4. bundle ----------

info "step 4/4: bundle portable release tarball"

# Cross-compile mldsa87keygen for common consumer platforms so the
# release tarball is verifiable offline without a Go toolchain. We
# always include the host-platform build; add GOOS/GOARCH matrix for
# broader coverage as needed.
BUNDLE_DIR="$OUT_DIR/qrl-popup-release-$VERSION"
rm -rf "$BUNDLE_DIR"
mkdir -p "$BUNDLE_DIR/verify"

# Copy OCI tarballs + manifest + signature
for t in "${BUILT[@]}"; do
    cp -p "$t" "$BUNDLE_DIR/"
done
cp -p "$OUT_DIR/SHA256SUMS.txt"        "$BUNDLE_DIR/"
cp -p "$OUT_DIR/SHA256SUMS.txt.qrlsig" "$BUNDLE_DIR/"

# Copy verifier + PQ-crypto binary. Consumer runs
# ./verify/verify-release.sh from inside the bundle. The bundle-side
# verifier is a separate file (popup-node/verify-release.sh) with paths
# hardcoded for the bundle layout — cleaner than sed-rewriting the
# docker/verify-images.sh, which is authored for in-repo use.
cp -p "$SCRIPT_DIR/popup-node/verify-release.sh" "$BUNDLE_DIR/verify/verify-release.sh"
chmod +x "$BUNDLE_DIR/verify/verify-release.sh"

# Include the mldsa87keygen binary — build for the host platform.
# For a real release we'd cross-compile to linux/amd64 + linux/arm64
# + darwin/arm64 + windows/amd64. For now, ship host-native only and
# note the limitation.
if [ ! -x "$MLDSA_BIN" ]; then
    info "  building mldsa87keygen for host platform..."
    (cd "$(dirname "$MLDSA_BIN")" && go build -buildvcs=false -o mldsa87keygen .) \
        || die "mldsa87keygen build failed"
fi
cp -p "$MLDSA_BIN" "$BUNDLE_DIR/verify/mldsa87keygen"

# RELEASE.md for consumers
if [ -f "$SCRIPT_DIR/RELEASE.md" ]; then
    cp -p "$SCRIPT_DIR/RELEASE.md" "$BUNDLE_DIR/"
fi

# Manifest with metadata about the release itself (not the artifacts).
python3 -c "
import json, subprocess, os
git_hash = subprocess.check_output(['git','-C','$REPO_ROOT','rev-parse','HEAD']).decode().strip() if os.path.isdir('$REPO_ROOT/.git') else 'unknown'
d = {
    'version': '$VERSION',
    'git_commit': git_hash,
    'signature_algorithm': 'ML-DSA-87 (FIPS 204)',
    'sbom_format': 'SPDX 2.3',
    'provenance_format': 'SLSA v0.2',
    'bundle_contents': sorted(os.listdir('$BUNDLE_DIR')),
}
print(json.dumps(d, indent=2))
" > "$BUNDLE_DIR/RELEASE.json"

# Tar it up
TARBALL="$OUT_DIR/qrl-popup-release-$VERSION.tar"
tar -cf "$TARBALL" -C "$OUT_DIR" "$(basename "$BUNDLE_DIR")"
TARBALL_HASH=$(shasum -a 256 "$TARBALL" | awk '{print $1}')
TARBALL_SIZE=$(du -h "$TARBALL" | awk '{print $1}')
ok "release bundle: $TARBALL ($TARBALL_SIZE, sha256:$TARBALL_HASH)"

echo
info "distribute:"
echo "    $TARBALL"
echo
info "consumer verifies with:"
echo "    tar -xf qrl-popup-release-$VERSION.tar"
echo "    cd qrl-popup-release-$VERSION"
echo "    ./verify/verify-release.sh"
