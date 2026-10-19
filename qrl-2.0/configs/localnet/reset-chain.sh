#!/bin/bash
# Zond Private Network - Chain Reset Script
# Creates a new genesis account and reinitializes the entire network
# Ensures beacon chain and execution layer genesis are properly aligned

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
DEFAULT_PASSWORD="testnet123"
DEFAULT_BALANCE="100000000"  # 100M Quanta
DEFAULT_NAME="Genesis"
DEFAULT_CHAIN_ID="32333"
DEFAULT_NETWORK_ID="32333"
DEFAULT_GENESIS_DELAY=60  # Seconds to delay genesis (gives time to start services)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load configuration from .env
if [ -f "$SCRIPT_DIR/.env" ]; then
    source "$SCRIPT_DIR/.env"
fi

# Set GQRL binary based on variant
GQRL_VARIANT="${GQRL_VARIANT:-standard}"
case "$GQRL_VARIANT" in
    "rescue-edition")
        GQRL_BIN="$SCRIPT_DIR/../../variants/rescue-edition/build/bin/gqrl"
        ;;
    "standard"|*)
        GQRL_BIN="$SCRIPT_DIR/../../upstream/go-qrl/build/bin/gqrl"
        ;;
esac
QRYSMCTL_BIN="$SCRIPT_DIR/../../upstream/qrysm/qrysmctl"

# Parse command line arguments
PASSWORD="$DEFAULT_PASSWORD"
BALANCE="$DEFAULT_BALANCE"
ACCOUNT_NAME="$DEFAULT_NAME"
CHAIN_ID="$DEFAULT_CHAIN_ID"
NETWORK_ID="$DEFAULT_NETWORK_ID"
GENESIS_DELAY="$DEFAULT_GENESIS_DELAY"
SKIP_BACKUP=false
KEEP_ACCOUNTS=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --password)
      PASSWORD="$2"
      shift 2
      ;;
    --balance)
      BALANCE="$2"
      shift 2
      ;;
    --name)
      ACCOUNT_NAME="$2"
      shift 2
      ;;
    --chain-id)
      CHAIN_ID="$2"
      NETWORK_ID="$2"
      shift 2
      ;;
    --genesis-delay)
      GENESIS_DELAY="$2"
      shift 2
      ;;
    --skip-backup)
      SKIP_BACKUP=true
      shift
      ;;
    --keep-accounts)
      KEEP_ACCOUNTS=true
      shift
      ;;
    --help)
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --password PASSWORD    Set genesis account password (default: $DEFAULT_PASSWORD)"
      echo "  --balance AMOUNT       Set genesis account balance in Quanta (default: $DEFAULT_BALANCE)"
      echo "  --name NAME           Set account name (default: $DEFAULT_NAME)"
      echo "  --chain-id ID         Set chain/network ID (default: $DEFAULT_CHAIN_ID)"
      echo "  --genesis-delay SECS  Seconds to delay genesis time (default: $DEFAULT_GENESIS_DELAY)"
      echo "  --skip-backup         Skip backing up old data"
      echo "  --keep-accounts       Keep existing keystore accounts from node1"
      echo "  --help                Show this help message"
      echo ""
      echo "Examples:"
      echo "  $0"
      echo "  $0 --password 'MyPassword123'"
      echo "  $0 --balance 500000000 --name 'MainFunder'"
      echo "  $0 --keep-accounts    # Reset chain but keep existing accounts"
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}   Zond Private Network - Full Chain Reset${NC}"
echo -e "${GREEN}   (Execution Layer + Beacon Chain Aligned Genesis)${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo ""

# Validate binaries exist
if [ ! -f "$GQRL_BIN" ]; then
  echo -e "${RED}ERROR: QRL binary not found at $GQRL_BIN${NC}"
  echo "Please build the QRL binary first:"
  if [ "$GQRL_VARIANT" = "rescue-edition" ]; then
    echo "  cd $SCRIPT_DIR/../go-zond-rescue-edition && make gzond"
  else
    echo "  cd $SCRIPT_DIR/../go-zond/go-qrl && make gqrl"
  fi
  exit 1
fi

if [ ! -f "$QRYSMCTL_BIN" ]; then
  echo -e "${RED}ERROR: qrysmctl binary not found at $QRYSMCTL_BIN${NC}"
  echo "Please ensure qrysmctl is built in the qrysm directory"
  exit 1
fi

# Show configuration
echo -e "${BLUE}Configuration:${NC}"
echo "  Account Name: $ACCOUNT_NAME"
echo "  Password: $(echo "$PASSWORD" | sed 's/./*/g') (hidden)"
echo "  Balance: $BALANCE Quanta"
echo "  Chain ID: $CHAIN_ID"
echo "  Network ID: $NETWORK_ID"
echo "  Genesis Delay: ${GENESIS_DELAY}s"
echo "  Keep Accounts: $KEEP_ACCOUNTS"
echo ""

