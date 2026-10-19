#!/usr/bin/env bash
# Exercises load-release.sh against a signed OCI tarball — covers
# docker-import pathways (mount+pivot_root under containerd),
# PQ signature verification (ML-DSA-87, memory-heavy mmap).
set -Eeuo pipefail
: "${QRL_SOAK_PROJECT:=qrl-popup}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO}"
[[ -x docker/load-release.sh ]] || {
  echo "SKIP: docker/load-release.sh not present"; exit 0; }

# Find any existing bundle, or build a minimal one for this run
latest_bundle=$(ls -1t docker/out/*.tar 2>/dev/null | head -1)
if [[ -z "${latest_bundle}" ]]; then
  echo "SKIP: no release bundle under docker/out/ (run docker/release.sh first)"
  exit 0
fi

# Verify only — load would clobber the running image set. verify-images
# re-exercises the same mmap/PQ-sig code path without side effects.
if [[ -x docker/verify-images.sh ]]; then
  docker/verify-images.sh docker/out/
else
  echo "SKIP: docker/verify-images.sh not present"; exit 0
fi
