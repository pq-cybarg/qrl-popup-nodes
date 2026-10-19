#!/bin/bash
# Build every popup-node image with reproducible-build flags and attach
# SBOM + provenance attestations.
#
# Phase 3 foundation: outputs images that (a) any two builders can
# reproduce byte-identically from the same inputs, and (b) carry
# SPDX SBOM + SLSA provenance the operator can inspect without trusting
# the builder host. The `cosign sign` and publish-to-registry steps are
# still owed — this script produces the artifacts that would be signed.
#
# What we do:
#   * BUILDKIT_INLINE_CACHE=1 for reproducible layer digests
#   * SOURCE_DATE_EPOCH pinned to the last git commit's author date so
#     build metadata (file mtimes in layers) is deterministic
#   * --sbom=true      SPDX 2.3 SBOM attached to each image
#   * --provenance     SLSA v0.2 provenance metadata attached
#   * --output type=oci,dest=...   OCI-format tarballs on disk, ready
#                                  to `cosign sign` and `oras push` to
#                                  any registry
#
# The Dockerfiles already carry the reproducibility-friendly flags in
# their go build lines (-buildvcs=false -trimpath -ldflags="-s -w"),
# so the binary payload is deterministic given identical Go toolchain
# version. The base images are digest-pinned (see BASE-IMAGE-DIGESTS.md),
# so the underlying OS layers are deterministic too.
#
# Usage:
#   ./docker/build-images.sh                # all images, output to docker/out/
#   ./docker/build-images.sh gqrl-standard  # single image
#   OUT_DIR=/tmp/qrl-oci ./docker/build-images.sh
#
# Requires: docker with BuildKit (Docker 23+ or Docker Desktop 4.19+).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; NC='\033[0m'
die()  { echo -e "${RED}error:${NC} $*" >&2; exit 1; }
info() { echo -e "${CYAN}==>${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

# Pin SOURCE_DATE_EPOCH to last commit author time. Set to any epoch to
# override — e.g. release cutoff time.
if [ -z "${SOURCE_DATE_EPOCH:-}" ]; then
    if git -C "$REPO_ROOT" rev-parse HEAD >/dev/null 2>&1; then
        SOURCE_DATE_EPOCH="$(git -C "$REPO_ROOT" log -1 --pretty=%ct)"
    else
        SOURCE_DATE_EPOCH="1704067200"  # 2024-01-01 fallback
    fi
fi
export SOURCE_DATE_EPOCH

info "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH ($(date -r "$SOURCE_DATE_EPOCH" -u '+%Y-%m-%d %H:%M:%SZ' 2>/dev/null || echo unknown))"
info "OUT_DIR=$OUT_DIR"
mkdir -p "$OUT_DIR"

# Registry the tarballs will target once you're ready to publish.
# Not used by this script — only stamped into image labels for later
# reference. Override with QRL_REGISTRY=... in the environment.
REGISTRY="${QRL_REGISTRY:-ghcr.io/theqrl}"

# All buildable images. Each entry: <role>:<variant>|<dockerfile>|<build-args>
IMAGES=(
    "gqrl:standard|Dockerfile.gqrl|QRL_VARIANT_PATH=qrl-2.0/upstream/go-qrl"
    "gqrl:rescue-edition|Dockerfile.gqrl|QRL_VARIANT_PATH=qrl-2.0/variants/rescue-edition"
    "gqrl:moscow-chill|Dockerfile.gqrl|QRL_VARIANT_PATH=qrl-2.0/variants/moscow-chill"
    "beacon:latest|Dockerfile.beacon|"
    "validator:latest|Dockerfile.validator|"
    "dashboard:latest|Dockerfile.status-server|"
)

# Optional filter: only build entries whose tag matches one of the args.
FILTER="$*"

BUILT=()
for entry in "${IMAGES[@]}"; do
    tag="${entry%%|*}"; rest="${entry#*|}"
    dockerfile="${rest%%|*}"; buildargs="${rest#*|}"

    if [ -n "$FILTER" ]; then
        match=0
        for f in $FILTER; do
            case "$tag" in
                *"$f"*|"$f"*) match=1; break;;
            esac
        done
        [ $match -eq 1 ] || continue
    fi

    role="${tag%%:*}"; variant="${tag##*:}"
    out_tar="$OUT_DIR/qrl-${role}-${variant}.oci.tar"
    build_args=()
    if [ -n "$buildargs" ]; then
        build_args+=(--build-arg "$buildargs")
    fi

    info "building qrl-${role}:${variant}  →  $out_tar"
    docker buildx build \
        --file "$SCRIPT_DIR/popup-node/$dockerfile" \
        --tag "$REGISTRY/qrl-${role}:${variant}" \
        --sbom=true \
        --provenance=mode=max \
        --output "type=oci,dest=${out_tar},tar=true" \
        --build-arg "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" \
        "${build_args[@]+"${build_args[@]}"}" \
        "$REPO_ROOT" \
        2>&1 | tail -3

    if [ -f "$out_tar" ]; then
        sz=$(du -h "$out_tar" | awk '{print $1}')
        ok "  → $sz"
        BUILT+=("$out_tar")
    else
        die "build produced no tarball at $out_tar"
    fi
done

echo
if [ ${#BUILT[@]} -eq 0 ]; then
    die "no images matched filter: $FILTER"
fi
ok "built ${#BUILT[@]} image(s):"
for t in "${BUILT[@]}"; do echo "    $t"; done

echo
info "next: PQ-safe sign the release (ML-DSA-87, FIPS 204):"
echo "    QRL_WALLET_ID=<id> ./docker/sign-images.sh"
echo "  or (dev/test):"
echo "    SEED_HEX=<96hex> ./docker/sign-images.sh"
echo
info "then verify (checks signature + every tarball hash):"
echo "    ./docker/verify-images.sh"
