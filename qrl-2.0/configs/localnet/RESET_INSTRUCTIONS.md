# Zond Private Network - Reset Instructions

Complete guide for resetting your local Zond private network with a new genesis account.

---

## Why Reset?

Reset the chain when you need to:
- ✅ Create a genesis account with known password
- ✅ Start with a clean blockchain (fresh state)
- ✅ Change genesis configuration (balance, chain ID)
- ✅ Recover from a corrupted blockchain
- ✅ Test deployment scenarios from scratch

---

## Quick Start

### Basic Reset (Default Settings)

```bash
cd ./zond-private
./reset-chain.sh
```

**Creates**:
- Genesis account with 100M Quanta
- Password: `QuarkNet2025Genesis`
- Chain ID: 32333
- All nodes reinitialized and started
- Account unlocked on node1

---

## Custom Reset Options

### Custom Password

```bash
./reset-chain.sh --password "MySecurePassword123"
```

**Use case**: Set your own secure password for the genesis account

---

### Custom Balance

```bash
./reset-chain.sh --balance 500000000
```

**Use case**: Create genesis account with 500M Quanta instead of default 100M

---

### Custom Account Name

```bash
./reset-chain.sh --name "MainFunder"
```

**Use case**: Set a descriptive name for documentation purposes

---

### Custom Chain ID

```bash
./reset-chain.sh --chain-id 42000
```

**Use case**: Change the network/chain ID from default 32333

---

### Skip Backup

```bash
./reset-chain.sh --skip-backup
```

**Use case**: Skip backing up old data (faster reset, but no recovery option)

---

### Full Customization

```bash
./reset-chain.sh \
  --password "SecurePass2025" \
  --balance 200000000 \
  --name "GenesisAccount" \
  --chain-id 32333
```

---

## What the Script Does

### Step-by-Step Process

1. **Stops all nodes**
   - Gracefully stops all 4 running Zond nodes
   - Waits for clean shutdown

2. **Backs up old data** (unless --skip-backup)
   - Creates `backup-YYYYMMDD-HHMMSS/` directory
   - Copies all node data directories
   - Backs up old genesis.json

3. **Creates new genesis account**
   - Generates new Zond account with your password
   - Creates encrypted keystore file
   - Extracts public address

4. **Generates new genesis.json**
   - Creates fresh genesis configuration
   - Allocates specified balance to new account
   - Sets chain ID and other parameters

5. **Reinitializes all nodes**
   - Clears old blockchain data from all nodes
   - Initializes each node with new genesis
   - Installs genesis account keystore on node1

6. **Starts all nodes**
   - Node 1: Started with genesis account unlocked
   - Nodes 2-4: Started without unlocked accounts
   - All nodes connect via bootnode

7. **Verifies node status**
   - Tests RPC connectivity to each node
   - Confirms nodes are responding

8. **Saves account information**
   - Creates `GENESIS_ACCOUNT_INFO.md`
   - Stores password, address, keystore location
   - Includes quick command examples

---

## After Reset

### Verify Setup

```bash
# Check node1 is running
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_blockNumber","params":[],"id":1}' \
  http://127.0.0.1:9545

# Check genesis account balance
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_getBalance","params":["GENESIS_ADDRESS","latest"],"id":1}' \
  http://127.0.0.1:9545 | jq

# Check unlocked accounts on node1
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_accounts","params":[],"id":1}' \
  http://127.0.0.1:9545 | jq
```

Expected outputs:
- Block number: `0x0` or `0x1` (new chain)
- Balance: Your specified amount in hex
- Accounts: Array with genesis address

---

### Use the Genesis Account

#### Via gzond Console

```bash
cd .
./go-zond/build/bin/gzond attach zond-private/node1/gzond.ipc

# In console - check accounts
> zond.accounts
["GENESIS_ADDRESS"]

# Check balance
> zond.getBalance("GENESIS_ADDRESS")
"100000000000000000000000000"

# Send transaction
> zond.sendTransaction({
  from: "GENESIS_ADDRESS",
  to: "RECIPIENT_ADDRESS",
  value: web3.toWei(1000, "ether"),
  gas: 21000
})
```

