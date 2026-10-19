#!/bin/bash
# Start follower beacon-chain instances for nodes 2/3/4.
# These beacons don't run validators — they just sync consensus from node1's
# beacon (via P2P) and drive their local execution client (gqrl 2/3/4) with
# engine_forkchoiceUpdated / engine_newPayload so the ELs actually import blocks.
#
# This makes the localnet a real 4-node network suitable for dev testing and
# for simulating multiple node runners on one machine.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHAINS_DIR="$QRL_ROOT/chains/localnet"
BEACON_BIN="$QRL_ROOT/upstream/qrysm/build/bin/beacon-chain"

# Node1's beacon must be up so followers can bootstrap from it
NODE1_BEACON_GATEWAY="http://localhost:3600"
QNR=$(curl -s --max-time 5 "$NODE1_BEACON_GATEWAY/qrl/v1/node/identity" \
      | python3 -c "import json,sys; print(json.load(sys.stdin).get('data',{}).get('qnr',''))")

if [ -z "$QNR" ]; then
    echo "✗ Could not fetch node1 beacon identity from $NODE1_BEACON_GATEWAY — is the primary beacon running?" >&2
    exit 1
fi
echo "Bootstrapping from node1 beacon qnr: ${QNR:0:60}..."

# Port allocation for follower beacons (offset from node1's beacon):
#   RPC (gRPC)      4100 → 4101, 4102, 4103
#   gRPC gateway    3600 → 3601, 3602, 3603
#   P2P UDP         13000 → 13001, 13002, 13003
#   P2P TCP         14000 → 14001, 14002, 14003
#   Monitoring      8180 → 8181, 8182, 8183
#   Execution auth  9551 (node1) → 9552, 9553, 9554 (per gqrl node)

for i in 2 3 4; do
    NODE_DIR="$CHAINS_DIR/node$i"
    BEACON_DIR="$CHAINS_DIR/beacon-node$i"
    JWT="$NODE_DIR/gqrl/jwtsecret"
    mkdir -p "$BEACON_DIR"

    if [ ! -f "$JWT" ]; then
        echo "✗ node$i jwtsecret missing at $JWT — is gqrl node$i running?" >&2
        continue
    fi

    if [ -f "$BEACON_DIR/beacon.pid" ] && ps -p "$(cat "$BEACON_DIR/beacon.pid")" > /dev/null 2>&1; then
        echo "✓ node$i beacon already running (pid $(cat "$BEACON_DIR/beacon.pid"))"
        continue
    fi

    AUTH_PORT=$((9550 + i))     # 9552, 9553, 9554
    RPC_PORT=$((4100 + i - 1))  # 4101, 4102, 4103
    GW_PORT=$((3600 + i - 1))   # 3601, 3602, 3603
    UDP_PORT=$((13000 + i - 1)) # 13001, 13002, 13003
    TCP_PORT=$((14000 + i - 1)) # 14001, 14002, 14003
    MON_PORT=$((8180 + i - 1))  # 8181, 8182, 8183

    echo "Starting follower beacon for node$i (RPC:$RPC_PORT, UDP:$UDP_PORT → EL:$AUTH_PORT)..."
    "$BEACON_BIN" \
      --datadir="$BEACON_DIR" \
      --min-sync-peers=1 \
      --genesis-state="$SCRIPT_DIR/genesis.ssz" \
      --chain-config-file="$SCRIPT_DIR/config.yml" \
      --config-file="$SCRIPT_DIR/config.yml" \
      --chain-id=32333 \
      --execution-endpoint="http://localhost:$AUTH_PORT" \
      --jwt-secret="$JWT" \
      --contract-deployment-block=0 \
      --interop-executiondata-votes \
      --disable-staking-contract-check \
      --minimum-peers-per-subnet=0 \
      --bootstrap-node="$QNR" \
      --p2p-static-id \
      --p2p-host-ip=127.0.0.1 \
      --p2p-local-ip=127.0.0.1 \
      --p2p-udp-port="$UDP_PORT" \
      --p2p-tcp-port="$TCP_PORT" \
      --grpc-gateway-port="$GW_PORT" \
      --rpc-port="$RPC_PORT" \
      --monitoring-port="$MON_PORT" \
      --accept-terms-of-use \
      --verbosity=info \
      --log-file="$BEACON_DIR/beacon.log" \
      --log-format=text \
      > "$BEACON_DIR/beacon-console.log" 2>&1 &

    echo $! > "$BEACON_DIR/beacon.pid"
    echo "  → pid $(cat "$BEACON_DIR/beacon.pid"), data at $BEACON_DIR"
    sleep 2
done

echo ""
echo "Follower beacons started. Give them a few slots (60s each) to sync from node1."
echo "Check block heights: for p in 9545 9546 9547 9548; do curl -s -X POST -H 'Content-Type: application/json' --data '{\"jsonrpc\":\"2.0\",\"method\":\"qrl_blockNumber\",\"params\":[],\"id\":1}' http://localhost:\$p | head -c 80; echo; done"
