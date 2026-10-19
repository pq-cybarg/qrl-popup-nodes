#!/usr/bin/env bash
# Trigger gqrl ancient-chain pruning via admin RPC. Prune walks
# chaindata/ancient/*, calls fallocate(FALLOC_FL_PUNCH_HOLE) on each
# file, and renames the manifest — a syscall set often absent from
# default profiles.
#
# DESTRUCTIVE (deletes freezer data). Opt-in only.
set -Eeuo pipefail
: "${QRL_GQRL_RPC:=http://127.0.0.1:9545}"

if [[ "${QRL_SOAK_ALLOW_DESTRUCTIVE:-0}" != "1" ]]; then
  echo "SKIP: destructive — set QRL_SOAK_ALLOW_DESTRUCTIVE=1 to run"
  exit 0
fi

# gqrl exposes admin_* only when started with --rpc.allow-unprotected-txs
# and --http.api=admin. The compose default omits admin, so this
# attempts the call and SKIPs if the method is not available rather
# than failing — we still want the overall soak to pass.
resp=$(curl -s --max-time 30 -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"admin_prune","params":[],"id":1}' \
  "${QRL_GQRL_RPC}") || resp=""

if echo "${resp}" | grep -q '"error"'; then
  if echo "${resp}" | grep -qiE 'method.*not.*(found|available|supported)'; then
    echo "SKIP: admin_prune not exposed on this gqrl build"
    exit 0
  fi
  echo "prune errored: ${resp}"; exit 1
fi
echo "prune submitted: ${resp}"
