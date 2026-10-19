#!/bin/bash
# Start validators for QRL Zond Private Network

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QRL_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHAINS_DIR="$QRL_ROOT/chains/localnet"
VAL_BIN="$QRL_ROOT/upstream/qrysm/build/bin/validator"
VAL_DATA="$CHAINS_DIR/validators"
BEACON_PID_FILE="$CHAINS_DIR/beacon/beacon.pid"
cd "$SCRIPT_DIR"
mkdir -p "$VAL_DATA"

if [ ! -f "$BEACON_PID_FILE" ]; then
    echo "Error: beacon-chain is not running. Start it first with ./start-beacon.sh"
    exit 1
fi

BPID=$(cat "$BEACON_PID_FILE")
if ! ps -p "$BPID" > /dev/null 2>&1; then
    echo "Error: beacon-chain PID $BPID is not running."
    exit 1
fi

if [ -f "$VAL_DATA/validator.pid" ]; then
    VPID=$(cat "$VAL_DATA/validator.pid")
    if ps -p "$VPID" > /dev/null 2>&1; then
        echo "Validators are already running with PID $VPID"
        exit 1
    fi
fi

echo "Starting validators for private network..."
"$VAL_BIN" \
  --datadir="$VAL_DATA" \
  --chain-config-file="$SCRIPT_DIR/config.yml" \
  --interop-num-validators=4 \
  --interop-start-index=0 \
  --beacon-rpc-provider=localhost:4100 \
  --suggested-fee-recipient=Q00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001 \
  --accept-terms-of-use \
  --verbosity=info \
  --log-file="$VAL_DATA/validator.log" \
  --log-format=text \
  > "$VAL_DATA/validator-console.log" 2>&1 &

echo $! > "$VAL_DATA/validator.pid"
echo "Validators started with PID $(cat "$VAL_DATA/validator.pid")"
echo "Logs: $VAL_DATA/validator.log"
