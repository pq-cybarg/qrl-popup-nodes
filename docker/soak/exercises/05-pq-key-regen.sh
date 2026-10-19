#!/usr/bin/env bash
# Create a fresh PQ keypair via the dashboard — exercises go-qrllib
# ML-DSA-87 keygen (getrandom + large mmap for the stacked tree) and
# Argon2id (clone+rseq for parallel lanes). Argon2 in particular
# often needs rseq/membarrier allowed.
set -Eeuo pipefail
: "${QRL_SOAK_DASHBOARD_URL:=http://127.0.0.1:8080}"

PW="soak-$(date +%s)-$RANDOM"
code=$(curl -s -o /tmp/kg.json -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -d "{\"network\":\"zond-private\",\"password\":\"${PW}\"}" \
  "${QRL_SOAK_DASHBOARD_URL}/api/wallet/create" --max-time 90)
[[ "${code}" == "200" ]] || { echo "wallet/create returned HTTP ${code}"; cat /tmp/kg.json; exit 1; }

wid=$(python3 -c "import json,sys; print(json.load(open('/tmp/kg.json')).get('walletId') or '')")
[[ -n "${wid}" ]] || { echo "no walletId in response"; cat /tmp/kg.json; exit 1; }

# round-trip: unlock with the same password proves the keystore
# serialisation path (AES-GCM, scrypt or argon2) also stays
# un-blocked
code=$(curl -s -o /tmp/un.json -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -d "{\"walletId\":\"${wid}\",\"password\":\"${PW}\"}" \
  "${QRL_SOAK_DASHBOARD_URL}/api/wallet/unlock" --max-time 60)
[[ "${code}" == "200" ]] || { echo "wallet/unlock returned HTTP ${code}"; cat /tmp/un.json; exit 1; }
