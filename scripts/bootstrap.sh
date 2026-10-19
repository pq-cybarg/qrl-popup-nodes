#!/usr/bin/env bash
# bootstrap.sh — one-shot setup after a fresh clone of qrl-popup-nodes.
#
# Fetches the three upstream dependencies that are not vendored in this
# repo (go-qrl, qrysm, go-qrllib) at the pinned commits, places them
# under qrl-2.0/upstream/, and runs a sanity check on the result.
#
# Idempotent — re-run after an upstream commit bump without penalty.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

UPSTREAM_DIR="qrl-2.0/upstream"
mkdir -p "${UPSTREAM_DIR}"

# Pinned upstream commits — bump deliberately, test the whole stack
# after bump via docker/reproducibility-check.sh.
declare -A UPSTREAMS=(
  [go-qrl]="https://github.com/theQRL/go-qrl.git b92884a3a1d5f03efc908c95b06eca4262debc47"
  [qrysm]="https://github.com/theQRL/qrysm.git 2c8cf1b6e37bebc63dd355debe182b81391bdcbb"
  [go-qrllib]="https://github.com/theQRL/go-qrllib.git 204c8f69b7e0da40a4e13156831ed63d6c73523b"
)

fetch_one() {
  local name="$1"; shift
  local url="$1"; shift
  local sha="$1"; shift
  local dest="${UPSTREAM_DIR}/${name}"

  if [[ -d "${dest}/.git" ]]; then
    echo "[bootstrap] ${name}: already present — fetching + checking out ${sha:0:12}"
    (cd "${dest}" && git fetch origin --tags --quiet && git checkout -q "${sha}")
  else
    echo "[bootstrap] ${name}: cloning ${url}"
    rm -rf "${dest}"
    git clone --quiet "${url}" "${dest}"
    (cd "${dest}" && git checkout -q "${sha}")
  fi
  echo "[bootstrap] ${name}: HEAD at $(cd "${dest}" && git rev-parse --short HEAD)"
}

for name in "${!UPSTREAMS[@]}"; do
  read -r url sha <<< "${UPSTREAMS[${name}]}"
  fetch_one "${name}" "${url}" "${sha}"
done

echo
echo "[bootstrap] upstream fetch complete."
echo
echo "Next steps:"
echo "  1. ./docker/configure.sh       # scaffold .env + build images + init chaindata"
echo "  2. qrlenv run -- docker compose -f docker/docker-compose.yaml \\"
echo "       -f docker/docker-compose.docker-sock.yaml up -d"
echo "  3. open http://127.0.0.1:8080/"
