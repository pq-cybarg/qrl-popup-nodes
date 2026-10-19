# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

This is a comprehensive QRL (Quantum Resistant Ledger) blockchain nodes repository containing **two distinct blockchain implementations**:

1. **Zond Networks** - Post-quantum proof-of-stake blockchain (execution + consensus layers)
2. **QRL Classic Networks** - Original QRL blockchain with XMSS post-quantum signatures

The repository supports **8 different network configurations** across both implementations, providing local development, public testnet/mainnet access, and Kurtosis-managed private networks.

---

## Architecture

### Two-Layer Architecture (Zond Networks)

**Execution Layer (gqrl)**:
- Source: `go-zond/go-qrl/` (Go implementation, Ethereum-compatible)  
- Binaries: `go-zond/go-qrl/build/bin/gqrl`, `bootnode`
- Protocol: JSON-RPC 2.0 with `qrl_*` method prefix (not `eth_*`)
- Address format: Q-addresses (64 hex chars after 'Q')

**Consensus Layer (qrysm)**:
- Source: `qrysm/` (Go implementation, Ethereum 2.0 fork)
- Binaries: `qrysm/bazel-bin/cmd/beacon-chain/beacon-chain_/beacon-chain`, `validator`
- Protocol: gRPC + REST gateway
- Features: Post-quantum secure, proof-of-stake consensus

### Single-Layer Architecture (QRL Classic Networks)

**QRL Classic Node**:
- Source: `qrl-classic/` (Python implementation)
- Protocol: gRPC with `qrl.PublicAPI` service
- Address format: Q-addresses (XMSS format)
- Features: XMSS post-quantum signatures, proof-of-work mining

---

## Build Commands

### Build All Components (Cross-Platform)

```bash
# Build for all platforms (macOS, Linux, Windows)
./build-all.sh

# Platform-specific builds in builds/ directory
# Creates: builds/{macos,linux,windows}/bin/
```

### Build Individual Components

```bash
# Build gqrl (execution layer)
cd go-zond/go-qrl
make gqrl

# Build qrysm (consensus layer)
cd qrysm
bazel build //cmd/beacon-chain:beacon-chain
bazel build //cmd/validator:validator

# QRL Classic (Python-based, no compilation needed)
cd qrl-classic
pip install -r requirements.txt
```

### Create Distribution Packages

```bash
# Create release packages with checksums
./package-distributions.sh
# Creates: distributions/qrl-zond-{platform}-v{version}.{tar.gz|zip}
```

---

## Network Management

### Start/Stop All Networks

```bash
# Start all 6+ networks
./start-all-networks.sh

# Stop all networks  
./stop-all-networks.sh

# Check status of all networks
./status-all-networks.sh
```

### Individual Network Control

Each network has its own directory with start/stop scripts:

**Zond Networks**:
```bash
# Zond Testnet (public)
./start-zond-testnet.sh
./stop-zond-testnet.sh

# Zond Private (4-node local)
cd zond-private && ./start-bootnode.sh && ./start-nodes.sh && ./start-beacon.sh
cd zond-private && ./stop-nodes.sh

# Zond Kurtosis (Docker-managed)
./start-zond-kurtosis.sh
./stop-zond-kurtosis.sh
```

**QRL Classic Networks**:
```bash
# QRL Classic Mainnet
./start-qrl-classic-mainnet.sh
./stop-qrl-classic-mainnet.sh

# QRL Classic Private (4-node local)  
cd qrl-classic-private && ./start-all.sh
cd qrl-classic-private && ./stop-all.sh
```

---

## Testing & Validation

### Comprehensive Network Testing

```bash
# Test all networks (starts each, validates connectivity)
./test-all-networks.sh

# Verify setup and configurations
./verify-setup.sh

# Quick connectivity test
./quick-test.sh
```

### Individual Network Testing

```bash
# Check specific network status via CLI
./qrl-cli.py status zond-testnet
./qrl-cli.py status classic-mainnet

# Test connectivity and sync
./check-zond-status.sh
./check-sync.sh
./check-peering.sh
```

---

## Key Network Endpoints & Protocols

### Zond Networks (JSON-RPC)

| Network | HTTP Endpoint | Protocol | Status |
|---------|---------------|----------|--------|
| Zond Testnet | `http://127.0.0.1:8545` | JSON-RPC 2.0 | Public |
| Zond Private Node 1 | `http://127.0.0.1:9545` | JSON-RPC 2.0 | Local |
| Zond Private Node 2-4 | `http://127.0.0.1:9546-9548` | JSON-RPC 2.0 | Local |

### QRL Classic Networks (gRPC)

| Network | gRPC Endpoint | HTTP Proxy | Status |
|---------|---------------|------------|--------|
| Classic Mainnet | `127.0.0.1:19009` | `http://127.0.0.1:18090` | Public |
| Classic Testnet | `127.0.0.1:29009` | Not configured | Public |
| Classic Private N1-4 | `127.0.0.1:39009,39019,39029,39039` | Not configured | Local |

**Important**: QRL Classic uses gRPC which requires special client libraries. HTTP proxy available for Mainnet only.

