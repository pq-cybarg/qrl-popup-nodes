#!/bin/bash
# Start all 4 nodes for QRL Zond Private Network
# Dynamically detects genesis account from node1/keystore

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"  # qrl-2.0/
CHAINS_DIR="$QRL_ROOT/chains/localnet"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Load configuration from .env (accept both new GQRL_ and legacy GZOND_ prefix)
if [ -f ".env" ]; then
    source .env
fi
# Backward compat: if legacy GZOND_VARIANT set and GQRL_VARIANT isn't, use it
GQRL_VARIANT="${GQRL_VARIANT:-${GZOND_VARIANT:-standard}}"

# Set binary based on variant. All variants share the same qrl_* API namespace
# because they're all built from qrl-2.0/upstream/go-qrl (the 64-byte base).
#   rescue-edition adds Rescue Prime (0x20) + STARK Verifier (0x21) + ML-DSA-87
#     verify-with-address (0x22) precompiles for zRonin.
#   moscow-chill adds SHAKE256 (0x03) + ML-DSA-87 detached verify (0x06)
#     precompiles matching the DigitalGuards draft QIP.
case "$GQRL_VARIANT" in
    "rescue-edition")
        GQRL="$QRL_ROOT/variants/rescue-edition/build/bin/gqrl"
        echo -e "${GREEN}Using rescue-edition (Rescue Prime + STARK Verifier + ML-DSA-87 verify-with-addr)${NC}"
        ;;
    "moscow-chill")
        GQRL="$QRL_ROOT/variants/moscow-chill/build/bin/gqrl"
        echo -e "${GREEN}Using moscow-chill (SHAKE256 @ 0x03 + ML-DSA-87 verify @ 0x06 — DigitalGuards draft QIP)${NC}"
        ;;
    "standard"|*)
        GQRL="$QRL_ROOT/upstream/go-qrl/build/bin/gqrl"
        echo -e "${YELLOW}Using standard go-qrl (gqrl v0.3.3)${NC}"
        ;;
esac

if [ ! -x "$GQRL" ]; then
    echo -e "${RED}Error: gqrl binary not found or not executable at $GQRL${NC}"
    echo "Build it with: cd $(dirname $(dirname $GQRL)) && go build -buildvcs=false -o build/bin/gqrl ./cmd/gqrl"
    exit 1
fi

# QRL network uses qnr discovery - skip external bootnode for local network
# Nodes will discover each other through local network discovery
echo -e "${YELLOW}Note: QRL network uses qnr discovery, skipping external bootnode${NC}"
BOOTNODE_URL=""

# API namespace — always qrl_* now (rescue-edition is built on go-qrl, not legacy go-zond)
API_METHOD="qrl_blockNumber"
HTTP_API="web3,qrl,net,admin,txpool,miner,debug"
WS_API="web3,qrl,net"
echo -e "${YELLOW}Using API methods: $HTTP_API${NC}"

# Check if genesis.json exists (config dir)
if [ ! -f "genesis.json" ]; then
    echo -e "${RED}Error: genesis.json not found in $SCRIPT_DIR.${NC}"
    echo "Run ./reset-chain.sh to generate genesis files."
    exit 1
fi

# Detect genesis account from genesis.json (the account with funds)
GENESIS_ACCOUNT=""
PASSWORD_FILE="$SCRIPT_DIR/genesis_password.txt"

# Try 64-byte Q prefix first (QRL 2.0), fall back to legacy 20-byte Z prefix
GENESIS_ACCOUNT=$(grep -B1 '"balance"' genesis.json | grep -oE 'Q[0-9a-fA-F]{128}' | head -1)
if [ -z "$GENESIS_ACCOUNT" ]; then
    GENESIS_ACCOUNT=$(grep -B1 '"balance"' genesis.json | grep -oE 'Z[0-9a-fA-F]{40}' | head -1)
fi

# Verify the account exists in node1 keystore (case-insensitive match)
KEYSTORE_DIR="$CHAINS_DIR/node1/keystore"
if [ -n "$GENESIS_ACCOUNT" ] && [ -d "$KEYSTORE_DIR" ]; then
    LOWER_ADDR=$(echo "$GENESIS_ACCOUNT" | tr '[:upper:]' '[:lower:]')
    if ! find "$KEYSTORE_DIR" -type f -iname "*${LOWER_ADDR}*" 2>/dev/null | grep -q .; then
        echo -e "${YELLOW}Warning: Genesis account $GENESIS_ACCOUNT not in $KEYSTORE_DIR${NC}"
        GENESIS_ACCOUNT=""
    fi
