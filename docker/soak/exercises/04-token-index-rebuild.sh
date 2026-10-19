#!/usr/bin/env bash
# Rebuild the dashboard token index — large sequential block scan
# plus leveldb writes. Hits chunk-boundary code paths that may call
# syscalls (fallocate, pwritev2, sync_file_range) absent from a
# default allowlist.
set -Eeuo pipefail
: "${QRL_SOAK_DASHBOARD_URL:=http://127.0.0.1:8080}"

# No dedicated rebuild endpoint exists today — the dashboard's
# in-process indexer self-triggers on a 60s poll. Instead, invalidate
# the token cache by probing the explorer tab endpoints so the
# indexer runs on-demand against unindexed blocks.
#
# Known endpoints (status-server.js router):
#   GET  /api/explorer/tokens?network=zond-private
#   GET  /api/explorer/richlist?network=zond-private
#
# NOTE: If a `POST /api/explorer/rebuild-tokenindex` is added later,
# switch this to use it so the exercise forces a FULL rebuild rather
# than piggy-backing on the natural poll.
code=$(curl -s -o /tmp/tok.json -w '%{http_code}' \
  "${QRL_SOAK_DASHBOARD_URL}/api/explorer/tokens?network=zond-private" --max-time 60)
[[ "${code}" == "200" ]] || { echo "tokens endpoint returned HTTP ${code}"; exit 1; }
code=$(curl -s -o /tmp/rich.json -w '%{http_code}' \
  "${QRL_SOAK_DASHBOARD_URL}/api/explorer/richlist?network=zond-private" --max-time 60)
[[ "${code}" == "200" ]] || { echo "richlist endpoint returned HTTP ${code}"; exit 1; }
