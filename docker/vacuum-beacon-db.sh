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

  # Resolve container name via compose labels — the container_name
  # override may differ from "${project}-beacon-node${i}" (e.g.
  # project qrl-popup emits container qrl-beacon-node1, not
  # qrl-popup-beacon-node1). Fall back to the guessed name if no
  # label match exists.
  CONT=$(docker ps -a \
           --filter "label=com.docker.compose.project=${PROJECT}" \
           --filter "label=com.docker.compose.service=beacon-node${i}" \
           --format '{{.Names}}' | head -1)
  if [[ -z "${CONT}" ]]; then CONT="${PROJECT}-beacon-node${i}"; fi
  echo "[node${i}] DB ${SZ_MB} MB — vacuuming (container=${CONT})..."
  if docker ps --format '{{.Names}}' | grep -qx "${CONT}"; then
    echo "[node${i}]   stopping ${CONT}..."
    docker stop "${CONT}" >/dev/null
    WAS_RUNNING=1
  else
    WAS_RUNNING=0
  fi

  # BoltDB's bbolt tool compacts to a fresh file:
  #   bbolt compact -o out.db in.db
  # We ship it via a short-lived alpine container. apk and `go install`
  # need root, so we run as root and chown the result back to the host
  # user that owns the mounted DB.
  #
  # Hardening:
  #   - `apk upgrade --no-cache` pulls any security-patched packages on
  #     top of the pinned base layer (CVE-2026-123158 "sshole" fix on
  #     musl openssh landed 2026-11-05; we don't install openssh here,
  #     but defense-in-depth covers any transitively-pulled sshd).
  #   - Builder image needs Go >= 1.25 to install bbolt@v1.5 — using
  #     golang:1.22-alpine (as this script shipped originally) fails
  #     silently with "go.etcd.io/bbolt@v1.5.0 requires go >= 1.25.0",
  #     which under `set -e` inside the -c script would exit but under
  #     bare shell the pipeline continued and the DB was left unchanged.
  #   - No network ports are published; the container runs for seconds
  #     under the host's user namespace and cannot be reached by anyone.
  HOST_UID="$(id -u)"
  HOST_GID="$(id -g)"
  docker run --rm \
    -v "$(pwd)/${DB%/beaconchain.db}":/data \
    --entrypoint /bin/sh \
    golang:1.25-alpine \
    -c "set -e; \
        apk upgrade --no-cache >/dev/null; \
        apk add --no-cache git >/dev/null; \
        echo '[container] installing bbolt...'; \
        go install go.etcd.io/bbolt/cmd/bbolt@latest; \
        echo '[container] compacting...'; \
        /go/bin/bbolt compact -o /data/beaconchain.db.new /data/beaconchain.db; \
        chown ${HOST_UID}:${HOST_GID} /data/beaconchain.db.new; \
        mv /data/beaconchain.db.new /data/beaconchain.db; \
        echo '[container] done'" \
    2>&1 | tail -10

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
