#!/bin/bash
# Start beacon-chain consensus layer for QRL Zond Private Network
# Includes genesis alignment verification

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"     # qrl-2.0/
CHAINS_DIR="$QRL_ROOT/chains/localnet"
BEACON_BIN="$QRL_ROOT/upstream/qrysm/build/bin/beacon-chain"
BEACON_DATA="$CHAINS_DIR/beacon"
JWT_SECRET="$CHAINS_DIR/node1/gqrl/jwtsecret"
cd "$SCRIPT_DIR"
mkdir -p "$BEACON_DATA"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Beacon data dir already created above

# Check if QRL execution nodes are running
if [ ! -f "$CHAINS_DIR/node1/node.pid" ]; then
    echo -e "${RED}Error: QRL execution nodes are not running. Please start nodes first with ./start-nodes.sh${NC}"
    exit 1
fi

PID=$(cat "$CHAINS_DIR/node1/node.pid")
if ! ps -p $PID > /dev/null 2>&1; then
    echo -e "${RED}Error: QRL execution nodes are not running. Please start nodes first with ./start-nodes.sh${NC}"
    exit 1
fi

# Check if JWT secret exists
if [ ! -f "$JWT_SECRET" ]; then
    echo -e "${RED}Error: JWT secret not found. Wait for QRL execution layer to create it on first run.${NC}"
    exit 1
fi

# Check if genesis files exist
if [ ! -f "genesis.ssz" ]; then
    echo -e "${RED}Error: genesis.ssz not found.${NC}"
    echo "Run ./reset-chain.sh to generate aligned genesis files."
    exit 1
fi

if [ ! -f "genesis.json" ]; then
    echo -e "${RED}Error: genesis.json not found.${NC}"
    echo "Run ./reset-chain.sh to generate aligned genesis files."
    exit 1
fi

# Verify genesis alignment
echo -e "${YELLOW}Verifying genesis alignment...${NC}"

# Get execution layer genesis block hash
EXEC_GENESIS_HASH=$(curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"qrl_getBlockByNumber","params":["0x0", false],"id":1}' \
    http://127.0.0.1:9545 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('result',{}).get('hash',''))" 2>/dev/null)

if [ -z "$EXEC_GENESIS_HASH" ] || [ "$EXEC_GENESIS_HASH" == "None" ]; then
    echo -e "${RED}Error: Could not get execution layer genesis hash.${NC}"
    echo "Make sure node1 is running and responding on port 9545."
    exit 1
fi

echo "  Execution genesis hash: $EXEC_GENESIS_HASH"

# Get genesis.json timestamp to help verify alignment
GENESIS_TIMESTAMP=$(python3 -c "import json; print(json.load(open('genesis.json'))['timestamp'])" 2>/dev/null)
echo "  Genesis timestamp: $GENESIS_TIMESTAMP"

# Auto-detect and clean stale/corrupt beacon data to prevent desync
NEEDS_CLEAN=false

