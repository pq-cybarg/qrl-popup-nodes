#!/usr/bin/env bash
# Wait for an epoch transition on the beacon then diff the audit log
# for new syscall numbers. Epoch processing fires expensive paths
# that don't run per-slot (shuffling, participation flag flipping,
# finality calc) and historically needs futex_waitv.
set -Eeuo pipefail
: "${QRL_SOAK_PROJECT:=qrl-popup}"
: "${QRL_BEACON_REST:=http://127.0.0.1:3600}"

epoch_at_slot() {
  curl -s --max-time 10 "${QRL_BEACON_REST}/qrl/v1/beacon/headers/head" \
    | python3 -c "import json,sys; d=json.load(sys.stdin); slot=int(d['data']['header']['message']['slot']); print(slot // 128)"
}

start_epoch=$(epoch_at_slot)
[[ -n "${start_epoch}" ]] || { echo "cannot read beacon head"; exit 1; }
# an epoch is 128 slots × SECONDS_PER_SLOT; cap wait at 15m
deadline=$(( $(date +%s) + 900 ))
while :; do
  cur=$(epoch_at_slot || echo "${start_epoch}")
  [[ "${cur}" -gt "${start_epoch}" ]] && { echo "epoch transition ${start_epoch} → ${cur}"; break; }
  [[ $(date +%s) -ge "${deadline}" ]] && { echo "no epoch transition in 15m"; exit 1; }
  sleep 15
done

# scan each beacon log for seccomp or EPERM since this script started
since="$(date -u -v-20M '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '-20 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
for c in $(docker ps --filter "label=com.docker.compose.project=${QRL_SOAK_PROJECT}" --format '{{.Names}}' | grep beacon); do
  hits=$(docker logs --since "${since}" "${c}" 2>&1 \
    | grep -E '(EPERM|Operation not permitted|seccomp|audit:[^ ]* type=1326)' || true)
  [[ -z "${hits}" ]] || { echo "epoch-boundary seccomp hit in ${c}:"; echo "${hits}" | head -20; exit 1; }
done