---

## Critical Configuration Files

### Zond Networks - Genesis Alignment

**CRITICAL**: Zond PoS requires aligned genesis files between execution and consensus layers:

- `{network}/genesis.json` - Execution layer genesis
- `{network}/genesis.ssz` - Consensus layer genesis  
- `{network}/config.yml` - Consensus chain configuration

These **MUST** be generated together using `qrysmctl` to ensure matching block hashes and timestamps.

### Port Allocation Scheme

**Zond Networks**:
- Testnet: `8545` (HTTP), `8551` (Auth), `4000` (Beacon), `3500` (gRPC)
- Private: `9545-9548` (HTTP), `9551-9554` (Auth), `31303-31306` (P2P)

**QRL Classic Networks**:
- Mainnet: `19009` (gRPC), `18090` (HTTP proxy)
- Testnet: `29009` (gRPC)
- Private: `39001-39040` (various APIs across 4 nodes)

**Kurtosis**: Dynamic ports managed by Docker

---

## Development Workflows

### Working with Zond Private Network

```bash
# Reset chain with new genesis account (RECOMMENDED for development)
cd zond-private
./reset-chain.sh

# Check genesis alignment (prevents sync issues)
./verify-genesis.sh

# Monitor sync status
./check-sync.sh
```

### Account & Wallet Management

**Zond Networks**:
```bash
# Create new account
./go-zond/go-qrl/build/bin/gqrl account new --datadir ./zond-private/node1

# Check balances via CLI
./qrl-cli.py balance zond-private Z1000000000000000000000000000000000000001
```

**QRL Classic Networks**:
```bash
# Generate XMSS wallet
cd qrl-classic
source venv/bin/activate  
qrl wallet_gen

# Check balance
./qrl-cli.py balance classic-mainnet Q010500...
```

### Post-Quantum Cryptography Features

**PQC Keystores** (Zond networks):
```bash
# Check keystore security level
./go-zond/build/bin/gzond pqc-info --datadir zond-private/node1

# Upgrade legacy keystores to PQC
./go-zond/build/bin/gzond pqc-upgrade --datadir zond-private/node1
```

**XMSS Signatures** (QRL Classic):
- Used by default for all transactions
- Stateful signatures (each key can only sign limited times)
- Quantum-resistant by design

---

## Development Environment Setup

### Prerequisites

```bash
# Go 1.19+ for gzond and qrysm
go version

# Python 3.14 specifically for QRL Classic  
python3.14 --version

# Bazel for building qrysm
bazel version

# Docker for Kurtosis networks
docker version
```

### Development Dependencies

```bash
# Install all compilers and tools
./setup-compilers.sh

# Install Python dependencies for QRL Classic
cd qrl-classic
python3.14 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

---

## Monitoring & Debugging

### Log Locations

**Zond Networks**:
- Execution: `{network}/node*/node.log`
- Beacon: `{network}/beacon-console.log`  
- Validators: `{network}/validator.log`

**QRL Classic**:
- `{network}/qrl.log`
- `{network}/simple-proxy.log` (if HTTP proxy enabled)

### Health Checks

```bash
# Process status
ps aux | grep -E "gzond|beacon-chain|start_qrl" | grep -v grep

# Port binding status  
lsof -i -P | grep LISTEN | grep -E "8545|9545|19009|18090"

# Block production check
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"zond_blockNumber","params":[],"id":1}' \
  http://127.0.0.1:9545
```

---

## Troubleshooting Guide

### Common Issues

**"Forkchoice requested unknown head" (Zond)**:
- Cause: Misaligned genesis files between execution and consensus layers
- Fix: `cd zond-private && ./reset-chain.sh --keep-accounts`

**Port conflicts**:
- Check: `lsof -i :{port}`
- Fix: Stop conflicting processes or use different ports

**QRL Classic gRPC not available**:
- Ensure Python 3.14 virtual environment activated
- Check `cd qrl-classic && source venv/bin/activate`

### Recovery Procedures

**Full network reset**:
```bash
./stop-all-networks.sh
cd zond-private && ./reset-chain.sh
./start-all-networks.sh
```

**Genesis misalignment fix**:
```bash
cd zond-private
./stop-nodes.sh && pkill -f beacon-chain && pkill -f validator
./reset-chain.sh --keep-accounts
./start-bootnode.sh && ./start-nodes.sh && ./start-beacon.sh && ./start-validators.sh
```

---

## Critical Security Notes

- **All networks are for LOCAL DEVELOPMENT only**
- **NEVER expose nodes to internet** (no firewall rules, no port forwarding)  
- **NEVER use development accounts/keys on mainnet**
- Genesis accounts have well-known passwords - suitable for testing only
- For production: use hardware wallets and proper key management

---

## See Also

- `CLI-README.md` - Comprehensive CLI tool documentation
- `NETWORK-STATUS.md` - Detailed network configurations and endpoints  
- `zond-private/CLAUDE.md` - In-depth private network management
- `PORT-ALLOCATION.md` - Complete port allocation scheme
- `PQC-KEYSTORE-IMPLEMENTATION.md` - Post-quantum cryptography details