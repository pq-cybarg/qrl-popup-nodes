#!/bin/bash
# Wire full P2P mesh across the 4 gqrl nodes on the localnet.
# QRL uses qnr (QNR — QRL Node Records) instead of enode; qnr is discoverable via
# admin_nodeInfo on each node's HTTP RPC. This script fetches each node's qnr and
# feeds it to every other node via admin_addPeer.
#
# Safe to re-run: admin_addPeer is idempotent (returns true whether the peer was
# already registered or is being added).

set -e

# HTTP ports for the 4 nodes (from start-nodes.sh)
PORTS=(9545 9546 9547 9548)

# Wait for all 4 nodes to respond to admin_nodeInfo (max 30s)
for port in "${PORTS[@]}"; do
    for _ in $(seq 1 30); do
        if curl -s --max-time 2 -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}' \
            "http://localhost:$port" 2>/dev/null | grep -q '"qnr"'; then
            break
        fi
        sleep 1
    done
done

# Fetch qnr for each node
declare -a QNR
for i in 0 1 2 3; do
    port=${PORTS[$i]}
    QNR[$i]=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}' \
        "http://localhost:$port" | python3 -c "import json,sys; print(json.load(sys.stdin).get('result',{}).get('qnr',''))")
    if [ -z "${QNR[$i]}" ]; then
        echo "  ✗ node$((i+1)) (port $port) — no qnr, is it running?" >&2
        exit 1
    fi
done

# Full mesh: each node calls admin_addPeer for every other node's qnr
added=0
for i in 0 1 2 3; do
    src_port=${PORTS[$i]}
    for j in 0 1 2 3; do
        [ "$i" = "$j" ] && continue
        rsp=$(curl -s -X POST -H "Content-Type: application/json" \
            --data "{\"jsonrpc\":\"2.0\",\"method\":\"admin_addPeer\",\"params\":[\"${QNR[$j]}\"],\"id\":1}" \
            "http://localhost:$src_port" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('result','?'))" )
        if [ "$rsp" = "True" ]; then
            added=$((added+1))
        fi
    done
done

echo "✓ Wired $added peer edges across ${#PORTS[@]} nodes"

# Confirm
for port in "${PORTS[@]}"; do
    peers=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}' \
        "http://localhost:$port" | python3 -c "import json,sys; print(int(json.load(sys.stdin)['result'], 16))")
    echo "  node@$port: $peers peers"
done
