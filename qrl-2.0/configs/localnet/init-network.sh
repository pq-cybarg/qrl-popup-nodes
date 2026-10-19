#!/bin/bash
# Initialize QRL Zond Private Network (4 nodes)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GQRL="../go-zond/go-qrl/build/bin/gqrl"
BOOTNODE="../go-zond/go-qrl/build/bin/bootnode"

echo "Initializing 4-node QRL private network..."
echo ""

# Initialize each node with genesis
for i in {1..4}; do
    echo "Initializing node$i..."
    $GQRL init --datadir="node$i" genesis.json
done

echo ""
echo "Generating bootnode key..."
$BOOTNODE --genkey=boot.key

echo ""
echo "Network initialized successfully!"
echo ""
echo "Next steps:"
echo "1. Run ./start-bootnode.sh to start the bootnode"
echo "2. Run ./start-nodes.sh to start all 4 nodes"
echo "3. Use ./status.sh to check node status"
echo "4. Use ./stop-all.sh to stop all nodes"
