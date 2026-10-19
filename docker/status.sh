#!/bin/bash
# One-command health snapshot of a running popup-node stack.
#
# Aggregates:
#   * docker/podman ps state (started / healthy / restarting / dead)
#   * gqrl block heights + latest block hash (4 nodes)
#   * beacon peer count + syncing state + head slot (4 beacons)
#   * dashboard HTTP reachability
#
# Prints a compact status table. Exits 0 if all four gqrl agree on
# block height AND all beacons are peered (activePeers >= 3), else
# exits 1 (useful in CI / watchdog scripts).
#
# Usage:
#   ./docker/status.sh                       # default: 4-node local compose
#   ./docker/status.sh --watch               # refresh every 5s until Ctrl-C
#   CONTAINER_ENGINE=podman ./docker/status.sh

set -euo pipefail

WATCH=0
[ "${1:-}" = "--watch" ] && WATCH=1
[ "${1:-}" = "-w" ] && WATCH=1

ENGINE="${CONTAINER_ENGINE:-docker}"
GQRL_PORTS=(9545 9546 9547 9548)
BEACON_GW_PORTS=(3600 3601 3602 3603)
DASHBOARD_PORT="${QRL_DASHBOARD_PORT:-8080}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

emit() {
    echo -e "==================================================="
    date -u +"popup-node status @ %Y-%m-%dT%H:%M:%SZ"
    echo -e "==================================================="

    # ---- container health ----
    echo
    echo -e "${CYAN}containers:${NC}"
    local ps_out
    ps_out=$("$ENGINE" ps --filter label=com.docker.compose.project=qrl-popup \
             --format '{{.Names}}\t{{.Status}}' 2>/dev/null || true)
    if [ -z "$ps_out" ]; then
        echo -e "  ${RED}✗ no popup-node containers running${NC} (bring up: qrlenv run -- docker compose up -d)"
        return 1
    fi
    printf '%s\n' "$ps_out" | sort | while IFS=$'\t' read -r name status; do
        case "$status" in
            *healthy*)    printf "  ${GREEN}✓${NC} %-22s %s\n" "$name" "$status" ;;
            *starting*|*health:*) printf "  ${YELLOW}∘${NC} %-22s %s\n" "$name" "$status" ;;
            *unhealthy*|*Restarting*|*Exited*) printf "  ${RED}✗${NC} %-22s %s\n" "$name" "$status" ;;
            *)            printf "    %-22s %s\n" "$name" "$status" ;;
        esac
    done

    # ---- gqrl block heights ----
    echo
    echo -e "${CYAN}gqrl (execution layer):${NC}"
    local heights=()
    for i in 0 1 2 3; do
        local port="${GQRL_PORTS[$i]}"
        local n=$((i + 1))
        local resp
        resp=$(curl -s --max-time 3 -X POST -H 'Content-Type: application/json' \
               --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \
               "http://127.0.0.1:$port" 2>/dev/null || true)
        local hex_bn dec_bn
        hex_bn=$(printf '%s' "$resp" | sed -n 's/.*"result":"\(0x[^"]*\)".*/\1/p')
        if [ -n "$hex_bn" ]; then
            dec_bn=$((hex_bn))
            heights+=("$dec_bn")
            printf "  node%d (port %d):  block=%d\n" "$n" "$port" "$dec_bn"
        else
            heights+=("?")
            printf "  ${RED}node%d (port %d):  unreachable${NC}\n" "$n" "$port"
        fi
    done
    # Convergence check: all heights equal
    local first="${heights[0]}"
    local converged=1
    for h in "${heights[@]}"; do
        [ "$h" != "$first" ] && converged=0
    done
    if [ "$first" = "?" ]; then
        echo -e "  ${YELLOW}⚠  convergence:  no nodes reachable${NC}"
        return 1
    elif [ $converged -eq 1 ]; then
        echo -e "  ${GREEN}✓  convergence:  all 4 nodes at block $first${NC}"
    else
        echo -e "  ${YELLOW}⚠  convergence:  heights diverge (${heights[*]})${NC}"
    fi

    # ---- beacon peers + sync ----
    echo
    echo -e "${CYAN}beacon (consensus layer):${NC}"
    local peer_ok=1
    for i in 0 1 2 3; do
        local port="${BEACON_GW_PORTS[$i]}"
        local n=$((i + 1))
        local container="qrl-beacon-node$n"
        # node1's port is published; followers need docker exec.
        local peers_resp sync_resp
        if [ "$n" = "1" ]; then
            peers_resp=$(curl -s --max-time 3 "http://127.0.0.1:$port/qrl/v1/node/peer_count" 2>/dev/null || true)
            sync_resp=$(curl -s --max-time 3 "http://127.0.0.1:$port/qrl/v1/node/syncing" 2>/dev/null || true)
        else
            peers_resp=$("$ENGINE" exec "$container" wget -qO- --timeout=3 "http://127.0.0.1:$port/qrl/v1/node/peer_count" 2>/dev/null || true)
            sync_resp=$("$ENGINE" exec "$container" wget -qO- --timeout=3 "http://127.0.0.1:$port/qrl/v1/node/syncing" 2>/dev/null || true)
        fi
        local connected head_slot is_syncing
        connected=$(printf '%s' "$peers_resp" | sed -n 's/.*"connected":"\([0-9]*\)".*/\1/p')
        head_slot=$(printf '%s' "$sync_resp" | sed -n 's/.*"head_slot":"\([0-9]*\)".*/\1/p')
        is_syncing=$(printf '%s' "$sync_resp" | sed -n 's/.*"is_syncing":\([a-z]*\).*/\1/p')

        if [ -z "$connected" ]; then
            printf "  ${RED}node%d (gw %d):  unreachable${NC}\n" "$n" "$port"
            peer_ok=0
            continue
        fi
        [ "$connected" -ge 3 ] || peer_ok=0
        local sync_str="syncing=${is_syncing:-?}"
        [ -n "$head_slot" ] && sync_str="$sync_str head_slot=$head_slot"
        printf "  node%d (gw %d):  peers=%d  %s\n" "$n" "$port" "$connected" "$sync_str"
    done
    if [ $peer_ok -eq 1 ]; then
        echo -e "  ${GREEN}✓  all beacons at full-mesh (>= 3 peers)${NC}"
    else
        echo -e "  ${YELLOW}⚠  P2P mesh incomplete${NC}"
    fi

    # ---- dashboard ----
    echo
    echo -e "${CYAN}dashboard:${NC}"
    if curl -s --max-time 3 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$DASHBOARD_PORT/api/status" 2>/dev/null | grep -q '^200'; then
        echo -e "  ${GREEN}✓${NC} http://127.0.0.1:$DASHBOARD_PORT/  (HTTP 200)"
    else
        echo -e "  ${RED}✗${NC} http://127.0.0.1:$DASHBOARD_PORT/  (unreachable)"
    fi

    # Optional TLS check
    if curl -sk --max-time 3 -o /dev/null -w '%{http_code}' "https://127.0.0.1:${QRL_DASHBOARD_TLS_PORT:-8443}/api/status" 2>/dev/null | grep -q '^200'; then
        local group
        group=$(echo | openssl s_client -connect "127.0.0.1:${QRL_DASHBOARD_TLS_PORT:-8443}" -groups X25519MLKEM768 -tls1_3 -trace 2>&1 \
                 | sed -n 's/Negotiated TLS1.3 group: //p' | head -1)
        echo -e "  ${GREEN}✓${NC} https://127.0.0.1:${QRL_DASHBOARD_TLS_PORT:-8443}/  (HTTP 200, group=${group:-?})"
    fi

    # Return code: 0 if converged + full-mesh, 1 otherwise
    if [ $converged -eq 1 ] && [ $peer_ok -eq 1 ] && [ "$first" != "?" ]; then
        return 0
    fi
    return 1
}

if [ $WATCH -eq 1 ]; then
    while true; do
        clear
        emit || true
        sleep 5
    done
else
    emit
    exit $?
fi
