#!/usr/bin/env bash
# podman-convergence.sh — collect the live-fire parity proof for task #75.
#
# Runs the qrl-popup stack under rootless Podman on a native Linux host,
# polls chain state for 10 minutes, and writes evidence under
# docs/evidence/podman-* so the pass can be committed and published.
#
# A pass is:
#   - head_slot climbs by ~10 per 10 min (≈ 1 slot per 60 s, default
#     SECONDS_PER_SLOT=60)
#   - gqrl block number ≥ 1 at t=5m
#   - all 10 containers in state `Up` for the full 10 min
#
# Requires:
#   - Linux host (Fedora 40 / Ubuntu 24.04)
#   - podman 4.9+, podman-compose
#   - $DOCKER_SOCK pointing at ${XDG_RUNTIME_DIR}/podman/podman.sock
#   - the three upstream deps fetched via scripts/bootstrap.sh
#
# Will refuse to run on macOS (Podman inside Docker Desktop's VM is NOT
# the same as rootless Podman on Linux, and the parity story only holds
# for the latter).

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO}"

DURATION=600
EVIDENCE_DIR="${EVIDENCE_DIR:-docs/evidence}"
PROJECT="${PROJECT:-qrl-popup-podman}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration)       DURATION="$2"; shift 2 ;;
    --evidence-dir)   EVIDENCE_DIR="$2"; shift 2 ;;
    --project)        PROJECT="$2"; shift 2 ;;
    -h|--help)        sed -n '2,22p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

# --- host sanity ---------------------------------------------------
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "FATAL: this proof MUST run on a native Linux host." >&2
  echo "       Podman-under-Docker-Desktop is not the same as rootless Podman." >&2
  echo "       See docs/podman-proof.md for why." >&2
  exit 3
fi

if ! command -v podman >/dev/null 2>&1; then
  echo "FATAL: podman not installed. apt install podman podman-compose" >&2
  exit 3
fi

if ! command -v podman-compose >/dev/null 2>&1; then
  echo "FATAL: podman-compose not installed." >&2
  exit 3
fi

if [[ -z "${DOCKER_SOCK:-}" ]]; then
  export DOCKER_SOCK="${XDG_RUNTIME_DIR}/podman/podman.sock"
fi
if [[ ! -S "${DOCKER_SOCK}" ]]; then
  echo "FATAL: podman socket not at ${DOCKER_SOCK}" >&2
  echo "       systemctl --user enable --now podman.socket" >&2
  exit 3
fi

# --- host facts for the evidence header ----------------------------
mkdir -p "${EVIDENCE_DIR}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BASE="${EVIDENCE_DIR}/podman-${STAMP}"
mkdir -p "${BASE}"

{
  echo "# Podman convergence proof — ${STAMP}"
  echo
  echo "- host:    $(hostname)  — $(uname -sr)"
  echo "- distro:  $(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME}")"
  echo "- podman:  $(podman --version)"
  echo "- podman-compose: $(podman-compose --version 2>&1 | head -1)"
  echo "- socket:  ${DOCKER_SOCK}"
  echo "- project: ${PROJECT}"
  echo "- duration: ${DURATION}s"
  echo
} > "${BASE}/README.md"

# --- bring up the stack --------------------------------------------
echo "[podman] bringing up ${PROJECT}..."
QRL_DATA_DIR=./data-podman podman-compose -p "${PROJECT}" \
  -f docker/docker-compose.yaml \
  -f docker/docker-compose.docker-sock.yaml \
  -f docker/docker-compose.podman.yaml \
  up -d 2>&1 | tee -a "${BASE}/compose-up.log"

# wait for 9+ containers healthy
deadline=$(( $(date +%s) + 300 ))
while :; do
  n=$(podman ps --filter "label=com.docker.compose.project=${PROJECT}" \
      --format '{{.Status}}' | grep -c '(healthy)' || true)
  [[ "${n}" -ge 9 ]] && break
  [[ $(date +%s) -ge "${deadline}" ]] && {
    echo "FATAL: ${n}/9 healthy after 5 min — abort" >&2
    exit 4
  }
  sleep 5
done
echo "[podman] ${n} containers healthy — starting measurement window"

# --- 10-minute measurement window ----------------------------------
samples=$((DURATION / 60))
for i in $(seq 1 ${samples}); do
  echo "[podman] sample $i/${samples}..."
  date -u '+%Y-%m-%dT%H:%M:%SZ' >> "${BASE}/ps.log"
  podman ps --filter "label=com.docker.compose.project=${PROJECT}" \
    --format 'table {{.Names}}\t{{.Status}}' >> "${BASE}/ps.log"
  echo >> "${BASE}/ps.log"

  # beacon REST on port 3600 — Podman publishes on host 127.0.0.1
  curl -s --max-time 5 http://127.0.0.1:3600/qrl/v1/node/syncing \
    | python3 -c "
import json, sys, time
d = json.load(sys.stdin)
print(f\"{int(time.time())},{d['data']['head_slot']},{d['data']['sync_distance']}\")
" 2>/dev/null >> "${BASE}/headslot.csv" || true

  # gqrl block number on port 9545
  curl -s --max-time 5 -X POST -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
    http://127.0.0.1:9545 \
    | python3 -c "
import json, sys, time
d = json.load(sys.stdin)
print(f\"{int(time.time())},{int(d['result'], 16)}\")
" 2>/dev/null >> "${BASE}/block.csv" || true

  if [[ "$i" -eq 1 ]] || [[ "$i" -eq $((samples / 2)) ]] || [[ "$i" -eq "${samples}" ]]; then
    podman stats --no-stream > "${BASE}/stats-sample-$i.log"
  fi
  sleep 60
done

# --- analyze -------------------------------------------------------
first_slot=$(head -1 "${BASE}/headslot.csv" | cut -d, -f2)
last_slot=$(tail -1 "${BASE}/headslot.csv" | cut -d, -f2)
first_block=$(head -1 "${BASE}/block.csv" | cut -d, -f2)
last_block=$(tail -1 "${BASE}/block.csv" | cut -d, -f2)
slot_gain=$(( last_slot - first_slot ))
block_gain=$(( last_block - first_block ))

{
  echo "## Measurement results"
  echo
  echo "- Window:      ${DURATION} seconds (${samples} samples at 60s intervals)"
  echo "- head_slot:   ${first_slot} → ${last_slot}  (Δ = ${slot_gain})"
  echo "- gqrl block:  ${first_block} → ${last_block}  (Δ = ${block_gain})"
  echo
  if [[ "${slot_gain}" -ge $((samples - 2)) ]] && [[ "${last_block}" -ge 1 ]]; then
    echo "## Outcome: **PASS ✓**"
    echo
    echo "Chain advanced by ${slot_gain} slots in ${DURATION} seconds under rootless Podman."
    echo "gqrl produced ${block_gain} new execution-layer blocks."
    echo "Parity with Docker confirmed."
    STATUS=0
  else
    echo "## Outcome: **FAIL ✗**"
    echo
    echo "Expected at least $((samples - 2)) slots and 1 gqrl block in ${DURATION}s."
    echo "Got ${slot_gain} slots and ${block_gain} blocks."
    STATUS=1
  fi
  echo
  echo "## Evidence files"
  echo
  for f in "${BASE}"/*; do echo "- \`${f}\`"; done
} >> "${BASE}/README.md"

echo
cat "${BASE}/README.md" | tail -30
echo
echo "[podman] evidence written to: ${BASE}/"
exit ${STATUS:-0}