#### Via RPC

```bash
# Send transaction
curl -X POST -H "Content-Type: application/json" \
  --data '{
    "jsonrpc":"2.0",
    "method":"zond_sendTransaction",
    "params":[{
      "from":"GENESIS_ADDRESS",
      "to":"RECIPIENT_ADDRESS",
      "value":"0x3635C9ADC5DEA00000",
      "gas":"0x5208"
    }],
    "id":1
  }' \
  http://127.0.0.1:9545
```

---

## Integration with QuarkNet

After resetting the chain, fund QuarkNet accounts:

### Step 1: View Genesis Account Info

```bash
cat ./zond-private/GENESIS_ACCOUNT_INFO.md
```

### Step 2: Update QuarkNet Funding Script

Update the genesis address in QuarkNet's funding script:

```bash
# Edit fund-accounts.js
cd ./Desktop/projects/blockchain/qrl/l2/quark/predicates/drand/quarknet-kyber/deploy

# Update GENESIS_ACCOUNT with new address from GENESIS_ACCOUNT_INFO.md
```

### Step 3: Run Funding Script

```bash
cd .
./go-zond/build/bin/gzond attach zond-private/node1/gzond.ipc

# In console
> loadScript("./Desktop/projects/blockchain/qrl/l2/quark/predicates/drand/quarknet-kyber/deploy/fund-accounts.js")
```

This will distribute funds to the 5 QuarkNet deployment accounts.

---

## File Locations

After reset, these files are created/updated:

### Account Information
- **GENESIS_ACCOUNT_INFO.md** - Password, address, keystore location, commands
- **node1/keystore/UTC--...** - Encrypted private key

