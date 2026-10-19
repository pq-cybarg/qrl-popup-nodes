#!/usr/bin/env bash
# Takes a chain snapshot via the repo's compose-aware helper. Covers
# gqrl ancient reads + beacon state SSZ dump + tar.gz over a long
# fsync-heavy stream — the exact mix that trips an undersized
# seccomp allowlist on madvise/renameat2/getrandom.
set -Eeuo pipefail
: "${QRL_SOAK_PROJECT:=qrl-popup}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO}"
[[ -x docker/chain-snapshot.sh ]] || {
  echo "SKIP: docker/chain-snapshot.sh not present"; exit 0; }
OUT_DIR="docker/snapshots/soak-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "${OUT_DIR}"
QRL_SNAPSHOT_PROJECT="${QRL_SOAK_PROJECT}" \
  docker/chain-snapshot.sh --out "${OUT_DIR}" --no-prompt
# verify the snapshot deserialises — reading back exercises
# different syscalls than writing
latest=$(ls -1t "${OUT_DIR}"/*.tar.* 2>/dev/null | head -1)
[[ -n "${latest}" ]] || { echo "no snapshot produced"; exit 1; }
tar -tf "${latest}" >/dev/null
# cleanup — keep only the two newest soak snapshots to bound disk
ls -1t docker/snapshots/soak-* 2>/dev/null | tail -n +3 | xargs -r rm -rf