# Confirm with user
read -p "This will RESET the entire blockchain (execution + beacon). Continue? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
  echo -e "${YELLOW}Aborted by user${NC}"
  exit 0
fi
echo ""

# Step 1: Stop all running services
echo -e "${YELLOW}[1/10] Stopping all Zond services...${NC}"
pkill -f "beacon-chain" 2>/dev/null || true
pkill -f "validator" 2>/dev/null || true
sleep 1
pkill -f "gzond" 2>/dev/null || true
pkill -f "bootnode" 2>/dev/null || true
sleep 2

# Clean up PID files
rm -f "$SCRIPT_DIR"/*.pid
rm -f "$SCRIPT_DIR"/node*/*.pid

echo -e "${GREEN}✓ All services stopped${NC}"
echo ""

# Step 2: Backup old data
if [ "$SKIP_BACKUP" = false ]; then
  BACKUP_DIR="$SCRIPT_DIR/backup-$(date +%Y%m%d-%H%M%S)"
  echo -e "${YELLOW}[2/10] Backing up old data to $BACKUP_DIR...${NC}"
  mkdir -p "$BACKUP_DIR"

  # Backup node data
  for i in {1..4}; do
    if [ -d "$SCRIPT_DIR/node$i" ]; then
      echo "  Backing up node$i..."
      cp -r "$SCRIPT_DIR/node$i" "$BACKUP_DIR/"
    fi
  done

  # Backup beacon and validator data
  [ -d "$SCRIPT_DIR/beacondata" ] && cp -r "$SCRIPT_DIR/beacondata" "$BACKUP_DIR/"
  [ -d "$SCRIPT_DIR/validatordata" ] && cp -r "$SCRIPT_DIR/validatordata" "$BACKUP_DIR/"

  # Backup genesis files
  [ -f "$SCRIPT_DIR/genesis.json" ] && cp "$SCRIPT_DIR/genesis.json" "$BACKUP_DIR/"
  [ -f "$SCRIPT_DIR/genesis.ssz" ] && cp "$SCRIPT_DIR/genesis.ssz" "$BACKUP_DIR/"

  echo -e "${GREEN}✓ Backup complete${NC}"
else
  echo -e "${YELLOW}[2/10] Skipping backup (--skip-backup flag)${NC}"
fi
echo ""

# Step 3: Handle accounts
GENESIS_ADDRESS=""
KEYSTORE_FILE=""

if [ "$KEEP_ACCOUNTS" = true ] && [ -d "$SCRIPT_DIR/node1/keystore" ]; then
  echo -e "${YELLOW}[3/10] Preserving existing accounts...${NC}"

  # Find the genesis account (Z prefix)
  KEYSTORE_FILE=$(find "$SCRIPT_DIR/node1/keystore" -type f -name "*Z*" 2>/dev/null | head -1)
  if [ -n "$KEYSTORE_FILE" ]; then
    # Extract address from filename
    GENESIS_ADDRESS=$(basename "$KEYSTORE_FILE" | grep -oE 'Z[0-9a-fA-F]{40}' | head -1)
    if [ -z "$GENESIS_ADDRESS" ]; then
      # Try to extract from file content
      GENESIS_ADDRESS=$(grep -oE 'Z[0-9a-fA-F]{40}' "$KEYSTORE_FILE" 2>/dev/null | head -1)
    fi
  fi

  if [ -z "$GENESIS_ADDRESS" ]; then
    echo -e "${YELLOW}  No existing genesis account found, creating new one...${NC}"
    KEEP_ACCOUNTS=false
  else
    echo -e "${GREEN}✓ Found existing genesis account: $GENESIS_ADDRESS${NC}"
    # Save keystore temporarily
    TEMP_KEYSTORE_BACKUP="$SCRIPT_DIR/.temp-keystore-backup"
    mkdir -p "$TEMP_KEYSTORE_BACKUP"
    cp -r "$SCRIPT_DIR/node1/keystore/"* "$TEMP_KEYSTORE_BACKUP/" 2>/dev/null || true
  fi
else
  KEEP_ACCOUNTS=false
fi

