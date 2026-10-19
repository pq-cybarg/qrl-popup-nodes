#!/bin/bash
# Zond Network Sync Check Script
# Detects and prevents genesis desync issues

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check if network is running
check_network_running() {
    local response=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"zond_blockNumber","params":[],"id":1}' \
        http://127.0.0.1:9545 --connect-timeout 3 2>/dev/null)

    if [[ -z "$response" ]]; then
        echo -e "${RED}✗ Execution layer not responding${NC}"
        return 1
    fi

    local block=$(echo "$response" | grep -o '"result":"[^"]*"' | cut -d'"' -f4)
    echo -e "${GREEN}✓ Execution layer running - Block: $block${NC}"
    return 0
}

# Check beacon chain status
check_beacon_status() {
    if ! pgrep -f "beacon-chain" > /dev/null; then
        echo -e "${RED}✗ Beacon chain not running${NC}"
        return 1
    fi

    # Check for desync errors in last 50 lines of log
    local errors=$(tail -50 "$SCRIPT_DIR/beacon-console.log" 2>/dev/null | \
        grep -E "(unknown head|unknown justified root|Forkchoice requested unknown)" | wc -l)

    if [[ $errors -gt 0 ]]; then
        echo -e "${RED}✗ Beacon chain DESYNC DETECTED! ($errors error(s))${NC}"
        echo -e "${YELLOW}  Run: ./reset-chain.sh --keep-accounts${NC}"
        return 2
    fi

    echo -e "${GREEN}✓ Beacon chain healthy${NC}"
    return 0
}

# Check genesis alignment
check_genesis_alignment() {
    if [[ ! -f genesis.json ]] || [[ ! -f genesis.ssz ]]; then
        echo -e "${RED}✗ Missing genesis files${NC}"
        return 1
    fi

    # Get execution genesis hash from node
    local exec_genesis=$(curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"zond_getBlockByNumber","params":["0x0",false],"id":1}' \
        http://127.0.0.1:9545 --connect-timeout 3 2>/dev/null | grep -o '"hash":"[^"]*"' | head -1 | cut -d'"' -f4)

    if [[ -z "$exec_genesis" ]]; then
        echo -e "${YELLOW}⚠ Cannot verify - node not responding${NC}"
        return 0
    fi

    # Check genesis.json hash matches
    local json_hash=$(grep -o '"hash"[[:space:]]*:[[:space:]]*"[^"]*"' genesis.json 2>/dev/null | head -1 | cut -d'"' -f4)

    if [[ -n "$json_hash" ]] && [[ "$exec_genesis" != "$json_hash" ]]; then
        echo -e "${RED}✗ Genesis hash mismatch!${NC}"
        echo -e "${YELLOW}  Node: $exec_genesis${NC}"
        echo -e "${YELLOW}  File: $json_hash${NC}"
        return 1
    fi

    echo -e "${GREEN}✓ Genesis alignment OK${NC}"
    return 0
}

# Check for stale beacon data
check_beacon_data_freshness() {
    if [[ ! -d beacondata ]]; then
        echo -e "${GREEN}✓ No beacon data (fresh start)${NC}"
        return 0
    fi

    # Compare genesis.ssz modification time with beacondata
    local genesis_mtime=$(stat -f %m genesis.ssz 2>/dev/null || stat -c %Y genesis.ssz 2>/dev/null)
    local beacon_mtime=$(stat -f %m beacondata 2>/dev/null || stat -c %Y beacondata 2>/dev/null)

    if [[ -n "$genesis_mtime" ]] && [[ -n "$beacon_mtime" ]]; then
        if [[ $genesis_mtime -gt $beacon_mtime ]]; then
            echo -e "${RED}✗ Beacon data is STALE (older than genesis.ssz)${NC}"
            echo -e "${YELLOW}  Run: rm -rf beacondata validatordata${NC}"
            return 1
        fi
    fi

    echo -e "${GREEN}✓ Beacon data freshness OK${NC}"
    return 0
}

# Check transaction processing
check_tx_processing() {
    # Extract address from markdown - handles `Zxxx` format
    local account=$(cat GENESIS_ACCOUNT_INFO.md 2>/dev/null | grep -i "address" | sed 's/.*`\(Z[a-fA-F0-9]*\)`.*/\1/' | head -1)

    if [[ -z "$account" ]]; then
        echo -e "${YELLOW}⚠ Cannot check - no genesis account info${NC}"
        return 0
    fi

    # Check if account has balance
    local balance=$(curl -s -X POST -H "Content-Type: application/json" \
        --data "{\"jsonrpc\":\"2.0\",\"method\":\"zond_getBalance\",\"params\":[\"$account\",\"latest\"],\"id\":1}" \
        http://127.0.0.1:9545 --connect-timeout 3 2>/dev/null | grep -o '"result":"[^"]*"' | cut -d'"' -f4)

    if [[ "$balance" == "0x0" ]] || [[ -z "$balance" ]]; then
        echo -e "${YELLOW}⚠ Genesis account has no balance${NC}"
        return 0
    fi

    echo -e "${GREEN}✓ Genesis account funded: $balance${NC}"
    return 0
}

# Main check
main() {
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}   Zond Network Health Check${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    echo ""

    local has_errors=0

    echo "[1/5] Checking execution layer..."
    check_network_running || has_errors=1

    echo "[2/5] Checking beacon chain..."
    check_beacon_status
    local beacon_result=$?
    [[ $beacon_result -ne 0 ]] && has_errors=1
    [[ $beacon_result -eq 2 ]] && has_errors=2  # Desync is critical

    echo "[3/5] Checking genesis alignment..."
    check_genesis_alignment || has_errors=1

    echo "[4/5] Checking beacon data freshness..."
    check_beacon_data_freshness || has_errors=1

    echo "[5/5] Checking transaction capability..."
    check_tx_processing || has_errors=1

    echo ""
    if [[ $has_errors -eq 2 ]]; then
        echo -e "${RED}═══════════════════════════════════════════════════════${NC}"
        echo -e "${RED}   CRITICAL: DESYNC DETECTED - RESET REQUIRED${NC}"
        echo -e "${RED}═══════════════════════════════════════════════════════${NC}"
        echo ""
        echo -e "Run: ${YELLOW}./reset-chain.sh --keep-accounts${NC}"
        exit 2
    elif [[ $has_errors -eq 1 ]]; then
        echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
        echo -e "${YELLOW}   Some checks failed - review above${NC}"
        echo -e "${YELLOW}═══════════════════════════════════════════════════════${NC}"
        exit 1
    else
        echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
        echo -e "${GREEN}   All checks passed! Network is healthy.${NC}"
        echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
        exit 0
    fi
}

main "$@"
