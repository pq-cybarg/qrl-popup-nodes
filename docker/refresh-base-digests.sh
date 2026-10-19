#!/bin/bash
# Refresh the digest pins on every popup-node Dockerfile's base images.
#
# Run this on a monthly cadence (or when a CVE lands against a pinned
# base). Regenerates the sha256 pins from whatever the current tag
# points to upstream, then patches each Dockerfile in place. Diff the
# result and commit if the resulting bring-up still passes the phase 1
# convergence proof.
#
# Tags refreshed:
#   golang:1.25-alpine    → used as BUILDER_IMAGE in gqrl/beacon/validator
#   alpine:3.24           → used as RUNTIME_IMAGE in gqrl/beacon/validator
#                            (bumped from 3.20 to unlock OpenSSL 3.5+ ML-KEM)
#   node:22-alpine        → used as RUNTIME_IMAGE in status-server
#
# Requires: docker (any recent version); no external tools.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POPUP_DIR="$SCRIPT_DIR/popup-node"

declare -A TAGS=(
    ["golang:1.25-alpine"]="Dockerfile.gqrl Dockerfile.beacon Dockerfile.validator"
    ["alpine:3.24"]="Dockerfile.gqrl Dockerfile.beacon Dockerfile.validator"
    ["node:22-alpine"]="Dockerfile.status-server"
)

echo "==> pulling latest for each pinned tag..."
for tag in "${!TAGS[@]}"; do
    digest=$(docker pull "$tag" 2>&1 | grep -E "^Digest:" | head -1 | awk '{print $2}')
    if [ -z "$digest" ] || [[ "$digest" != sha256:* ]]; then
        echo "ERROR: could not resolve digest for $tag" >&2
        exit 1
    fi
    echo "  $tag  ->  $digest"

    for df in ${TAGS[$tag]}; do
        path="$POPUP_DIR/$df"
        [ -f "$path" ] || { echo "WARN: $path not found; skipping"; continue; }
        # Only touch `ARG XXX_IMAGE=<tag>...` lines — leave comments alone.
        # awk so we don't have to fight BSD-vs-GNU sed block-syntax differences.
        awk -v tag="$tag" -v digest="$digest" '
            /^ARG [A-Z_]*IMAGE=/ && index($0, tag) > 0 {
                # Strip only the digest attached to THIS tag (leave other
                # tags on other ARG lines untouched), then re-pin.
                sub(tag "@sha256:[a-f0-9]+", tag)
                sub(tag, tag "@" digest)
            }
            { print }
        ' "$path" > "$path.new" && mv "$path.new" "$path"
        # Verify the substitution took in an ARG line.
        grep -qE "^ARG [A-Z_]*IMAGE=${tag}@${digest}" "$path" || {
            echo "ERROR: pin substitution failed in $path" >&2
            exit 1
        }
        echo "    pinned in $df"
    done
done

echo
echo "==> done. Diff:"
git -C "$SCRIPT_DIR/.." diff --stat -- docker/popup-node/Dockerfile.* 2>/dev/null || true
echo
echo "Verify with a clean bring-up:"
echo "  ./configure.sh --force --regen-genesis"
echo "  qrlenv run -- docker compose up -d"
echo "  # wait for 4-node convergence, then:"
echo "  docker compose down -v"