if [ "$KEEP_ACCOUNTS" = false ]; then
  echo -e "${YELLOW}[3/10] Creating new genesis account...${NC}"

  # Create temporary keystore directory
  TEMP_KEYSTORE="$SCRIPT_DIR/temp-keystore"
  rm -rf "$TEMP_KEYSTORE"
  mkdir -p "$TEMP_KEYSTORE"

  # Create account
  echo "$PASSWORD" > "$SCRIPT_DIR/.temp-password"
  ACCOUNT_OUTPUT=$("$GQRL_BIN" account new --datadir "$TEMP_KEYSTORE" --password "$SCRIPT_DIR/.temp-password" 2>&1)
  rm "$SCRIPT_DIR/.temp-password"

  # Extract address from output (Q addresses for gqrl v0.3.3+)
  GENESIS_ADDRESS=$(echo "$ACCOUNT_OUTPUT" | grep -oE 'Q[0-9a-fA-F]{40}' | head -1)

  if [ -z "$GENESIS_ADDRESS" ]; then
    echo -e "${RED}ERROR: Failed to create genesis account${NC}"
    echo "$ACCOUNT_OUTPUT"
    exit 1
  fi

  # Find keystore file
  KEYSTORE_FILE=$(find "$TEMP_KEYSTORE/keystore" -type f -name "UTC--*" | head -1)

  if [ -z "$KEYSTORE_FILE" ]; then
    echo -e "${RED}ERROR: Keystore file not found${NC}"
    exit 1
  fi

  echo -e "${GREEN}✓ Genesis account created: $GENESIS_ADDRESS${NC}"
fi
echo ""

# Step 4: Convert balance to hex
echo -e "${YELLOW}[4/10] Converting balance to hex...${NC}"
BALANCE_WEI=$(python3 -c "print($BALANCE * 10**18)")
BALANCE_HEX=$(python3 -c "print(hex($BALANCE_WEI))")
echo -e "${GREEN}✓ Balance: $BALANCE Quanta = $BALANCE_HEX Wei${NC}"
echo ""

# Step 5: Generate aligned genesis.json (will be updated by qrysmctl)
echo -e "${YELLOW}[5/10] Generating base genesis.json...${NC}"

# Note: We generate a base genesis.json, then qrysmctl will create the final
# aligned version with matching timestamp

cat > "$SCRIPT_DIR/genesis-base.json" << EOF
{
  "config": {
    "chainId": $CHAIN_ID
  },
  "extraData": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "gasLimit": "0x1312d00",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "Z0000000000000000000000000000000000000000",
  "alloc": {
    "$GENESIS_ADDRESS": {
      "balance": "$BALANCE_HEX"
    }
  },
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "baseFeePerGas": null
}
EOF

echo -e "${GREEN}✓ Base genesis.json created${NC}"
echo ""

# Step 6: Generate aligned genesis files using qrysmctl
echo -e "${YELLOW}[6/10] Generating aligned genesis.ssz and genesis.json...${NC}"
echo "  This ensures beacon chain and execution layer have matching genesis hashes."

# qrysmctl generates both files with matching timestamps
"$QRYSMCTL_BIN" testnet generate-genesis \
  --num-validators=4 \
  --genesis-time-delay=$GENESIS_DELAY \
  --chain-config-file="$SCRIPT_DIR/config.yml" \
  --gzond-genesis-json-in="$SCRIPT_DIR/genesis-base.json" \
  --gzond-genesis-json-out="$SCRIPT_DIR/genesis.json" \
  --output-ssz="$SCRIPT_DIR/genesis.ssz" \
  2>&1 | while read line; do echo "  $line"; done

# Clean up base genesis
rm -f "$SCRIPT_DIR/genesis-base.json"

# Verify both files were created
if [ ! -f "$SCRIPT_DIR/genesis.json" ] || [ ! -f "$SCRIPT_DIR/genesis.ssz" ]; then
  echo -e "${RED}ERROR: Failed to generate aligned genesis files${NC}"
  exit 1
fi

echo -e "${GREEN}✓ Aligned genesis.json and genesis.ssz created${NC}"
echo ""

# Step 7: Clean and reinitialize all execution layer nodes
echo -e "${YELLOW}[7/10] Reinitializing all execution layer nodes...${NC}"

