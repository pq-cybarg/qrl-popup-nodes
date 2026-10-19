#!/bin/bash
# Start bootnode for QRL Zond Private Network

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# On macOS, start caffeinate to prevent sleep (runs indefinitely until killed)
if [[ "$(uname)" == "Darwin" ]]; then
    # Kill any existing caffeinate from previous runs
    if [ -f "caffeinate.pid" ]; then
        OLD_PID=$(cat caffeinate.pid)
        kill $OLD_PID 2>/dev/null
        rm -f caffeinate.pid
    fi
    # Start caffeinate without -w flag so it runs indefinitely
    caffeinate -d -i -s &
    CAFF_PID=$!
    echo $CAFF_PID > caffeinate.pid
    echo "Started caffeinate (PID: $CAFF_PID) - Mac will stay awake until ./stop-all.sh"
fi

# Load configuration from .env
if [ -f ".env" ]; then
    source .env
fi

# Set bootnode binary based on variant
GZOND_VARIANT="${GZOND_VARIANT:-standard}"
case "$GZOND_VARIANT" in
    "rescue-edition")
        BOOTNODE="../go-zond-rescue-edition/build/bin/bootnode"
        ;;
    "standard"|*)
        BOOTNODE="../go-zond/build/bin/bootnode"
        ;;
esac

if [ ! -f "boot.key" ]; then
    echo "Error: boot.key not found. Run ./init-network.sh first."
    exit 1
fi

# Check if already running
if [ -f "bootnode.pid" ]; then
    PID=$(cat bootnode.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "Bootnode is already running with PID $PID"
        exit 1
    fi
fi

# Start bootnode
echo "Starting bootnode..."
$BOOTNODE --nodekey=boot.key --addr=:30301 > bootnode.log 2>&1 &

echo $! > bootnode.pid
sleep 1

# Get bootnode enode address
BOOTNODE_ENODE=$($BOOTNODE --nodekey=boot.key --writeaddress)
BOOTNODE_URL="enode://${BOOTNODE_ENODE}@127.0.0.1:30301"

echo "Bootnode started with PID $(cat bootnode.pid)"
echo "Bootnode enode: $BOOTNODE_URL"
echo ""
echo "Save this enode URL for connecting nodes!"
echo "$BOOTNODE_URL" > bootnode.enode
echo "Bootnode URL saved to bootnode.enode"
