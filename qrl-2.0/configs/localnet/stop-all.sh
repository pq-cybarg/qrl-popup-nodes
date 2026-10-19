#!/bin/bash
# Stop all QRL Zond Private Network processes

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "Stopping all nodes and bootnode..."
echo ""

# Stop all nodes
for i in {1..4}; do
    if [ -f "node$i/node.pid" ]; then
        PID=$(cat node$i/node.pid)
        if ps -p $PID > /dev/null 2>&1; then
            echo "Stopping node$i (PID $PID)..."
            kill $PID
            sleep 1
            if ps -p $PID > /dev/null 2>&1; then
                kill -9 $PID
            fi
            rm node$i/node.pid
        else
            echo "Node$i is not running"
            rm node$i/node.pid
        fi
    else
        echo "No PID file for node$i"
    fi
done

# Stop bootnode
if [ -f "bootnode.pid" ]; then
    PID=$(cat bootnode.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "Stopping bootnode (PID $PID)..."
        kill $PID
        sleep 1
        if ps -p $PID > /dev/null 2>&1; then
            kill -9 $PID
        fi
        rm bootnode.pid
    else
        echo "Bootnode is not running"
        rm bootnode.pid
    fi
else
    echo "No bootnode PID file found"
fi

# Stop caffeinate on macOS
if [[ "$(uname)" == "Darwin" ]]; then
    if [ -f "caffeinate.pid" ]; then
        CAFF_PID=$(cat caffeinate.pid)
        if ps -p $CAFF_PID > /dev/null 2>&1; then
            echo "Stopping caffeinate (PID $CAFF_PID)..."
            kill $CAFF_PID 2>/dev/null
        fi
        rm -f caffeinate.pid
    fi
    # Also kill any stray caffeinate processes for this directory
    pkill -f "caffeinate.*zond-private" 2>/dev/null
    echo "Mac sleep prevention disabled"
fi

echo ""
echo "All processes stopped"
