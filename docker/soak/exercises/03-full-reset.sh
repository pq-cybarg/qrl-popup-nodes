#!/usr/bin/env bash
# Hits /api/compose/reset — stops every container in the project,
# wipes bind-mounted chain state, re-brings up. Covers docker socket
# writes under contention + rapid-fire unlink/rmdir syscalls.
#
# DESTRUCTIVE — refuses to run unless the operator opts in.
set -Eeuo pipefail
: "${QRL_SOAK_PROJECT:=qrl-popup}"
: "${QRL_SOAK_DASHBOARD_URL:=http://127.0.0.1:8080}"

if [[ "${QRL_SOAK_ALLOW_DESTRUCTIVE:-0}" != "1" ]]; then
  echo "SKIP: destructive — set QRL_SOAK_ALLOW_DESTRUCTIVE=1 to run"
  exit 0
fi

# snapshot first — reset wipes chain state
"$(dirname "$0")/01-chain-snapshot.sh"

code=$(curl -s -o /tmp/reset.json -w '%{http_code}' -X POST \
  "${QRL_SOAK_DASHBOARD_URL}/api/compose/reset" --max-time 120)
[[ "${code}" == "200" ]] || { echo "reset returned HTTP ${code}"; cat /tmp/reset.json; exit 1; }

# wait for the project to come back healthy
deadline=$(( $(date +%s) + 240 ))
while :; do
  n=$(docker ps --filter "label=com.docker.compose.project=${QRL_SOAK_PROJECT}" \
    --format '{{.Status}}' | grep -c '(healthy)' || true)
  [[ "${n}" -ge 9 ]] && break
  [[ $(date +%s) -ge "${deadline}" ]] && { echo "stack did not recover from reset"; exit 1; }
  sleep 5
done
