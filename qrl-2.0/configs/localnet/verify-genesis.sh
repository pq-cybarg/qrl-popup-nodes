#!/bin/bash
# Verify genesis alignment between execution layer and beacon chain
# Run this script to diagnose "Forkchoice requested unknown head" errors

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}   Zond Private Network - Genesis Alignment Verification${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo ""

ISSUES_FOUND=0

# Check 1: Genesis files exist
echo -e "${YELLOW}[1/5] Checking genesis files...${NC}"

if [ -f "genesis.json" ]; then
    echo -e "  ${GREEN}✓${NC} genesis.json exists"
    GENESIS_JSON_TIMESTAMP=$(python3 -c "import json; print(json.load(open('genesis.json'))['timestamp'])" 2>/dev/null)
    echo "    Timestamp: $GENESIS_JSON_TIMESTAMP"
else
    echo -e "  ${RED}✗${NC} genesis.json NOT FOUND"
    ISSUES_FOUND=$((ISSUES_FOUND + 1))
fi

if [ -f "genesis.ssz" ]; then
    echo -e "  ${GREEN}✓${NC} genesis.ssz exists"
    GENESIS_SSZ_SIZE=$(ls -lh genesis.ssz | awk '{print $5}')
    echo "    Size: $GENESIS_SSZ_SIZE"
else
    echo -e "  ${RED}✗${NC} genesis.ssz NOT FOUND"
    ISSUES_FOUND=$((ISSUES_FOUND + 1))
fi
echo ""

# Check 2: Execution layer genesis hash
echo -e "${YELLOW}[2/5] Checking execution layer genesis...${NC}"

EXEC_RESPONSE=$(curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"zond_getBlockByNumber","params":["0x0", false],"id":1}' \
    http://127.0.0.1:9545 2>/dev/null)

if [ -n "$EXEC_RESPONSE" ] && echo "$EXEC_RESPONSE" | grep -q '"result"'; then
    EXEC_GENESIS_HASH=$(echo "$EXEC_RESPONSE" | python3 -c "import json,sys; print(json.load(sys.stdin)['result']['hash'])" 2>/dev/null)
    EXEC_GENESIS_TIMESTAMP=$(echo "$EXEC_RESPONSE" | python3 -c "import json,sys; print(json.load(sys.stdin)['result']['timestamp'])" 2>/dev/null)

    echo -e "  ${GREEN}✓${NC} Execution layer responding"
    echo "    Genesis hash: $EXEC_GENESIS_HASH"
    echo "    Genesis timestamp: $EXEC_GENESIS_TIMESTAMP"

    # Check if timestamps match
    if [ "$EXEC_GENESIS_TIMESTAMP" == "$GENESIS_JSON_TIMESTAMP" ]; then
        echo -e "    ${GREEN}✓${NC} Timestamps match genesis.json"
    else
        echo -e "    ${RED}✗${NC} Timestamps DO NOT match genesis.json!"
        echo "      Expected: $GENESIS_JSON_TIMESTAMP"
        echo "      Got: $EXEC_GENESIS_TIMESTAMP"
        echo "      This means the execution nodes were initialized with a different genesis.json"
        ISSUES_FOUND=$((ISSUES_FOUND + 1))
    fi
else
    echo -e "  ${RED}✗${NC} Execution layer not responding on port 9545"
    echo "    Make sure nodes are running: ./start-nodes.sh"
    ISSUES_FOUND=$((ISSUES_FOUND + 1))
fi
echo ""

# Check 3: Current block height
echo -e "${YELLOW}[3/5] Checking current block height...${NC}"

BLOCK_NUMBER=$(curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"zond_blockNumber","params":[],"id":1}' \
    http://127.0.0.1:9545 2>/dev/null | python3 -c "import json,sys; r=json.load(sys.stdin); print(int(r['result'], 16) if 'result' in r else 'N/A')" 2>/dev/null)

if [ "$BLOCK_NUMBER" != "N/A" ] && [ -n "$BLOCK_NUMBER" ]; then
    echo -e "  ${GREEN}✓${NC} Current block height: $BLOCK_NUMBER"

    if [ "$BLOCK_NUMBER" == "0" ]; then
        echo -e "    ${YELLOW}⚠${NC} Chain is at genesis - beacon may not be producing blocks"
    else
        echo -e "    ${GREEN}✓${NC} Chain is producing blocks"
    fi
else
    echo -e "  ${RED}✗${NC} Could not get block number"
    ISSUES_FOUND=$((ISSUES_FOUND + 1))
fi
echo ""

# Check 4: Beacon chain status
echo -e "${YELLOW}[4/5] Checking beacon chain...${NC}"

if [ -f "beacon.pid" ]; then
    BEACON_PID=$(cat beacon.pid)
    if ps -p $BEACON_PID > /dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Beacon chain running (PID: $BEACON_PID)"

        # Check for recent forkchoice errors
        if [ -f "beacon-console.log" ]; then
            RECENT_ERRORS=$(tail -100 beacon-console.log 2>/dev/null | grep -c "Forkchoice requested unknown head" || echo "0")
            if [ "$RECENT_ERRORS" -gt "0" ]; then
                echo -e "    ${RED}✗${NC} Found $RECENT_ERRORS 'unknown head' errors in recent logs"
                echo "      This indicates genesis mismatch between beacon and execution layer"
                ISSUES_FOUND=$((ISSUES_FOUND + 1))

                # Extract the requested hash
                REQUESTED_HASH=$(tail -100 beacon-console.log 2>/dev/null | grep "Forkchoice requested unknown head" | tail -1 | grep -oE "hash=[a-f0-9.]+" | head -1)
                if [ -n "$REQUESTED_HASH" ]; then
                    echo "      Beacon is requesting: $REQUESTED_HASH"
                    echo "      Execution has: ${EXEC_GENESIS_HASH:0:10}..."
                fi
            else
                echo -e "    ${GREEN}✓${NC} No forkchoice errors in recent logs"
            fi

            # Check for block proposals
            RECENT_PROPOSALS=$(tail -100 beacon-console.log 2>/dev/null | grep -c "Finished building block" || echo "0")
            echo "    Recent block proposals: $RECENT_PROPOSALS"
        fi
    else
        echo -e "  ${YELLOW}⚠${NC} Beacon chain not running (stale PID file)"
    fi
else
    echo -e "  ${YELLOW}⚠${NC} Beacon chain not running"
fi
echo ""

# Check 5: Beacon data state
echo -e "${YELLOW}[5/5] Checking beacon data state...${NC}"

if [ -d "beacondata/beaconchaindata" ]; then
    BEACON_DATA_SIZE=$(du -sh beacondata 2>/dev/null | cut -f1)
    echo "  Beacon data exists (size: $BEACON_DATA_SIZE)"

    # Check if beacon data might be from a different genesis
    if [ "$ISSUES_FOUND" -gt "0" ]; then
        echo -e "    ${YELLOW}⚠${NC} Beacon data may be stale if genesis was recently regenerated"
    fi
else
    echo "  No beacon data directory found (will be created on first run)"
fi
echo ""

# Summary
echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
if [ "$ISSUES_FOUND" -eq "0" ]; then
    echo -e "${GREEN}   ✓ All checks passed - Genesis appears aligned${NC}"
else
    echo -e "${RED}   ✗ Found $ISSUES_FOUND issue(s)${NC}"
    echo ""
    echo -e "${YELLOW}   Recommended fix:${NC}"
    echo "   1. Stop all services: ./stop-nodes.sh && pkill -f beacon-chain && pkill -f validator"
    echo "   2. Reset chain: ./reset-chain.sh --keep-accounts"
    echo "   3. Start services in order:"
    echo "      ./start-bootnode.sh"
    echo "      ./start-nodes.sh"
    echo "      ./start-beacon.sh"
    echo "      ./start-validators.sh"
fi
echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo ""

exit $ISSUES_FOUND