### Configuration
- **genesis.json** - New genesis configuration
- **node1/gzond/** - Fresh blockchain data (all nodes)

### Backups (if not skipped)
- **backup-YYYYMMDD-HHMMSS/** - Old node data and genesis

### Logs
- **node1/gzond.log** - Node 1 logs
- **node2/gzond.log** - Node 2 logs
- **node3/gzond.log** - Node 3 logs
- **node4/gzond.log** - Node 4 logs

---

## Troubleshooting

### Script Fails: "gzond binary not found"

**Problem**: gzond is not built

**Solution**:
```bash
cd ./go-zond
make gzond
```

---

### Nodes Don't Start After Reset

**Problem**: Ports may be in use

**Solution**:
```bash
# Check what's using the ports
lsof -i :9545
lsof -i :9546
lsof -i :9547
lsof -i :9548

# Kill any stuck processes
pkill -9 gzond

# Retry reset
cd ./zond-private
./reset-chain.sh
```

---

### Genesis Account Not Unlocked

**Problem**: Node1 didn't unlock account on startup

**Solution**:
```bash
# Get password from account info
cat ./zond-private/GENESIS_ACCOUNT_INFO.md

# Attach to console
cd .
./go-zond/build/bin/gzond attach zond-private/node1/gzond.ipc

# Try to list accounts
> zond.accounts

# If personal API is available, unlock manually
> personal.unlockAccount("GENESIS_ADDRESS", "PASSWORD", 0)
```

---

### Cannot Send Transactions

**Problem**: Account might be locked or insufficient balance

**Check 1 - Verify account is unlocked**:
```bash
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_accounts","params":[],"id":1}' \
  http://127.0.0.1:9545 | jq
```

**Check 2 - Verify balance**:
```bash
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_getBalance","params":["GENESIS_ADDRESS","latest"],"id":1}' \
  http://127.0.0.1:9545 | jq
```

**Check 3 - Check node logs**:
```bash
tail -100 ./zond-private/node1/gzond.log
```

---

### Python Not Available

**Problem**: Script uses Python for hex conversion

**Solution**:
```bash
# Install Python 3 (if not installed)
brew install python3

# Or use pre-calculated hex values
# 100M Quanta = 0x52b7d2dcc80cd2e4000000 Wei
```

---

### Backup Directory Full

**Problem**: Multiple backups taking up space

**Solution**:
```bash
cd ./zond-private

# List backups
ls -lh backup-*

# Remove old backups
rm -rf backup-20241201-*

# Or use --skip-backup flag
./reset-chain.sh --skip-backup
```

---

## Common Use Cases

### Test Deployment from Scratch

```bash
# 1. Reset chain
cd ./zond-private
./reset-chain.sh

# 2. Fund QuarkNet accounts
cd .
./go-zond/build/bin/gzond attach zond-private/node1/gzond.ipc
> loadScript(".../ fund-accounts.js")

# 3. Deploy QuarkNet
cd ./Desktop/projects/blockchain/qrl/l2/quark/predicates/drand/quarknet-kyber/deploy
./zond-deploy.sh
```

---

### Change Network Parameters

```bash
# Reset with different chain ID
./reset-chain.sh --chain-id 42000

# Update all configs to use new chain ID
# Update QuarkNet configs to match
```

---

### Create Testnet Environment

```bash
# Larger balance for extensive testing
./reset-chain.sh \
  --password "TestnetGenesis2025" \
  --balance 1000000000 \
  --name "TestnetFunder" \
  --chain-id 32382
```

---

## Best Practices

### 1. Always Backup Important Data

Before reset, manually backup critical data:
```bash
# Backup keystores
cp -r node1/keystore keystore.backup-$(date +%Y%m%d)

# Backup account info (if exists)
cp GENESIS_ACCOUNT_INFO.md GENESIS_ACCOUNT_INFO.backup
```

### 2. Document Custom Configurations

When using custom parameters, document them:
```bash
echo "Chain ID: 42000, Balance: 500M" >> CUSTOM_CONFIG.txt
```

### 3. Test After Reset

Always verify the reset worked:
```bash
# Run verification commands
curl http://127.0.0.1:9545 ...
```

### 4. Secure Password Storage

Store passwords securely:
```bash
# Encrypt account info
gpg --encrypt GENESIS_ACCOUNT_INFO.md

# Store encrypted version only
rm GENESIS_ACCOUNT_INFO.md
mv GENESIS_ACCOUNT_INFO.md.gpg secure-location/
```

---

## Security Warnings

⚠️ **LOCAL DEVELOPMENT ONLY**

This setup is designed for local development and testing ONLY:

- **DO NOT** expose nodes to the internet
- **DO NOT** use these accounts on mainnet
- **DO NOT** store large amounts of real value
- **DO NOT** reuse passwords across environments

### Production Checklist

Before using on production/mainnet:
- [ ] Use hardware wallet for genesis account
- [ ] Enable firewall rules
- [ ] Use strong, unique passwords
- [ ] Enable TLS/SSL for RPC
- [ ] Implement access controls
- [ ] Regular security audits
- [ ] Backup strategies
- [ ] Disaster recovery plan

---

## Additional Resources

### Documentation
- **CLAUDE.md** - Network overview and management
- **GENESIS_ACCOUNT_INFO.md** - Account details (created after reset)
- **genesis.json** - Genesis configuration

### Scripts
- **reset-chain.sh** - This reset script
- **start-nodes.sh** - Start all nodes
- **stop-nodes.sh** - Stop all nodes

### QuarkNet Integration
- **./Desktop/projects/blockchain/qrl/l2/quark/predicates/drand/quarknet-kyber/deploy/**
  - FUNDING_INSTRUCTIONS.md
  - QUARKNET_ACCOUNTS.md
  - fund-accounts.js

---

## Support

For issues or questions:
1. Check node logs in `node*/gzond.log`
2. Review `GENESIS_ACCOUNT_INFO.md` for account details
3. Check `CLAUDE.md` for network information
4. Review error messages in script output

---

**Last Updated**: 2025-12-01
**Script Version**: 1.0
**Tested On**: macOS with Zond v1.0