fi

if [ -z "$GENESIS_ACCOUNT" ]; then
    echo -e "${YELLOW}Warning: No genesis account found in $KEYSTORE_DIR${NC}"
    echo "Node1 will start without an unlocked account."
    echo "Run ./reset-chain.sh to create a funded genesis account."
fi

# Check password file
if [ -n "$GENESIS_ACCOUNT" ] && [ ! -f "$PASSWORD_FILE" ]; then
    echo -e "${YELLOW}Warning: genesis_password.txt not found${NC}"
    echo "Creating default password file..."
    echo "testnet123" > "$PASSWORD_FILE"
fi

# Base ports (offset to avoid conflict with testnet on 8545, 30303)
HTTP_PORT_BASE=9545
WS_PORT_BASE=9645
AUTH_PORT_BASE=9551
P2P_PORT_BASE=31303

echo "Starting 4 nodes..."
if [ -n "$GENESIS_ACCOUNT" ]; then
    echo -e "Genesis account: ${GREEN}$GENESIS_ACCOUNT${NC}"
fi
echo ""

for i in {1..4}; do
    NODE_DIR="$CHAINS_DIR/node$i"
    mkdir -p "$NODE_DIR"
    # Check if already running
    if [ -f "$NODE_DIR/node.pid" ]; then
        PID=$(cat "$NODE_DIR/node.pid")
        if ps -p $PID > /dev/null 2>&1; then
            echo -e "${YELLOW}Node$i is already running with PID $PID${NC}"
            continue
        fi
    fi

    HTTP_PORT=$((HTTP_PORT_BASE + i - 1))
    WS_PORT=$((WS_PORT_BASE + i - 1))
    AUTH_PORT=$((AUTH_PORT_BASE + i - 1))
    P2P_PORT=$((P2P_PORT_BASE + i - 1))

    echo "Starting node$i on ports: HTTP=$HTTP_PORT, WS=$WS_PORT, Auth=$AUTH_PORT, P2P=$P2P_PORT"

    # Note: --unlock/--password flags removed from gqrl for security.
    # To send transactions, use gqrl attach (personal.unlockAccount) or an external wallet.
    UNLOCK_FLAG=""

    $GQRL \
        --datadir="$NODE_DIR" \
        --keystore="$NODE_DIR/keystore" \
        --networkid=32333 \
        --port=$P2P_PORT \
        --http \
        --http.port=$HTTP_PORT \
        --http.addr="127.0.0.1" \
        --http.api="$HTTP_API" \
        --http.corsdomain="*" \
        --http.vhosts="*" \
        --ws \
        --ws.port=$WS_PORT \
        --ws.addr="127.0.0.1" \
        --ws.api="$WS_API" \
        --authrpc.port=$AUTH_PORT \
        $([ -n "$BOOTNODE_URL" ] && echo "--bootnodes=\"$BOOTNODE_URL\"") \
        --nat=extip:127.0.0.1 \
        --syncmode=full \
        $UNLOCK_FLAG \
        > "$NODE_DIR/node.log" 2>&1 &

    echo $! > "$NODE_DIR/node.pid"
    echo -e "  ${GREEN}Node$i started with PID $(cat "$NODE_DIR/node.pid")${NC}"
done

echo ""
echo -e "${GREEN}All nodes started!${NC}"

# Wire full P2P mesh via admin_addPeer (QRL doesn't run a bootnode in this setup)
echo -e "${YELLOW}Wiring P2P mesh...${NC}"
"$SCRIPT_DIR/connect-peers.sh" 2>&1 | sed 's/^/  /'

echo "Wait a few seconds for nodes to discover each other."
echo ""
echo "Node HTTP Ports: 9545, 9546, 9547, 9548"
echo "Node P2P Ports: 31303, 31304, 31305, 31306"
echo ""
echo "Check peer count:"
echo "  curl -X POST -H 'Content-Type: application/json' --data '{\"jsonrpc\":\"2.0\",\"method\":\"net_peerCount\",\"params\":[],\"id\":1}' http://localhost:9545"
echo ""
# API methods already set above

echo "Check block number:"
echo "  curl -X POST -H 'Content-Type: application/json' --data '{\"jsonrpc\":\"2.0\",\"method\":\"$API_METHOD\",\"params\":[],\"id\":1}' http://localhost:9545"