for i in {1..4}; do
  NODE_DIR="$SCRIPT_DIR/node$i"

  echo "  Reinitializing node$i..."

  # Remove old blockchain data (but preserve keystore if needed)
  rm -rf "$NODE_DIR/gzond"

  # Create node directory if needed
  mkdir -p "$NODE_DIR"
  mkdir -p "$NODE_DIR/keystore"

  # Initialize with aligned genesis
  "$GQRL_BIN" init --datadir "$NODE_DIR" "$SCRIPT_DIR/genesis.json" > /dev/null 2>&1

  # Restore/copy keystore to node1
  if [ $i -eq 1 ]; then
    if [ "$KEEP_ACCOUNTS" = true ] && [ -d "$TEMP_KEYSTORE_BACKUP" ]; then
      cp "$TEMP_KEYSTORE_BACKUP"/* "$NODE_DIR/keystore/" 2>/dev/null || true
      echo "    ✓ Existing accounts restored"
    elif [ -n "$KEYSTORE_FILE" ]; then
      cp "$KEYSTORE_FILE" "$NODE_DIR/keystore/"
      echo "    ✓ Genesis account keystore installed"
    fi
  fi
done

# Clean up temporary files
rm -rf "$SCRIPT_DIR/temp-keystore"
rm -rf "$SCRIPT_DIR/.temp-keystore-backup"

echo -e "${GREEN}✓ All execution layer nodes reinitialized${NC}"
echo ""

# Step 8: Clean beacon chain and validator data
echo -e "${YELLOW}[8/10] Cleaning beacon chain and validator data...${NC}"
rm -rf "$SCRIPT_DIR/beacondata"
rm -rf "$SCRIPT_DIR/validatordata"
echo -e "${GREEN}✓ Beacon and validator data cleaned${NC}"
echo ""

# Step 9: Save account and genesis information
echo -e "${YELLOW}[9/10] Saving configuration information...${NC}"

# Get the genesis block hash that will be created
GENESIS_TIMESTAMP=$(python3 -c "import json; print(json.load(open('$SCRIPT_DIR/genesis.json'))['timestamp'])")

# Save password file for start-nodes.sh
echo "$PASSWORD" > "$SCRIPT_DIR/genesis_password.txt"

# Update GENESIS_ACCOUNT_INFO.md
cat > "$SCRIPT_DIR/GENESIS_ACCOUNT_INFO.md" << EOF
# Genesis Account Information

**Created**: $(date)
**Network**: Zond Private (ID: $NETWORK_ID)

---

## Account Details

**Account Name**: $ACCOUNT_NAME
**Address**: \`$GENESIS_ADDRESS\`
**Balance**: $BALANCE Quanta ($BALANCE_HEX Wei)
**Password**: \`$PASSWORD\`

---

## Genesis Configuration

**Genesis Timestamp**: $GENESIS_TIMESTAMP
**Chain ID**: $CHAIN_ID
**Genesis Files**:
- Execution: \`$SCRIPT_DIR/genesis.json\`
- Beacon: \`$SCRIPT_DIR/genesis.ssz\`

These files are **aligned** - the beacon genesis.ssz references the correct
execution layer genesis block hash.

---

## Quick Start

### Start the network:
\`\`\`bash
cd $SCRIPT_DIR
./start-bootnode.sh
./start-nodes.sh
./start-beacon.sh
./start-validators.sh
\`\`\`

### Check block production:
\`\`\`bash
# Execution layer block number
curl -s -X POST -H "Content-Type: application/json" \\
  --data '{"jsonrpc":"2.0","method":"qrl_blockNumber","params":[],"id":1}' \\
  http://127.0.0.1:9545

# Check genesis account balance
curl -s -X POST -H "Content-Type: application/json" \\
  --data '{"jsonrpc":"2.0","method":"qrl_getBalance","params":["$GENESIS_ADDRESS","latest"],"id":1}' \\
  http://127.0.0.1:9545
\`\`\`

---

## Security Notes

- This is for LOCAL DEVELOPMENT ONLY
- NEVER use this account/password on mainnet
- NEVER expose these nodes to the internet

---

**Generated by**: reset-chain.sh
EOF

echo -e "${GREEN}✓ Configuration saved to GENESIS_ACCOUNT_INFO.md${NC}"
echo ""

# Step 10: Summary
echo -e "${YELLOW}[10/10] Reset complete!${NC}"
echo ""

echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}        Chain Reset Complete! ✓${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}Genesis Account:${NC} $GENESIS_ADDRESS"
echo -e "${BLUE}Balance:${NC} $BALANCE Quanta"
echo -e "${BLUE}Password:${NC} $PASSWORD"
echo ""
echo -e "${BLUE}Genesis Files (ALIGNED):${NC}"
echo "  • genesis.json  - Execution layer genesis"
echo "  • genesis.ssz   - Beacon chain genesis state"
echo ""
echo -e "${YELLOW}⚠️  IMPORTANT: Start services in this order:${NC}"
echo "  1. ./start-bootnode.sh"
echo "  2. ./start-nodes.sh"
echo "  3. ./start-beacon.sh"
echo "  4. ./start-validators.sh"
echo ""
echo -e "${YELLOW}Genesis will activate in ~${GENESIS_DELAY} seconds after qrysmctl ran.${NC}"
echo -e "${YELLOW}Start all services quickly to catch genesis time!${NC}"
echo ""
