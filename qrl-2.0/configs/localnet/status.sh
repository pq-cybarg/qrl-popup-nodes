#!/bin/bash
# Check status of QRL Zond Private Network

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== QRL Zond Private Network Status ==="
echo ""

# Check bootnode
if [ -f "bootnode.pid" ]; then
    PID=$(cat bootnode.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "✓ Bootnode is running (PID $PID)"
        if [ -f "bootnode.enode" ]; then
            echo "  Enode: $(cat bootnode.enode)"
        fi
    else
        echo "✗ Bootnode PID file exists but process is not running"
    fi
else
    echo "✗ Bootnode is not running"
fi

echo ""

# Check nodes
for i in {1..4}; do
    if [ -f "node$i/node.pid" ]; then
        PID=$(cat node$i/node.pid)
        if ps -p $PID > /dev/null 2>&1; then
            HTTP_PORT=$((9545 + i - 1))
            echo "✓ Node$i is running (PID $PID, HTTP: $HTTP_PORT)"

            # Try to get peer count
            PEERS=$(curl -s -X POST -H "Content-Type: application/json" \
                --data '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}' \
                http://localhost:$HTTP_PORT 2>/dev/null | grep -o '"result":"[^"]*"' | cut -d'"' -f4)

            if [ ! -z "$PEERS" ]; then
                PEER_COUNT=$((16#${PEERS#0x}))
                echo "  Peers: $PEER_COUNT"
            fi
        else
            echo "✗ Node$i PID file exists but process is not running"
        fi
    else
        echo "✗ Node$i is not running"
    fi
done

echo ""
echo "Node HTTP Ports: 9545, 9546, 9547, 9548"
echo "Node P2P Ports: 31303, 31304, 31305, 31306"
