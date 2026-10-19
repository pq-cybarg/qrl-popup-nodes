#!/bin/bash
# Consumer-side counterpart to release.sh.
#
# Takes a signed release tarball produced by release.sh (or downloaded
# from a QRL release channel), verifies its signature + all image hashes,
# and loads every OCI image tarball into the local container engine
# (docker or podman). Refuses to load anything unless verification
# passes end-to-end.
#
# Usage:
#   ./docker/load-release.sh <tarball>                          # docker load
#   CONTAINER_ENGINE=podman ./docker/load-release.sh <tarball>  # podman load
#   REGISTRY=my.local/qrl ./docker/load-release.sh <tarball>    # retag on load
#   PINNED_PUBKEY=<hex> ./docker/load-release.sh <tarball>      # enforce pubkey match
#
# By default the bundled verifier prints the signer pubkey and asks the
# operator to cross-reference against a pinned trusted key. Automation
# should pass PINNED_PUBKEY=<hex> which makes load-release.sh abort if
# the signature's pubkey doesn't match — no interactive confirmation.

set -euo pipefail

TAR="${1:-}"
[ -n "$TAR" ] && [ -f "$TAR" ] || {
    echo "usage: $0 <release-tarball>" >&2
    echo "  e.g. $0 qrl-popup-release-v0.4.0.tar" >&2
    exit 1
}
TAR="$(cd "$(dirname "$TAR")" && pwd)/$(basename "$TAR")"

ENGINE="${CONTAINER_ENGINE:-docker}"
REGISTRY="${REGISTRY:-ghcr.io/theqrl}"
PINNED_PUBKEY="${PINNED_PUBKEY:-}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
warn() { echo -e "${YELLOW}!!!${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# ---------- pre-flight ----------

command -v "$ENGINE" >/dev/null 2>&1 || die "$ENGINE not found (set CONTAINER_ENGINE=docker or =podman)"

# Extract into a private temp dir. Removed on exit; cleaner than
# leaving loose files under /tmp when the operator re-runs.
EXTRACT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/qrl-release-XXXXXXXX")"
trap 'rm -rf "$EXTRACT_DIR"' EXIT

info "extracting $TAR → $EXTRACT_DIR"
tar -xf "$TAR" -C "$EXTRACT_DIR"

# Bundle root is the sole directory the tarball unpacks into.
BUNDLE_DIR=$(find "$EXTRACT_DIR" -mindepth 1 -maxdepth 1 -type d | head -1)
[ -d "$BUNDLE_DIR" ] || die "tarball extracted no directory — invalid bundle"

VERIFIER="$BUNDLE_DIR/verify/verify-release.sh"
[ -x "$VERIFIER" ] || die "bundle missing verify/verify-release.sh"

# ---------- pubkey enforcement (before verify) ----------

if [ -n "$PINNED_PUBKEY" ]; then
    info "enforcing PINNED_PUBKEY match (non-interactive)"
    SIG_FILE="$BUNDLE_DIR/SHA256SUMS.txt.qrlsig"
    [ -f "$SIG_FILE" ] || die "bundle missing SHA256SUMS.txt.qrlsig"
    ACTUAL_PUBKEY=$(python3 -c "
import json
d = json.load(open('$SIG_FILE'))
print(d['publicKey'])
")
    if [ "$ACTUAL_PUBKEY" != "$PINNED_PUBKEY" ]; then
        die "pubkey mismatch — release signed by unknown key
  pinned:   ${PINNED_PUBKEY:0:32}...${PINNED_PUBKEY: -32}
  release:  ${ACTUAL_PUBKEY:0:32}...${ACTUAL_PUBKEY: -32}"
    fi
    ok "pubkey matches pinned trusted key"
fi

# ---------- verify ----------

info "verifying release (signature + hash-inventory)"
if ! "$VERIFIER"; then
    die "verification FAILED — refusing to load images
       do NOT run any container from this bundle; the artifacts are
       tampered with or the signature is invalid"
fi
ok "release verified"

# ---------- load ----------

info "loading images into $ENGINE"

LOADED=()
for tar in "$BUNDLE_DIR"/qrl-*.oci.tar; do
    [ -f "$tar" ] || continue
    name="$(basename "$tar" .oci.tar)"
    # Derive role and variant from filename: qrl-<role>-<variant>.oci.tar
    role_var="${name#qrl-}"
    role="${role_var%%-*}"
    variant="${role_var#*-}"

    tag="$REGISTRY/qrl-${role}:${variant}"
    info "  loading $tar → $tag"

    # `docker load` reads OCI tar via containerd storage snapshotter in
    # newer versions; `podman load` uses OCI natively. Both output the
    # loaded image ID/tag on stdout.
    load_out=$("$ENGINE" load -i "$tar" 2>&1) || die "$ENGINE load failed for $tar
$load_out"

    # If REGISTRY differs from what the tarball baked in, retag.
    # Load output is like: "Loaded image: ghcr.io/theqrl/qrl-gqrl:standard"
    orig_ref=$(printf '%s' "$load_out" | sed -n 's/^Loaded image: *//p' | head -1)
    if [ -n "$orig_ref" ] && [ "$orig_ref" != "$tag" ]; then
        "$ENGINE" tag "$orig_ref" "$tag" || warn "  retag failed: $orig_ref → $tag"
        info "    retagged: $orig_ref → $tag"
    fi
    LOADED+=("$tag")
done

if [ ${#LOADED[@]} -eq 0 ]; then
    die "no qrl-*.oci.tar found in bundle — is the tarball corrupt?"
fi

# ---------- done ----------

echo
ok "loaded ${#LOADED[@]} image(s):"
for t in "${LOADED[@]}"; do echo "    $t"; done

echo
info "next: bring up the popup-node stack"
echo "    cd docker/ && ./configure.sh && qrlenv run -- $ENGINE compose up"
echo "  or for podman:"
echo "    cd docker/ && ./configure.sh && qrlenv run -- podman compose \\"
echo "        -f docker-compose.yaml -f docker-compose.podman.yaml up"
