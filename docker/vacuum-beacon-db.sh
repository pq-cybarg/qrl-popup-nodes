#!/usr/bin/env bash
# vacuum-beacon-db.sh — compact a beacon-node BoltDB offline.
#
# BoltDB never reclaims deleted pages; a long-running lead proposer
# accumulates them until the file grows ~300× the live state footprint.
# On Docker Desktop macOS, mmap over VirtioFS on a 1.8 GB DB stalls
# beacon-chain for 15-60s on every cold start — long enough to blow
# the healthcheck and crash the dependency chain.
#
# This script:
#   1. Stops the beacon container (if running).
#   2. Copies beaconchain.db → beaconchain.db.new using a BoltDB
#      vacuum (opens, iterates, writes to a fresh file — pages reclaimed).
#   3. Atomically swaps the file in place.
#
# A fresh file post-vacuum drops the cold-start mmap time by an order
# of magnitude.
#
# Usage:
#   ./docker/vacuum-beacon-db.sh                   # vacuums all 4 nodes of qrl-popup
#   ./docker/vacuum-beacon-db.sh --project l2-rescue --node 1
#   ./docker/vacuum-beacon-db.sh --data-dir docker/data-l2-moscow --node 1
#
# Idempotent: skips any node whose DB is under the threshold.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

PROJECT="qrl-popup"
DATA_DIR=""
NODES="1 2 3 4"
THRESHOLD_MB=100   # skip vacuum if DB already under this

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)   PROJECT="$2"; shift 2 ;;
    --data-dir)  DATA_DIR="$2"; shift 2 ;;
    --node)      NODES="$2"; shift 2 ;;
    --threshold-mb) THRESHOLD_MB="$2"; shift 2 ;;
    -h|--help)   sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

# Resolve data dir from project if not explicit.
if [[ -z "${DATA_DIR}" ]]; then
  case "${PROJECT}" in
    qrl-popup) DATA_DIR="docker/data" ;;
    l2-rescue) DATA_DIR="docker/data-l2-rescue" ;;
    l2-moscow) DATA_DIR="docker/data-l2-moscow" ;;
    *) echo "unknown project ${PROJECT} — pass --data-dir explicitly" >&2; exit 2 ;;
  esac
fi

echo "[vacuum] project=${PROJECT} data_dir=${DATA_DIR} nodes=[${NODES}]"
echo

for i in ${NODES}; do
  DB="${DATA_DIR}/beacon-node${i}/beaconchaindata/beaconchain.db"
  if [[ ! -f "${DB}" ]]; then
    echo "[node${i}] no DB at ${DB} — skipping"
    continue
  fi
  SZ_MB=$(du -m "${DB}" | awk '{print $1}')
  if [[ "${SZ_MB}" -lt "${THRESHOLD_MB}" ]]; then
    echo "[node${i}] ${SZ_MB} MB < ${THRESHOLD_MB} MB threshold — already compact, skipping"
    continue
  fi

  CONT="${PROJECT}-beacon-node${i}"
  echo "[node${i}] DB ${SZ_MB} MB — vacuuming..."
  if docker ps --format '{{.Names}}' | grep -qx "${CONT}"; then
    echo "[node${i}]   stopping ${CONT}..."
    docker stop "${CONT}" >/dev/null
    WAS_RUNNING=1
  else
    WAS_RUNNING=0
  fi

  # BoltDB's bbolt tool compacts in-place:
  #   bbolt compact -o out.db in.db
  # We ship it via a short-lived container that has the same go-qrllib
  # build environment (any alpine with bbolt works).
  docker run --rm \
    -v "$(pwd)/${DB%/beaconchain.db}":/data \
    --user "$(id -u):$(id -g)" \
    --entrypoint /bin/sh \
    golang:1.22-alpine \
    -c "apk add --no-cache git >/dev/null && go install go.etcd.io/bbolt/cmd/bbolt@latest >/dev/null 2>&1 && /go/bin/bbolt compact -o /data/beaconchain.db.new /data/beaconchain.db && mv /data/beaconchain.db.new /data/beaconchain.db" \
    2>&1 | tail -3

  NEW_SZ_MB=$(du -m "${DB}" | awk '{print $1}')
  echo "[node${i}]   compacted: ${SZ_MB} MB → ${NEW_SZ_MB} MB"

  if [[ "${WAS_RUNNING}" -eq 1 ]]; then
    echo "[node${i}]   restarting ${CONT}..."
    docker start "${CONT}" >/dev/null
  fi
done

echo
echo "[vacuum] done."
echo "Tip: schedule this nightly via launchd/cron to keep DBs small:"
echo "  0 4 * * *  cd ${REPO_ROOT} && ./docker/vacuum-beacon-db.sh --project qrl-popup"