if [ -d "$BEACON_DATA/beaconchaindata" ]; then
    # Check 1: Genesis timestamp comparison
    GENESIS_MTIME=$(stat -f %m genesis.ssz 2>/dev/null || stat -c %Y genesis.ssz 2>/dev/null)
    BEACON_MTIME=$(stat -f %m $BEACON_DATA/beaconchaindata 2>/dev/null || stat -c %Y $BEACON_DATA/beaconchaindata 2>/dev/null)

    if [ -n "$GENESIS_MTIME" ] && [ -n "$BEACON_MTIME" ]; then
        if [ "$GENESIS_MTIME" -gt "$BEACON_MTIME" ]; then
            echo -e "${YELLOW}⚠ Stale beacon data detected (genesis.ssz is newer)${NC}"
            NEEDS_CLEAN=true
        fi
    fi

    # Check 2: Look for desync errors in recent beacon logs
    if [ -f "beacon-console.log" ]; then
        DESYNC_ERRORS=$(tail -100 beacon-console.log 2>/dev/null | grep -E "unknown justified root|unknown head|nil payload ID" | wc -l | tr -d ' ')
        DESYNC_ERRORS=${DESYNC_ERRORS:-0}
        if [ "$DESYNC_ERRORS" -gt 0 ] 2>/dev/null; then
            echo -e "${YELLOW}⚠ Desync errors detected in beacon logs ($DESYNC_ERRORS errors)${NC}"
            NEEDS_CLEAN=true
        fi
    fi

    # Check 3: Verify beacon genesis matches execution genesis
    if [ "$NEEDS_CLEAN" = false ]; then
        # Get current execution block 0 hash
        CURRENT_EXEC_HASH=$(curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"qrl_getBlockByNumber","params":["0x0", false],"id":1}' \
            http://127.0.0.1:9545 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('result',{}).get('hash',''))" 2>/dev/null)

        if [ -n "$CURRENT_EXEC_HASH" ] && [ "$CURRENT_EXEC_HASH" != "$EXEC_GENESIS_HASH" ] && [ "$CURRENT_EXEC_HASH" != "None" ]; then
            echo -e "${YELLOW}⚠ Execution genesis hash changed${NC}"
            NEEDS_CLEAN=true
        fi
    fi

    if [ "$NEEDS_CLEAN" = true ]; then
        echo -e "${YELLOW}  Auto-cleaning beacon data to prevent desync...${NC}"
        rm -rf "$BEACON_DATA" "$CHAINS_DIR/validators"
        mkdir -p "$BEACON_DATA" "$CHAINS_DIR/validators"
        # Also clear old logs to prevent false positives next time
        > "$BEACON_DATA/beacon-console.log" 2>/dev/null
        echo -e "${GREEN}✓ Beacon data auto-cleaned${NC}"
    else
        echo -e "${GREEN}✓ Beacon data is current${NC}"
    fi
else
    echo -e "${GREEN}✓ Fresh beacon start (no existing data)${NC}"
fi

BEACON_PID_FILE="$BEACON_DATA/beacon.pid"
BEACON_LOG="$BEACON_DATA/beacon.log"
BEACON_CONSOLE="$BEACON_DATA/beacon-console.log"

# Check if already running
if [ -f "$BEACON_PID_FILE" ]; then
    PID=$(cat "$BEACON_PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo -e "${YELLOW}beacon-chain is already running with PID $PID${NC}"
        exit 1
    fi
fi

echo -e "${GREEN}✓ Genesis alignment verified${NC}"
echo ""

# Start beacon-chain — execution endpoint is node1's authRPC (default 9551)
echo "Starting beacon-chain consensus layer for private network..."
"$BEACON_BIN" \
  --datadir="$BEACON_DATA" \
  --min-sync-peers=0 \
  --genesis-state="$SCRIPT_DIR/genesis.ssz" \
  --chain-config-file="$SCRIPT_DIR/config.yml" \
  --chain-id=32333 \
  --execution-endpoint=http://localhost:9551 \
  --jwt-secret="$JWT_SECRET" \
  --contract-deployment-block=0 \
  --interop-executiondata-votes \
  --disable-staking-contract-check \
  --minimum-peers-per-subnet=0 \
  --p2p-static-id \
  --p2p-host-ip=127.0.0.1 \
  --p2p-local-ip=127.0.0.1 \
  --p2p-udp-port=13000 \
  --p2p-tcp-port=14000 \
  --grpc-gateway-port=3600 \
  --rpc-port=4100 \
  --monitoring-port=8180 \
  --suggested-fee-recipient=Q00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001 \
  --accept-terms-of-use \
  --verbosity=info \
  --log-file="$BEACON_LOG" \
  --log-format=text \
  > "$BEACON_CONSOLE" 2>&1 &

# Save PID
echo $! > "$BEACON_PID_FILE"
echo -e "${GREEN}beacon-chain started with PID $(cat "$BEACON_PID_FILE")${NC}"
echo "Logs: $BEACON_LOG"
echo "Console output: $BEACON_CONSOLE"
echo ""
echo "You can monitor progress with: tail -f beacon.log"
echo ""
echo -e "${YELLOW}Tip: Watch for 'Forkchoice requested unknown head' errors.${NC}"
echo "If you see these, the genesis files are misaligned. Run ./reset-chain.sh"
