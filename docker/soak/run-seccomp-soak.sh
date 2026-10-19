#!/usr/bin/env bash
# run-seccomp-soak.sh — exercise long-running edge paths of popup-node
# containers against the pinned seccomp allowlist and fail fast on
# the first blocked syscall.
#
# A syscall blocked by the profile surfaces as EPERM inside the
# container and is almost always misdiagnosed as a logic bug. This
# harness runs each known slow-firing exercise in a loop until the
# requested duration elapses, scans every service log for seccomp
# block signatures, parses the host audit log for type=1326 records,
# and exits non-zero on the first miss — with a markdown report at
# docker/soak/last-run.md.
#
# Usage:
#   docker/soak/run-seccomp-soak.sh [--duration 14400] [--project qrl-popup]
#
# Env overrides:
#   QRL_SEC_PROFILE=/path/to/seccomp.json   (default: docker/seccomp-profile.json)
#   QRL_SOAK_ALLOW_DESTRUCTIVE=1            (unlocks reset + prune exercises)
#   QRL_SOAK_DASHBOARD_URL=http://127.0.0.1:8080

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}"

DURATION=14400
PROJECT="qrl-popup"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration) DURATION="$2"; shift 2 ;;
    --project)  PROJECT="$2";  shift 2 ;;
    -h|--help)
      sed -n '2,22p' "$0"
      exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

export QRL_SEC_PROFILE="${QRL_SEC_PROFILE:-docker/seccomp-profile.json}"
export QRL_SOAK_PROJECT="${PROJECT}"
export QRL_SOAK_DASHBOARD_URL="${QRL_SOAK_DASHBOARD_URL:-http://127.0.0.1:8080}"

if [[ ! -f "${QRL_SEC_PROFILE}" ]]; then
  echo "FATAL: seccomp profile not found at ${QRL_SEC_PROFILE}" >&2
  exit 1
fi

REPORT="${SCRIPT_DIR}/last-run.md"
LOG_DIR="${SCRIPT_DIR}/logs/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "${LOG_DIR}"
exec > >(tee -a "${LOG_DIR}/soak.log") 2>&1

START_EPOCH=$(date -u +%s)
END_EPOCH=$((START_EPOCH + DURATION))

echo "=== seccomp soak run ==="
echo "  profile:  ${QRL_SEC_PROFILE} (sha256: $(shasum -a 256 "${QRL_SEC_PROFILE}" | awk '{print $1}'))"
echo "  project:  ${PROJECT}"
echo "  duration: ${DURATION}s (ends at $(date -u -r "${END_EPOCH}" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d @"${END_EPOCH}" '+%Y-%m-%dT%H:%M:%SZ'))"
echo "  logs:     ${LOG_DIR}"
echo "  dashboard: ${QRL_SOAK_DASHBOARD_URL}"
echo

# --- bring the stack up with the seccomp overlay ------------------
COMPOSE_FILES=(
  -f docker/docker-compose.yaml
  -f docker/docker-compose.docker-sock.yaml
  -f docker/docker-compose.seccomp.yaml
)
export QRL_SEC_PROFILE
docker compose -p "${PROJECT}" "${COMPOSE_FILES[@]}" up -d
# wait for healthy — fail fast if the profile breaks startup
echo "waiting for healthy containers…"
deadline=$(( $(date +%s) + 300 ))
while :; do
  n_healthy=$(docker ps --filter "label=com.docker.compose.project=${PROJECT}" \
    --format '{{.Status}}' | grep -c '(healthy)' || true)
  [[ "${n_healthy}" -ge 9 ]] && break
  [[ $(date +%s) -ge "${deadline}" ]] && {
    echo "FATAL: ${n_healthy}/9 healthy after 5 min — profile likely blocking a boot syscall" >&2
    "${SCRIPT_DIR}/parse-audit.sh" "${PROJECT}" > "${LOG_DIR}/audit-at-boot-fail.txt" || true
    exit 3
  }
  sleep 5
done
echo "  OK — ${n_healthy} containers healthy"

# --- exercises ----------------------------------------------------
EXERCISES=(
  "01-chain-snapshot.sh"
  "02-release-load.sh"
  "03-full-reset.sh"
  "04-token-index-rebuild.sh"
  "05-pq-key-regen.sh"
  "06-epoch-boundary-wait.sh"
  "07-ancient-prune.sh"
)

SEC_BLOCK_RX='(EPERM|Operation not permitted|operation not permitted|seccomp|audit:[^ ]* type=1326)'

iter=0
first_fail=""
while :; do
  iter=$((iter + 1))
  echo
  echo "=== iteration ${iter} (elapsed $(( $(date +%s) - START_EPOCH ))s) ==="
  for ex in "${EXERCISES[@]}"; do
    path="${SCRIPT_DIR}/exercises/${ex}"
    [[ -x "${path}" ]] || { echo "WARN: ${ex} missing or not executable"; continue; }
    echo "--- ${ex} ---"
    if ! bash "${path}" 2>&1 | tee "${LOG_DIR}/iter${iter}-${ex}.log"; then
      first_fail="exercise ${ex} returned non-zero in iteration ${iter}"
      break 2
    fi
  done

  # scan every service log for a block signature since this iteration began
  since="$(date -u -v-30S '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '-30 seconds' '+%Y-%m-%dT%H:%M:%SZ')"
  for c in $(docker ps --filter "label=com.docker.compose.project=${PROJECT}" --format '{{.Names}}'); do
    hits=$(docker logs --since "${since}" "${c}" 2>&1 | grep -E "${SEC_BLOCK_RX}" || true)
    if [[ -n "${hits}" ]]; then
      echo "SECCOMP BLOCK HIT in ${c}:"
      echo "${hits}" | head -20
      first_fail="seccomp block pattern in ${c} at iter ${iter}"
      break 2
    fi
  done

  # scan host audit (if available)
  if "${SCRIPT_DIR}/parse-audit.sh" "${PROJECT}" > "${LOG_DIR}/iter${iter}-audit.txt" 2>/dev/null; then
    if grep -q 'type=1326' "${LOG_DIR}/iter${iter}-audit.txt"; then
      echo "AUDIT type=1326 (seccomp) hit — see ${LOG_DIR}/iter${iter}-audit.txt"
      first_fail="audit type=1326 at iter ${iter}"
      break
    fi
  fi

  if [[ $(date +%s) -ge "${END_EPOCH}" ]]; then
    echo "duration elapsed cleanly"
    break
  fi
  sleep 30
done

STATUS=0
[[ -n "${first_fail}" ]] && STATUS=1

# --- report -------------------------------------------------------
{
  echo "# seccomp soak run — $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  echo
  echo "- profile: \`${QRL_SEC_PROFILE}\` sha256 \`$(shasum -a 256 "${QRL_SEC_PROFILE}" | awk '{print $1}')\`"
  echo "- project: \`${PROJECT}\`"
  echo "- duration: ${DURATION}s"
  echo "- iterations: ${iter}"
  echo "- outcome: $([[ ${STATUS} -eq 0 ]] && echo 'PASS ✓' || echo "FAIL — ${first_fail}")"
  echo "- logs: \`${LOG_DIR}\`"
  echo
  echo "## syscalls hit during run"
  echo
  if [[ -s "${LOG_DIR}/iter${iter}-audit.txt" ]]; then
    echo '```'
    "${SCRIPT_DIR}/parse-audit.sh" "${PROJECT}" | sort -u | head -200
    echo '```'
  else
    echo "_no audit source available on this host (needs auditd or journald --unit=audit)_"
  fi
} > "${REPORT}"

echo
echo "report: ${REPORT}"
exit ${STATUS}
