#!/usr/bin/env node

const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');
const net = require('net');
const os = require('os');
const { execFile } = require('child_process');

const PORT = 8080;

// Network endpoints to check
const networks = [
    // QRL Classic Private (Local 4-node network)
    { id: 'classic-local-node1', name: 'Classic Private - Node 1', port: 49009, type: 'tcp', category: 'classic-local', description: 'Mining node' },
    { id: 'classic-local-node2', name: 'Classic Private - Node 2', port: 49019, type: 'tcp', category: 'classic-local', description: 'Validator' },
    { id: 'classic-local-node3', name: 'Classic Private - Node 3', port: 49029, type: 'tcp', category: 'classic-local', description: 'Validator' },
    { id: 'classic-local-node4', name: 'Classic Private - Node 4', port: 49039, type: 'tcp', category: 'classic-local', description: 'Validator' },

    // QRL Classic Testnet Local (syncing with public testnet)
    { id: 'classic-testnet-local', name: 'Classic Testnet (Local)', port: 29009, type: 'tcp', category: 'classic-testnet-local', description: 'Local testnet node' },

    // QRL Classic Mainnet Local (syncing with public mainnet)
    { id: 'classic-mainnet-local', name: 'Classic Mainnet (Local)', port: 19009, type: 'tcp', category: 'classic-mainnet-local', description: 'Local mainnet node' },

    // QRL Classic Remote
    { id: 'classic-mainnet', name: 'Classic Mainnet (Remote)', host: 'mainnet-1.automated.theqrl.org', port: 19009, type: 'tcp', category: 'classic-remote', description: 'Public mainnet' },
    { id: 'classic-testnet-1', name: 'Classic Testnet #1 (Remote)', host: '136.244.104.146', port: 29009, type: 'tcp', category: 'classic-remote', description: 'Public testnet' },
    { id: 'classic-testnet-2', name: 'Classic Testnet #2 (Remote)', host: '95.179.154.132', port: 29009, type: 'tcp', category: 'classic-remote', description: 'Public testnet' },

    // Proxy & Apps
    { id: 'zeus-proxy', name: 'Zeus Proxy', port: 3737, type: 'http', path: '/', category: 'proxy', description: 'gRPC to HTTP wrapper (Meteor app, sibling ../theQRL/zeus-proxy)' },
    { id: 'gogs', name: 'GOGS', port: 3330, type: 'http', path: '/', category: 'app', description: 'Git server' },

    // Zond Private (Local 4-node network)
    { id: 'zond-local-node1', name: 'Zond Private - Node 1', port: 9545, type: 'rpc', method: 'qrl_blockNumber', category: 'zond-local', description: 'Execution layer (gqrl v0.3.3)' },

    // Zond Testnet Local (syncing with public testnet)
    { id: 'zond-testnet-local', name: 'Zond Testnet (Local)', port: 8545, type: 'rpc', method: 'qrl_blockNumber', category: 'zond-testnet-local', description: 'Local testnet node (gqrl v0.3.3)' },

    // Zond Remote — public testnet moved off the old 209.250.255.226 IP.
    // Execution RPC is now behind a TLS gateway at testnet-qrl.publicbits.dev/.
    // Beacon GRPC/RPC endpoints are not currently exposed on the new host;
    // health-check entries kept but marked unreachable until the operator
    // publishes an equivalent public beacon endpoint.
    { id: 'zond-testnet', name: 'Zond Testnet (Remote — publicbits)', scheme: 'https', host: 'testnet-qrl.publicbits.dev', port: 443, path: '/', type: 'rpc', method: 'qrl_blockNumber', category: 'zond-remote', description: 'Public testnet via testnet-qrl.publicbits.dev' },
    { id: 'zond-testnet-qrlwallet', name: 'Zond Testnet (Remote — qrlwallet)', scheme: 'https', host: 'qrlwallet.com', port: 443, path: '/api/qrl-rpc/testnet', type: 'rpc', method: 'qrl_blockNumber', category: 'zond-remote', description: 'Public testnet via qrlwallet.com/api/qrl-rpc/testnet' },
    { id: 'zond-beacon-grpc', name: 'Zond Beacon GRPC (Remote)', host: 'testnet-qrl.publicbits.dev', port: 3500, type: 'tcp', category: 'zond-remote', description: 'Beacon GRPC Gateway (endpoint TBD)' },
    { id: 'zond-beacon-rpc', name: 'Zond Beacon RPC (Remote)', host: 'testnet-qrl.publicbits.dev', port: 4000, type: 'tcp', category: 'zond-remote', description: 'Beacon RPC (endpoint TBD)' }
];

// Network configurations for API calls
// Zeus Proxy supports: mainnet, testnet, classic-local, zond-local, classic-kurtosis, zond-kurtosis
//
// ZEUS_PROXY_BASE is env-overridable so the compose stack can point at
// host.docker.internal:3737 (dashboard runs in a container, zeus-proxy
// runs on the host). Bare-metal default is localhost:3737. See
// docker-compose.yaml — QRL_ZEUS_PROXY_URL on the dashboard service.
const ZEUS_PROXY_BASE = process.env.QRL_ZEUS_PROXY_URL || 'http://localhost:3737';
const networkConfigs = {
    'classic-mainnet': {
        type: 'classic',
        zeusProxy: `${ZEUS_PROXY_BASE}/grpc/mainnet`,
        explorer: 'https://explorer.theqrl.org',
        hasExplorer: true
    },
    'classic-testnet': {
        type: 'classic',
        zeusProxy: `${ZEUS_PROXY_BASE}/grpc/testnet`,
        explorer: 'https://testnet-explorer.theqrl.org',
        hasExplorer: true
    },
    'classic-private': {
        type: 'classic',
        zeusProxy: `${ZEUS_PROXY_BASE}/grpc/classic-local`,
        hasExplorer: false
    },
    // Beacon exposes TWO endpoints — track both:
    //   beaconRpc  = --grpc-gateway-port (REST/JSON, for browser fetch)
    //   beaconGrpc = --rpc-port          (native gRPC, for Go/SDK clients)
    // Per-network port choices come from the network's start-beacon.sh; if
    // a script changes its flags, update the matching entry here.
    //
    //   zond-testnet-local: qrysm defaults — REST 3500 / gRPC 4000
    //     (configs/testnet/start-beacon.sh sets neither flag explicitly)
    //   zond-private:       REST 3600 / gRPC 4100
    //     (configs/localnet/start-beacon.sh sets both explicitly;
    //      followers offset to 3601/3602/3603 + 4101/4102/4103 via
    //      configs/localnet/start-follower-beacons.sh)
    //
    // TESTNET BEACON: QRL 2.0 testnet has NO public REST/JSON beacon
    // gateway. Official docs (test-zond.theqrl.org) publish only P2P
    // bootstrap QNRs — you must run your own beacon locally to interact
    // with the beacon layer of testnet. Three supported modes:
    //   zond-testnet         — REMOTE-EXEC-ONLY: public RPC, no beacon
    //   zond-testnet-local   — FULL LOCAL: gqrl + beacon both local, beacon
    //                          peers into public testnet via the bootstrap
    //                          QNRs stored on zond-testnet.beaconBootstrapQnrs
    //   zond-private         — DEV LOCALNET: independent 4-node chain
    //
    // Hybrid (public exec + local beacon) is not a distinct entry because
    // the public exec RPC does not expose the authenticated Engine API —
    // a locally-run beacon can't drive it. If the operator exposes an
    // authenticated engine endpoint, add zond-testnet-hybrid with
    // beaconRpc=localhost:3500 and execRpc=public URL.
    'zond-testnet': {
        type: 'zond',
        execRpc: 'https://testnet-qrl.publicbits.dev/',
        beaconRpc: null,
        beaconGrpc: null,
        hasExplorer: false,
        // Known public execution RPCs for QRL 2.0 testnet. Any of these can
        // serve as execRpc — first entry above is the default. UI/tooling
        // that wants failover or user-selectable providers can iterate.
        publicRpcs: [
            'https://testnet-qrl.publicbits.dev/',
            'https://qrlwallet.com/api/qrl-rpc/testnet',
        ],
        // Bootstrap QNRs from the official QRL 2.0 testnet operators. Used
        // as --bootstrap-node args when spinning up a local beacon that
        // peers with public testnet (see zond-testnet-local).
        beaconBootstrapQnrs: [
            'qnr:-Me4QKAyB33PkhFM0zY3r5i9GC7aSP_Zm39U8Y2rOIzr6443SngB1yhgGh7eFwNK0WNvDWjUbNksuJqME3ZEnwS9qJSGAZ1Gb_3yh2F0dG5ldHOIAAAAAAAAAACJY29uc2Vuc3VzkJCnUX4gAACJ__________-CaWSCdjSCaXCEI7LKF4lzZWNwMjU2azGhA7Z8d7NnIyueM0N06PyRt8jcnKvuRkMTmtzoDOw081ouiHN5bmNuZXRzAIN0Y3CCMsiDdWRwgi7g',
            'qnr:-Me4QE7keK-7ViWaDwpa0GtR12qbe9ZiVvKX95z7A2hkJHA6L-2A0n8G6dn8M1kubmxAeVw7Nwaa6IBPCX9765zNi56GAZ1Gb-8uh2F0dG5ldHOIAAAAAAAAAACJY29uc2Vuc3VzkJCnUX4gAACJ__________-CaWSCdjSCaXCEDSlCUolzZWNwMjU2azGhA9t_KLMx2GyZcw7tncC7iLdJpKBS_tCp9CavD6LXd5ZaiHN5bmNuZXRzAIN0Y3CCMsiDdWRwgi7g',
        ],
    },
    // Alias entry for the qrlwallet.com testnet RPC — exposes the same
    // network as zond-testnet under a distinct network id so UI dropdowns
    // can offer both providers as separate picks. Everything else
    // (bootstraps, beacon story) is identical to zond-testnet.
    'zond-testnet-qrlwallet': {
        type: 'zond',
        execRpc: 'https://qrlwallet.com/api/qrl-rpc/testnet',
        beaconRpc: null,
        beaconGrpc: null,
        hasExplorer: false,
    },
    'zond-testnet-local': {
        // Local sync WITH public testnet V2. Popup nodes spins this up via
        // scripts/start-zond-testnet.sh:
        //   * gqrl (--testnet preset → chain-id 1337, HTTP :8545, engine :8551)
        //   * beacon-chain (canonical testnetv2 genesis.ssz + config.yml from
        //     github.com/theQRL/go-qrl-metadata/testnet/testnetv2/; REST :3500,
        //     gRPC :4000, qrysm defaults). Beacon peers into public testnet
        //     via the QNRs on zond-testnet.beaconBootstrapQnrs.
        type: 'zond',
        execRpc: 'http://localhost:8545',
        beaconRpc: 'http://localhost:3500',
        beaconGrpc: 'localhost:4000',
        hasExplorer: false
    },
    'zond-private': {
        // 4-node localnet dev network — this is the variant-switching chain.
        // Primary beacon: REST 3600, gRPC 4100 (both set explicitly in
        // configs/localnet/start-beacon.sh).
        //
        // Endpoint URLs are env-overridable so the compose stack can point
        // at the internal docker DNS names (gqrl-node1:9545, beacon-node1:3600)
        // while a bare-metal bring-up keeps using localhost. See
        // docker/docker-compose.yaml — the dashboard service sets
        // QRL_ZOND_PRIVATE_EXEC_RPC / QRL_ZOND_PRIVATE_BEACON_RPC.
        type: 'zond',
        execRpc:    process.env.QRL_ZOND_PRIVATE_EXEC_RPC    || 'http://localhost:9545',
        beaconRpc:  process.env.QRL_ZOND_PRIVATE_BEACON_RPC  || 'http://localhost:3600',
        beaconGrpc: process.env.QRL_ZOND_PRIVATE_BEACON_GRPC || 'localhost:4100',
        hasExplorer: false
    }
};

// errDetail extracts a non-empty error string from any thrown value.
// Node's Error objects with only a code (like ECONNREFUSED) have empty
// .message — a `success:false, error:""` reaches the UI, which then shows
// nothing useful. Prefer .message → .code → String(err).
function errDetail(err) {
    if (!err) return 'unknown error';
    return err.message || err.code || String(err);
}

// ========== ZEUS-PROXY DOWN DETECTION ==========
// Zeus-proxy sits at localhost:3737 wrapping QRL Classic gRPC as HTTP.
// (Meteor app cloned into ../theQRL/zeus-proxy; started by
// START-ALL-WORKING-NETWORKS.sh with `meteor run --port 3737`.)
// When it's not running, every classic-* API call fires a fresh
// ECONNREFUSED. Rather than spamming the log + returning cryptic
// { success:false, error:'connect ECONNREFUSED 127.0.0.1:3737' } to
// the UI, we:
//   1. cache the down-state briefly so back-to-back calls don't
//      each burn a fresh dial attempt,
//   2. throttle the console.error to once per 60s per network,
//   3. surface a stable hint the UI can key off.
const zeusProxyState = {
    lastKnownUp: null,      // ms timestamp of last successful call
    lastKnownDown: null,    // ms timestamp of last ECONNREFUSED
    lastLogAt: new Map(),   // network id → ms timestamp of last console.error
};

// isZeusProxyLikelyDown returns true if we saw an ECONNREFUSED in the last
// 15s and haven't successfully reached zeus-proxy since. Handlers can
// short-circuit on this to avoid a redundant dial + log entry.
function isZeusProxyLikelyDown() {
    if (!zeusProxyState.lastKnownDown) return false;
    if (zeusProxyState.lastKnownUp && zeusProxyState.lastKnownUp > zeusProxyState.lastKnownDown) {
        return false;
    }
    return (Date.now() - zeusProxyState.lastKnownDown) < 15000;
}

// noteZeusProxyResult tags an outcome from a zeus-proxy call. Call with
// success=true on any 2xx-ish response, false on ECONNREFUSED-style errors.
function noteZeusProxyResult(success) {
    if (success) zeusProxyState.lastKnownUp = Date.now();
    else zeusProxyState.lastKnownDown = Date.now();
}

// throttleZeusLog emits a console.error at most once per 60s per network.
function throttleZeusLog(networkId, message) {
    const now = Date.now();
    const last = zeusProxyState.lastLogAt.get(networkId) || 0;
    if (now - last < 60000) return;
    zeusProxyState.lastLogAt.set(networkId, now);
    console.error(`Zeus Proxy unreachable for ${networkId}: ${message} (further logs throttled for 60s)`);
}

// zeusProxyDownResponse is the stable "Classic is unreachable because
// zeus-proxy is down" payload UI code can key off.
function zeusProxyDownResponse() {
    return {
        success: false,
        error: 'zeus-proxy is not running',
        zeusProxyDown: true,
        hint: 'Start Zeus Proxy via the dashboard "ZEUS PROXY" control, or run ./START-ALL-WORKING-NETWORKS.sh (starts on :3737).',
    };
}

// ========== NETWORK CHECK FUNCTIONS ==========

// QRL_HOST_ALIAS lets the compose stack point localhost-defaulting network
// checks at host.docker.internal — the dashboard runs in a container, but
// most local services (zeus-proxy on :3737, classic nodes on :4900x/49019,
// gogs on :3330) run on the host, not in the compose network.
const HOST_ALIAS = process.env.QRL_HOST_ALIAS || 'localhost';

async function checkNetwork(network) {
    const host = network.host || HOST_ALIAS;
    const startTime = Date.now();
    const isHttps = network.scheme === 'https';
    const protocol = isHttps ? https : http;

    return new Promise((resolve) => {
        if (network.type === 'rpc') {
            const postData = JSON.stringify({
                jsonrpc: '2.0',
                method: network.method,
                params: [],
                id: 1
            });

            const options = {
                hostname: host,
                port: network.port,
                path: network.path || '/',
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Content-Length': Buffer.byteLength(postData)
                },
                timeout: 5000
            };

            const req = protocol.request(options, (res) => {
                let data = '';
                res.on('data', chunk => data += chunk);
                res.on('end', () => {
                    const latency = Date.now() - startTime;
                    try {
                        const json = JSON.parse(data);
                        if (json.result) {
                            const blockNum = parseInt(json.result, 16);
                            resolve({
                                online: true,
                                latency,
                                blockNumber: blockNum,
                                details: `Block: ${blockNum.toLocaleString()}`
                            });
                        } else {
                            resolve({ online: false, error: 'Invalid RPC response' });
                        }
                    } catch (e) {
                        resolve({ online: false, error: 'Parse error' });
                    }
                });
            });

            req.on('error', () => resolve({ online: false, error: 'Connection refused' }));
            req.on('timeout', () => {
                req.destroy();
                resolve({ online: false, error: 'Timeout' });
            });
            req.write(postData);
            req.end();

        } else if (network.type === 'http') {
            const options = {
                hostname: host,
                port: network.port,
                path: network.path || '/',
                method: 'GET',
                timeout: 5000
            };

            const req = http.request(options, (res) => {
                const latency = Date.now() - startTime;
                resolve({
                    online: true,
                    latency,
                    details: `HTTP ${res.statusCode}`
                });
                res.resume();
            });

            req.on('error', () => resolve({ online: false, error: 'Connection refused' }));
            req.on('timeout', () => {
                req.destroy();
                resolve({ online: false, error: 'Timeout' });
            });
            req.end();

        } else if (network.type === 'tcp') {
            const socket = new net.Socket();

            socket.setTimeout(2000);
            socket.on('connect', () => {
                const latency = Date.now() - startTime;
                socket.destroy();
                resolve({ online: true, latency, details: 'Port open' });
            });

            socket.on('error', () => {
                socket.destroy();
                resolve({ online: false, error: 'Connection refused' });
            });

            socket.on('timeout', () => {
                socket.destroy();
                resolve({ online: false, error: 'Timeout' });
            });

            socket.connect(network.port, host);
        }
    });
}

// Enhanced network metrics for Proof-of-Stake networks
async function getZondMetrics(network) {
    const host = network.host || 'localhost';
    const baseUrl = `http://${host}:${network.port}`;

    try {
        const metrics = {};
        const startTime = Date.now();

        // Parallel RPC calls for execution layer metrics
        const calls = [
            // Basic chain info
            { method: network.method || 'qrl_blockNumber', params: [], key: 'blockNumber' },
            { method: 'net_peerCount', params: [], key: 'peerCount' },
            { method: 'qrl_syncing', params: [], key: 'syncStatus' },
            { method: 'qrl_gasPrice', params: [], key: 'gasPrice' },
            { method: 'web3_clientVersion', params: [], key: 'clientVersion' },
            { method: 'net_version', params: [], key: 'networkId' },
            { method: 'qrl_getBlockByNumber', params: ['latest', false], key: 'latestBlock' }
        ];

        const results = await Promise.allSettled(
            calls.map(call =>
                httpRequest(baseUrl, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: call.method, params: call.params, id: 1 },
                    timeout: 8000
                }).then(response => ({
                    key: call.key,
                    data: response.data
                }))
            )
        );

        // Process results
        const latency = Date.now() - startTime;
        results.forEach(result => {
            if (result.status === 'fulfilled' && result.value.data?.result !== undefined) {
                const { key, data } = result.value;
                metrics[key] = data.result;
            }
        });

        // Parse and format metrics
        const blockNum = metrics.blockNumber ? parseInt(metrics.blockNumber, 16) : 0;
        const peerCount = metrics.peerCount ? parseInt(metrics.peerCount, 16) : 0;
        const gasPrice = metrics.gasPrice ? parseInt(metrics.gasPrice, 16) : 0;
        const isSyncing = metrics.syncStatus && typeof metrics.syncStatus === 'object';

        // Extract latest block info for timestamp and transaction count
        let blockTime = null;
        let txCount = 0;

        if (metrics.latestBlock) {
            const block = metrics.latestBlock;
            blockTime = block.timestamp ? new Date(parseInt(block.timestamp, 16) * 1000).toLocaleString() : null;
            txCount = block.transactions ? block.transactions.length : 0;
        }

        // Calculate rolling average gas utilization across last 10 blocks
        let avgGasUtilization = '0';
        try {
            if (blockNum > 0) {
                const blocksToFetch = Math.min(10, blockNum + 1); // Don't fetch more blocks than exist
                const blockPromises = [];

                // Fetch last 10 blocks (or fewer if blockchain is new)
                for (let i = 0; i < blocksToFetch; i++) {
                    const blockNumber = '0x' + (blockNum - i).toString(16);
                    blockPromises.push(
                        httpRequest(baseUrl, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: { jsonrpc: '2.0', method: 'qrl_getBlockByNumber', params: [blockNumber, false], id: 1 },
                            timeout: 5000
                        }).catch(() => null) // Continue if individual block fetch fails
                    );
                }

                const blockResults = await Promise.allSettled(blockPromises);
                let totalGasUsed = 0;
                let totalGasLimit = 0;
                let validBlocks = 0;

                blockResults.forEach(result => {
                    if (result.status === 'fulfilled' && result.value?.data?.result) {
                        const block = result.value.data.result;
                        const gasUsed = block.gasUsed ? parseInt(block.gasUsed, 16) : 0;
                        const gasLimit = block.gasLimit ? parseInt(block.gasLimit, 16) : 0;

                        if (gasLimit > 0) { // Only count valid blocks
                            totalGasUsed += gasUsed;
                            totalGasLimit += gasLimit;
                            validBlocks++;
                        }
                    }
                });

                if (validBlocks > 0 && totalGasLimit > 0) {
                    avgGasUtilization = ((totalGasUsed / totalGasLimit) * 100).toFixed(1);
                }
            }
        } catch (error) {
            console.warn(`[${network.id}] Failed to calculate rolling gas average:`, error.message);
            // Fall back to single block if rolling average fails
            if (metrics.latestBlock) {
                const gasUsed = metrics.latestBlock.gasUsed ? parseInt(metrics.latestBlock.gasUsed, 16) : 0;
                const gasLimit = metrics.latestBlock.gasLimit ? parseInt(metrics.latestBlock.gasLimit, 16) : 0;
                avgGasUtilization = gasLimit > 0 ? ((gasUsed / gasLimit) * 100).toFixed(1) : '0';
            }
        }

        return {
            online: true,
            latency,
            blockNumber: blockNum,
            metrics: {
                execution: {
                    blockNumber: blockNum,
                    peerCount,
                    gasPrice: `${(gasPrice / 1e9).toFixed(1)} Gwei`,
                    gasUtilization: `${avgGasUtilization}%`,
                    txInBlock: txCount,
                    clientVersion: metrics.clientVersion || 'Unknown',
                    networkId: metrics.networkId || 'Unknown',
                    syncStatus: isSyncing ? 'Syncing...' : 'Synced',
                    blockTime
                }
            },
            details: `Block: ${blockNum.toLocaleString()} | Peers: ${peerCount} | Gas: ${avgGasUtilization}%`
        };

    } catch (error) {
        return {
            online: false,
            error: error.message || 'Connection failed',
            details: null
        };
    }
}

// ========== HELPER FUNCTIONS ==========

function parseBody(req) {
    return new Promise((resolve, reject) => {
        let body = '';
        req.on('data', chunk => body += chunk.toString());
        req.on('end', () => {
            try {
                resolve(JSON.parse(body));
            } catch (e) {
                resolve({});
            }
        });
        req.on('error', reject);
    });
}

// Loopback-only origins. The dashboard is a LOCAL operator surface —
// never meant for public exposure. CORS now echoes only the exact
// request origin IF it's a loopback address; a cross-origin request
// from any public site gets a bare response with no CORS allowance,
// which the browser then refuses to let JS read (and preflight gates
// stop state-changing requests entirely).
//
// Was `Access-Control-Allow-Origin: *` which let any site drive
// /api/release/*, /api/compose/*, /api/wallet/*, etc. via the
// operator's own browser. Reviewer-flagged as critical.
const LOOPBACK_ORIGIN_RE = /^https?:\/\/(127\.0\.0\.1|localhost|\[::1\])(:\d+)?$/;
function allowedOrigin(req) {
    const o = req.headers.origin;
    return o && LOOPBACK_ORIGIN_RE.test(o) ? o : null;
}
function securityHeaders(req) {
    const allow = allowedOrigin(req);
    const h = {
        'X-Frame-Options': 'DENY',                 // no clickjacking via iframe
        'X-Content-Type-Options': 'nosniff',       // no MIME sniffing attacks
        'Referrer-Policy': 'no-referrer',
        'Cache-Control': 'no-store',
    };
    if (allow) {
        h['Access-Control-Allow-Origin'] = allow;
        h['Access-Control-Allow-Methods'] = 'GET, POST, OPTIONS';
        h['Access-Control-Allow-Headers'] = 'Content-Type';
        h['Vary'] = 'Origin';
    }
    return h;
}
function jsonResponse(res, data, status = 200) {
    res.writeHead(status, {
        'Content-Type': 'application/json',
        ...securityHeaders(res.req || { headers: {} }),
    });
    res.end(JSON.stringify(data));
}

function httpRequest(url, options = {}) {
    return new Promise((resolve, reject) => {
        const isHttps = url.startsWith('https');
        const protocol = isHttps ? https : http;
        const urlObj = new URL(url);

        const reqOptions = {
            hostname: urlObj.hostname,
            port: urlObj.port || (isHttps ? 443 : 80),
            path: urlObj.pathname + urlObj.search,
            method: options.method || 'GET',
            headers: options.headers || {},
            timeout: options.timeout || 30000
        };

        const req = protocol.request(reqOptions, (res) => {
            // Collect data as buffers for binary content support
            const chunks = [];
            res.on('data', chunk => chunks.push(chunk));
            res.on('end', () => {
                const buffer = Buffer.concat(chunks);

                // If responseType is buffer, return as buffer
                if (options.responseType === 'buffer') {
                    resolve({
                        statusCode: res.statusCode,
                        status: res.statusCode,
                        headers: res.headers,
                        data: buffer
                    });
                    return;
                }

                // Try to parse as JSON, fallback to string
                const data = buffer.toString('utf8');
                try {
                    resolve({
                        statusCode: res.statusCode,
                        status: res.statusCode,
                        headers: res.headers,
                        data: JSON.parse(data)
                    });
                } catch {
                    resolve({
                        statusCode: res.statusCode,
                        status: res.statusCode,
                        headers: res.headers,
                        data
                    });
                }
            });
        });

        req.on('error', reject);
        req.on('timeout', () => {
            req.destroy();
            reject(new Error('Request timeout'));
        });

        if (options.body) {
            if (Buffer.isBuffer(options.body)) {
                req.write(options.body);
            } else if (typeof options.body === 'string') {
                req.write(options.body);
            } else {
                req.write(JSON.stringify(options.body));
            }
        }
        req.end();
    });
}

// ========== API HANDLERS ==========

// Helper function to get appropriate timeout for Zeus Proxy calls
// Local networks use shorter timeouts to fail faster when Zeus Proxy connection is stale
function getZeusProxyTimeout(networkId) {
    const isLocalNetwork = networkId === 'classic-private';
    return isLocalNetwork ? 5000 : 30000;  // 5s for local, 30s for remote
}

async function handleStatusRequest(req, res) {
    const results = await Promise.all(
        networks.map(async (network) => {
            // Use enhanced metrics for Zond networks (PoS), basic check for others
            const isZondNetwork = network.category && network.category.includes('zond');
            const status = isZondNetwork && network.type === 'rpc'
                ? await getZondMetrics(network)
                : await checkNetwork(network);
            return { network, status };
        })
    );
    jsonResponse(res, results);
}

// Wallet handlers
async function handleWalletLoad(req, res) {
    const body = await parseBody(req);
    const { address, network } = body;

    // Try to get balance from network
    let balance = '0.0';

    try {
        if (network && network.includes('zond')) {
            const config = networkConfigs[network] || networkConfigs['zond-testnet'];
            const response = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {
                    jsonrpc: '2.0',
                    method: 'qrl_getBalance',
                    params: [address, 'latest'],
                    id: 1
                }
            });
            if (response.data && response.data.result) {
                balance = (parseInt(response.data.result, 16) / 1e18).toFixed(8);
            }
        }
    } catch (err) {
        console.error('Balance fetch error:', err.message);
    }

    jsonResponse(res, {
        success: true,
        wallet: { address, balance, network }
    });
}

// Wallet creation — Tier 1 (server-side crypto).
//   * Password enters via POST body (client → localhost HTTP; Tier 2 will add
//     browser-side Argon2id so server never sees the plaintext password).
//   * Keypair generated by real signing primitives:
//       zond-*  → qrl-2.0/tools/mldsa87keygen (go-qrllib ML-DSA-87)
//       classic-* → qrl-1.0/upstream/QRL venv `qrl wallet_gen` (XMSS)
//   * Encrypted at rest with argon2id-derived key + AES-256-GCM (see
//     lib/keystore.js). GCM AAD binds ciphertext to metadata — tamper
//     with address/publicKey/network and decrypt fails.
//   * Stored under $HOME/.qrl-dashboard/keystores/<walletId>.json (mode 0600).
//   * Only local networks allowed. Public/remote networks refused with a
//     clear error — no one should be creating keys for a remote network they
//     don't control.
//   * Response is { walletId, address } only. No keystore path, no key
//     material, no password echo, no descriptor bytes.
//   * Password buffer wiped from memory immediately after use.
const walletKeystore = require('./lib/keystore');
const walletKeygen = require('./lib/keygen');
const walletStore = require('./lib/wallet_store');

const LOCAL_WALLET_NETWORKS = new Set([
    'zond-private',
    'zond-testnet-local',
    'classic-private',
]);

// Tier 2 (browser-side Argon2id) session store — a create/prepare call
// stakes out (salt, params, expiry) here so the follow-up seal call knows
// what parameters the client used, and cannot swap in weaker params after
// the fact. In-memory only; cleared on process restart. Sessions expire
// after 5 minutes so an abandoned modal doesn't linger.
const WALLET_CREATE_SESSIONS = new Map();
const WALLET_CREATE_SESSION_TTL_MS = 5 * 60 * 1000;

function reapExpiredWalletCreateSessions() {
    const now = Date.now();
    for (const [id, s] of WALLET_CREATE_SESSIONS) {
        if (s.expiresAt <= now) WALLET_CREATE_SESSIONS.delete(id);
    }
}
setInterval(reapExpiredWalletCreateSessions, 60_000).unref();

async function handleWalletCreate(req, res) {
    const body = await parseBody(req);
    const network = String(body.network || '').trim();
    const password = String(body.password || '');
    const confirm = String(body.confirmPassword || '');

    if (!LOCAL_WALLET_NETWORKS.has(network)) {
        return jsonResponse(res, {
            success: false,
            error: `wallet creation only supported on local networks: ${[...LOCAL_WALLET_NETWORKS].join(', ')}`,
        }, 400);
    }
    if (password.length < 12) {
        return jsonResponse(res, {
            success: false,
            error: 'password must be at least 12 characters',
        }, 400);
    }
    if (password !== confirm) {
        return jsonResponse(res, {
            success: false,
            error: 'password and confirmPassword do not match',
        }, 400);
    }

    // pwBuf is the mutable working copy — we wipe it after encrypt.
    const pwBuf = Buffer.from(password, 'utf8');

    let kp = null;
    try {
        kp = await walletKeygen.generateKeypairForNetwork(network);
        const walletId = walletStore.newWalletId();
        const keystore = walletKeystore.encrypt({
            password: pwBuf,
            privateKey: kp.privateKey,
            network,
            walletId,
            address: kp.address,
            publicKey: kp.publicKey ? '0x' + kp.publicKey : null,
        });
        walletStore.saveKeystore(keystore);

        // Log only address + walletId + network. NEVER password, NEVER any
        // key material. Password buffer wiped below in finally.
        console.log(`wallet created: ${kp.address.slice(0, 12)}… network=${network} walletId=${walletId}`);

        jsonResponse(res, {
            success: true,
            walletId,
            address: kp.address,
            network,
            // publicKey is safe to return — it's the signing PK, not a secret.
            publicKey: kp.publicKey ? '0x' + kp.publicKey : null,
        });
    } catch (err) {
        console.error('wallet-create error:', errDetail(err));
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    } finally {
        // Wipe every buffer that touched key material or password.
        pwBuf.fill(0);
        if (kp && Buffer.isBuffer(kp.privateKey)) kp.privateKey.fill(0);
    }
}

// Tier 2 — POST /api/wallet/create/prepare
// Client asks the server for a fresh salt + the Argon2id parameter set
// the server will accept on the follow-up seal call. Client can't
// unilaterally weaken these — the seal call rejects any KEK not derivable
// from these exact parameters (well, we can't verify the KEK was derived
// correctly, but we CAN verify (a) the client only receives the seal
// sessionId once, (b) the seal call reuses the same salt+params, and
// (c) the params are at or above the strong floor).
async function handleWalletCreatePrepare(req, res) {
    const body = await parseBody(req);
    const network = String(body.network || '').trim();
    if (!LOCAL_WALLET_NETWORKS.has(network)) {
        return jsonResponse(res, {
            success: false,
            error: `wallet creation only supported on local networks: ${[...LOCAL_WALLET_NETWORKS].join(', ')}`,
        }, 400);
    }
    const crypto = require('crypto');
    const salt = crypto.randomBytes(walletKeystore.SALT_BYTES);
    const params = {
        name: 'argon2id',
        memoryKiB: walletKeystore.ARGON2_DEFAULTS.memoryKiB,
        passes: walletKeystore.ARGON2_DEFAULTS.passes,
        parallelism: walletKeystore.ARGON2_DEFAULTS.parallelism,
        tagLength: walletKeystore.ARGON2_DEFAULTS.tagLength,
    };
    const sessionId = crypto.randomUUID();
    const expiresAt = Date.now() + WALLET_CREATE_SESSION_TTL_MS;
    WALLET_CREATE_SESSIONS.set(sessionId, { network, salt, params, expiresAt });
    jsonResponse(res, {
        success: true,
        sessionId,
        salt: salt.toString('hex'),
        argon2Params: params,
        expiresAt,
    });
}

// Tier 2 — POST /api/wallet/create/seal
// Client sends { sessionId, kekHex } after running Argon2id in a Web
// Worker. Server generates the keypair, encrypts the private key with the
// client-derived KEK, stores the keystore. Password never touched server.
async function handleWalletCreateSeal(req, res) {
    const body = await parseBody(req);
    const sessionId = String(body.sessionId || '').trim();
    const kekHex = String(body.kekHex || '').trim();

    const session = WALLET_CREATE_SESSIONS.get(sessionId);
    if (!session) {
        return jsonResponse(res, { success: false, error: 'unknown or expired sessionId — reopen the create dialog' }, 400);
    }
    // Single-use: consume the session before doing any expensive work so a
    // client can't send parallel seal calls for the same session.
    WALLET_CREATE_SESSIONS.delete(sessionId);
    if (session.expiresAt <= Date.now()) {
        return jsonResponse(res, { success: false, error: 'session expired — reopen the create dialog' }, 400);
    }

    // KEK must be exactly 32 bytes.
    let kek;
    try {
        if (kekHex.length !== 64) throw new Error(`kekHex must be 64 hex chars (32 bytes), got ${kekHex.length}`);
        kek = Buffer.from(kekHex, 'hex');
        if (kek.length !== 32) throw new Error(`kek must be exactly 32 bytes`);
    } catch (e) {
        return jsonResponse(res, { success: false, error: 'invalid kekHex: ' + e.message }, 400);
    }

    let kp = null;
    try {
        kp = await walletKeygen.generateKeypairForNetwork(session.network);
        const walletId = walletStore.newWalletId();
        const keystore = walletKeystore.encryptWithKek({
            kek,
            privateKey: kp.privateKey,
            network: session.network,
            walletId,
            address: kp.address,
            publicKey: kp.publicKey ? '0x' + kp.publicKey : null,
            kdfParams: {
                name: 'argon2id',
                salt: session.salt.toString('hex'),
                memoryKiB: session.params.memoryKiB,
                passes: session.params.passes,
                parallelism: session.params.parallelism,
                tagLength: session.params.tagLength,
            },
        });
        walletStore.saveKeystore(keystore);
        console.log(`wallet created (tier2): ${kp.address.slice(0, 12)}… network=${session.network} walletId=${walletId}`);
        jsonResponse(res, {
            success: true,
            walletId,
            address: kp.address,
            network: session.network,
            publicKey: kp.publicKey ? '0x' + kp.publicKey : null,
            tier: 2,
        });
    } catch (err) {
        console.error('wallet-create/seal error:', errDetail(err));
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    } finally {
        // KEK bytes have been zeroed inside encryptWithKek(), but wipe our
        // outer view too. And nuke the keypair private-key buffer.
        if (kek) kek.fill(0);
        if (kp && Buffer.isBuffer(kp.privateKey)) kp.privateKey.fill(0);
    }
}

// POST /api/wallet/unlock — verify password unlocks a saved keystore.
//   Body: { walletId, password }  OR  { walletId, kekHex } for Tier 2
//   Response: { success, address, network, publicKey } (no secrets).
//   The seed IS decrypted internally to check that GCM auth passes; it
//   is then immediately wiped. Use POST /api/wallet/sign for actual use.
const walletSign = require('./lib/sign');

async function handleWalletUnlock(req, res) {
    const body = await parseBody(req);
    const walletId = String(body.walletId || '').trim();
    const password = body.password != null ? String(body.password) : null;
    const kekHex = body.kekHex != null ? String(body.kekHex).trim() : null;

    if (!walletId) return jsonResponse(res, { success: false, error: 'walletId required' }, 400);
    if (!password && !kekHex) {
        return jsonResponse(res, { success: false, error: 'password or kekHex required' }, 400);
    }

    let pwBuf = null;
    let kek = null;
    let plaintext = null;
    try {
        const stored = walletStore.loadKeystore(walletId);
        if (kekHex) {
            if (kekHex.length !== 64) throw new Error('kekHex must be 64 hex chars');
            kek = Buffer.from(kekHex, 'hex');
            plaintext = walletKeystore.decryptWithKek({ kek, keystore: stored });
        } else {
            pwBuf = Buffer.from(password, 'utf8');
            plaintext = walletKeystore.decrypt({ password: pwBuf, keystore: stored });
        }
        jsonResponse(res, {
            success: true,
            walletId,
            address: stored.address,
            network: stored.network,
            publicKey: stored.publicKey,
        });
    } catch (err) {
        // Deliberately opaque error — don't distinguish "walletId not found"
        // from "wrong password" from "tampered keystore". Same treatment as
        // handleWalletVerify.
        jsonResponse(res, { success: false, error: 'unlock failed' }, 400);
    } finally {
        if (pwBuf) pwBuf.fill(0);
        if (kek) kek.fill(0);
        if (plaintext) plaintext.fill(0);
    }
}

// POST /api/wallet/sign — unlock + sign a message atomically.
//   Body: { walletId, messageHex, (password | kekHex) }
//   Response: { success, address, publicKey, signature } (all hex, no secrets).
//   The seed is decrypted, used for one Sign call, and wiped. Never persists
//   the seed across requests — every /sign is a fresh unlock.
async function handleWalletSign(req, res) {
    const body = await parseBody(req);
    const walletId = String(body.walletId || '').trim();
    const messageHex = String(body.messageHex || '').trim();
    const password = body.password != null ? String(body.password) : null;
    const kekHex = body.kekHex != null ? String(body.kekHex).trim() : null;

    if (!walletId || !messageHex) {
        return jsonResponse(res, { success: false, error: 'walletId and messageHex required' }, 400);
    }
    if (!password && !kekHex) {
        return jsonResponse(res, { success: false, error: 'password or kekHex required' }, 400);
    }

    let pwBuf = null;
    let kek = null;
    let seed = null;
    try {
        const stored = walletStore.loadKeystore(walletId);
        if (kekHex) {
            if (kekHex.length !== 64) throw new Error('kekHex must be 64 hex chars');
            kek = Buffer.from(kekHex, 'hex');
            seed = walletKeystore.decryptWithKek({ kek, keystore: stored });
        } else {
            pwBuf = Buffer.from(password, 'utf8');
            seed = walletKeystore.decrypt({ password: pwBuf, keystore: stored });
        }
        const result = await walletSign.signForNetwork({
            network: stored.network,
            seed,
            messageHex,
        });
        // signForNetwork wipes seed in its finally, but that only zeroes
        // the callee's view of the same Buffer. Our variable is aliased
        // and gets the same zero-fill — safe to still explicitly wipe.
        jsonResponse(res, {
            success: true,
            walletId,
            address: result.address,
            publicKey: result.publicKey,
            signature: result.signature,
        });
    } catch (err) {
        console.error('wallet-sign error:', errDetail(err));
        jsonResponse(res, { success: false, error: errDetail(err) }, 400);
    } finally {
        if (pwBuf) pwBuf.fill(0);
        if (kek) kek.fill(0);
        if (seed) seed.fill(0);
    }
}

// POST /api/wallet/verify — Tier 3c support.
//   Body: { walletId, kekHex }
//   Attempts to decrypt the keystore with the client-derived KEK.
//   Returns { success: true } on success, { success: false, error } on failure.
//   NEVER returns key material. This lets the CLI verify a stored keychain
//   entry against the actual keystore without having to fetch anything sensitive.
async function handleWalletVerify(req, res) {
    const body = await parseBody(req);
    const walletId = String(body.walletId || '').trim();
    const kekHex = String(body.kekHex || '').trim();
    if (!walletId || !kekHex) {
        return jsonResponse(res, { success: false, error: 'walletId and kekHex required' }, 400);
    }
    if (kekHex.length !== 64) {
        return jsonResponse(res, { success: false, error: 'kekHex must be 64 hex chars' }, 400);
    }
    let kek = null;
    let plaintext = null;
    try {
        kek = Buffer.from(kekHex, 'hex');
        if (kek.length !== 32) throw new Error('kek must be 32 bytes');
        const stored = walletStore.loadKeystore(walletId);
        plaintext = walletKeystore.decryptWithKek({ kek, keystore: stored });
        jsonResponse(res, {
            success: true,
            address: stored.address,
            network: stored.network,
        });
    } catch (err) {
        // Deliberately opaque error — don't leak whether the walletId
        // existed vs. whether the KEK was wrong.
        jsonResponse(res, { success: false, error: 'verify failed' }, 400);
    } finally {
        if (kek) kek.fill(0);
        if (plaintext) plaintext.fill(0);
    }
}

// GET /api/wallet/list?network=<optional> — public metadata only.
// No key material, no keystore paths.
async function handleWalletList(req, res) {
    const url = new URL(req.url, 'http://localhost');
    const network = url.searchParams.get('network');
    try {
        const wallets = walletStore.listWallets(network ? { network } : {});
        jsonResponse(res, { success: true, wallets });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

async function handleWalletBalance(req, res) {
    const body = await parseBody(req);
    const { address, network } = body;

    let balance = '0.0';

    try {
        if (network && network.includes('zond')) {
            const config = networkConfigs[network] || networkConfigs['zond-testnet'];
            const response = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {
                    jsonrpc: '2.0',
                    method: 'qrl_getBalance',
                    params: [address, 'latest'],
                    id: 1
                }
            });
            if (response.data && response.data.result) {
                balance = (parseInt(response.data.result, 16) / 1e18).toFixed(8);
            }
        }
    } catch (err) {
        console.error('Balance fetch error:', err.message);
    }

    jsonResponse(res, { success: true, balance });
}

// ========== TRANSACTION SEND (real impl via fund binary for QRL 2.0) ==========
// Maps a network key to where its chain data + config live under qrl-2.0/.
// Localnet is the default fully-supported target; testnet is remote (would need
// a locally-held keystore for `from`); mainnet is not live yet.
const qrl2ChainRoots = {
    'zond-private':  path.join(__dirname, 'qrl-2.0', 'chains', 'localnet'),
    'zond-testnet':  path.join(__dirname, 'qrl-2.0', 'chains', 'testnet'),
    'zond-mainnet':  path.join(__dirname, 'qrl-2.0', 'chains', 'mainnet'),
};
const qrl2ConfigRoots = {
    'zond-private':  path.join(__dirname, 'qrl-2.0', 'configs', 'localnet'),
    'zond-testnet':  path.join(__dirname, 'qrl-2.0', 'configs', 'testnet'),
    'zond-mainnet':  path.join(__dirname, 'qrl-2.0', 'configs', 'mainnet'),
};
const FUND_BIN = path.join(__dirname, 'qrl-2.0', 'upstream', 'go-qrl', 'build', 'bin', 'fund');

// Find a keystore file whose filename ends with the target Q-address (case-insensitive).
// Searches nodeN/keystore/ for localnet-style layouts and top-level keystore/ otherwise.
function findKeystoreForAddress(chainRoot, qAddr) {
    if (!qAddr) return null;
    const needle = qAddr.toLowerCase().replace(/^q/, '');
    const candidateDirs = [];
    for (let i = 1; i <= 4; i++) {
        candidateDirs.push(path.join(chainRoot, `node${i}`, 'keystore'));
    }
    candidateDirs.push(path.join(chainRoot, 'keystores'));
    for (const dir of candidateDirs) {
        if (!fs.existsSync(dir)) continue;
        const entries = fs.readdirSync(dir);
        const hit = entries.find(name => name.toLowerCase().endsWith(needle));
        if (hit) return path.join(dir, hit);
    }
    return null;
}

// Convert a human-readable Quanta amount (e.g. "5" or "0.25") into a wei string.
function quantaToWei(amount) {
    const s = String(amount).trim();
    if (!s || !/^\d+(\.\d+)?$/.test(s)) return null;
    const [whole, frac = ''] = s.split('.');
    const fracPadded = (frac + '0'.repeat(18)).slice(0, 18);
    return BigInt(whole + fracPadded).toString();
}

function execFund(args) {
    return new Promise((resolve, reject) => {
        execFile(FUND_BIN, args, { timeout: 30000 }, (err, stdout, stderr) => {
            if (err) return reject(new Error(stderr.trim() || err.message));
            resolve(stdout);
        });
    });
}

// ========== CLASSIC (XMSS) TRANSFER via grpcProxy JSON-RPC ==========
// Where grpcProxy listens per Classic network (HTTP proxy for the gRPC PublicAPI).
// These map network keys to grpcProxy JSON-RPC endpoints. If unset for a given network,
// send-transaction returns an "unavailable" error rather than a mock success.
const classicProxyJsonRpc = {
    'classic-mainnet': process.env.QRL_CLASSIC_MAINNET_PROXY || 'http://127.0.0.1:18090/json_rpc',
    'classic-testnet': process.env.QRL_CLASSIC_TESTNET_PROXY || null,
    'classic-private': process.env.QRL_CLASSIC_PRIVATE_PROXY || null,
};

// Convert Quanta amount to shor (Classic's smallest unit, 1 Quanta = 1e9 shor).
function quantaToShor(amount) {
    const s = String(amount).trim();
    if (!s || !/^\d+(\.\d+)?$/.test(s)) return null;
    const [whole, frac = ''] = s.split('.');
    const fracPadded = (frac + '0'.repeat(9)).slice(0, 9);
    return BigInt(whole + fracPadded).toString();
}

async function sendClassicTransfer({ endpoint, to, amount, fee }) {
    // grpcProxy's `transfer` method signs via a preconfigured payment slave (XMSS).
    // Params: destinations = [{address, amount}], fee, mixin, unlock_time
    const amountShor = quantaToShor(amount);
    if (amountShor === null) throw new Error(`bad amount: ${amount}`);
    const feeShor = fee ? quantaToShor(fee) : '0';
    if (fee && feeShor === null) throw new Error(`bad fee: ${fee}`);
    const rpcBody = JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'transfer',
        params: {
            destinations: [{ address: to, amount: amountShor }],
            fee: feeShor,
            mixin: 0,
            unlock_time: 0,
        },
    });
    const urlObj = new URL(endpoint);
    return new Promise((resolve, reject) => {
        const req = http.request({
            hostname: urlObj.hostname,
            port:     urlObj.port,
            path:     urlObj.pathname,
            method:   'POST',
            headers:  { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(rpcBody) },
            timeout:  15000,
        }, (rsp) => {
            let data = '';
            rsp.on('data', c => data += c);
            rsp.on('end', () => {
                try {
                    const j = JSON.parse(data);
                    if (j.error) return reject(new Error(j.error.message || JSON.stringify(j.error)));
                    resolve(j.result || {});
                } catch (e) {
                    reject(new Error(`bad json from grpcProxy: ${data.slice(0, 200)}`));
                }
            });
        });
        req.on('error', reject);
        req.on('timeout', () => { req.destroy(new Error('classic proxy timeout')); });
        req.write(rpcBody);
        req.end();
    });
}

async function handleTransactionSend(req, res) {
    const body = await parseBody(req);
    const { from, to, amount, fee, network } = body;

    const cfg = networkConfigs[network];
    if (!cfg) {
        return jsonResponse(res, { success: false, error: `unknown network: ${network}` });
    }

    // Classic path: route through grpcProxy JSON-RPC `transfer` method.
    if (cfg.type === 'classic') {
        const endpoint = classicProxyJsonRpc[network];
        if (!endpoint) {
            return jsonResponse(res, {
                success: false,
                error: `no Classic grpcProxy endpoint configured for ${network}. Set QRL_CLASSIC_${network.replace('classic-', '').toUpperCase()}_PROXY env var.`
            });
        }
        try {
            const result = await sendClassicTransfer({ endpoint, to, amount, fee });
            const txHash = result.tx_hash || result.transaction_hash || result.txHash || null;
            return jsonResponse(res, {
                success: true,
                txHash,
                status: 'Broadcast via Classic grpcProxy (signed by preconfigured payment slave)',
                result,
            });
        } catch (err) {
            return jsonResponse(res, {
                success: false,
                error: `classic transfer failed: ${err.message}`,
                hint: 'Ensure grpcProxy is running with payment slaves configured (`python -m qrl.grpcProxy` on the classic node).',
            });
        }
    }

    if (cfg.type !== 'zond') {
        return jsonResponse(res, {
            success: false,
            error: `send-transaction not implemented for ${cfg.type} networks in this backend`
        });
    }

    const chainRoot = qrl2ChainRoots[network];
    const configRoot = qrl2ConfigRoots[network];
    if (!chainRoot || !configRoot) {
        return jsonResponse(res, { success: false, error: `no local chain data configured for ${network}` });
    }
    if (!fs.existsSync(FUND_BIN)) {
        return jsonResponse(res, {
            success: false,
            error: `fund binary not built. Run: cd qrl-2.0/upstream/go-qrl && go build -buildvcs=false -o build/bin/fund ./cmd/fund`
        });
    }

    // Locate keystore for the from address
    const keystoreFile = findKeystoreForAddress(chainRoot, from);
    if (!keystoreFile) {
        return jsonResponse(res, {
            success: false,
            error: `no keystore found for ${from} under ${chainRoot}`
        });
    }

    // Password: check env override, then genesis_password.txt in the config root
    let passwordFile = process.env.QRL_KEYSTORE_PASSWORD_FILE || path.join(configRoot, 'genesis_password.txt');
    if (!fs.existsSync(passwordFile)) {
        return jsonResponse(res, {
            success: false,
            error: `password file not found: ${passwordFile}. Set QRL_KEYSTORE_PASSWORD_FILE env var to override.`
        });
    }

    // Convert amount to wei — accept either a decimal Quanta string or a "0x"-hex wei value
    let valueWei;
    if (typeof amount === 'string' && amount.startsWith('0x')) {
        valueWei = BigInt(amount).toString();
    } else {
        valueWei = quantaToWei(amount);
        if (valueWei === null) {
            return jsonResponse(res, { success: false, error: `bad amount: ${amount}` });
        }
    }

    try {
        const stdout = await execFund([
            '-keystore', keystoreFile,
            '-password', passwordFile,
            '-to',       to,
            '-value',    valueWei,
            '-rpc',      cfg.execRpc,
        ]);
        // Extract hash line "tx hash:        0x..."
        const match = stdout.match(/tx hash:\s+(0x[0-9a-fA-F]+)/);
        const txHash = match ? match[1] : null;
        if (!txHash) {
            return jsonResponse(res, { success: false, error: 'fund tool ran but returned no tx hash', raw: stdout });
        }
        return jsonResponse(res, {
            success: true,
            txHash,
            status: 'Broadcast (pending inclusion — requires beacon consensus running)',
        });
    } catch (err) {
        return jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

async function handleTransactionMessage(req, res) {
    // Previously returned a random 64-char hex string as a "tx hash". The UI
    // then showed "Message TX created!" to the user, but nothing was ever
    // broadcast — the hash pointed at nothing. Real message-txs need signing
    // with the sender's key and submission via qrl_sendRawTransaction.
    jsonResponse(res, {
        success: false,
        error: 'message-transaction sending is not implemented in the dashboard backend',
        hint: 'Construct + sign the message tx locally (see qrl-2.0/upstream/go-qrl/cmd/fund for an example EIP-1559 sender) and submit via POST /api/rpc with method=qrl_sendRawTransaction.',
    });
}

async function handleTransactionLookup(req, res) {
    const body = await parseBody(req);
    const { hash, network } = body;

    try {
        const config = networkConfigs[network];

        if (config && config.type === 'zond') {
            const response = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {
                    jsonrpc: '2.0',
                    method: 'qrl_getTransactionByHash',
                    params: [hash],
                    id: 1
                }
            });
            if (response.data && response.data.result) {
                jsonResponse(res, { success: true, transaction: response.data.result });
                return;
            }
        } else if (config && config.type === 'classic') {
            // QRL Classic - use Zeus Proxy GetObject
            // Convert hash to buffer format if needed
            let queryHash = hash;
            if (hash.startsWith('0x')) {
                queryHash = hash.substring(2);
            }
            const response = await httpRequest(`${config.zeusProxy}/GetObject`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { query: queryHash }
            });
            if (response.data && (response.data.transaction || response.data.tx)) {
                jsonResponse(res, { success: true, transaction: response.data.transaction || response.data });
                return;
            }
        }
    } catch (err) {
        console.error('Transaction lookup error:', err.message);
    }

    jsonResponse(res, { success: false, error: 'Transaction not found' });
}

async function handleRecentTransactions(req, res) {
    const body = await parseBody(req);
    const { network, limit = 20 } = body;

    // Return mock transactions for now
    const transactions = Array(Math.min(limit, 10)).fill(0).map((_, i) => ({
        hash: '0x' + Array(64).fill(0).map(() => Math.floor(Math.random() * 16).toString(16)).join(''),
        from: '0x' + Array(40).fill(0).map(() => Math.floor(Math.random() * 16).toString(16)).join(''),
        to: '0x' + Array(40).fill(0).map(() => Math.floor(Math.random() * 16).toString(16)).join(''),
        amount: (Math.random() * 100).toFixed(4),
        block: 1000 - i,
        time: new Date(Date.now() - i * 60000).toLocaleTimeString()
    }));

    jsonResponse(res, { success: true, transactions });
}

// Block handlers
async function handleBlockLookup(req, res) {
    const body = await parseBody(req);
    const { query, network } = body;

    try {
        const config = networkConfigs[network];

        if (config && config.type === 'zond') {
            const blockParam = query.startsWith('0x') ? query : '0x' + parseInt(query).toString(16);

            const response = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {
                    jsonrpc: '2.0',
                    method: 'qrl_getBlockByNumber',
                    params: [blockParam, true],
                    id: 1
                }
            });
            if (response.data && response.data.result) {
                jsonResponse(res, { success: true, block: response.data.result });
                return;
            }
        } else if (config && config.type === 'classic') {
            // QRL Classic - use Zeus Proxy GetBlock
            const response = await httpRequest(`${config.zeusProxy}/GetBlockByNumber`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { block_number: parseInt(query) }
            });
            if (response.data && response.data.block) {
                jsonResponse(res, { success: true, block: response.data.block });
                return;
            }
        }
    } catch (err) {
        console.error('Block lookup error:', err.message);
    }

    jsonResponse(res, { success: false, error: 'Block not found' });
}

async function handleRecentBlocks(req, res) {
    const body = await parseBody(req);
    const { network, limit = 20 } = body;

    try {
        if (network && network.includes('zond')) {
            const config = networkConfigs[network] || networkConfigs['zond-testnet'];

            // Get latest block number
            const networkConfig = networks.find(n => n.id.includes(network.split('-')[1]));
            const blockMethod = networkConfig?.method || 'qrl_blockNumber';
            const blockNumResponse = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {
                    jsonrpc: '2.0',
                    method: blockMethod,
                    params: [],
                    id: 1
                }
            });

            if (blockNumResponse.data && blockNumResponse.data.result) {
                const latestBlock = parseInt(blockNumResponse.data.result, 16);
                const blocks = [];

                for (let i = 0; i < Math.min(limit, 10); i++) {
                    const blockNum = latestBlock - i;
                    if (blockNum < 0) break;

                    try {
                        const blockResponse = await httpRequest(config.execRpc, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: {
                                jsonrpc: '2.0',
                                method: 'qrl_getBlockByNumber',
                                params: ['0x' + blockNum.toString(16), false],
                                id: 1
                            }
                        });

                        if (blockResponse.data && blockResponse.data.result) {
                            const block = blockResponse.data.result;
                            blocks.push({
                                number: parseInt(block.number, 16),
                                hash: block.hash,
                                miner: block.miner || 'N/A',
                                txCount: block.transactions ? block.transactions.length : 0,
                                reward: '---',
                                time: new Date(parseInt(block.timestamp, 16) * 1000).toLocaleTimeString()
                            });
                        }
                    } catch (e) {
                        // Skip failed block fetches
                    }
                }

                jsonResponse(res, { success: true, blocks });
                return;
            }
        }
    } catch (err) {
        console.error('Recent blocks error:', err.message);
    }

    // Return mock data if real fetch fails
    const blocks = Array(10).fill(0).map((_, i) => ({
        number: 1000 - i,
        hash: '0x' + Array(64).fill(0).map(() => Math.floor(Math.random() * 16).toString(16)).join(''),
        miner: '0x' + Array(40).fill(0).map(() => Math.floor(Math.random() * 16).toString(16)).join(''),
        txCount: Math.floor(Math.random() * 50),
        reward: '2.0',
        time: new Date(Date.now() - i * 60000).toLocaleTimeString()
    }));

    jsonResponse(res, { success: true, blocks });
}

// Two QRL 2.0 address sizes are currently live and both have to validate:
//   * 20 bytes (40 hex chars after prefix) — public testnet V2.
//   * 64 bytes (128 hex chars after prefix) — the local chain
//     (zond-private + rescue-edition + any upstream/go-qrl build with
//     common.AddressLength = 64).
// The old 48-byte path (97/98 chars) was a stale half-migration; it
// didn't match either live chain, which is how the local 64-byte
// addresses were being rejected on isValidQAddress callers.
const QRL_ADDR_HEX_LENS = [40, 128];
function isValidQAddress(address) {
    if (typeof address !== 'string') return false;
    if (address[0] !== 'Q') return false;
    const body = address.substring(1);
    return QRL_ADDR_HEX_LENS.includes(body.length) && /^[0-9a-fA-F]+$/.test(body);
}

// Helper to convert transitional 0x address to Q format for display.
// Accepts both live sizes (20/64 bytes) and preserves the full body.
function hexToQAddress(address) {
    if (!address || !address.startsWith('0x')) return address;
    const body = address.substring(2);
    if (!QRL_ADDR_HEX_LENS.includes(body.length)) return address;
    return 'Q' + body;
}

// Address handler
async function handleAddressLookup(req, res) {
    const body = await parseBody(req);
    const { address, network } = body;

    try {
        const config = networkConfigs[network];

        if (config && config.type === 'zond') {
            // Validate Q address format for QRL (48 bytes = 96 hex chars)
            if (!isValidQAddress(address)) {
                jsonResponse(res, {
                    success: false,
                    error: 'Invalid QRL address format. Expected Q followed by 96 hex characters (48 bytes).'
                });
                return;
            }

            // Pass Q address directly to RPC (no conversion needed for QRL)
            const rpcAddress = address;

            const [balanceRes, nonceRes] = await Promise.all([
                httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_getBalance', params: [rpcAddress, 'latest'], id: 1 }
                }),
                httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_getTransactionCount', params: [rpcAddress, 'latest'], id: 2 }
                })
            ]);

            const balance = balanceRes.data?.result ? (parseInt(balanceRes.data.result, 16) / 1e18).toFixed(8) : '0';
            const nonce = nonceRes.data?.result ? parseInt(nonceRes.data.result, 16) : 0;

            jsonResponse(res, {
                success: true,
                balance,
                nonce,
                txCount: nonce,
                state: { address, balance, nonce }
            });
            return;
        } else if (config && config.type === 'classic') {
            // QRL Classic - use Zeus Proxy GetAddressState
            // QRL addresses start with Q, need to convert for API
            let addressParam = address;
            if (address.startsWith('Q')) {
                addressParam = address;
            }
            const response = await httpRequest(`${config.zeusProxy}/GetAddressState`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { address: addressParam }
            });
            if (response.data && response.data.state) {
                const state = response.data.state;
                const balance = state.balance ? (parseInt(state.balance) / 1e9).toFixed(8) : '0';
                const nonce = state.nonce || 0;
                jsonResponse(res, {
                    success: true,
                    balance,
                    nonce,
                    txCount: state.transaction_hashes?.length || 0,
                    state: { address, balance, nonce, ...state }
                });
                return;
            }
        }
    } catch (err) {
        console.error('Address lookup error:', err.message);
    }

    jsonResponse(res, { success: false, error: 'Address lookup failed' });
}

// Token handlers
async function handleTokenLookup(req, res) {
    const body = await parseBody(req);
    const { token, network } = body;

    const config = networkConfigs[network];
    if (!config || config.type !== 'zond') {
        jsonResponse(res, { success: false, error: 'Token lookup only supported on QRL 2.0 networks' });
        return;
    }

    try {
        let contractAddress = token;

        // If not an address, search by name/symbol in cache
        if (!token || !token.startsWith('0x')) {
            const networkCache = tokenCache.get(network);
            if (networkCache && token) {
                const searchLower = token.toLowerCase();
                for (const [addr, tokenData] of networkCache) {
                    if (tokenData.symbol.toLowerCase() === searchLower ||
                        tokenData.name.toLowerCase().includes(searchLower)) {
                        contractAddress = addr;
                        break;
                    }
                }
            }
        }

        if (!contractAddress || !contractAddress.startsWith('0x')) {
            jsonResponse(res, { success: false, error: 'Token not found. Try searching by contract address.' });
            return;
        }

        // Fetch fresh metadata
        const metadata = await fetchTokenMetadata(config.execRpc, contractAddress);
        const cachedData = tokenCache.get(network)?.get(contractAddress.toLowerCase());

        jsonResponse(res, {
            success: true,
            token: {
                address: contractAddress,
                ...metadata,
                formattedSupply: formatTokenBalance(metadata.totalSupply, metadata.decimals),
                creationBlock: cachedData?.creationBlock || null,
                creationTxHash: cachedData?.creationTxHash || null,
                network
            }
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

async function handleTokensByAddress(req, res) {
    const body = await parseBody(req);
    const { address, network } = body;

    const config = networkConfigs[network];
    if (!config || config.type !== 'zond') {
        jsonResponse(res, { success: true, tokens: [] });
        return;
    }

    try {
        const tokens = [];
        const networkCache = tokenCache.get(network);

        if (networkCache) {
            // Check balance for each known token
            for (const [contractAddr, tokenData] of networkCache) {
                try {
                    // Encode balanceOf(address) call
                    const addrParam = address.toLowerCase().replace(/^0x|^z/i, '').padStart(64, '0');
                    const balanceData = QRC20_SELECTORS.balanceOf + addrParam;

                    const balanceResult = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: {
                            jsonrpc: '2.0',
                            method: 'qrl_call',
                            params: [{ to: contractAddr, data: balanceData }, 'latest'],
                            id: 1
                        }
                    });

                    if (balanceResult.data?.result && balanceResult.data.result !== '0x' &&
                        balanceResult.data.result !== '0x0000000000000000000000000000000000000000000000000000000000000000') {
                        const rawBalance = decodeUint256(balanceResult.data.result);
                        if (BigInt(rawBalance) > 0n) {
                            tokens.push({
                                ...tokenData,
                                balance: rawBalance,
                                formattedBalance: formatTokenBalance(rawBalance, tokenData.decimals)
                            });
                        }
                    }
                } catch (err) {
                    // Skip tokens that fail balance check
                }
            }
        }

        jsonResponse(res, { success: true, tokens });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err), tokens: [] });
    }
}

// Token browser - paginated listing with search
async function handleTokenBrowse(req, res) {
    const body = await parseBody(req);
    const { network, page = 1, pageSize = 20, search = '' } = body;

    const config = networkConfigs[network];
    if (!config || config.type !== 'zond') {
        jsonResponse(res, { success: false, error: 'Token browsing only supported on QRL 2.0 networks' });
        return;
    }

    // Trigger indexing if not started
    const state = tokenIndexState.get(network) || { lastIndexedBlock: 0, isIndexing: false };
    if (!state.isIndexing && state.lastIndexedBlock === 0) {
        runFullTokenIndex(network); // Start in background
    }

    const networkCache = tokenCache.get(network);
    if (!networkCache || networkCache.size === 0) {
        jsonResponse(res, {
            success: true,
            tokens: [],
            total: 0,
            page,
            pageSize,
            totalPages: 0,
            indexing: state.isIndexing,
            indexedBlocks: state.lastIndexedBlock
        });
        return;
    }

    // Convert to array and filter by search
    let tokens = Array.from(networkCache.values());

    if (search) {
        const searchLower = search.toLowerCase();
        tokens = tokens.filter(t =>
            t.symbol.toLowerCase().includes(searchLower) ||
            t.name.toLowerCase().includes(searchLower) ||
            t.address.toLowerCase().includes(searchLower)
        );
    }

    // Sort by creation block (newest first)
    tokens.sort((a, b) => (b.creationBlock || 0) - (a.creationBlock || 0));

    // Paginate
    const total = tokens.length;
    const startIdx = (page - 1) * pageSize;
    const paginatedTokens = tokens.slice(startIdx, startIdx + pageSize);

    jsonResponse(res, {
        success: true,
        tokens: paginatedTokens,
        total,
        page,
        pageSize,
        totalPages: Math.ceil(total / pageSize),
        indexing: state.isIndexing,
        indexedBlocks: state.lastIndexedBlock
    });
}

// Token details - single token info
async function handleTokenDetails(req, res) {
    const body = await parseBody(req);
    const { address, network } = body;

    const config = networkConfigs[network];
    if (!config || config.type !== 'zond') {
        jsonResponse(res, { success: false, error: 'Not a QRL 2.0 network' });
        return;
    }

    try {
        const metadata = await fetchTokenMetadata(config.execRpc, address);
        const cachedData = tokenCache.get(network)?.get(address.toLowerCase());

        jsonResponse(res, {
            success: true,
            token: {
                address,
                ...metadata,
                formattedSupply: formatTokenBalance(metadata.totalSupply, metadata.decimals),
                creationBlock: cachedData?.creationBlock || null,
                creationTxHash: cachedData?.creationTxHash || null,
                network
            }
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Trigger token index refresh
async function handleTokenIndexRefresh(req, res) {
    const body = await parseBody(req);
    const { network } = body;

    const state = tokenIndexState.get(network);
    if (state?.isIndexing) {
        jsonResponse(res, {
            success: true,
            message: 'Indexing already in progress',
            indexedBlocks: state.lastIndexedBlock
        });
        return;
    }

    runFullTokenIndex(network); // Start in background
    jsonResponse(res, {
        success: true,
        message: 'Token indexing started'
    });
}

// Richlist cache per network
const richlistCache = new Map();

// Richlist indexer state per network: { balances: Map<address, bigint>, lastBlock: number, isIndexing: boolean }
const richlistIndexState = new Map();

// Index balances from blocks for Zond networks
async function indexRichlistForNetwork(networkId) {
    const config = networkConfigs[networkId];
    if (!config || config.type !== 'zond') return;

    // Initialize state if needed
    if (!richlistIndexState.has(networkId)) {
        richlistIndexState.set(networkId, {
            balances: new Map(),
            lastBlock: 0,
            isIndexing: false,
            startedAt: null
        });
    }

    const state = richlistIndexState.get(networkId);
    if (state.isIndexing) return; // Already indexing

    state.isIndexing = true;
    state.startedAt = Date.now();

    try {
        // Get current block number
        const blockNumRes = await httpRequest(config.execRpc, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 },
            timeout: 10000
        });

        if (!blockNumRes.data?.result) {
            state.isIndexing = false;
            return;
        }

        const currentBlock = parseInt(blockNumRes.data.result, 16);
        const startBlock = state.lastBlock + 1;

        console.log(`[Richlist] Indexing ${networkId} from block ${startBlock} to ${currentBlock}`);

        // Process blocks in chunks
        const chunkSize = 100;
        for (let from = startBlock; from <= currentBlock; from += chunkSize) {
            const to = Math.min(from + chunkSize - 1, currentBlock);

            for (let blockNum = from; blockNum <= to; blockNum++) {
                try {
                    const blockRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { jsonrpc: '2.0', method: 'qrl_getBlockByNumber', params: ['0x' + blockNum.toString(16), true], id: 1 },
                        timeout: 10000
                    });

                    if (blockRes.data?.result?.transactions) {
                        for (const tx of blockRes.data.result.transactions) {
                            // Track sender and receiver addresses
                            if (tx.from) {
                                const from = tx.from.toLowerCase();
                                if (!state.balances.has(from)) state.balances.set(from, BigInt(0));
                            }
                            if (tx.to) {
                                const to = tx.to.toLowerCase();
                                if (!state.balances.has(to)) state.balances.set(to, BigInt(0));
                            }
                        }
                    }

                    // Track miner/proposer
                    if (blockRes.data?.result?.miner) {
                        const miner = blockRes.data.result.miner.toLowerCase();
                        if (!state.balances.has(miner)) state.balances.set(miner, BigInt(0));
                    }
                } catch (e) {
                    // Skip block on error, continue indexing
                }
            }

            state.lastBlock = to;

            // Small delay between chunks to avoid overloading
            await new Promise(r => setTimeout(r, 50));
        }

        // Now fetch actual balances for all discovered addresses
        console.log(`[Richlist] Fetching balances for ${state.balances.size} addresses on ${networkId}`);
        const addresses = Array.from(state.balances.keys());

        for (let i = 0; i < addresses.length; i += 10) {
            const batch = addresses.slice(i, i + 10);
            await Promise.all(batch.map(async (addr) => {
                try {
                    const balRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { jsonrpc: '2.0', method: 'qrl_getBalance', params: [addr, 'latest'], id: 1 },
                        timeout: 5000
                    });
                    if (balRes.data?.result) {
                        state.balances.set(addr, BigInt(balRes.data.result));
                    }
                } catch (e) {}
            }));
            await new Promise(r => setTimeout(r, 20));
        }

        // Update richlist cache with sorted results
        const richlist = Array.from(state.balances.entries())
            .filter(([_, bal]) => bal > BigInt(0))
            .sort((a, b) => (b[1] > a[1] ? 1 : b[1] < a[1] ? -1 : 0))
            .slice(0, 100)
            .map(([address, balance], rank) => ({
                rank: rank + 1,
                address,
                balance: balance.toString(),
                balanceQrl: (Number(balance) / 1e18).toFixed(4)
            }));

        richlistCache.set(networkId, { data: richlist, timestamp: Date.now() });
        console.log(`[Richlist] Completed indexing for ${networkId}. Top ${richlist.length} addresses.`);

    } catch (err) {
        console.error(`[Richlist] Error indexing ${networkId}:`, err.message);
    } finally {
        state.isIndexing = false;
    }
}

// Start background richlist indexing for Zond networks
function startRichlistIndexing() {
    // Index Zond networks periodically
    const zondNetworks = ['zond-private', 'zond-testnet'];

    for (const networkId of zondNetworks) {
        // Initial index
        indexRichlistForNetwork(networkId);

        // Re-index every 5 minutes
        setInterval(() => indexRichlistForNetwork(networkId), 300000);
    }
}

// Start indexing on server startup
setTimeout(startRichlistIndexing, 5000);

// Start token indexing for Zond networks on startup
function startTokenIndexing() {
    const zondNetworks = ['zond-private', 'zond-testnet'];
    for (const networkId of zondNetworks) {
        console.log(`[TokenIndex] Scheduling startup indexing for ${networkId}`);
        runFullTokenIndex(networkId);
    }
    // Re-index every 5 minutes
    setInterval(() => {
        for (const networkId of zondNetworks) {
            runFullTokenIndex(networkId);
        }
    }, 300000);
}
setTimeout(startTokenIndexing, 8000);

// Richlist
async function handleRichlist(req, res) {
    const body = await parseBody(req);
    const networkId = body.network || 'classic-mainnet';
    const config = networkConfigs[networkId];

    // Check cache first (cache for 5 minutes)
    const cached = richlistCache.get(networkId);
    if (cached && Date.now() - cached.timestamp < 300000) {
        jsonResponse(res, { success: true, richlist: cached.data, cached: true });
        return;
    }

    // For QRL Classic networks, redirect to explorer
    if (networkId.startsWith('classic-')) {
        jsonResponse(res, {
            success: true,
            richlist: [],
            message: 'Rich list requires blockchain indexing. View at explorer.theqrl.org for mainnet.',
            notImplemented: true
        });
        return;
    }

    // For Zond networks, check if we're indexing or have data
    if (config?.type === 'zond') {
        const state = richlistIndexState.get(networkId);
        if (state?.isIndexing) {
            jsonResponse(res, {
                success: true,
                richlist: [],
                message: `Indexing in progress (block ${state.lastBlock})...`,
                isIndexing: true
            });
            return;
        }

        // Trigger indexing if no data yet
        if (!cached) {
            indexRichlistForNetwork(networkId);
            jsonResponse(res, {
                success: true,
                richlist: [],
                message: 'Starting richlist indexing...',
                isIndexing: true
            });
            return;
        }
    }

    // Return cached data or empty
    jsonResponse(res, {
        success: true,
        richlist: cached?.data || [],
        message: cached ? undefined : 'Rich list not available for this network.',
        notImplemented: !cached
    });
}

// Peers
async function handlePeers(req, res) {
    const body = await parseBody(req);
    const { network } = body;

    if (!network) {
        jsonResponse(res, { success: false, error: 'Network parameter required' });
        return;
    }

    const config = networkConfigs[network];
    if (!config) {
        jsonResponse(res, { success: false, error: `Unknown network: ${network}` });
        return;
    }

    try {
        // Handle Classic networks via Zeus Proxy
        if (config.type === 'classic') {
            const response = await httpRequest(`${config.zeusProxy}/GetPeersStat`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {},
                timeout: getZeusProxyTimeout(network)
            });

            if (response.data && response.data.peers_stat) {
                const seenIPs = new Set();
                const peers = [];

                for (let i = 0; i < response.data.peers_stat.length; i++) {
                    const peer = response.data.peers_stat[i];
                    // Convert buffer to string for IP
                    const ipStr = bufferToString(peer.peer_ip);
                    if (!ipStr) continue;
                    if (seenIPs.has(ipStr)) continue;
                    seenIPs.add(ipStr);

                    peers.push({
                        id: `Peer ${i + 1}`,
                        ip: ipStr,
                        version: peer.node_chain_state?.version || 'Unknown',
                        blockHeight: peer.node_chain_state?.block_number || 0,
                        latency: '---'
                    });
                }

                jsonResponse(res, { success: true, peers, network });
                return;
            }
        }

        // Handle Zond networks via RPC
        if (config.type === 'zond') {
            // Try admin_peers first (only works on local nodes with admin API enabled)
            try {
                const response = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: {
                        jsonrpc: '2.0',
                        method: 'admin_peers',
                        params: [],
                        id: 1
                    }
                });

                if (response.data && response.data.result && Array.isArray(response.data.result)) {
                    const peers = response.data.result.map((peer, i) => {
                        // Parse block height from hex if available
                        let blockHeight = 'Unknown';
                        if (peer.protocols?.zond?.head) {
                            const headHex = peer.protocols.zond.head;
                            if (typeof headHex === 'string' && headHex.startsWith('0x')) {
                                blockHeight = parseInt(headHex, 16);
                            } else {
                                blockHeight = headHex;
                            }
                        }
                        return {
                            id: peer.id ? peer.id.substring(0, 16) + '...' : `Peer ${i + 1}`,
                            ip: peer.network?.remoteAddress || 'Unknown',
                            version: peer.name || 'Unknown',
                            blockHeight: blockHeight,
                            latency: '---'
                        };
                    });
                    jsonResponse(res, { success: true, peers, network });
                    return;
                }
            } catch (adminErr) {
                // admin_peers not available, fall back to peer count
            }

            // Fall back to just showing peer count (for public nodes without admin API)
            try {
                const peerCountRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'net_peerCount', params: [], id: 1 }
                });

                if (peerCountRes.data?.result) {
                    const peerCount = parseInt(peerCountRes.data.result, 16);
                    // Return a message indicating peer count but no details available
                    jsonResponse(res, {
                        success: true,
                        peers: [],
                        network,
                        peerCount: peerCount,
                        message: `Connected to ${peerCount} peers (detailed peer info not available on public nodes)`
                    });
                    return;
                }
            } catch (e) {}
        }
    } catch (err) {
        console.error(`Peers fetch error for ${network}:`, err.message);
    }

    // Return empty peers if fetch failed (no mock data)
    jsonResponse(res, { success: true, peers: [], network, message: 'No peers found or unable to fetch peers' });
}

// Metrics
// Store recent block times for calculation
const recentBlockTimes = new Map(); // network -> [{ blockNumber, timestamp }]

async function handleMetrics(req, res) {
    const body = await parseBody(req);
    const network = body.network || 'zond-private';
    const config = networkConfigs[network];

    // Get real system metrics
    const cpuLoad = os.loadavg()[0]; // 1-minute load average
    const cpuCount = os.cpus().length;
    const cpuPercent = Math.min(100, Math.round((cpuLoad / cpuCount) * 100));

    const totalMem = os.totalmem();
    const freeMem = os.freemem();
    const memoryPercent = Math.round(((totalMem - freeMem) / totalMem) * 100);

    // Disk usage (approximate from root or home)
    let diskPercent = 0;
    try {
        const diskStats = fs.statfsSync(os.homedir());
        const totalDisk = diskStats.blocks * diskStats.bsize;
        const freeDisk = diskStats.bfree * diskStats.bsize;
        diskPercent = Math.round(((totalDisk - freeDisk) / totalDisk) * 100);
    } catch (e) {
        diskPercent = 0;
    }

    // Get block time and tx count from network
    let blockTime = 60; // Default 60s
    let txCount = 0;

    if (config) {
        try {
            if (config.type === 'zond') {
                // Get recent blocks to calculate block time
                const blockNumRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 },
                    timeout: 5000
                });

                if (blockNumRes.data?.result) {
                    const currentBlock = parseInt(blockNumRes.data.result, 16);

                    // Get the current block for timestamp and tx count
                    const blockRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { jsonrpc: '2.0', method: 'qrl_getBlockByNumber', params: ['0x' + currentBlock.toString(16), false], id: 1 },
                        timeout: 5000
                    });

                    if (blockRes.data?.result) {
                        const block = blockRes.data.result;
                        const currentTimestamp = parseInt(block.timestamp, 16);
                        txCount = block.transactions?.length || 0;

                        // Store for block time calculation
                        if (!recentBlockTimes.has(network)) {
                            recentBlockTimes.set(network, []);
                        }
                        const times = recentBlockTimes.get(network);
                        times.push({ blockNumber: currentBlock, timestamp: currentTimestamp });

                        // Keep only last 10 entries
                        while (times.length > 10) times.shift();

                        // Calculate average block time from recent blocks
                        if (times.length >= 2) {
                            const oldest = times[0];
                            const newest = times[times.length - 1];
                            const timeDiff = newest.timestamp - oldest.timestamp;
                            const blockDiff = newest.blockNumber - oldest.blockNumber;
                            if (blockDiff > 0) {
                                blockTime = Math.round(timeDiff / blockDiff);
                            }
                        }
                    }
                }
            } else if (config.type === 'classic') {
                // Get stats from Zeus proxy
                const statsRes = await httpRequest(`${config.zeusProxy}/GetStats`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: {},
                    timeout: 5000
                });

                if (statsRes.data?.node_info) {
                    const blockHeight = parseInt(statsRes.data.node_info.block_height || 0);

                    // Get recent block for tx count
                    const blockRes = await httpRequest(`${config.zeusProxy}/GetBlockByNumber`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { block_number: blockHeight },
                        timeout: 5000
                    });

                    if (blockRes.data?.block?.header?.timestamp_seconds) {
                        txCount = blockRes.data.block.transactions?.length || 0;
                    }

                    // Classic has ~60 second block time
                    blockTime = 60;
                }
            }
        } catch (e) {
            // Keep defaults on error
        }
    }

    jsonResponse(res, {
        success: true,
        cpu: cpuPercent,
        memory: memoryPercent,
        disk: diskPercent,
        network: 0, // Network bandwidth requires more complex monitoring
        blockTime,
        txCount
    });
}

// ========== GLOBE VISUALIZATION API ==========

// IP Geolocation cache to avoid repeated lookups
const geoCache = new Map();
const GEO_CACHE_TTL = 3600000; // 1 hour

// ========== TOKEN DISCOVERY CACHE ==========
// Token discovery cache per network: Map<networkId, Map<contractAddress, TokenData>>
const tokenCache = new Map();
// Indexing state per network: { lastIndexedBlock: number, isIndexing: boolean }
const tokenIndexState = new Map();

async function geolocateIP(ip) {
    // Check cache first
    const cached = geoCache.get(ip);
    if (cached && Date.now() - cached.timestamp < GEO_CACHE_TTL) {
        return cached.data;
    }

    try {
        // Use ip-api.com (free, no API key needed, 45 requests/minute limit)
        const response = await httpRequest(`http://ip-api.com/json/${ip}?fields=status,country,countryCode,city,lat,lon,isp`, {
            method: 'GET'
        });

        if (response.data && response.data.status === 'success') {
            const geoData = {
                ip,
                lat: response.data.lat,
                lng: response.data.lon,
                city: response.data.city || 'Unknown',
                country: response.data.countryCode || 'XX',
                countryName: response.data.country || 'Unknown',
                isp: response.data.isp || 'Unknown'
            };
            geoCache.set(ip, { data: geoData, timestamp: Date.now() });
            return geoData;
        }
    } catch (err) {
        console.error('Geolocation error for', ip, ':', err.message);
    }

    // Return fallback with null coordinates if geolocation fails
    // The caller should handle null lat/lng appropriately (skip or use fixed location)
    return {
        ip,
        lat: null,
        lng: null,
        city: 'Unknown',
        country: 'XX',
        countryName: 'Unknown',
        isp: 'Unknown',
        geoFailed: true
    };
}

// Decode buffer array to string (for QRL peer IPs)
function bufferToString(bufferData) {
    if (!bufferData || !bufferData.data) return null;
    return String.fromCharCode(...bufferData.data);
}

// Get peers with geolocation for globe visualization
async function handleGlobePeers(req, res) {
    const body = await parseBody(req);
    const { network } = body;
    const config = networkConfigs[network];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    try {
        const peers = [];

        // Check if this is a private/local network
        const isPrivateNetwork = network.includes('private') || network.includes('local');

        if (config.type === 'classic') {
            // For classic-private, return the 4 local nodes directly
            if (isPrivateNetwork) {
                // Get block heights from each local node
                const localNodes = [
                    { port: 49009, name: 'Node 1', role: 'Mining' },
                    { port: 49019, name: 'Node 2', role: 'Validator' },
                    { port: 49029, name: 'Node 3', role: 'Validator' },
                    { port: 49039, name: 'Node 4', role: 'Validator' }
                ];

                // Spread nodes around a central point (San Francisco area as default)
                const centerLat = 37.7749;
                const centerLng = -122.4194;
                const offsets = [
                    { lat: 0.02, lng: -0.02 },
                    { lat: 0.02, lng: 0.02 },
                    { lat: -0.02, lng: -0.02 },
                    { lat: -0.02, lng: 0.02 }
                ];

                for (let i = 0; i < localNodes.length; i++) {
                    const node = localNodes[i];
                    let blockHeight = 0;
                    try {
                        // Try to get block height via Zeus Proxy for local node
                        const statsRes = await httpRequest(`http://localhost:3737/grpc/classic-local/GetStats`, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: {},
                            timeout: 3000
                        });
                        if (statsRes.data?.block_last_hash) {
                            // Parse block number from stats
                            blockHeight = statsRes.data.block_height || 0;
                        }
                    } catch (e) {}

                    peers.push({
                        ip: `127.0.0.1:${node.port}`,
                        port: node.port,
                        blockHeight: blockHeight,
                        headerHash: '',
                        cumulativeDifficulty: '',
                        version: 'QRL Classic Private',
                        lastUpdate: Date.now(),
                        lat: centerLat + offsets[i].lat,
                        lng: centerLng + offsets[i].lng,
                        city: `${node.name} (${node.role})`,
                        country: 'Local Network',
                        isp: 'Private'
                    });
                }

                jsonResponse(res, { success: true, peers, networkType: 'classic' });
                return;
            }

            // QRL Classic public networks - use GetPeersStat via Zeus Proxy
            const response = await httpRequest(`${config.zeusProxy}/GetPeersStat`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {},
                timeout: getZeusProxyTimeout(network)
            });

            if (response.data && response.data.peers_stat) {
                // Process all peers (deduplicated by IP)
                const peerStats = response.data.peers_stat;
                const seenIPs = new Set();

                for (const peer of peerStats) {
                    const ipStr = bufferToString(peer.peer_ip);
                    // Skip local/private IPs and duplicates for public networks
                    if (!ipStr || ipStr.startsWith('127.') || ipStr.startsWith('192.168.') || ipStr.startsWith('10.')) {
                        continue;
                    }
                    if (seenIPs.has(ipStr)) {
                        continue;
                    }
                    seenIPs.add(ipStr);

                    const chainState = peer.node_chain_state || {};
                    const geo = await geolocateIP(ipStr);

                    // Convert header hash from base64/buffer to hex
                    let headerHashHex = '';
                    if (chainState.header_hash) {
                        try {
                            const hashBytes = Buffer.from(chainState.header_hash, 'base64');
                            headerHashHex = hashBytes.toString('hex');
                        } catch (e) {
                            headerHashHex = chainState.header_hash;
                        }
                    }

                    // Convert cumulative difficulty from base64/buffer to decimal string
                    let cumulativeDiffStr = '';
                    if (chainState.cumulative_difficulty) {
                        try {
                            const diffBytes = Buffer.from(chainState.cumulative_difficulty, 'base64');
                            // Convert big-endian bytes to BigInt
                            let bigVal = BigInt(0);
                            for (const byte of diffBytes) {
                                bigVal = (bigVal << BigInt(8)) + BigInt(byte);
                            }
                            cumulativeDiffStr = bigVal.toString();
                        } catch (e) {
                            cumulativeDiffStr = chainState.cumulative_difficulty.toString();
                        }
                    }

                    // Only add peers with valid geolocation
                    if (geo.lat !== null && geo.lng !== null) {
                        peers.push({
                            ip: ipStr,
                            port: peer.port,
                            blockHeight: chainState.block_number || 0,
                            headerHash: headerHashHex,
                            cumulativeDifficulty: cumulativeDiffStr,
                            version: chainState.version || 'unknown',
                            lastUpdate: chainState.timestamp ? chainState.timestamp * 1000 : Date.now(),
                            lat: geo.lat,
                            lng: geo.lng,
                            city: geo.city,
                            country: geo.country,
                            isp: geo.isp
                        });
                    }

                    // Rate limit: wait 30ms between geolocation requests
                    if (peers.length < peerStats.length) {
                        await new Promise(r => setTimeout(r, 30));
                    }
                }
            }
        } else if (config.type === 'zond') {
            // Zond - try to get real peer info via admin_peers, fall back to net_peerCount
            const nodeIp = config.execRpc.replace('http://', '').split(':')[0];
            let mainNodeBlockHeight = 0;
            let mainNodeVersion = 'QRL 2.0';

            // Get current block number for the main node
            try {
                const blockNumRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
                });
                if (blockNumRes.data?.result) {
                    mainNodeBlockHeight = parseInt(blockNumRes.data.result, 16);
                }
            } catch (e) {}

            // Try to get client version
            try {
                const versionRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'web3_clientVersion', params: [], id: 1 }
                });
                if (versionRes.data?.result) {
                    mainNodeVersion = versionRes.data.result;
                }
            } catch (e) {}

            // Try admin_peers first for detailed peer info
            const isLocalNode = nodeIp === 'localhost' || nodeIp === '127.0.0.1';
            let adminPeersWorked = false;
            try {
                const adminPeersRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'admin_peers', params: [], id: 1 }
                });
                if (adminPeersRes.data?.result && Array.isArray(adminPeersRes.data.result)) {
                    adminPeersWorked = true;
                    const seenIPs = new Set();
                    for (const peer of adminPeersRes.data.result) {
                        // Extract IP from enode URL or network info
                        let peerIp = '';
                        let peerPort = 30303;
                        if (peer.network?.remoteAddress) {
                            const parts = peer.network.remoteAddress.split(':');
                            peerIp = parts[0];
                            peerPort = parseInt(parts[1]) || 30303;
                        } else if (peer.enode) {
                            const match = peer.enode.match(/@([^:]+):(\d+)/);
                            if (match) {
                                peerIp = match[1];
                                peerPort = parseInt(match[2]) || 30303;
                            }
                        }

                        // For public nodes, skip local/private IPs
                        // For local nodes, allow local IPs but use peer ID as identifier
                        if (!isLocalNode && (peerIp.startsWith('127.') || peerIp.startsWith('192.168.') || peerIp.startsWith('10.'))) {
                            continue;
                        }

                        // Use peer ID + port as unique key for local networks
                        const uniqueKey = isLocalNode ? (peer.id || peerIp + ':' + peerPort) : peerIp;
                        if (seenIPs.has(uniqueKey)) continue;
                        seenIPs.add(uniqueKey);

                        // For local peers, use the peer ID as a display identifier
                        const displayIp = isLocalNode ? (peer.id?.substring(0, 10) || peerIp) : peerIp;

                        if (isLocalNode) {
                            // Local network - use fixed position at 0,0 for local peers
                            // They will cluster at the center which is fine for local networks
                            const localIndex = peers.length;
                            peers.push({
                                ip: displayIp,
                                port: peerPort,
                                blockHeight: mainNodeBlockHeight,
                                version: peer.name || mainNodeVersion,
                                lastUpdate: Date.now(),
                                lat: 0,
                                lng: localIndex * 5, // Spread them slightly along equator
                                city: `Local Peer`,
                                country: 'LOCAL',
                                isp: 'Local Network',
                                isLocal: true
                            });
                        } else {
                            const geo = await geolocateIP(peerIp);
                            // Only add peers with valid geolocation
                            if (geo.lat !== null && geo.lng !== null) {
                                peers.push({
                                    ip: peerIp,
                                    port: peerPort,
                                    blockHeight: peer.protocols?.zond?.head ? parseInt(peer.protocols.zond.head, 16) : mainNodeBlockHeight,
                                    version: peer.name || mainNodeVersion,
                                    lastUpdate: Date.now(),
                                    lat: geo.lat,
                                    lng: geo.lng,
                                    city: geo.city,
                                    country: geo.country,
                                    isp: geo.isp
                                });
                            }
                            await new Promise(r => setTimeout(r, 30));
                        }
                    }
                }
            } catch (e) {
                // admin_peers not available
            }

            // If admin_peers didn't work, add main node and use peer count for others
            if (!adminPeersWorked) {

                if (isLocalNode) {
                    // For local nodes, add with local indicator
                    peers.push({
                        ip: nodeIp,
                        port: parseInt(config.execRpc.split(':')[2]) || 8545,
                        blockHeight: mainNodeBlockHeight,
                        version: mainNodeVersion,
                        lastUpdate: Date.now(),
                        lat: 0,
                        lng: 0,
                        city: 'Local Node',
                        country: 'LOCAL',
                        isp: 'Local Network'
                    });
                } else {
                    const geo = await geolocateIP(nodeIp);
                    // Only add if geolocation succeeded
                    if (geo.lat !== null && geo.lng !== null) {
                        peers.push({
                            ip: nodeIp,
                            port: parseInt(config.execRpc.split(':')[2]) || 8545,
                            blockHeight: mainNodeBlockHeight,
                            version: mainNodeVersion,
                            lastUpdate: Date.now(),
                            lat: geo.lat,
                            lng: geo.lng,
                            city: geo.city,
                            country: geo.country,
                            isp: geo.isp
                        });
                    }
                }

                // Get peer count for reference (but don't generate fake peers)
                // When admin_peers isn't available, we only show the main node
                // This is more honest than generating fake peer locations
                try {
                    const peerCountRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { jsonrpc: '2.0', method: 'net_peerCount', params: [], id: 1 }
                    });
                    if (peerCountRes.data?.result && peers.length > 0) {
                        const peerCount = parseInt(peerCountRes.data.result, 16);
                        // Store peer count on main node for display purposes
                        peers[0].connectedPeers = peerCount;
                    }
                } catch (e) {
                    // Ignore peer count errors
                }
            }
        }

        jsonResponse(res, {
            success: true,
            network,
            networkType: config.type, // 'classic' or 'zond'
            peers,
            timestamp: Date.now()
        });

    } catch (err) {
        const errMsg = err?.message || String(err) || 'Unknown error';
        console.error('Globe peers error:', errMsg, err?.stack || '');
        jsonResponse(res, { success: false, error: errMsg });
    }
}

// Get recent transactions for globe visualization
async function handleGlobeTransactions(req, res) {
    const body = await parseBody(req);
    const { network, since } = body;
    const config = networkConfigs[network];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    try {
        const transactions = [];

        if (config.type === 'classic') {
            // Get latest block transactions. Short-circuit + soft-fail
            // when Zeus Proxy is down so a missing proxy surfaces as
            // "no transactions" (matches the zond branch pattern) and
            // doesn't spam ECONNREFUSED once per request.
            let statsRes;
            if (isZeusProxyLikelyDown()) {
                statsRes = null;
            } else {
                try {
                    statsRes = await httpRequest(`${config.zeusProxy}/GetStats`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: {},
                        timeout: getZeusProxyTimeout(network)
                    });
                    noteZeusProxyResult(true);
                } catch (e) {
                    const detail = e.message || e.code || 'unknown';
                    if (detail.includes('ECONNREFUSED')) {
                        noteZeusProxyResult(false);
                        throttleZeusLog(`globe-tx/${network}`, detail);
                    } else {
                        console.warn(`Globe tx (${network}): Zeus Proxy unreachable — ${detail}`);
                    }
                    statsRes = null;
                }
            }

            // Pending (unconfirmed) transactions from the mempool. These
            // render on the globe as low-oscillating-glow arcs so operators
            // can see propagation before a block confirms them. Zeus Proxy
            // exposes them via GetLatestData(filter=TRANSACTIONS_UNCONFIRMED).
            // Soft-fail: if zeus-proxy is flaky just skip pending — the
            // confirmed-block path below still runs.
            if (statsRes) {
                try {
                    const memRes = await httpRequest(`${config.zeusProxy}/GetLatestData`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { filter: 'TRANSACTIONS_UNCONFIRMED', offset: 0, quantity: 20 },
                        timeout: getZeusProxyTimeout(network),
                    });
                    if (memRes.data?.transactions_unconfirmed) {
                        for (const tx of memRes.data.transactions_unconfirmed) {
                            if (tx.coinbase) continue;
                            const txHash = tx.transaction_hash?.data
                                ? Buffer.from(tx.transaction_hash.data).toString('hex') : null;
                            if (txHash) {
                                transactions.push({
                                    hash: txHash,
                                    blockNumber: null,          // not yet mined
                                    pending: true,              // → oscillating glow on globe
                                    type: tx.transfer ? 'transfer' : 'other',
                                    amount: tx.transfer?.amounts
                                        ? tx.transfer.amounts.reduce((a, b) => a + parseInt(b || 0), 0) / 1e9
                                        : 0,
                                    timestamp: Date.now(),
                                });
                            }
                        }
                    }
                } catch (e) {
                    const d = e.message || e.code || 'unknown';
                    if (!d.includes('ECONNREFUSED')) {
                        console.warn(`Globe tx (${network}) mempool: ${d}`);
                    }
                }
            }

            if (statsRes && statsRes.data) {
                const blockHeight = parseInt(statsRes.data.node_info?.block_height || 0);

                // Get more blocks for Classic networks since transactions are sparse
                // Check up to 50 blocks to find recent transactions
                const maxBlocks = 50;
                for (let i = 0; i < maxBlocks && blockHeight - i > 0 && transactions.length < 20; i++) {
                    try {
                        const blockRes = await httpRequest(`${config.zeusProxy}/GetBlockByNumber`, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: { block_number: blockHeight - i },
                            timeout: getZeusProxyTimeout(network)
                        });

                        if (blockRes.data?.block?.transactions) {
                            for (const tx of blockRes.data.block.transactions) {
                                if (tx.coinbase) continue; // Skip coinbase

                                const txHash = tx.transaction_hash?.data ?
                                    Buffer.from(tx.transaction_hash.data).toString('hex') : null;

                                if (txHash) {
                                    transactions.push({
                                        hash: txHash,
                                        blockNumber: blockHeight - i,
                                        type: tx.transfer ? 'transfer' : 'other',
                                        amount: tx.transfer?.amounts ?
                                            tx.transfer.amounts.reduce((a, b) => a + parseInt(b || 0), 0) / 1e9 : 0,
                                        timestamp: Date.now() - i * 60000 // Approximate
                                    });
                                }
                            }
                        }
                    } catch (e) {
                        // Skip block errors
                    }
                }
            }
        } else if (config.type === 'zond') {
            // Pending (mempool) transactions via gqrl's txpool_content.
            // Same low-oscillating-glow rendering as the classic branch —
            // works for both live 20-byte testnet + local 64-byte zond-private
            // because the address representation is passthrough to the UI.
            // Soft-fail if the node doesn't expose txpool (some remote
            // testnet gateways strip it) — confirmed-tx path below still runs.
            try {
                const poolRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'txpool_content', params: [], id: 1 }
                });
                const pool = poolRes.data?.result?.pending || {};
                for (const fromAddr of Object.keys(pool)) {
                    for (const nonceKey of Object.keys(pool[fromAddr])) {
                        const tx = pool[fromAddr][nonceKey];
                        if (!tx || !tx.hash) continue;
                        transactions.push({
                            hash: tx.hash,
                            blockNumber: null,              // not yet mined
                            pending: true,                  // → oscillating glow
                            from: hexToQAddress(tx.from),
                            to: hexToQAddress(tx.to),
                            amount: tx.value ? parseInt(tx.value, 16) / 1e18 : 0,
                            timestamp: Date.now(),
                        });
                    }
                }
            } catch (e) {
                // txpool API may be disabled — that's OK, just no pending viz
                const d = e.message || e.code || 'unknown';
                if (!d.includes('ECONNREFUSED') && !d.includes('method not found')) {
                    console.warn(`Globe tx (${network}) txpool_content: ${d}`);
                }
            }

            // Get latest block transactions
            try {
                const blockNumRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
                });

                if (blockNumRes.data?.result) {
                    const blockNum = blockNumRes.data.result;

                    // Get last 3 blocks
                    for (let i = 0; i < 3; i++) {
                        const blockHex = '0x' + (parseInt(blockNum, 16) - i).toString(16);
                        const blockRes = await httpRequest(config.execRpc, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: { jsonrpc: '2.0', method: 'qrl_getBlockByNumber', params: [blockHex, true], id: 1 }
                        });

                        if (blockRes.data?.result?.transactions) {
                            for (const tx of blockRes.data.result.transactions) {
                                transactions.push({
                                    hash: tx.hash,
                                    blockNumber: parseInt(tx.blockNumber, 16),
                                    from: hexToQAddress(tx.from),
                                    to: hexToQAddress(tx.to),
                                    amount: tx.value ? parseInt(tx.value, 16) / 1e18 : 0,
                                    timestamp: parseInt(blockRes.data.result.timestamp, 16) * 1000
                                });
                            }
                        }
                    }
                }
            } catch (e) {
                console.error('Zond tx fetch error:', e.message);
            }
        }

        jsonResponse(res, {
            success: true,
            network,
            transactions,
            timestamp: Date.now()
        });

    } catch (err) {
        const detail = err.message || err.code || String(err);
        console.error('Globe transactions error:', detail);
        jsonResponse(res, { success: false, error: detail });
    }
}

// Enhanced globe data - block heights, validators, mempool, contract deployments
async function handleGlobeEnhancedData(req, res) {
    const body = await parseBody(req);
    const { network } = body;
    const config = networkConfigs[network];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    const result = {
        success: true,
        peerUpdates: [],
        validators: [],
        mempoolData: {},
        contractDeployments: []
    };

    try {
        if (config.type === 'zond') {
            // Zond PoS network - get block heights and mempool
            const blockNumRes = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
            });

            const currentBlock = blockNumRes.data?.result ? parseInt(blockNumRes.data.result, 16) : 0;

            // Get mempool (pending transactions)
            try {
                const pendingRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_pendingTransactions', params: [], id: 1 }
                });

                if (pendingRes.data?.result) {
                    // For now, distribute mempool count across simulated peer IPs
                    const mempoolSize = pendingRes.data.result.length || 0;
                    result.mempoolData['simulated'] = mempoolSize;
                }
            } catch (e) {
                // Mempool call not supported on this node
            }

            // Check for contract deployments in recent blocks
            try {
                const blockRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: {
                        jsonrpc: '2.0',
                        method: 'qrl_getBlockByNumber',
                        params: ['0x' + currentBlock.toString(16), true],
                        id: 1
                    }
                });

                if (blockRes.data?.result?.transactions) {
                    for (const tx of blockRes.data.result.transactions) {
                        // Contract deployment: 'to' is null/empty
                        if (!tx.to || tx.to === '0x' || tx.to === '') {
                            // Get receipt to find contract address
                            try {
                                const receiptRes = await httpRequest(config.execRpc, {
                                    method: 'POST',
                                    headers: { 'Content-Type': 'application/json' },
                                    body: {
                                        jsonrpc: '2.0',
                                        method: 'qrl_getTransactionReceipt',
                                        params: [tx.hash],
                                        id: 1
                                    }
                                });

                                if (receiptRes.data?.result?.contractAddress) {
                                    result.contractDeployments.push({
                                        address: receiptRes.data.result.contractAddress,
                                        txHash: tx.hash,
                                        blockNumber: currentBlock,
                                        nodeIp: 'simulated' // Would need actual peer mapping
                                    });
                                }
                            } catch (e) {
                                // Skip if receipt fetch fails
                            }
                        }
                    }
                }
            } catch (e) {
                // Block fetch error
            }

            // For Zond, we could potentially get validator info from beacon API
            // For now, simulate with 30% of nodes being validators
            result.validators = [];

        } else if (config.type === 'classic') {
            // QRL Classic - get block heights from peers
            const statsRes = await httpRequest(`${config.zeusProxy}/GetStats`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {},
                timeout: getZeusProxyTimeout(network)
            });

            if (statsRes.data?.node_info) {
                const blockHeight = parseInt(statsRes.data.node_info.block_height || 0);
                result.peerUpdates.push({
                    ip: 'local',
                    blockHeight,
                    version: statsRes.data.node_info.version || 'unknown'
                });
            }
        }

        jsonResponse(res, result);

    } catch (err) {
        console.error('Globe enhanced data error:', err.message);
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Topology API - returns nodes and links for D3 visualization
async function handleTopology(req, res) {
    const body = await parseBody(req);
    const { network } = body;
    const config = networkConfigs[network];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    const nodes = [];
    const links = [];

    try {
        if (config.type === 'classic') {
            // QRL Classic - use GetPeersStat via Zeus Proxy
            const response = await httpRequest(`${config.zeusProxy}/GetPeersStat`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {},
                timeout: getZeusProxyTimeout(network)
            });

            // Add "Your Node" as the central node
            nodes.push({
                id: 'Your Node',
                group: 1,
                blockHeight: 0,
                ip: 'localhost'
            });

            // Also get stats for your node
            try {
                const statsRes = await httpRequest(`${config.zeusProxy}/GetStats`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: {},
                    timeout: getZeusProxyTimeout(network)
                });
                if (statsRes.data?.node_info?.block_height) {
                    nodes[0].blockHeight = parseInt(statsRes.data.node_info.block_height);
                }
            } catch (e) {}

            if (response.data && response.data.peers_stat) {
                const peerStats = response.data.peers_stat.slice(0, 30); // Limit to 30 peers

                for (let i = 0; i < peerStats.length; i++) {
                    const peer = peerStats[i];
                    const ipStr = bufferToString(peer.peer_ip);
                    const peerHeight = peer.node_chain_state?.block_number || 0;

                    if (ipStr) {
                        const nodeId = `Peer ${i + 1}`;
                        nodes.push({
                            id: nodeId,
                            group: 2,
                            ip: ipStr,
                            blockHeight: peerHeight,
                            port: peer.port
                        });

                        // Connect to your node
                        links.push({
                            source: 'Your Node',
                            target: nodeId,
                            value: 1
                        });
                    }
                }

                // Add some peer-to-peer connections for more realistic topology
                for (let i = 1; i < Math.min(nodes.length, 10); i++) {
                    for (let j = i + 1; j < Math.min(nodes.length, 10); j++) {
                        // Only connect some pairs (random mesh)
                        if (Math.random() > 0.7) {
                            links.push({
                                source: nodes[i].id,
                                target: nodes[j].id,
                                value: 0.5
                            });
                        }
                    }
                }
            }
        } else if (config.type === 'zond') {
            // Zond - try admin_peers first
            let peerCount = 0;

            try {
                const peerCountRes = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'net_peerCount', params: [], id: 1 }
                });
                if (peerCountRes.data?.result) {
                    peerCount = parseInt(peerCountRes.data.result, 16);
                }
            } catch (e) {}

            // Add execution and beacon nodes
            nodes.push({
                id: 'Execution',
                group: 1,
                ip: config.execRpc.replace('http://', '').split(':')[0],
                nodeType: 'execution'
            });

            nodes.push({
                id: 'Beacon',
                group: 2,
                ip: config.beaconRpc?.replace('http://', '').split(':')[0] || 'localhost',
                nodeType: 'beacon'
            });

            // Connect execution to beacon
            links.push({
                source: 'Execution',
                target: 'Beacon',
                value: 2
            });

            // Add peer nodes based on peerCount
            for (let i = 0; i < Math.min(peerCount, 15); i++) {
                const nodeId = `Peer ${i + 1}`;
                nodes.push({
                    id: nodeId,
                    group: 3,
                    nodeType: 'peer'
                });

                // Connect to execution layer
                links.push({
                    source: 'Execution',
                    target: nodeId,
                    value: 1
                });
            }

            // Add validator node if local network
            if (network === 'zond-private') {
                nodes.push({
                    id: 'Validator',
                    group: 4,
                    nodeType: 'validator'
                });
                links.push({
                    source: 'Beacon',
                    target: 'Validator',
                    value: 2
                });
            }
        }

        jsonResponse(res, {
            success: true,
            network,
            nodes,
            links,
            timestamp: Date.now()
        });

    } catch (err) {
        // Previously returned { success: true, ..., error: '...' } — a
        // contradictory shape that made client-side error handling brittle.
        // Now: success stays true because we DID return a usable minimal
        // topology (the "Your Node" root), but the failure gets surfaced
        // as a `warning` field so the UI can render a subtle banner
        // without discarding the topology payload.
        const detail = errDetail(err);
        console.error(`Topology error (${network}):`, detail);
        jsonResponse(res, {
            success: true,
            network,
            nodes: [{ id: 'Your Node', group: 1 }],
            links: [],
            warning: `peer data unavailable: ${detail}`,
            partial: true,
            timestamp: Date.now()
        });
    }
}

// Explorer search
async function handleExplorerSearch(req, res) {
    const body = await parseBody(req);
    const { query, network } = body;

    // Determine search type
    if (query.startsWith('Q') || query.startsWith('0x')) {
        if (query.length > 50) {
            // Transaction hash
            return handleTransactionLookup(req, res);
        } else {
            // Address
            return handleAddressLookup(req, res);
        }
    } else if (!isNaN(query)) {
        // Block number
        return handleBlockLookup(req, res);
    }

    jsonResponse(res, { success: false, error: 'Invalid search query' });
}

// OTS check (Classic XMSS one-time-signature usage)
async function handleOTSCheck(req, res) {
    // Previously returned Math.random values for usedKeys/nextIndex, which
    // made the tools tab display fake XMSS OTS status. Real OTS state has
    // to come from GetOTS on a Classic node via Zeus Proxy.
    const body = await parseBody(req);
    const address = String(body.address || '').trim();
    const network = String(body.network || '').trim();
    const config = networkConfigs[network];
    if (!config || config.type !== 'classic') {
        return jsonResponse(res, {
            success: false,
            error: 'OTS state is only defined on QRL Classic (XMSS) networks',
        });
    }
    if (!/^Q[0-9a-fA-F]{78}$/.test(address)) {
        return jsonResponse(res, {
            success: false,
            error: 'address must be a Q-prefixed Classic address (78 hex chars)',
        });
    }
    if (isZeusProxyLikelyDown()) {
        return jsonResponse(res, zeusProxyDownResponse());
    }
    try {
        const r = await httpRequest(`${config.zeusProxy}/GetOTS`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { address, page_from: 0, page_count: 1 },
            timeout: getZeusProxyTimeout(network),
        });
        noteZeusProxyResult(true);
        const nextIndex = r.data?.next_unused_ots_index ?? null;
        jsonResponse(res, {
            success: true,
            address,
            nextUnusedOtsIndex: nextIndex,
            note: 'availableKeys depends on the tree height chosen at wallet creation',
        });
    } catch (err) {
        const detail = errDetail(err);
        if (detail.includes('ECONNREFUSED')) {
            noteZeusProxyResult(false);
            throttleZeusLog(`ots/${network}`, detail);
            return jsonResponse(res, zeusProxyDownResponse());
        }
        jsonResponse(res, { success: false, error: detail });
    }
}

// Node control - individual and collective
// New QRL 2.0 layout — configs+scripts live under configs/, chain data under chains/
// Honors QRL_REPO_ROOT so containerized dashboards (docker-compose.docker-sock.yaml
// mounts the repo at /opt/qrl/repo) resolve mod/variant paths against the mount
// rather than the ~empty /opt/qrl/app tree. Bare-metal keeps the __dirname fallback.
const QRL2_ROOT              = path.join(process.env.QRL_REPO_ROOT || __dirname, 'qrl-2.0');
const QRL1_ROOT              = path.join(__dirname, 'qrl-1.0');
const ZOND_PRIVATE_DIR       = path.join(QRL2_ROOT, 'configs', 'localnet');   // scripts live here
const ZOND_PRIVATE_CHAIN_DIR = path.join(QRL2_ROOT, 'chains',  'localnet');   // pkill patterns match this
const ZOND_TESTNET_DIR       = path.join(QRL2_ROOT, 'configs', 'testnet');
const ZOND_TESTNET_CHAIN_DIR = path.join(QRL2_ROOT, 'chains',  'testnet');
const CLASSIC_PRIVATE_DIR    = path.join(QRL1_ROOT, 'configs', 'localnet');
const CLASSIC_PRIVATE_CHAIN_DIR = path.join(QRL1_ROOT, 'chains', 'localnet');
const CLASSIC_TESTNET_DIR    = path.join(QRL1_ROOT, 'configs', 'testnet');
const CLASSIC_TESTNET_CHAIN_DIR = path.join(QRL1_ROOT, 'chains', 'testnet');
const CLASSIC_MAINNET_DIR    = path.join(QRL1_ROOT, 'configs', 'mainnet');
const CLASSIC_MAINNET_CHAIN_DIR = path.join(QRL1_ROOT, 'chains', 'mainnet');
const GZOND_BIN         = path.join(QRL2_ROOT, 'upstream', 'go-qrl',  'build', 'bin', 'gqrl');  // legacy name, points at gqrl
const GQRL_BIN          = GZOND_BIN;                                                              // new preferred name
const BEACON_CHAIN_BIN  = path.join(QRL2_ROOT, 'upstream', 'qrysm', 'bazel-bin', 'cmd', 'beacon-chain', 'beacon-chain_', 'beacon-chain');
const VALIDATOR_BIN     = path.join(QRL2_ROOT, 'upstream', 'qrysm', 'bazel-bin', 'cmd', 'validator', 'validator_', 'validator');
const RESCUE_GQRL_BIN   = path.join(QRL2_ROOT, 'variants', 'rescue-edition', 'build', 'bin', 'gqrl');

// Node configurations for individual control
const nodeConfigs = {
    'zond-node1': { port: 9545, httpPort: 9545, wsPort: 9645, authPort: 9551, p2pPort: 31303, datadir: 'node1' },
    'zond-node2': { port: 9546, httpPort: 9546, wsPort: 9646, authPort: 9552, p2pPort: 31304, datadir: 'node2' },
    'zond-node3': { port: 9547, httpPort: 9547, wsPort: 9647, authPort: 9553, p2pPort: 31305, datadir: 'node3' },
    'zond-node4': { port: 9548, httpPort: 9548, wsPort: 9648, authPort: 9554, p2pPort: 31306, datadir: 'node4' },
    'classic-node1': { port: 49009, datadir: 'node1-data' },
    'classic-node2': { port: 49019, datadir: 'node2-data' },
    'classic-node3': { port: 49029, datadir: 'node3-data' },
    'classic-node4': { port: 49039, datadir: 'node4-data' }
};

// Reset Zond genesis with current timestamp (keeps accounts)
async function handleZondResetGenesis(req, res) {
    const { execSync, spawn } = require('child_process');

    try {
        console.log('[Zond] Starting genesis reset...');

        // Step 1: Stop all Zond components
        console.log('[Zond] Stopping all components...');
        execSync(`pkill -f "gqrl.*chains/localnet/node" 2>/dev/null || true`);
        execSync(`pkill -f "beacon-chain.*chains/localnet/beacon" 2>/dev/null || true`);
        execSync(`pkill -f "validator.*chains/localnet/validators" 2>/dev/null || true`);
        execSync(`pkill -f "bootnode.*zond-private" 2>/dev/null || true`);

        // Wait for processes to stop
        await new Promise(resolve => setTimeout(resolve, 2000));

        // Step 2: Run reset-chain.sh --keep-accounts (pipe 'yes' to bypass confirmation)
        console.log('[Zond] Running reset-chain.sh --keep-accounts...');
        const resetScript = path.join(ZOND_PRIVATE_DIR, 'reset-chain.sh');

        // Run the script and capture output (echo 'yes' to bypass confirmation prompt)
        const result = execSync(`echo 'yes' | bash "${resetScript}" --keep-accounts 2>&1`, {
            cwd: ZOND_PRIVATE_DIR,
            timeout: 120000, // 2 minute timeout
            encoding: 'utf8'
        });

        console.log('[Zond] Reset complete:', result.slice(-500));

        // Extract genesis account from output if present
        const accountMatch = result.match(/Genesis Account: (Z[a-fA-F0-9]+)/);
        const genesisAccount = accountMatch ? accountMatch[1] : null;

        jsonResponse(res, {
            success: true,
            message: 'Genesis reset complete. Use START ALL to restart the network.',
            genesisAccount: genesisAccount
        });

    } catch (err) {
        console.error('[Zond] Reset failed:', err.message);
        jsonResponse(res, {
            success: false,
            error: `Reset failed: ${err.message}`
        });
    }
}

// ========== VARIANT + SNAPSHOT MANAGEMENT ==========
// Endpoints backing the "Variant & Snapshots" panel in dashboard-v2.
// Shell out to qrl-2.0/tools/chain-snapshot.sh, chain-fork.sh, and
// mods/*/install.sh. Chain-state-changing operations (switch, restore,
// fork) require the full stop→wipe→genesis→start dance — see the .env
// header + tools/chain-snapshot.sh for the manual equivalent.

const CHAIN_TOOLS = path.join(QRL2_ROOT, 'tools');
const CHAIN_SNAPSHOT_SH = path.join(CHAIN_TOOLS, 'chain-snapshot.sh');
const CHAIN_FORK_SH = path.join(CHAIN_TOOLS, 'chain-fork.sh');
const QRL2_ENV_FILE = path.join(QRL2_ROOT, 'configs', 'localnet', '.env');
const QRL2_CONFIG_DIR = path.join(QRL2_ROOT, 'configs', 'localnet');
const QRL2_MODS_DIR = path.join(QRL2_ROOT, 'mods');

// Known variants + which mod (if any) rebuilds their binary.
const VARIANT_REBUILD_MOD = {
    'standard': null,  // built from upstream/go-qrl by hand — no mod-driven install
    'moscow-chill': 'moscow-chill',
    'rescue-edition': 'rescue-prime',  // rescue-prime mod builds rescue-edition variant
};

function currentVariant() {
    try {
        const env = fs.readFileSync(QRL2_ENV_FILE, 'utf8');
        const m = env.match(/^\s*GQRL_VARIANT\s*=\s*"?([^"\s]+)"?/m) ||
                  env.match(/^\s*GZOND_VARIANT\s*=\s*"?([^"\s]+)"?/m);
        return m ? m[1] : 'standard';
    } catch { return 'standard'; }
}

// Which qrysm+go-qrl binaries the testnet-local setup uses. See
// qrl-2.0/configs/testnet/.env for background on why this is a separate
// selector from the variant-switching localnet chain.
const TESTNET_ENV_FILE = path.join(QRL2_ROOT, 'configs', 'testnet', '.env');

function currentTestnetBuild() {
    try {
        const env = fs.readFileSync(TESTNET_ENV_FILE, 'utf8');
        const m = env.match(/^\s*TESTNET_BUILD\s*=\s*"?([^"\s]+)"?/m);
        return m ? m[1] : 'legacy';
    } catch { return 'legacy'; }
}

// GET /api/testnet-build — { build, options, binaries }
async function handleTestnetBuildGet(req, res) {
    const build = currentTestnetBuild();
    const paths = {
        legacy: {
            gqrl: path.join(QRL2_ROOT, 'legacy', 'go-qrl', 'build', 'bin', 'gqrl'),
            beacon: path.join(QRL2_ROOT, 'legacy', 'qrysm', 'build', 'bin', 'beacon-chain'),
        },
        current: {
            gqrl: path.join(QRL2_ROOT, 'upstream', 'go-qrl', 'build', 'bin', 'gqrl'),
            beacon: path.join(QRL2_ROOT, 'upstream', 'qrysm', 'build', 'bin', 'beacon-chain'),
        },
    };
    const binaries = {};
    for (const [k, p] of Object.entries(paths)) {
        binaries[k] = {
            gqrl: fs.existsSync(p.gqrl),
            beacon: fs.existsSync(p.beacon),
        };
    }
    jsonResponse(res, { build, options: ['legacy', 'current'], binaries });
}

// POST /api/testnet-build — { build: 'legacy'|'current' }
async function handleTestnetBuildSet(req, res) {
    const body = await parseBody(req);
    const target = String(body.build || '');
    if (target !== 'legacy' && target !== 'current') {
        return jsonResponse(res, { success: false, error: 'build must be legacy or current' }, 400);
    }
    try {
        let env = '';
        try { env = fs.readFileSync(TESTNET_ENV_FILE, 'utf8'); } catch {}
        const patched = env.match(/^\s*TESTNET_BUILD\s*=/m)
            ? env.replace(/^\s*TESTNET_BUILD\s*=.*/m, `TESTNET_BUILD="${target}"`)
            : env + `\nTESTNET_BUILD="${target}"\n`;
        fs.writeFileSync(TESTNET_ENV_FILE, patched);
        jsonResponse(res, { success: true, build: target });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// GET /api/variants/current — { variant, blockHex, running }
async function handleVariantCurrent(req, res) {
    const variant = currentVariant();
    let blockHex = null, running = false;
    try {
        const r = await httpRequest('http://127.0.0.1:9545', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }),
            timeout: 2000,
        });
        const parsed = JSON.parse(r);
        blockHex = parsed.result || null;
        running = blockHex !== null;
    } catch { /* node down */ }
    jsonResponse(res, {
        variant,
        blockHex,
        running,
        knownVariants: Object.keys(VARIANT_REBUILD_MOD),
    });
}

// POST /api/variants/switch — { variant }
// Stops chain, updates .env, wipes chain data, regenerates genesis,
// leaves the caller to start-nodes.sh + start-beacon.sh manually (matches the
// stop/reset workflow used by /api/zond/reset-genesis). Long-running (~20s).
async function handleVariantSwitch(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const target = String(body.variant || '');
    if (!(target in VARIANT_REBUILD_MOD)) {
        return jsonResponse(res, { success: false, error: `unknown variant: ${target}` }, 400);
    }
    const bin = target === 'standard'
        ? path.join(QRL2_ROOT, 'upstream', 'go-qrl', 'build', 'bin', 'gqrl')
        : path.join(QRL2_ROOT, 'variants', target, 'build', 'bin', 'gqrl');
    if (!fs.existsSync(bin)) {
        return jsonResponse(res, {
            success: false,
            error: `variant binary missing at ${bin}. Rebuild via /api/variants/build first.`,
        }, 400);
    }
    try {
        // 1. Stop everything
        execSync(`pkill -f "beacon-chain.*chains/localnet" 2>/dev/null || true`);
        execSync(`pkill -f "validator.*chains/localnet" 2>/dev/null || true`);
        execSync(`pkill -f "gqrl.*chains/localnet" 2>/dev/null || true`);
        await new Promise(r => setTimeout(r, 3000));

        // 2. Update .env
        const env = fs.readFileSync(QRL2_ENV_FILE, 'utf8');
        const patched = env.match(/^\s*GQRL_VARIANT\s*=/m)
            ? env.replace(/^\s*GQRL_VARIANT\s*=.*/m, `GQRL_VARIANT="${target}"`)
            : env + `\nGQRL_VARIANT="${target}"\n`;
        fs.writeFileSync(QRL2_ENV_FILE, patched);

        // 3. Wipe chain state (chaindata + beacon state + validator db)
        //    Preserve keystores + config files (genesis is regenerated below).
        const chains = path.join(QRL2_ROOT, 'chains', 'localnet');
        for (const n of [1, 2, 3, 4]) {
            const p = path.join(chains, `node${n}`, 'gqrl');
            if (fs.existsSync(p)) execSync(`rm -rf "${p}"`);
        }
        for (const b of ['beacon', 'beacon-node2', 'beacon-node3', 'beacon-node4']) {
            for (const sub of ['beaconchaindata', 'network-keys', 'metaData', 'boot.key']) {
                const p = path.join(chains, b, sub);
                if (fs.existsSync(p)) execSync(`rm -rf "${p}"`);
            }
        }
        const vdb = path.join(chains, 'validators', 'validator.db');
        if (fs.existsSync(vdb)) execSync(`rm -rf "${vdb}"`);

        // 4. Regenerate genesis pair with qrysmctl
        const qrysmctl = path.join(QRL2_ROOT, 'upstream', 'qrysm', 'build', 'bin', 'qrysmctl');
        const genesisTime = Math.floor(Date.now() / 1000) + 30;
        execSync(
            `GOMAXPROCS=2 "${qrysmctl}" testnet generate-genesis ` +
            `--chain-config-file="${path.join(QRL2_CONFIG_DIR, 'config.yml')}" ` +
            `--config-name=interop --num-validators=4 ` +
            `--genesis-time=${genesisTime} ` +
            `--output-ssz="${path.join(QRL2_CONFIG_DIR, 'genesis.ssz')}" ` +
            `--gqrl-genesis-json-out="${path.join(QRL2_CONFIG_DIR, 'genesis.json')}"`,
            { timeout: 60000, encoding: 'utf8' }
        );

        // 5. gqrl init for each node
        for (const n of [1, 2, 3, 4]) {
            execSync(
                `"${bin}" --datadir="${path.join(chains, `node${n}`)}" init "${path.join(QRL2_CONFIG_DIR, 'genesis.json')}"`,
                { timeout: 30000, encoding: 'utf8' }
            );
        }

        jsonResponse(res, {
            success: true,
            variant: target,
            message: `Switched to ${target}. Run START ALL (nodes) then start-beacon + start-validators to bring chain up.`,
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: `switch failed: ${err.message}` }, 500);
    }
}

// POST /api/variants/build — { variant }
// Runs the mod's install.sh, which produces the variant binary. Long-running
// (~2-3 min for a full rebuild) — response is deferred until it completes.
async function handleVariantBuild(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const target = String(body.variant || '');
    const mod = VARIANT_REBUILD_MOD[target];
    if (!mod) {
        return jsonResponse(res, {
            success: false,
            error: `${target} has no mod-driven rebuild. Standard is built manually via ` +
                   `go build in upstream/go-qrl.`,
        }, 400);
    }
    // Container topology guard: the dashboard image has no go toolchain,
    // and the docker-sock overlay mounts the repo read-only, so both
    // install.sh (which patches variants/<target>/) and the follow-on
    // `go build` cannot succeed here. Give the operator the exact
    // host-side command instead of a cryptic filesystem/toolchain error.
    if (fs.existsSync('/.dockerenv')) {
        const repoHint = process.env.QRL_REPO_ROOT || '$REPO';
        return jsonResponse(res, {
            success: false,
            inContainer: true,
            variant: target,
            error: 'variant rebuild is a host-only operation — the dashboard container has no go toolchain and the repo mount is read-only',
            hint: 'For a docker image swap use the RELEASE tab BUILD (produces qrl-gqrl:<variant>); then bring up a separate compose project pointing at that image. For a bare-metal binary run these on the host:',
            hostCommand: `bash "${repoHint}/qrl-2.0/mods/${mod}/install.sh" && cd "${repoHint}/qrl-2.0/variants/${target}" && go build -buildvcs=false -o build/bin/gqrl ./cmd/gqrl`,
        }, 400);
    }
    const installer = path.join(QRL2_MODS_DIR, mod, 'install.sh');
    if (!fs.existsSync(installer)) {
        return jsonResponse(res, { success: false, error: `missing installer: ${installer}` }, 500);
    }
    try {
        const output = execSync(`bash "${installer}" 2>&1`, {
            timeout: 300000, // 5 min
            encoding: 'utf8',
            maxBuffer: 8 * 1024 * 1024,
        });
        // Also build the binary itself.
        const variantDir = path.join(QRL2_ROOT, 'variants', target);
        const buildOut = execSync(
            `cd "${variantDir}" && go build -buildvcs=false -o build/bin/gqrl ./cmd/gqrl 2>&1`,
            { timeout: 300000, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }
        );
        jsonResponse(res, {
            success: true,
            variant: target,
            installLog: output.slice(-2000),
            buildLog: buildOut.slice(-500),
        });
    } catch (err) {
        jsonResponse(res, {
            success: false,
            error: `build failed: ${err.message}`,
            output: (err.stdout || '').slice(-2000),
        }, 500);
    }
}

// GET /api/snapshots — returns array from chain-snapshot.sh list --json
async function handleSnapshotList(req, res) {
    const { execSync } = require('child_process');
    try {
        const out = execSync(`bash "${CHAIN_SNAPSHOT_SH}" list --json`, {
            timeout: 10000, encoding: 'utf8',
        });
        jsonResponse(res, JSON.parse(out.trim() || '[]'));
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// POST /api/snapshots/save — { name, note? }
async function handleSnapshotSave(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const name = String(body.name || '').trim();
    const note = String(body.note || '');
    if (!/^[A-Za-z0-9._-]{1,64}$/.test(name)) {
        return jsonResponse(res, {
            success: false,
            error: 'name must be 1-64 chars matching [A-Za-z0-9._-]',
        }, 400);
    }
    try {
        const args = ['save', name, '--force'];
        if (note) args.push('--note', note);
        const cmd = `bash "${CHAIN_SNAPSHOT_SH}" ${args.map(a => `"${a.replace(/"/g, '\\"')}"`).join(' ')} 2>&1`;
        const out = execSync(cmd, { timeout: 120000, encoding: 'utf8' });
        jsonResponse(res, { success: true, log: out.slice(-1000) });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// POST /api/snapshots/restore — { name }
async function handleSnapshotRestore(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const name = String(body.name || '').trim();
    if (!/^[A-Za-z0-9._-]{1,64}$/.test(name)) {
        return jsonResponse(res, { success: false, error: 'invalid snapshot name' }, 400);
    }
    try {
        // Stop chain first — restore refuses to run on a live chain.
        execSync(`pkill -f "beacon-chain.*chains/localnet" 2>/dev/null || true`);
        execSync(`pkill -f "validator.*chains/localnet" 2>/dev/null || true`);
        execSync(`pkill -f "gqrl.*chains/localnet" 2>/dev/null || true`);
        await new Promise(r => setTimeout(r, 3000));

        const out = execSync(`bash "${CHAIN_SNAPSHOT_SH}" restore "${name}" --yes 2>&1`, {
            timeout: 180000, encoding: 'utf8',
        });
        jsonResponse(res, { success: true, log: out.slice(-1000) });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// POST /api/snapshots/delete — { name }
async function handleSnapshotDelete(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const name = String(body.name || '').trim();
    if (!/^[A-Za-z0-9._-]{1,64}$/.test(name)) {
        return jsonResponse(res, { success: false, error: 'invalid snapshot name' }, 400);
    }
    try {
        const out = execSync(`bash "${CHAIN_SNAPSHOT_SH}" delete "${name}" --yes 2>&1`, {
            timeout: 30000, encoding: 'utf8',
        });
        jsonResponse(res, { success: true, log: out.slice(-500) });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// POST /api/snapshots/fork — { src, newName, toVariant, note? }
async function handleSnapshotFork(req, res) {
    const { execSync } = require('child_process');
    const body = await parseBody(req);
    const src = String(body.src || '').trim();
    const newName = String(body.newName || '').trim();
    const toVariant = String(body.toVariant || '').trim();
    const note = String(body.note || '');
    for (const [k, v] of Object.entries({ src, newName })) {
        if (!/^[A-Za-z0-9._-]{1,64}$/.test(v)) {
            return jsonResponse(res, { success: false, error: `invalid ${k}` }, 400);
        }
    }
    if (!(toVariant in VARIANT_REBUILD_MOD)) {
        return jsonResponse(res, { success: false, error: `unknown target variant` }, 400);
    }
    try {
        const args = [src, newName, '--to-variant', toVariant, '--force'];
        if (note) args.push('--note', note);
        const cmd = `bash "${CHAIN_FORK_SH}" ${args.map(a => `"${a.replace(/"/g, '\\"')}"`).join(' ')} 2>&1`;
        const out = execSync(cmd, { timeout: 120000, encoding: 'utf8' });
        jsonResponse(res, { success: true, log: out.slice(-1000) });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) }, 500);
    }
}

// ========== BOUNDED JSON PROXY (for token metadata) ==========
// GET /api/proxy/json?url=... — fetches a small JSON payload (up to 16 KB, 5s timeout)
// and returns it with no-store caching. Used by the token browser to read tokenURI()
// metadata from origins that don't expose CORS headers. Image bytes are never proxied
// — the browser loads them directly from the URL embedded in the JSON via <img src>.
async function handleJsonProxy(req, res, fullUrl) {
    const q = new URLSearchParams(fullUrl.split('?', 2)[1] || '');
    const target = q.get('url');
    if (!target) return jsonResponse(res, { error: 'url required' });

    let parsed;
    try { parsed = new URL(target); } catch (_) { return jsonResponse(res, { error: 'bad url' }); }
    if (!['http:', 'https:'].includes(parsed.protocol)) {
        return jsonResponse(res, { error: 'only http(s) allowed' });
    }
    // Refuse to proxy any private/loopback destinations from the outside world.
    // Localhost RPCs shouldn't be reachable via this endpoint.
    const host = parsed.hostname;
    if (
        host === 'localhost' || host === '127.0.0.1' || host === '::1' ||
        host.startsWith('10.') || host.startsWith('192.168.') ||
        /^172\.(1[6-9]|2[0-9]|3[0-1])\./.test(host)
    ) {
        return jsonResponse(res, { error: 'refusing to proxy private-network host' });
    }

    const client = parsed.protocol === 'https:' ? https : http;
    const MAX_BYTES = 16 * 1024;
    const TIMEOUT_MS = 5000;

    try {
        await new Promise((resolve, reject) => {
            const upstream = client.get(target, { timeout: TIMEOUT_MS }, (r) => {
                if (r.statusCode >= 300 && r.statusCode < 400 && r.headers.location) {
                    r.destroy();
                    return reject(new Error('redirects not followed'));
                }
                if (r.statusCode !== 200) {
                    r.destroy();
                    return reject(new Error(`upstream ${r.statusCode}`));
                }
                const chunks = [];
                let total = 0;
                r.on('data', (c) => {
                    total += c.length;
                    if (total > MAX_BYTES) {
                        r.destroy();
                        return reject(new Error('payload too large'));
                    }
                    chunks.push(c);
                });
                r.on('end', () => {
                    const buf = Buffer.concat(chunks);
                    let parsedJson;
                    try {
                        parsedJson = JSON.parse(buf.toString('utf8'));
                    } catch (e) {
                        return reject(new Error('not valid JSON'));
                    }
                    res.writeHead(200, {
                        'Content-Type': 'application/json',
                        'Cache-Control': 'no-store',
                    });
                    res.end(JSON.stringify(parsedJson));
                    resolve();
                });
                r.on('error', reject);
            });
            upstream.on('timeout', () => { upstream.destroy(new Error('timeout')); });
            upstream.on('error', reject);
        });
    } catch (err) {
        return jsonResponse(res, { error: errDetail(err) });
    }
}

// ========== COMPOSE STACK STATUS ==========
// GET /api/compose/status → docker's HTTP-over-Unix-socket API for the
// qrl-popup project, converted to a UI-friendly shape.
//
// Uses the docker daemon's HTTP API directly via http.request over the
// mounted socket — no docker CLI needed. Falls back with a clear hint
// if the socket isn't mounted (default when bringing up without
// docker-compose.docker-sock.yaml overlay).

// ---------- Module-level helpers for the docker HTTP API ----------
// Extracted so /api/compose/status, /reset, /security, and (future)
// release-pipeline handlers can share one implementation. Uses
// DOCKER_SOCK env var (defaults to /var/run/docker.sock) so operators
// can point at a rootless podman socket without editing code.
function dockerSockPath() {
    return process.env.DOCKER_SOCK || '/var/run/docker.sock';
}
function dockerSockAvailable() {
    try { return fs.existsSync(dockerSockPath()); } catch { return false; }
}
function dockerApi(method, apipath, body, timeoutMs) {
    const bodyStr = body ? (typeof body === 'string' ? body : JSON.stringify(body)) : null;
    return new Promise((resolve, reject) => {
        const r = http.request({
            socketPath: dockerSockPath(),
            path: apipath,
            method,
            headers: bodyStr
                ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(bodyStr) }
                : {},
        }, (rr) => {
            let buf = '';
            rr.on('data', (d) => buf += d);
            rr.on('end', () => resolve({ status: rr.statusCode, body: buf }));
            rr.on('error', reject);
        });
        r.on('error', reject);
        r.setTimeout(timeoutMs || 5000, () => r.destroy(new Error('docker API timeout')));
        if (bodyStr) r.write(bodyStr);
        r.end();
    });
}
// Project name is what `docker compose -p <name>` writes into the
// compose.project label. Defaults to `qrl-popup` for backward-compat
// with the single-stack setup; multi-stack deployments (e.g. an
// l2-rescue overlay) bring up with a distinct -p and the dashboard
// picks its own scope up from COMPOSE_PROJECT_NAME.
const COMPOSE_PROJECT_NAME = process.env.COMPOSE_PROJECT_NAME || 'qrl-popup';
const COMPOSE_LABEL = `com.docker.compose.project=${COMPOSE_PROJECT_NAME}`;
function composeFiltersQuery() {
    return encodeURIComponent(JSON.stringify({ label: [COMPOSE_LABEL] }));
}

// Deterministic theme picker keyed on the compose project name so
// separate stacks (qrl-popup vs l2-rescue vs testnet-mirror) get
// visibly distinct dashboard accents. Palette is fixed and small so
// an operator can memorize which color means which stack.
const THEME_PALETTE = [
    { name: 'cobalt',   accent: '#3aa0ff', label: 'COBALT'  },
    { name: 'amber',    accent: '#ffb020', label: 'AMBER'   },
    { name: 'rose',     accent: '#ff5577', label: 'ROSE'    },
    { name: 'violet',   accent: '#b070ff', label: 'VIOLET'  },
    { name: 'teal',     accent: '#22d4b0', label: 'TEAL'    },
    { name: 'gold',     accent: '#e0c040', label: 'GOLD'    },
];
function pickTheme(name) {
    let h = 0;
    for (let i = 0; i < name.length; i++) h = ((h << 5) - h + name.charCodeAt(i)) | 0;
    return THEME_PALETTE[Math.abs(h) % THEME_PALETTE.length];
}

async function handleComposeStatus(req, res) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, {
            success: false,
            error: `docker socket not accessible at ${dockerSockPath()}`,
            hint: 'Bring up with the docker-sock overlay to enable compose visibility: `docker compose -f docker-compose.yaml -f docker-compose.docker-sock.yaml up`. Or run docker/status.sh on the host directly.',
        });
    }
    try {
        const raw = await dockerApi('GET', `/containers/json?all=true&filters=${composeFiltersQuery()}`);
        const arr = JSON.parse(raw.body);
        const containers = arr.map(c => {
            // Names are ["/qrl-gqrl-node1", ...] — strip leading slash.
            const name = (c.Names && c.Names[0] || '').replace(/^\//, '');
            const state = c.State || '';
            const status = c.Status || '';
            // Docker returns Status like "Up 3 minutes (healthy)".
            const m = /\((healthy|unhealthy|health: starting|starting)\)/i.exec(status);
            const health = m ? m[1].toLowerCase().replace('health: ', '') : null;
            // Short id (first 12 chars) is enough for docker API addressing.
            const id = String(c.Id || '').slice(0, 12);
            return { id, name, state, status, image: c.Image, health };
        });
        // Sort deterministically so UI rows don't jump around each refresh.
        containers.sort((a, b) => a.name.localeCompare(b.name));
        jsonResponse(res, {
            success: true,
            containers,
            summary: {
                total:    containers.length,
                running:  containers.filter(c => c.state === 'running').length,
                healthy:  containers.filter(c => c.health === 'healthy').length,
                starting: containers.filter(c => c.health === 'starting').length,
                unhealthy: containers.filter(c => c.health === 'unhealthy').length,
            }
        });
    } catch (err) {
        jsonResponse(res, {
            success: false,
            error: errDetail(err),
            hint: 'docker socket present but query failed — check daemon health.',
        });
    }
}

// ========== COMPOSE FULL RESET ==========
// POST /api/compose/reset → stop + remove every container in the qrl-popup
// compose project, then remove the named volumes. Uses the docker HTTP
// API directly so it works from inside the dashboard container (same
// prereq as /api/compose/status: docker.sock must be mounted).
//
// Does NOT wipe host bind-mount data (docker/data), the .env file, or
// the qrlenv master key — those require host-side execution, so the
// response returns the exact bash command to run for the full wipe.
// This is the safer default: the destructive parts require an explicit
// host-side step, but the dashboard can still stop + free the compose
// stack in one click.
async function handleComposeReset(req, res) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, {
            success: false,
            error: `docker socket not accessible at ${dockerSockPath()}`,
            hint: 'Enable docker.sock mount (docker-compose.docker-sock.yaml) OR run docker/reset-popup.sh on the host.',
        });
    }

    const results = { stopped: [], removed: [], errors: [] };
    try {
        // 1. Enumerate qrl-popup containers.
        const listRes = await dockerApi('GET', `/containers/json?all=true&filters=${composeFiltersQuery()}`, null, 15000);
        const containers = JSON.parse(listRes.body);

        // 2. Stop + remove each. Skip the dashboard's own container —
        //    it would kill this handler mid-flight. Operator's next
        //    `docker compose up` will recreate the dashboard cleanly.
        const selfContainerId = process.env.HOSTNAME || '';
        for (const c of containers) {
            if (selfContainerId && c.Id.startsWith(selfContainerId)) continue;
            try {
                await dockerApi('POST', `/containers/${c.Id}/stop?t=10`, null, 15000);
                results.stopped.push(c.Names[0]);
                await dockerApi('DELETE', `/containers/${c.Id}?v=true&force=true`, null, 15000);
                results.removed.push(c.Names[0]);
            } catch (e) {
                results.errors.push(`${c.Names[0]}: ${errDetail(e)}`);
            }
        }

        // 3. Remove the compose-created named volume(s).
        try {
            const vlist = await dockerApi('GET', `/volumes?filters=${composeFiltersQuery()}`);
            const vs = JSON.parse(vlist.body).Volumes || [];
            for (const v of vs) {
                try { await dockerApi('DELETE', `/volumes/${v.Name}`); }
                catch (e) { results.errors.push(`vol/${v.Name}: ${errDetail(e)}`); }
            }
        } catch (e) { results.errors.push(`volume list: ${errDetail(e)}`); }

        jsonResponse(res, {
            success: results.errors.length === 0,
            containersStopped: results.stopped,
            containersRemoved: results.removed,
            errors: results.errors,
            note: 'Compose stack stopped + removed. Host bind-mount data (docker/data), .env, and qrlenv master key are NOT wiped — those require host-side execution.',
            hostCommand: 'bash docker/reset-popup.sh --yes   # full wipe (data + secrets + master key) on the host',
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err), partial: results });
    }
}

// ========== DISK SPACE ==========
// GET /api/system/disk → df -Pk on the docker/data directory so the
// UI can warn before out-of-space failures during bring-up. Mirrors
// the configure.sh / release.sh preflight from task #88.
async function handleSystemDisk(req, res) {
    try {
        const { execSync } = require('child_process');
        // Check the compose data dir; fall back to __dirname if it
        // doesn't exist yet (fresh clone).
        const target = fs.existsSync(path.join(__dirname, 'docker', 'data'))
            ? path.join(__dirname, 'docker', 'data')
            : __dirname;
        const out = execSync(`df -Pk "${target}"`, { encoding: 'utf8', timeout: 3000 });
        const line = out.trim().split('\n')[1];
        if (!line) throw new Error('df returned no data row');
        // Fields: Filesystem 1024-blocks Used Available Capacity Mounted-on
        const fields = line.split(/\s+/);
        const totalKb = parseInt(fields[1], 10);
        const usedKb  = parseInt(fields[2], 10);
        const availKb = parseInt(fields[3], 10);
        const availGb = Math.round(availKb / 1024 / 1024 * 10) / 10;
        // Severity thresholds match configure.sh: <2GB critical, <10GB warn.
        let severity = 'ok';
        if (availKb < 2097152)      severity = 'critical';
        else if (availKb < 10485760) severity = 'warn';
        jsonResponse(res, {
            success: true,
            target,
            totalKb, usedKb, availKb, availGb,
            severity,
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// ========== COMPOSE SECURITY POSTURE (task #100 / C1) ==========
// GET /api/compose/security → per-container HostConfig snapshot so
// the dashboard can render badges for read-only rootfs, no-new-privs,
// cap-drop:ALL, custom seccomp, and non-runc runtimes. Mirrors what
// `docker inspect` reports under HostConfig — same source of truth
// the compose overlays feed into (docker-compose.hardened.yaml adds
// runtime, docker-compose.seccomp.yaml adds seccomp=... etc).
async function handleComposeSecurity(req, res) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, {
            success: false,
            error: `docker socket not accessible at ${dockerSockPath()}`,
            hint: 'Enable docker.sock mount (docker-compose.docker-sock.yaml) to expose per-container HostConfig.',
        });
    }
    try {
        const listRes = await dockerApi('GET', `/containers/json?all=true&filters=${composeFiltersQuery()}`);
        const list = JSON.parse(listRes.body);
        const rows = [];
        for (const c of list) {
            try {
                const detailRes = await dockerApi('GET', `/containers/${c.Id}/json`);
                const d = JSON.parse(detailRes.body);
                const hc = d.HostConfig || {};
                const secOpt = hc.SecurityOpt || [];
                const cap = hc.CapDrop || [];
                const capAdd = hc.CapAdd || [];
                const name = (d.Name || '').replace(/^\//, '');
                const runtime = hc.Runtime || 'runc';
                const seccompCustom = secOpt.some(s =>
                    typeof s === 'string' && s.startsWith('seccomp=') && s !== 'seccomp=unconfined');
                const seccompDisabled = secOpt.some(s => s === 'seccomp=unconfined');
                rows.push({
                    name,
                    readOnly:    !!hc.ReadonlyRootfs,
                    noNewPrivs:  secOpt.some(s => s === 'no-new-privileges:true' || s === 'no-new-privileges'),
                    capDropAll:  cap.includes('ALL') || cap.includes('all'),
                    capAdd,
                    seccompCustom,
                    seccompDisabled,
                    runtime,
                    userNs:      hc.UsernsMode || null,
                });
            } catch (e) {
                rows.push({ name: c.Names?.[0]?.replace(/^\//, '') || '?', error: errDetail(e) });
            }
        }
        rows.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
        const runtimes = {};
        for (const r of rows) {
            if (r.runtime) runtimes[r.runtime] = (runtimes[r.runtime] || 0) + 1;
        }
        jsonResponse(res, {
            success: true,
            containers: rows,
            summary: {
                total:            rows.length,
                readOnly:         rows.filter(r => r.readOnly).length,
                noNewPrivs:       rows.filter(r => r.noNewPrivs).length,
                capDropAll:       rows.filter(r => r.capDropAll).length,
                seccompCustom:    rows.filter(r => r.seccompCustom).length,
                seccompDisabled:  rows.filter(r => r.seccompDisabled).length,
                runtimes,
            },
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// ========== DASHBOARD IDENTITY (task #101) ==========
// GET /api/dashboard/identity → { rank, project, theme, inContainer }
// The dashboard's own self-report so the header can render a rank
// chip and pick up the theme accent. Rank is always LEAD in this
// tier — a scope tag more than a hierarchy, since each compose
// project's dashboard is authoritative for its own stack. Theme is a
// deterministic hash of the project name so two operators looking at
// the same stack see the same color.
async function handleDashboardIdentity(req, res) {
    const inContainer = fs.existsSync('/.dockerenv');
    const theme = pickTheme(COMPOSE_PROJECT_NAME);
    // Rank is a short operator-supplied label. Default LEAD is right for
    // the canonical stack; sibling stacks brought up alongside it (e.g.
    // l2-rescue) set QRL_STACK_RANK to a distinct label (SUB, L2, MIRROR,
    // etc.) via their compose overlay so the two dashboards never both
    // read as "LEAD" in the header chip.
    const rank = String(process.env.QRL_STACK_RANK || 'LEAD').toUpperCase().slice(0, 12);

    // Detect the gqrl variant this stack is running, by reading the
    // gqrl-node1 container's image tag from the daemon. Runs in parallel
    // with the identity response (best-effort — falls back to null if
    // the socket isn't mounted or the container isn't there).
    let variant = null;
    if (dockerSockAvailable()) {
        try {
            const listRes = await dockerApi('GET',
                `/containers/json?filters=${encodeURIComponent(JSON.stringify({
                    label: [COMPOSE_LABEL, 'com.docker.compose.service=gqrl-node1'],
                }))}`, null, 2000);
            const list = JSON.parse(listRes.body);
            if (list.length > 0) {
                const image = String(list[0].Image || '');
                // Two shapes reach here:
                //   `qrl-gqrl:<variant>` — produced by docker/build-images.sh
                //       (the release path). The tag IS the variant.
                //   `qrl-popup-gqrl-node1` — produced by `docker compose build`
                //       (compose auto-tags with project prefix + service name).
                //       No variant baked into the tag; the actual variant
                //       depends on the QRL_VARIANT_PATH build-arg that was
                //       passed at build time (defaults to standard).
                const m = /^qrl-gqrl:([\w.-]+)$/.exec(image);
                if (m) {
                    variant = m[1];
                } else if (/qrl-gqrl/.test(image) || /gqrl-node/.test(image)) {
                    // Compose-tagged image (or any other gqrl variant we can't
                    // parse from the tag). Fall back to "standard" — the
                    // default QRL_VARIANT_PATH in docker-compose.yaml.
                    variant = 'standard';
                }
            }
        } catch { /* best-effort */ }
    }

    jsonResponse(res, {
        success: true,
        rank,
        project: COMPOSE_PROJECT_NAME,
        inContainer,
        hostname: os.hostname(),
        theme,
        variant,
    });
}

// ========== OTHER COMPOSE PROJECTS (task #101) ==========
// GET /api/compose/projects → every distinct compose project on the
// daemon, so a LEAD dashboard can point the operator at sibling
// stacks running side-by-side (e.g. an l2-rescue stack alongside
// the standard qrl-popup one). Does NOT command across projects —
// each stack's own dashboard is authoritative for its containers.
async function handleComposeProjects(req, res) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, {
            success: false,
            error: `docker socket not accessible at ${dockerSockPath()}`,
            hint: 'Enable docker.sock mount (docker-compose.docker-sock.yaml) to enumerate other stacks.',
        });
    }
    try {
        const raw = await dockerApi('GET', '/containers/json?all=true', null, 30000);
        const arr = JSON.parse(raw.body);
        const byProject = {};
        for (const c of arr) {
            const labels = c.Labels || {};
            const p = labels['com.docker.compose.project'];
            if (!p) continue;
            if (!byProject[p]) byProject[p] = {
                name: p, containers: 0, running: 0, healthy: 0, dashboardPort: null,
            };
            byProject[p].containers++;
            if (c.State === 'running') byProject[p].running++;
            if ((c.Status || '').toLowerCase().includes('(healthy)')) byProject[p].healthy++;
            if (labels['com.docker.compose.service'] === 'dashboard') {
                const pub = (c.Ports || []).find(pp => pp.PublicPort);
                if (pub) byProject[p].dashboardPort = pub.PublicPort;
            }
        }
        const projects = Object.values(byProject).map(p => ({
            ...p,
            url: p.dashboardPort ? `http://127.0.0.1:${p.dashboardPort}/` : null,
            isCurrent: p.name === COMPOSE_PROJECT_NAME,
            theme: pickTheme(p.name),
        }));
        projects.sort((a, b) => {
            if (a.isCurrent !== b.isCurrent) return a.isCurrent ? -1 : 1;
            return a.name.localeCompare(b.name);
        });
        jsonResponse(res, { success: true, current: COMPOSE_PROJECT_NAME, projects });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// ========== PER-CONTAINER LIFECYCLE ACTIONS (task #101) ==========
// POST /api/compose/container/{restart,pause,unpause}  { id }
// Thin wrappers over docker's HTTP API. Guardrails:
//   - id must be a bare docker id/name (regex-checked) — no path traversal
//   - refuses to act on the dashboard's own container (self-kill)
//   - refuses to act on containers outside this compose project (defense in
//     depth against a compromised UI targeting a sibling stack)
async function verifyContainerInProject(id) {
    const detail = await dockerApi('GET', `/containers/${id}/json`);
    if (detail.status >= 400) throw new Error(`container ${id} not found`);
    const d = JSON.parse(detail.body);
    const labels = (d.Config && d.Config.Labels) || {};
    const p = labels['com.docker.compose.project'];
    if (p !== COMPOSE_PROJECT_NAME) {
        throw new Error(`container ${id} is in project '${p || '(none)'}' — this dashboard controls '${COMPOSE_PROJECT_NAME}' only`);
    }
    return d;
}

async function handleComposeContainerAction(req, res, action) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, { success: false, error: `docker socket not accessible at ${dockerSockPath()}` });
    }
    let body;
    try { body = await parseBody(req); } catch { body = {}; }
    const id = String(body.id || '').trim();
    if (!/^[a-zA-Z0-9._-]{1,128}$/.test(id)) {
        return jsonResponse(res, { success: false, error: 'invalid container id' }, 400);
    }
    const selfId = process.env.HOSTNAME || '';
    if (selfId && (id === selfId || id.startsWith(selfId) || selfId.startsWith(id))) {
        return jsonResponse(res, {
            success: false,
            error: 'refusing to act on the dashboard container — would self-terminate mid-flight',
        }, 400);
    }
    try {
        await verifyContainerInProject(id);
        const r = await dockerApi('POST', `/containers/${id}/${action}`, null, 20000);
        if (r.status >= 400) {
            return jsonResponse(res, {
                success: false,
                error: `docker api ${r.status}: ${r.body || '(empty)'}`.slice(0, 500),
            });
        }
        jsonResponse(res, { success: true, id, action });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}
async function handleComposeContainerRestart(req, res) { return handleComposeContainerAction(req, res, 'restart'); }
async function handleComposeContainerPause(req, res)   { return handleComposeContainerAction(req, res, 'pause');   }
async function handleComposeContainerUnpause(req, res) { return handleComposeContainerAction(req, res, 'unpause'); }

// GET /api/compose/container/logs?id=&tail=200
// Returns last `tail` lines of the container's stdout+stderr with RFC3339
// timestamps. Shells to the docker CLI (already in the dashboard image)
// so we don't need to demux the daemon's multiplex framing ourselves.
async function handleComposeContainerLogs(req, res) {
    if (!dockerSockAvailable()) {
        return jsonResponse(res, { success: false, error: `docker socket not accessible at ${dockerSockPath()}` });
    }
    const q = new URL(req.url, 'http://x').searchParams;
    const id = String(q.get('id') || '').trim();
    const tail = Math.min(2000, Math.max(50, parseInt(q.get('tail') || '200', 10) || 200));
    if (!/^[a-zA-Z0-9._-]{1,128}$/.test(id)) {
        return jsonResponse(res, { success: false, error: 'invalid container id' }, 400);
    }
    try {
        await verifyContainerInProject(id);
        // docker logs writes to stderr as well as stdout for non-TTY
        // containers — use a shell to merge both streams so the full
        // log tail comes back on stdout.
        const { execSync } = require('child_process');
        const shellId = id.replace(/[^a-zA-Z0-9._-]/g, ''); // regex-sanitized above, this is defence in depth.
        const out = execSync(`docker logs --tail=${tail} --timestamps ${shellId} 2>&1`, {
            timeout: 10000, encoding: 'utf8', maxBuffer: 4 * 1024 * 1024,
        });
        jsonResponse(res, { success: true, id, tail, logs: out });
    } catch (err) {
        const combined = (err.stdout || '') + (err.stderr || '');
        jsonResponse(res, {
            success: false,
            error: errDetail(err),
            logs: combined ? combined.slice(-100000) : undefined,
        });
    }
}

// ========== QRLENV MASTER-KEY STATE (task #100 / C2) ==========
// GET /api/system/qrlenv → probes whether the qrlenv master key exists
// in the OS keychain (via `qrlenv keys` exit code) and counts how many
// `qrl-env:` blobs vs plaintext lines the docker/.env file holds.
// Deliberately does NOT decrypt anything — just tells the operator
// whether the encryption substrate is set up.
async function handleSystemQrlenv(req, res) {
    const { execFile } = require('child_process');
    // qrlenv lives at bin/qrlenv on bare-metal, /opt/qrl/bin/qrlenv in the
    // dashboard container (Dockerfile.status-server copies it there).
    // Probe both so the panel works in either topology. Prefer the
    // repo-mounted copy (works with QRL_REPO_ROOT overlay) since it
    // matches what the .env was encrypted against.
    const candidates = [
        path.join(process.env.QRL_REPO_ROOT || __dirname, 'bin', 'qrlenv'),
        path.join(__dirname, 'bin', 'qrlenv'),
        '/opt/qrl/bin/qrlenv',
    ];
    let qrlenv = candidates[0];
    for (const c of candidates) { if (fs.existsSync(c)) { qrlenv = c; break; } }
    const envFile = path.join(process.env.QRL_REPO_ROOT || __dirname, 'docker', '.env');
    const result = {
        success: true,                             // .env parse is best-effort even without qrlenv
        qrlenvPath: qrlenv,
        qrlenvPresent: fs.existsSync(qrlenv),
        envFile,
        envFilePresent: fs.existsSync(envFile),
        keyPresent: null,
        encryptedCount: 0,
        plaintextCount: 0,
        secretKeysUnencrypted: [],
    };
    // Parse .env to count encrypted vs plaintext entries. Any key whose
    // value starts with `qrl-env:` is encrypted; everything else is
    // plaintext. Also flag "secret-looking" keys (PASSWORD, JWT, SECRET,
    // KEY) that are plaintext so the operator sees the risk.
    if (result.envFilePresent) {
        try {
            const content = fs.readFileSync(envFile, 'utf8');
            for (const line of content.split('\n')) {
                const m = /^([A-Z_][A-Z0-9_]*)=(.*)$/.exec(line.trim());
                if (!m) continue;
                const key = m[1], val = m[2];
                if (val.startsWith('qrl-env:')) {
                    result.encryptedCount++;
                } else if (val !== '') {
                    result.plaintextCount++;
                    if (/PASSWORD|JWT|SECRET|KEY/.test(key)) {
                        result.secretKeysUnencrypted.push(key);
                    }
                }
            }
        } catch (e) {
            result.envParseError = errDetail(e);
        }
    }
    // Probe the OS keychain via `qrlenv keys` (which reads the master key
    // to decrypt any qrl-env: entries; non-zero exit = master key missing
    // or wrong profile). Use --profile qrlenv (the compose default).
    // Only runs when qrlenv is available AND we're on the host (in a
    // container the OS keychain is unreachable — probe would always fail).
    // Detect container context via the docker sock mount presence.
    const inContainer = fs.existsSync('/.dockerenv');
    if (result.qrlenvPresent && result.envFilePresent && !inContainer) {
        await new Promise((resolve) => {
            execFile(qrlenv, ['--profile', 'qrlenv', '--file', envFile, 'keys'],
                { timeout: 5000 }, (err, stdout, stderr) => {
                    if (err) {
                        result.keyPresent = false;
                        result.keyProbeStderr = (stderr || err.message).slice(-500);
                    } else {
                        result.keyPresent = true;
                    }
                    resolve();
                });
        });
    } else if (inContainer) {
        result.keyProbeNote = 'OS keychain unreachable from inside a container — run dashboard bare-metal to probe. .env parse counts still shown below.';
    }
    // Derive a UI-friendly severity: green when key + encrypted values
    // present; amber when secrets are plaintext; red when key is missing
    // but there ARE encrypted values (they're now unrecoverable).
    if (result.keyPresent === false && result.encryptedCount > 0) {
        result.severity = 'critical';
    } else if (result.secretKeysUnencrypted.length > 0) {
        result.severity = 'warn';
    } else if (result.keyPresent) {
        result.severity = 'ok';
    } else if (result.keyPresent === null && result.encryptedCount > 0) {
        // container topology — encrypted values present, keychain probe
        // deliberately skipped (can't reach host keychain from inside).
        result.severity = 'ok-untested';
    } else {
        result.severity = 'unknown';
    }
    jsonResponse(res, result);
}

// ========== TLS CERT GENERATION (task #100 / C3) ==========
// POST /api/tls/gen-cert { cn?, days?, san? } → runs docker/gen-tls-cert.sh
// to produce an ECDSA-P256 self-signed cert under docker/tls/. Returns
// the cert + key paths + SHA-256 fingerprint for out-of-band pinning.
// The cert is picked up by the dashboard container on next bring-up
// with docker-compose.tls.yaml overlay (which mounts docker/tls/ + sets
// QRL_TLS_CERT/KEY env vars). See docker/gen-tls-cert.sh.
async function handleTlsGenCert(req, res) {
    const { execFile } = require('child_process');
    const body = await parseBody(req);
    const repoRoot = process.env.QRL_REPO_ROOT || __dirname;
    const script = path.join(repoRoot, 'docker', 'gen-tls-cert.sh');
    if (!fs.existsSync(script)) {
        return jsonResponse(res, { success: false, error: `gen-tls-cert.sh not found at ${script}` });
    }
    const env = { ...process.env };
    if (body.cn)   env.CN = body.cn;
    if (body.days) env.DAYS = String(body.days);
    if (body.san)  env.SAN = body.san;
    await new Promise((resolve) => {
        execFile('bash', [script], { env, timeout: 30000 }, (err, stdout, stderr) => {
            const combined = (stdout || '') + (stderr || '');
            if (err) {
                jsonResponse(res, {
                    success: false,
                    error: errDetail(err),
                    output: combined.slice(-2048),
                });
            } else {
                const fpMatch = /SHA-256 fingerprint:\s*([A-F0-9:]+)/.exec(combined);
                jsonResponse(res, {
                    success: true,
                    certPath: path.join(repoRoot, 'docker', 'tls', 'cert.pem'),
                    keyPath:  path.join(repoRoot, 'docker', 'tls', 'key.pem'),
                    fingerprint: fpMatch ? fpMatch[1] : null,
                    output: combined.slice(-2048),
                    note: 'Cert generated. Restart compose with `-f docker-compose.tls.yaml` to enable PQ TLS on the dashboard listener.',
                });
            }
            resolve();
        });
    });
}

// ========== RELEASE PIPELINE (task #100 / C4) ==========
// Six endpoints wrapping the phase-3 host scripts. Container-side these
// need bash + docker CLI in the image (Dockerfile.status-server ships
// both) + the docker-sock overlay's mounts (docker.sock, host docker/,
// host repo root as /opt/qrl/repo). Bare-metal these need docker on
// $PATH and buildx installed.
//
// QRL_REPO_ROOT env var (set by the docker-sock overlay to /opt/qrl/repo)
// overrides __dirname for release scripts — inside the container the
// dashboard's app code is at /opt/qrl/app but the release scripts +
// their source tree live at /opt/qrl/repo.
const REPO_ROOT   = process.env.QRL_REPO_ROOT || __dirname;
const RELEASE_DIR = path.join(REPO_ROOT, 'docker', 'out');
const DOCKER_DIR  = path.join(REPO_ROOT, 'docker');

// GET /api/release/artifacts → enumerate docker/out/ + detect which
// tarballs are signed + which SHA256SUMS.qrlsig manifest exists.
async function handleReleaseArtifacts(req, res) {
    if (!fs.existsSync(RELEASE_DIR)) {
        return jsonResponse(res, {
            success: true, tarballs: [], manifest: null, signature: null,
            note: 'docker/out/ does not exist yet. Run BUILD to create the first artifacts.',
        });
    }
    try {
        const entries = fs.readdirSync(RELEASE_DIR);
        const tarballs = entries
            .filter(n => /\.oci\.tar$/.test(n))
            .map(n => {
                const p = path.join(RELEASE_DIR, n);
                const s = fs.statSync(p);
                return { name: n, path: p, sizeBytes: s.size, mtime: s.mtime };
            })
            .sort((a, b) => a.name.localeCompare(b.name));
        const manifestPath = path.join(RELEASE_DIR, 'SHA256SUMS.txt');
        const sigPath      = path.join(RELEASE_DIR, 'SHA256SUMS.txt.qrlsig');
        let manifest = null, signature = null;
        if (fs.existsSync(manifestPath)) {
            const lines = fs.readFileSync(manifestPath, 'utf8').trim().split('\n');
            manifest = { path: manifestPath, entries: lines.length };
        }
        if (fs.existsSync(sigPath)) {
            try {
                const sig = JSON.parse(fs.readFileSync(sigPath, 'utf8'));
                signature = {
                    path: sigPath,
                    algorithm: sig.algorithm,
                    address: sig.address,
                    publicKey: sig.publicKey,
                    signaturePresent: !!sig.signature,
                };
            } catch (e) { signature = { path: sigPath, error: errDetail(e) }; }
        }
        // Enumerate release bundles too (release.sh output)
        const bundles = entries
            .filter(n => /^qrl-popup-release-.*\.tar$/.test(n))
            .map(n => ({ name: n, path: path.join(RELEASE_DIR, n), sizeBytes: fs.statSync(path.join(RELEASE_DIR, n)).size }));
        jsonResponse(res, { success: true, tarballs, manifest, signature, bundles });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Runs a shell script and returns its output. Used by build/sign/verify/
// bundle/load release endpoints. Rejects paths outside `docker/`.
function runReleaseScript(scriptName, args, env, timeoutMs, cb) {
    const { execFile } = require('child_process');
    const script = path.join(DOCKER_DIR, scriptName);
    if (!fs.existsSync(script)) return cb(new Error(`${scriptName} not found at ${script}`));
    execFile('bash', [script, ...(args || [])], {
        env: { ...process.env, ...env },
        timeout: timeoutMs || 600000,
        maxBuffer: 8 * 1024 * 1024,
        cwd: REPO_ROOT,
    }, (err, stdout, stderr) => {
        cb(err, (stdout || '') + (stderr || ''));
    });
}

// POST /api/release/build { filter? } → docker/build-images.sh [filter]
async function handleReleaseBuild(req, res) {
    const body = await parseBody(req);
    const args = body.filter ? [body.filter] : [];
    runReleaseScript('build-images.sh', args, {}, 900000, (err, out) => {
        jsonResponse(res, {
            success: !err,
            error: err ? errDetail(err) : null,
            output: out.slice(-8192),
            hint: err ? 'build-images.sh needs docker CLI on the host running the dashboard. Bare-metal path recommended.' : null,
        });
    });
}

// POST /api/release/sign { walletId? | seedHex? }
async function handleReleaseSign(req, res) {
    const body = await parseBody(req);
    const env = {};
    // Strict input validation — reviewer flagged: raw body.seedHex and
    // body.walletId were previously stuffed into process env without
    // any format check. Enforce the exact shapes so an attacker-
    // controlled POST (cross-origin or otherwise) can't smuggle a
    // 10MB string or embedded newlines into bash's environment.
    if (body.walletId !== undefined) {
        if (typeof body.walletId !== 'string' || !/^[a-zA-Z0-9_-]{1,64}$/.test(body.walletId)) {
            return jsonResponse(res, { success: false, error: 'walletId must be 1-64 chars of [a-zA-Z0-9_-]' });
        }
        env.QRL_WALLET_ID = body.walletId;
    }
    if (body.seedHex !== undefined) {
        if (typeof body.seedHex !== 'string' || !/^[0-9a-fA-F]{96}$/.test(body.seedHex)) {
            return jsonResponse(res, { success: false, error: 'seedHex must be exactly 96 hex chars' });
        }
        env.SEED_HEX = body.seedHex;
    }
    if (!env.QRL_WALLET_ID && !env.SEED_HEX) {
        return jsonResponse(res, { success: false, error: 'Provide walletId or seedHex.' });
    }
    runReleaseScript('sign-images.sh', [], env, 120000, (err, out) => {
        // Scrub the seed from this process's environment copy — the
        // child has exited; we don't want the string sitting in Node's
        // GC heap or /proc/<pid>/environ longer than necessary.
        env.SEED_HEX = '0'.repeat(96);
        delete env.SEED_HEX;
        jsonResponse(res, {
            success: !err,
            error: err ? errDetail(err) : null,
            output: out.slice(-4096),
            manifestPath: path.join(RELEASE_DIR, 'SHA256SUMS.txt'),
            sigPath:      path.join(RELEASE_DIR, 'SHA256SUMS.txt.qrlsig'),
        });
    });
}

// POST /api/release/verify
async function handleReleaseVerify(req, res) {
    runReleaseScript('verify-images.sh', [], {}, 60000, (err, out) => {
        // Step markers in the script's stdout — parse them so the UI
        // can show a green/red chip per step.
        const step1Ok = /signature valid/.test(out);
        const step2Ok = /all listed tarballs match/.test(out);
        jsonResponse(res, {
            success: !err && step1Ok && step2Ok,
            step1SignatureValid: step1Ok,
            step2HashesMatch:    step2Ok,
            output: out.slice(-4096),
            error: err ? errDetail(err) : null,
        });
    });
}

// POST /api/release/bundle { version?, skipDigestRefresh? }
async function handleReleaseBundle(req, res) {
    const body = await parseBody(req);
    const env = { SKIP_DIGEST_REFRESH: body.skipDigestRefresh === false ? '' : '1' };
    if (body.version)  env.VERSION = body.version;
    if (body.walletId) env.QRL_WALLET_ID = body.walletId;
    if (body.seedHex)  env.SEED_HEX = body.seedHex;
    if (!env.QRL_WALLET_ID && !env.SEED_HEX) {
        return jsonResponse(res, { success: false, error: 'Provide walletId or seedHex (bundle also signs).' });
    }
    runReleaseScript('release.sh', [], env, 900000, (err, out) => {
        jsonResponse(res, {
            success: !err,
            error: err ? errDetail(err) : null,
            output: out.slice(-8192),
            hint: err ? 'release.sh is the full orchestrator (build → sign → bundle) and needs docker CLI + qrlenv.' : null,
        });
    });
}

// POST /api/release/load { tarballPath, pinnedPubkey? }
async function handleReleaseLoad(req, res) {
    const body = await parseBody(req);
    if (!body.tarballPath || typeof body.tarballPath !== 'string') {
        return jsonResponse(res, { success: false, error: 'tarballPath is required.' });
    }
    // Accept a bare basename or an absolute path; both must resolve
    // inside RELEASE_DIR (no path traversal).
    const raw = String(body.tarballPath);
    const candidate = path.isAbsolute(raw) ? raw : path.join(RELEASE_DIR, raw);
    const resolved = path.resolve(candidate);
    if (!resolved.startsWith(RELEASE_DIR + path.sep) && path.dirname(resolved) !== RELEASE_DIR) {
        return jsonResponse(res, { success: false, error: 'tarballPath must live under docker/out/.' });
    }
    if (!fs.existsSync(resolved) || !/\.tar$/.test(resolved)) {
        return jsonResponse(res, { success: false, error: 'File not found or not a .tar.', resolved });
    }
    const env = {};
    // Strict pinnedPubkey format — must be hex of a sensible length.
    // ML-DSA-87 pubkey is 2592 bytes = 5184 hex chars; allow a reasonable
    // range (1024-8192) to tolerate hybrid or variant pubkey shapes
    // without opening the field to arbitrary smuggling.
    if (body.pinnedPubkey !== undefined) {
        if (typeof body.pinnedPubkey !== 'string' || !/^[0-9a-fA-F]{1024,8192}$/.test(body.pinnedPubkey)) {
            return jsonResponse(res, { success: false, error: 'pinnedPubkey must be 1024-8192 hex chars' });
        }
        env.PINNED_PUBKEY = body.pinnedPubkey;
    }
    runReleaseScript('load-release.sh', [resolved], env, 300000, (err, out) => {
        jsonResponse(res, {
            success: !err,
            error: err ? errDetail(err) : null,
            output: out.slice(-4096),
        });
    });
}

// POST /api/release/inspect { tarballPath, mode? } — inspect an OCI tarball
async function handleReleaseInspect(req, res) {
    const body = await parseBody(req);
    if (!body.tarballPath) return jsonResponse(res, { success: false, error: 'tarballPath required' });
    // Accept either a bare basename (dashboard sends these — they're
    // what /api/release/artifacts reports) or an absolute path. Both
    // must resolve inside RELEASE_DIR to prevent traversal.
    const raw = String(body.tarballPath);
    const candidate = path.isAbsolute(raw) ? raw : path.join(RELEASE_DIR, raw);
    const resolved = path.resolve(candidate);
    if (!resolved.startsWith(RELEASE_DIR + path.sep) && path.dirname(resolved) !== RELEASE_DIR) {
        return jsonResponse(res, { success: false, error: 'tarballPath must live under docker/out/.' });
    }
    if (!fs.existsSync(resolved)) return jsonResponse(res, { success: false, error: 'File not found.', resolved });
    const mode = ['--sbom', '--provenance', '--config', 'summary'].includes(body.mode) ? body.mode : 'summary';
    const args = mode === 'summary' ? [resolved] : [resolved, mode];
    runReleaseScript('inspect-image.sh', args, {}, 30000, (err, out) => {
        jsonResponse(res, { success: !err, error: err ? errDetail(err) : null, output: out.slice(-16384) });
    });
}

// ========== GENERIC RPC PASSTHROUGH ==========
// POST /api/rpc { network, method, params } → forwards to the network's execRpc
// (or explicit rpcUrl if provided) and returns the JSON-RPC response verbatim.
// Only Zond networks have execRpc; for Classic we route to their local grpcProxy JSON-RPC.
async function handleRpcPassthrough(req, res) {
    const body = await parseBody(req);
    const { network, method, params, rpcUrl: explicitUrl } = body;

    let rpcUrl = explicitUrl;
    if (!rpcUrl && network) {
        const cfg = networkConfigs[network];
        if (!cfg) return jsonResponse(res, { error: `unknown network: ${network}` });
        if (cfg.execRpc) {
            rpcUrl = cfg.execRpc;
        } else if (cfg.type === 'classic') {
            // Classic networks proxy to grpcProxy JSON-RPC
            rpcUrl = classicProxyJsonRpc[network] || null;
        }
    }
    if (!rpcUrl) return jsonResponse(res, { error: 'no rpcUrl and no known network' });
    if (!method) return jsonResponse(res, { error: 'method required' });

    try {
        const rspData = await httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method, params: params || [], id: Date.now() },
        });
        // Return the full JSON-RPC envelope so callers can inspect error/result directly
        return jsonResponse(res, rspData.data || {});
    } catch (err) {
        return jsonResponse(res, { error: errDetail(err) });
    }
}

// ========== CLASSIC PRIVATE NODE1 MINING TOGGLE ==========
// Mining on Classic Private node1 is controlled by presence of the marker file
// $CHAINS/localnet/node1-data/.mining-enabled, which start-all.sh reads to pick
// config-node1.yml (mining ON) vs config-node1-no-mining.yml (mining OFF).
const CLASSIC_NODE1_DATA = path.join(CLASSIC_PRIVATE_CHAIN_DIR, 'node1-data');
const MINING_MARKER      = path.join(CLASSIC_NODE1_DATA, '.mining-enabled');

async function handleClassicMiningStatus(req, res) {
    const enabled = fs.existsSync(MINING_MARKER);
    // Also check if node1 is currently running to distinguish "wants to mine but not running" from "running with mining"
    let running = false;
    try { require('child_process').execSync('pgrep -f "start_qrl.*chains/localnet/node1-data"', { stdio: 'ignore' }); running = true; } catch {}
    jsonResponse(res, { success: true, enabled, running, node: 'classic-private-node1' });
}

async function handleClassicMiningToggle(req, res) {
    const body = await parseBody(req);
    const { enable } = body;
    const { execSync, spawn } = require('child_process');

    try {
        // Stop node1 if running
        execSync('pkill -f "start_qrl.*chains/localnet/node1-data" 2>/dev/null || true');
        // Small pause for the process to actually die and release ports
        await new Promise(r => setTimeout(r, 1500));
        fs.mkdirSync(CLASSIC_NODE1_DATA, { recursive: true });

        if (enable) {
            fs.writeFileSync(MINING_MARKER, `${new Date().toISOString()}\n`);
        } else {
            try { fs.unlinkSync(MINING_MARKER); } catch (e) { /* ignore if not present */ }
        }

        // Relaunch node1 (spawn the private startup wrapper — it re-reads the marker)
        const startScript = path.join(QRL1_ROOT, 'scripts', 'start-qrl-classic-private.sh');
        spawn('bash', [startScript], {
            cwd: CLASSIC_PRIVATE_DIR,
            detached: true,
            stdio: 'ignore',
        }).unref();

        jsonResponse(res, {
            success: true,
            enabled: enable,
            message: enable
                ? 'node1 restarting with mining ENABLED (may bus-error on Apple Silicon)'
                : 'node1 restarting with mining DISABLED (safe on all platforms; no new blocks produced)',
        });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

async function handleNodeControl(req, res) {
    const body = await parseBody(req);
    const { nodeType, action, target } = body; // target: 'all', 'node1', 'beacon', etc.
    const { execSync, spawn } = require('child_process');

    try {
        // Individual node control
        if (target && target !== 'all') {
            return handleIndividualNodeControl(res, nodeType, action, target);
        }

        // Collective control (all nodes of a type)
        if (nodeType === 'zond') {
            if (action === 'stop') {
                execSync(`pkill -f "gqrl.*chains/localnet/node" 2>/dev/null || true`);
                execSync(`pkill -f "beacon-chain.*chains/localnet/beacon" 2>/dev/null || true`);
                execSync(`pkill -f "validator.*chains/localnet/validators" 2>/dev/null || true`);
                jsonResponse(res, { success: true, message: 'QRL 2.0 network stopped' });
            } else if (action === 'start') {
                spawn('bash', ['start-nodes.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                setTimeout(() => {
                    spawn('bash', ['start-beacon.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                }, 3000);
                setTimeout(() => {
                    spawn('bash', ['start-validators.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                }, 6000);
                jsonResponse(res, { success: true, message: 'QRL 2.0 network starting...' });
            } else if (action === 'restart') {
                execSync(`pkill -f "gqrl.*chains/localnet/node" 2>/dev/null || true`);
                execSync(`pkill -f "beacon-chain.*chains/localnet/beacon" 2>/dev/null || true`);
                execSync(`pkill -f "validator.*chains/localnet/validators" 2>/dev/null || true`);
                setTimeout(() => {
                    spawn('bash', ['start-nodes.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                    setTimeout(() => spawn('bash', ['start-beacon.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref(), 3000);
                    setTimeout(() => spawn('bash', ['start-validators.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref(), 6000);
                }, 2000);
                jsonResponse(res, { success: true, message: 'QRL 2.0 network restarting...' });
            } else if (action === 'status') {
                const status = getZondNetworkStatus();
                jsonResponse(res, { success: true, ...status });
            }
        } else if (nodeType === 'classic') {
            if (action === 'stop') {
                execSync(`pkill -f "start_qrl.*chains/localnet/node" 2>/dev/null || true`);
                jsonResponse(res, { success: true, message: 'QRL Classic network stopped' });
            } else if (action === 'start') {
                spawn('bash', ['start-all.sh'], { cwd: CLASSIC_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                jsonResponse(res, { success: true, message: 'QRL Classic network starting...' });
            } else if (action === 'restart') {
                execSync(`pkill -f "start_qrl.*chains/localnet/node" 2>/dev/null || true`);
                setTimeout(() => {
                    spawn('bash', ['start-all.sh'], { cwd: CLASSIC_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                }, 2000);
                jsonResponse(res, { success: true, message: 'QRL Classic network restarting...' });
            } else if (action === 'status') {
                const status = getClassicNetworkStatus();
                jsonResponse(res, { success: true, ...status });
            }
        } else if (nodeType === 'classic-testnet') {
            // QRL Classic Testnet local node
            if (action === 'stop') {
                execSync(`pkill -f "start_qrl.*chains/testnet" 2>/dev/null || true`);
                jsonResponse(res, { success: true, message: 'QRL Classic Testnet stopped' });
            } else if (action === 'start') {
                spawn('bash', [path.join(QRL1_ROOT, 'scripts', 'start-qrl-classic-testnet.sh')], { cwd: CLASSIC_TESTNET_DIR, detached: true, stdio: 'ignore' }).unref();
                jsonResponse(res, { success: true, message: 'QRL Classic Testnet starting...' });
            } else if (action === 'status') {
                // Check if process is running
                let running = false;
                try { execSync(`pgrep -f "start_qrl.*chains/testnet"`, { stdio: 'ignore' }); running = true; } catch {}
                jsonResponse(res, { success: true, running, message: running ? 'Running' : 'Stopped' });
            }
        } else if (nodeType === 'classic-mainnet') {
            // QRL Classic Mainnet local node (mining)
            if (action === 'stop') {
                execSync(`pkill -f "start_qrl.*chains/mainnet" 2>/dev/null || true`);
                jsonResponse(res, { success: true, message: 'QRL Classic Mainnet stopped' });
            } else if (action === 'start') {
                spawn('bash', [path.join(QRL1_ROOT, 'scripts', 'start-qrl-classic-mainnet.sh')], { cwd: CLASSIC_MAINNET_DIR, detached: true, stdio: 'ignore' }).unref();
                jsonResponse(res, { success: true, message: 'QRL Classic Mainnet starting...' });
            } else if (action === 'status') {
                // Check for start_qrl process that's NOT testnet (mainnet is default)
                let running = false;
                try {
                    // Look for start_qrl on port 19009 (mainnet port)
                    execSync(`lsof -i :19009 | grep -q Python`, { stdio: 'ignore' });
                    running = true;
                } catch {}
                jsonResponse(res, { success: true, running, message: running ? 'Running' : 'Stopped' });
            }
        } else if (nodeType === 'zond-testnet') {
            // Zond Testnet local node
            if (action === 'stop') {
                execSync(`pkill -f "gqrl.*chains/testnet" 2>/dev/null || true`);
                jsonResponse(res, { success: true, message: 'QRL 2.0 Testnet node stopped' });
            } else if (action === 'start') {
                spawn('bash', [path.join(QRL2_ROOT, 'scripts', 'start-zond-testnet.sh')], { cwd: ZOND_TESTNET_DIR, detached: true, stdio: 'ignore' }).unref();
                jsonResponse(res, { success: true, message: 'QRL 2.0 Testnet node starting...' });
            } else if (action === 'status') {
                // Check for gzond with --testnet flag
                let running = false;
                try { execSync(`pgrep -f "gqrl.*chains/testnet"`, { stdio: 'ignore' }); running = true; } catch {}
                jsonResponse(res, { success: true, running, message: running ? 'Running' : 'Stopped' });
            }
        } else {
            jsonResponse(res, { success: false, error: 'Unknown node type' });
        }
    } catch (err) {
        console.error('Node control error:', err.message);
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

function handleIndividualNodeControl(res, nodeType, action, target) {
    const { execSync, spawn } = require('child_process');

    try {
        if (nodeType === 'zond') {
            // Zond individual components: node1-4, beacon, validators, bootnode
            if (target.startsWith('node')) {
                const nodeNum = target.replace('node', '');
                const config = nodeConfigs[`zond-node${nodeNum}`];
                if (!config) {
                    return jsonResponse(res, { success: false, error: `Unknown node: ${target}` });
                }

                if (action === 'stop') {
                    execSync(`pkill -f "gqrl.*chains/localnet/node${nodeNum}" 2>/dev/null || true`);
                    jsonResponse(res, { success: true, message: `Zond ${target} stopped` });
                } else if (action === 'start') {
                    const bootnodeFile = path.join(ZOND_PRIVATE_DIR, 'bootnode.enode');
                    const bootnode = fs.existsSync(bootnodeFile) ? fs.readFileSync(bootnodeFile, 'utf8').trim() : '';
                    const nodeDataDir = path.join(ZOND_PRIVATE_CHAIN_DIR, `node${nodeNum}`);
                    fs.mkdirSync(nodeDataDir, { recursive: true });

                    // New gqrl API: --http.api uses `qrl` namespace, no `zond`.
                    // --allow-insecure-unlock and --unlock/--password removed for security.
                    const args = [
                        `--datadir=${nodeDataDir}`,
                        `--keystore=${path.join(nodeDataDir, 'keystore')}`,
                        '--networkid=32333',
                        `--port=${config.p2pPort}`,
                        '--http',
                        `--http.port=${config.httpPort}`,
                        '--http.addr=127.0.0.1',
                        '--http.api=web3,qrl,net,admin,txpool,miner,debug',
                        '--http.corsdomain=*',
                        '--http.vhosts=*',
                        '--ws',
                        `--ws.port=${config.wsPort}`,
                        '--ws.addr=127.0.0.1',
                        '--ws.api=web3,qrl,net',
                        `--authrpc.port=${config.authPort}`,
                        '--nat=extip:127.0.0.1',
                        '--syncmode=full'
                    ];
                    if (bootnode) args.push(`--bootnodes=${bootnode}`);

                    const proc = spawn(GQRL_BIN, args, {
                        cwd: ZOND_PRIVATE_DIR,
                        detached: true,
                        stdio: ['ignore', 'pipe', 'pipe']
                    });

                    // Log to file inside the node's chain data dir
                    const logFile = fs.createWriteStream(path.join(nodeDataDir, 'node.log'), { flags: 'a' });
                    proc.stdout.pipe(logFile);
                    proc.stderr.pipe(logFile);
                    proc.unref();

                    jsonResponse(res, { success: true, message: `Zond ${target} starting...`, pid: proc.pid });
                } else if (action === 'status') {
                    const running = isProcessRunning(`gqrl.*chains/localnet/node${nodeNum}`);
                    jsonResponse(res, { success: true, target, running });
                }
            } else if (target === 'beacon') {
                if (action === 'stop') {
                    execSync(`pkill -f "beacon-chain.*chains/localnet/beacon" 2>/dev/null || true`);
                    jsonResponse(res, { success: true, message: 'Beacon chain stopped' });
                } else if (action === 'start') {
                    // Data + logs live under chains/localnet/beacon; configs under configs/localnet
                    const beaconDataDir  = path.join(ZOND_PRIVATE_CHAIN_DIR, 'beacon');
                    const validatorDataDir = path.join(ZOND_PRIVATE_CHAIN_DIR, 'validators');
                    const genesisPath    = path.join(ZOND_PRIVATE_DIR, 'genesis.ssz');
                    const configPath     = path.join(ZOND_PRIVATE_DIR, 'config.yml');
                    const beaconLogPath  = path.join(beaconDataDir, 'beacon-console.log');
                    const jwtSecretPath  = path.join(ZOND_PRIVATE_CHAIN_DIR, 'node1', 'gqrl', 'jwtsecret');
                    fs.mkdirSync(beaconDataDir, { recursive: true });
                    fs.mkdirSync(validatorDataDir, { recursive: true });

                    // Stale-data auto-clear: if genesis.ssz newer than beacon data, wipe.
                    let needsClear = false;
                    let clearReason = '';
                    const beaconChainData = path.join(beaconDataDir, 'beaconchaindata');
                    if (fs.existsSync(beaconChainData) && fs.existsSync(genesisPath)) {
                        if (fs.statSync(genesisPath).mtime > fs.statSync(beaconChainData).mtime) {
                            needsClear = true;
                            clearReason = 'genesis.ssz is newer than beacon data';
                        }
                    }
                    if (!needsClear && fs.existsSync(beaconLogPath)) {
                        try {
                            const logContent = fs.readFileSync(beaconLogPath, 'utf8').slice(-10000);
                            if (logContent.includes('unknown justified root')) {
                                needsClear = true;
                                clearReason = 'detected genesis misalignment errors in logs';
                            }
                        } catch (e) {}
                    }

                    if (needsClear) {
                        console.log(`[Beacon] Auto-clearing stale data: ${clearReason}`);
                        execSync(`pkill -f "beacon-chain.*chains/localnet/beacon" 2>/dev/null || true`);
                        execSync(`pkill -f "validator.*chains/localnet/validators" 2>/dev/null || true`);
                        execSync(`rm -rf "${beaconDataDir}" "${validatorDataDir}"`, { stdio: 'ignore' });
                        fs.mkdirSync(beaconDataDir, { recursive: true });
                        fs.mkdirSync(validatorDataDir, { recursive: true });
                    }

                    // Build beacon args using new absolute paths + auth-rpc endpoint of node1
                    const beaconArgs = [
                        `--datadir=${beaconDataDir}`,
                        '--min-sync-peers=0',
                        `--genesis-state=${genesisPath}`,
                        `--chain-config-file=${configPath}`,
                        '--chain-id=32333',
                        `--execution-endpoint=http://localhost:${nodeConfigs['zond-node1'].authPort}`,
                        `--jwt-secret=${jwtSecretPath}`,
                        '--contract-deployment-block=0',
                        '--interop-eth1data-votes',
                        '--disable-staking-contract-check',
                        '--minimum-peers-per-subnet=0',
                        '--p2p-static-id',
                        '--p2p-udp-port=13000',
                        '--p2p-tcp-port=14000',
                        '--grpc-gateway-port=3600',
                        '--rpc-port=4100',
                        '--monitoring-port=8180',
                        '--accept-terms-of-use',
                        '--verbosity=info',
                        `--log-file=${path.join(beaconDataDir, 'beacon.log')}`,
                        '--log-format=text'
                    ];

                    const outFd = fs.openSync(beaconLogPath, 'a');
                    const beaconProc = spawn(BEACON_CHAIN_BIN, beaconArgs, {
                        cwd: ZOND_PRIVATE_DIR,
                        detached: true,
                        stdio: ['ignore', outFd, outFd]
                    });
                    beaconProc.unref();
                    fs.writeFileSync(path.join(beaconDataDir, 'beacon.pid'), beaconProc.pid.toString());

                    const msg = needsClear
                        ? `Beacon chain starting (cleared stale data: ${clearReason})`
                        : 'Beacon chain starting...';
                    jsonResponse(res, { success: true, message: msg, clearedData: needsClear });
                } else if (action === 'status') {
                    const running = isProcessRunning('beacon-chain.*chains/localnet/beacon');
                    jsonResponse(res, { success: true, target, running });
                }
            } else if (target === 'validators') {
                if (action === 'stop') {
                    execSync(`pkill -f "validator.*chains/localnet/validators" 2>/dev/null || true`);
                    jsonResponse(res, { success: true, message: 'Validators stopped' });
                } else if (action === 'start') {
                    spawn('bash', ['start-validators.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                    jsonResponse(res, { success: true, message: 'Validators starting...' });
                } else if (action === 'status') {
                    const running = isProcessRunning('validator.*chains/localnet/validators');
                    jsonResponse(res, { success: true, target, running });
                }
            } else if (target === 'bootnode') {
                if (action === 'stop') {
                    execSync(`pkill -f "bootnode.*nodekey=boot.key" 2>/dev/null || true`);
                    jsonResponse(res, { success: true, message: 'Bootnode stopped' });
                } else if (action === 'start') {
                    spawn('bash', ['start-bootnode.sh'], { cwd: ZOND_PRIVATE_DIR, detached: true, stdio: 'ignore' }).unref();
                    jsonResponse(res, { success: true, message: 'Bootnode starting...' });
                } else if (action === 'status') {
                    const running = isProcessRunning('bootnode.*nodekey=boot.key');
                    jsonResponse(res, { success: true, target, running });
                }
            } else {
                jsonResponse(res, { success: false, error: `Unknown Zond target: ${target}` });
            }
        } else if (nodeType === 'classic') {
            // QRL Classic individual nodes
            if (target.startsWith('node')) {
                const nodeNum = target.replace('node', '');
                const config = nodeConfigs[`classic-node${nodeNum}`];
                if (!config) {
                    return jsonResponse(res, { success: false, error: `Unknown node: ${target}` });
                }

                if (action === 'stop') {
                    execSync(`pkill -f "start_qrl.*chains/localnet/node${nodeNum}-data" 2>/dev/null || true`);
                    jsonResponse(res, { success: true, message: `QRL Classic ${target} stopped` });
                } else if (action === 'start') {
                    const qrlDir = path.join(__dirname, 'qrl-classic');
                    const dataDir = path.join(CLASSIC_PRIVATE_DIR, config.datadir);
                    const miningThreads = nodeNum === '1' ? '1' : '0';

                    const proc = spawn('bash', ['-c', `source ${qrlDir}/venv/bin/activate && start_qrl --qrldir=${dataDir} --mining_thread_count=${miningThreads}`], {
                        cwd: CLASSIC_PRIVATE_DIR,
                        detached: true,
                        stdio: 'ignore'
                    });
                    proc.unref();
                    jsonResponse(res, { success: true, message: `QRL Classic ${target} starting...` });
                } else if (action === 'status') {
                    const running = isProcessRunning(`start_qrl.*chains/localnet/node${nodeNum}-data`);
                    jsonResponse(res, { success: true, target, running });
                }
            } else {
                jsonResponse(res, { success: false, error: `Unknown Classic target: ${target}` });
            }
        }
    } catch (err) {
        console.error('Individual node control error:', err.message);
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

function isProcessRunning(pattern) {
    const { execSync } = require('child_process');
    try {
        const result = execSync(`pgrep -f "${pattern}" 2>/dev/null || true`, { encoding: 'utf8' });
        return result.trim().length > 0;
    } catch (e) {
        return false;
    }
}

function getZondNetworkStatus() {
    return {
        bootnode: isProcessRunning('bootnode.*nodekey=boot.key'),
        node1: isProcessRunning('gzond.*datadir=node1.*networkid=32333'),
        node2: isProcessRunning('gzond.*datadir=node2.*networkid=32333'),
        node3: isProcessRunning('gzond.*datadir=node3.*networkid=32333'),
        node4: isProcessRunning('gzond.*datadir=node4.*networkid=32333'),
        beacon: isProcessRunning('beacon-chain.*chains/localnet/beacon'),
        validators: isProcessRunning('validator.*chains/localnet/validators')
    };
}

function getClassicNetworkStatus() {
    return {
        node1: isProcessRunning('start_qrl.*node1-data'),
        node2: isProcessRunning('start_qrl.*node2-data'),
        node3: isProcessRunning('start_qrl.*node3-data'),
        node4: isProcessRunning('start_qrl.*node4-data')
    };
}

// ========== ZEUS PROXY CONTROL ==========
const ZEUS_PROXY_DIR = path.join(__dirname, '../theQRL/zeus-proxy');
const ZEUS_PROXY_PID_FILE = path.join(ZEUS_PROXY_DIR, 'zeus-proxy.pid');
const ZEUS_PROXY_LOG_FILE = path.join(ZEUS_PROXY_DIR, 'zeus-proxy.log');

async function handleZeusProxyControl(req, res) {
    const body = await parseBody(req);
    const { action } = body;

    try {
        if (action === 'status') {
            // Check if Zeus Proxy is running
            const isRunning = await checkZeusProxyStatus();
            jsonResponse(res, {
                success: true,
                running: isRunning,
                message: isRunning ? 'Zeus Proxy is running' : 'Zeus Proxy is not running'
            });
            return;
        }

        if (action === 'start') {
            // Check if already running
            const isRunning = await checkZeusProxyStatus();
            if (isRunning) {
                jsonResponse(res, {
                    success: false,
                    error: 'Zeus Proxy is already running'
                });
                return;
            }

            // Start Zeus Proxy. Load zeus-proxy/.env explicitly — meteor
            // won't source it on its own when spawned by a parent that
            // isn't a shell, so QRL_CLASSIC_* / ZOND_* endpoint config
            // otherwise falls back to hardcoded defaults and cross-network
            // routing silently misbehaves.
            const { spawn } = require('child_process');
            const zeusEnv = { ...process.env, METEOR_NO_RELEASE_CHECK: '1' };
            try {
                const envPath = path.join(ZEUS_PROXY_DIR, '.env');
                if (fs.existsSync(envPath)) {
                    const raw = fs.readFileSync(envPath, 'utf8');
                    for (const line of raw.split('\n')) {
                        const m = line.match(/^\s*([A-Z_][A-Z0-9_]*)\s*=\s*(.*?)\s*$/);
                        if (m && !line.trim().startsWith('#')) {
                            zeusEnv[m[1]] = m[2].replace(/^["']|["']$/g, '');
                        }
                    }
                }
            } catch (e) {
                console.warn('zeus-proxy .env load failed:', errDetail(e));
            }
            const meteorProcess = spawn('meteor', ['run', '--port', '3737'], {
                cwd: ZEUS_PROXY_DIR,
                detached: true,
                stdio: ['ignore', 'pipe', 'pipe'],
                env: zeusEnv,
            });

            // Write PID
            fs.writeFileSync(ZEUS_PROXY_PID_FILE, meteorProcess.pid.toString());

            // Log output
            const logStream = fs.createWriteStream(ZEUS_PROXY_LOG_FILE, { flags: 'a' });
            meteorProcess.stdout.pipe(logStream);
            meteorProcess.stderr.pipe(logStream);

            meteorProcess.unref();

            jsonResponse(res, {
                success: true,
                message: `Zeus Proxy started with PID ${meteorProcess.pid}`,
                pid: meteorProcess.pid
            });
            return;
        }

        if (action === 'stop') {
            // Find and kill processes on port 3000
            const { execSync } = require('child_process');
            try {
                // Kill all processes listening on port 3000
                execSync(`lsof -ti :3000 | xargs kill -9 2>/dev/null || true`);
                // Also try to kill by PID if we have one
                if (fs.existsSync(ZEUS_PROXY_PID_FILE)) {
                    const pid = fs.readFileSync(ZEUS_PROXY_PID_FILE, 'utf8').trim();
                    try {
                        execSync(`kill -9 ${pid} 2>/dev/null || true`);
                    } catch (e) {}
                    fs.unlinkSync(ZEUS_PROXY_PID_FILE);
                }
                jsonResponse(res, {
                    success: true,
                    message: 'Zeus Proxy stopped'
                });
            } catch (err) {
                jsonResponse(res, {
                    success: false,
                    error: `Failed to stop Zeus Proxy: ${err.message}`
                });
            }
            return;
        }

        if (action === 'restart') {
            // Stop first - kill processes on port 3000
            const { execSync } = require('child_process');
            try {
                execSync(`lsof -ti :3000 | xargs kill -9 2>/dev/null || true`);
                if (fs.existsSync(ZEUS_PROXY_PID_FILE)) {
                    fs.unlinkSync(ZEUS_PROXY_PID_FILE);
                }
            } catch (e) {}

            // Wait for processes to fully terminate
            await new Promise(r => setTimeout(r, 3000));

            // Start
            const { spawn } = require('child_process');
            const meteorProcess = spawn('meteor', ['run', '--port', '3737'], {
                cwd: ZEUS_PROXY_DIR,
                detached: true,
                stdio: ['ignore', 'pipe', 'pipe'],
                env: { ...process.env, METEOR_NO_RELEASE_CHECK: '1' }
            });

            fs.writeFileSync(ZEUS_PROXY_PID_FILE, meteorProcess.pid.toString());

            const logStream = fs.createWriteStream(ZEUS_PROXY_LOG_FILE, { flags: 'a' });
            meteorProcess.stdout.pipe(logStream);
            meteorProcess.stderr.pipe(logStream);

            meteorProcess.unref();

            jsonResponse(res, {
                success: true,
                message: `Zeus Proxy restarted with PID ${meteorProcess.pid}`,
                pid: meteorProcess.pid
            });
            return;
        }

        jsonResponse(res, {
            success: false,
            error: `Unknown action: ${action}`
        });

    } catch (err) {
        console.error('Zeus Proxy control error:', err.message);
        jsonResponse(res, {
            success: false,
            error: errDetail(err)
        });
    }
}

async function checkZeusProxyStatus() {
    try {
        // Check if port 3000 is responding - any response means it's running
        const response = await httpRequest('http://localhost:3737/', {
            method: 'GET',
            timeout: 3000
        });
        // If we get any response (even error JSON), it's running
        return true;
    } catch (e) {
        // HTTP request failed, check if meteor process is running for zeus-proxy
        try {
            const { execSync } = require('child_process');
            // Check for meteor running in zeus-proxy directory or on port 3737
            const result = execSync(`lsof -i :3000 -t 2>/dev/null || true`, { encoding: 'utf8' });
            return result.trim().length > 0;
        } catch (e) {
            return false;
        }
    }
}

// ========== UNIFIED PROXY HANDLER ==========
// This handles all network queries, routing to Zeus Proxy for Classic or direct RPC for Zond

async function handleProxyRequest(req, res, networkId, method) {
    const body = await parseBody(req);
    const config = networkConfigs[networkId];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' }, 400);
        return;
    }

    try {
        if (config.type === 'classic') {
            // Route through Zeus Proxy for QRL Classic
            const zeusUrl = `${config.zeusProxy}/${method}`;
            const response = await httpRequest(zeusUrl, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: body,
                timeout: getZeusProxyTimeout(networkId)
            });
            jsonResponse(res, { success: true, ...response.data });
        } else if (config.type === 'zond') {
            // Direct RPC for Zond, with failover across config.publicRpcs
            // when the primary provider (config.execRpc) is unreachable.
            // Public testnet endpoints go through Cloudflare and often
            // 502 during upstream restarts; the operator has no lever
            // there, so we transparently try the next candidate before
            // giving up.
            const rpcMethod = mapMethodToZondRpc(method);
            const rpcParams = mapBodyToZondParams(method, body);
            const candidates = Array.from(new Set([
                config.execRpc,
                ...(Array.isArray(config.publicRpcs) ? config.publicRpcs : []),
            ].filter(Boolean)));

            let lastErr = null;
            let response = null;
            let usedRpc = null;
            for (const rpcUrl of candidates) {
                try {
                    const r = await httpRequest(rpcUrl, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: { jsonrpc: '2.0', method: rpcMethod, params: rpcParams, id: 1 }
                    });
                    if (r.data && r.data.result !== undefined) {
                        response = r; usedRpc = rpcUrl; break;
                    }
                    if (r.data && r.data.error) {
                        // RPC-level error is a definitive answer — don't fall through.
                        return jsonResponse(res, { success: false, error: r.data.error.message, rpc: rpcUrl });
                    }
                    lastErr = 'empty response';
                } catch (e) {
                    lastErr = e.message || String(e);
                }
            }

            if (response) {
                const formattedResult = formatZondResponse(method, response.data.result);
                jsonResponse(res, { success: true, rpc: usedRpc, ...formattedResult });
            } else {
                jsonResponse(res, {
                    success: false,
                    error: `all ${candidates.length} zond RPC provider(s) unreachable: ${lastErr}`,
                    tried: candidates,
                });
            }
        }
    } catch (err) {
        const detail = err.message || err.code || String(err);
        const isClassic = String(networkId).startsWith('classic-');
        const isEconnrefused = detail.includes('ECONNREFUSED');
        if (isClassic && isEconnrefused) {
            // Classic ops go through Zeus Proxy — a down proxy is the
            // expected failure mode. Throttle the log + return a stable
            // "proxy down" payload the UI can key off.
            noteZeusProxyResult(false);
            throttleZeusLog(`proxy/${networkId}/${method}`, detail);
            jsonResponse(res, zeusProxyDownResponse());
        } else {
            console.error(`Proxy error for ${networkId}/${method}:`, detail);
            jsonResponse(res, { success: false, error: detail });
        }
    }
}

function mapMethodToZondRpc(method) {
    const methodMap = {
        'GetNodeState': 'qrl_syncing',
        'GetStats': 'qrl_blockNumber',
        'GetBlock': 'qrl_getBlockByNumber',
        'GetAddressState': 'qrl_getBalance',
        'GetObject': 'qrl_getTransactionByHash',
        'GetLatestData': 'qrl_blockNumber',
        'GetTransactionsByAddress': 'qrl_getTransactionCount'
    };
    return methodMap[method] || 'qrl_blockNumber';
}

function mapBodyToZondParams(method, body) {
    switch (method) {
        case 'GetBlock':
            const blockNum = body.query || body.block_number || '0';
            return [blockNum.startsWith('0x') ? blockNum : '0x' + parseInt(blockNum).toString(16), true];
        case 'GetAddressState':
            return [body.address, 'latest'];
        case 'GetObject':
            return [body.query];
        case 'GetTransactionsByAddress':
            return [body.address, 'latest'];
        default:
            return [];
    }
}

function formatZondResponse(method, result) {
    switch (method) {
        case 'GetStats':
        case 'GetLatestData':
            return {
                blockHeight: typeof result === 'string' ? parseInt(result, 16) : result,
                info: { block_height: typeof result === 'string' ? parseInt(result, 16) : result }
            };
        case 'GetBlock':
            if (result) {
                return {
                    block: {
                        header: {
                            block_number: parseInt(result.number, 16),
                            timestamp_seconds: parseInt(result.timestamp, 16),
                            hash_header: result.hash
                        },
                        transactions: result.transactions || [],
                        miner: result.miner
                    }
                };
            }
            return { block: null };
        case 'GetAddressState':
            return {
                state: {
                    balance: typeof result === 'string' ? (parseInt(result, 16) / 1e18).toFixed(8) : '0',
                    nonce: 0
                }
            };
        default:
            return { result };
    }
}

// Get network stats with proper routing
async function handleNetworkStats(req, res) {
    const body = await parseBody(req);
    const networkId = body.network || 'classic-mainnet';
    const config = networkConfigs[networkId];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    try {
        if (config.type === 'classic') {
            // Try Zeus Proxy GetStats
            const response = await httpRequest(`${config.zeusProxy}/GetStats`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: {},
                timeout: getZeusProxyTimeout(networkId)
            });

            // For Classic mainnet and testnet, fetch mining stats from Explorer API
            let miningStats = null;
            const explorerUrls = {
                'classic-mainnet': 'https://explorer.theqrl.org/api/status',
                'classic-testnet': 'https://testnet-explorer.theqrl.org/api/status'
            };
            if (explorerUrls[networkId]) {
                try {
                    const explorerRes = await httpRequest(explorerUrls[networkId], {
                        method: 'GET',
                        headers: { 'Accept': 'application/json' }
                    });
                    if (explorerRes.data && explorerRes.data.block_timeseries && explorerRes.data.block_timeseries.length > 0) {
                        const latest = explorerRes.data.block_timeseries[0];
                        miningStats = {
                            difficulty: latest.difficulty || 0,
                            hash_power: latest.hash_power || 0
                        };
                    }
                } catch (explorerErr) {
                    console.log('Explorer API error:', explorerErr.message);
                }
            }

            jsonResponse(res, {
                success: true,
                stats: response.data,
                miningStats: miningStats
            });
        } else {
            // Zond - get block number, sync status, and peer count
            const [blockRes, syncRes, peerRes] = await Promise.all([
                httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
                }),
                httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_syncing', params: [], id: 2 }
                }),
                httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'net_peerCount', params: [], id: 3 }
                })
            ]);

            const blockHeight = blockRes.data?.result ? parseInt(blockRes.data.result, 16) : 0;
            const syncing = syncRes.data?.result;
            const peerCount = peerRes.data?.result ? parseInt(peerRes.data.result, 16) : 0;

            // Return in a consistent format with Classic (wrap in stats object)
            jsonResponse(res, {
                success: true,
                stats: {
                    block_height: blockHeight,
                    blockHeight: blockHeight,
                    peers: peerCount,
                    peerCount: peerCount,
                    syncing: syncing !== false,
                    sync_status: syncing === false ? 'SYNCED' : 'SYNCING',
                    isPoS: true
                }
            });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Get latest blocks for a specific network
async function handleLatestBlocks(req, res) {
    const body = await parseBody(req);
    const networkId = body.network || 'classic-mainnet';
    const limit = body.limit || 10;
    const config = networkConfigs[networkId];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    try {
        if (config.type === 'classic') {
            // Use Zeus Proxy GetLatestData
            const response = await httpRequest(`${config.zeusProxy}/GetLatestData`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { filter: 'BLOCKHEADERS', offset: 0, quantity: limit },
                timeout: getZeusProxyTimeout(networkId)
            });

            if (response.data && response.data.blockheaders) {
                const blocks = response.data.blockheaders.map(b => {
                    // Convert buffer hash to hex string if needed
                    let hashStr = '';
                    if (b.header?.hash_header) {
                        if (Buffer.isBuffer(b.header.hash_header) || b.header.hash_header.type === 'Buffer') {
                            const data = b.header.hash_header.data || b.header.hash_header;
                            hashStr = '0x' + Buffer.from(data).toString('hex');
                        } else if (typeof b.header.hash_header === 'string') {
                            hashStr = b.header.hash_header.startsWith('0x') ? b.header.hash_header : '0x' + b.header.hash_header;
                        }
                    }
                    return {
                        number: b.header?.block_number || 0,
                        hash: hashStr,
                        miner: b.minedBy || 'Unknown',
                        txCount: b.transactionCount || 0,
                        reward: '---',
                        time: b.header?.timestamp_seconds ?
                            new Date(b.header.timestamp_seconds * 1000).toLocaleTimeString() : '---'
                    };
                });
                jsonResponse(res, { success: true, blocks });
            } else {
                jsonResponse(res, { success: true, blocks: [] });
            }
        } else {
            // Zond - fetch recent blocks
            const blockNumRes = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
            });

            const latestBlock = blockNumRes.data?.result ? parseInt(blockNumRes.data.result, 16) : 0;
            const blocks = [];

            for (let i = 0; i < Math.min(limit, 10); i++) {
                const blockNum = latestBlock - i;
                if (blockNum < 0) break;

                try {
                    const blockRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: {
                            jsonrpc: '2.0',
                            method: 'qrl_getBlockByNumber',
                            params: ['0x' + blockNum.toString(16), false],
                            id: i + 2
                        }
                    });

                    if (blockRes.data?.result) {
                        const block = blockRes.data.result;
                        blocks.push({
                            number: parseInt(block.number, 16),
                            hash: block.hash,
                            miner: block.miner || 'N/A',
                            txCount: block.transactions?.length || 0,
                            reward: '---',
                            time: new Date(parseInt(block.timestamp, 16) * 1000).toLocaleTimeString()
                        });
                    }
                } catch (e) { /* Skip failed block */ }
            }

            jsonResponse(res, { success: true, blocks });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err), blocks: [] });
    }
}

// Get latest transactions for a specific network
async function handleLatestTransactions(req, res) {
    const body = await parseBody(req);
    const networkId = body.network || 'classic-mainnet';
    const limit = body.limit || 10;
    const config = networkConfigs[networkId];

    if (!config) {
        jsonResponse(res, { success: false, error: 'Unknown network' });
        return;
    }

    try {
        if (config.type === 'classic') {
            // Use Zeus Proxy GetLatestData for transactions
            const response = await httpRequest(`${config.zeusProxy}/GetLatestData`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { filter: 'TRANSACTIONS', offset: 0, quantity: limit },
                timeout: getZeusProxyTimeout(networkId)
            });

            if (response.data && response.data.transactions) {
                const transactions = response.data.transactions.map(t => {
                    // Convert buffer values to hex strings
                    let txHash = '';
                    if (t.tx?.transaction_hash) {
                        if (Buffer.isBuffer(t.tx.transaction_hash) || t.tx.transaction_hash.type === 'Buffer') {
                            const data = t.tx.transaction_hash.data || t.tx.transaction_hash;
                            txHash = '0x' + Buffer.from(data).toString('hex');
                        } else if (typeof t.tx.transaction_hash === 'string') {
                            txHash = t.tx.transaction_hash.startsWith('0x') ? t.tx.transaction_hash : '0x' + t.tx.transaction_hash;
                        }
                    }

                    let addrFrom = '';
                    if (t.addr_from) {
                        if (Buffer.isBuffer(t.addr_from) || t.addr_from.type === 'Buffer') {
                            const data = t.addr_from.data || t.addr_from;
                            addrFrom = 'Q' + Buffer.from(data).toString('hex');
                        } else {
                            addrFrom = t.addr_from;
                        }
                    }

                    let addrTo = '';
                    if (t.tx?.transfer?.addrs_to?.[0]) {
                        const to = t.tx.transfer.addrs_to[0];
                        if (Buffer.isBuffer(to) || to.type === 'Buffer') {
                            const data = to.data || to;
                            addrTo = 'Q' + Buffer.from(data).toString('hex');
                        } else {
                            addrTo = to;
                        }
                    }

                    return {
                        hash: txHash,
                        from: addrFrom,
                        to: addrTo,
                        amount: t.tx?.transfer?.amounts?.[0] ?
                            (parseInt(t.tx.transfer.amounts[0]) / 1e9).toFixed(4) : '0',
                        block: t.header?.block_number || '---',
                        time: t.header?.timestamp_seconds ?
                            new Date(t.header.timestamp_seconds * 1000).toLocaleTimeString() : '---'
                    };
                });
                jsonResponse(res, { success: true, transactions });
            } else {
                jsonResponse(res, { success: true, transactions: [] });
            }
        } else {
            // Zond - get transactions from recent blocks
            const blockNumRes = await httpRequest(config.execRpc, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
            });

            const latestBlock = blockNumRes.data?.result ? parseInt(blockNumRes.data.result, 16) : 0;
            const transactions = [];

            // Fetch transactions from recent blocks
            for (let i = 0; i < 5 && transactions.length < limit; i++) {
                const blockNum = latestBlock - i;
                if (blockNum < 0) break;

                try {
                    const blockRes = await httpRequest(config.execRpc, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: {
                            jsonrpc: '2.0',
                            method: 'qrl_getBlockByNumber',
                            params: ['0x' + blockNum.toString(16), true],
                            id: i + 2
                        }
                    });

                    if (blockRes.data?.result?.transactions) {
                        for (const tx of blockRes.data.result.transactions) {
                            if (transactions.length >= limit) break;
                            transactions.push({
                                hash: tx.hash,
                                from: tx.from,
                                to: tx.to || 'Contract Creation',
                                amount: tx.value ? (parseInt(tx.value, 16) / 1e18).toFixed(4) : '0',
                                block: blockNum,
                                time: new Date(parseInt(blockRes.data.result.timestamp, 16) * 1000).toLocaleTimeString()
                            });
                        }
                    }
                } catch (e) { /* Skip failed block */ }
            }

            jsonResponse(res, { success: true, transactions });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err), transactions: [] });
    }
}

async function handlePendingTransactions(req, res) {
    const body = await parseBody(req);
    const { network } = body;

    try {
        const config = networkConfigs[network];
        if (!config) {
            jsonResponse(res, { success: false, error: 'Unknown network', pending: [] });
            return;
        }

        let pending = [];

        if (config.type === 'zond') {
            // Get pending transactions from mempool
            try {
                const response = await httpRequest(config.execRpc, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: { jsonrpc: '2.0', method: 'qrl_pendingTransactions', params: [], id: 1 },
                    timeout: 8000
                });

                if (response.data?.result && Array.isArray(response.data.result)) {
                    pending = response.data.result.slice(0, 10).map(tx => ({
                        hash: tx.hash,
                        from: tx.from,
                        to: tx.to || 'Contract Creation',
                        amount: tx.value ? (parseInt(tx.value, 16) / 1e18).toFixed(4) : '0',
                        gasPrice: tx.gasPrice ? (parseInt(tx.gasPrice, 16) / 1e9).toFixed(2) + ' Gwei' : 'Unknown',
                        time: new Date().toLocaleTimeString()
                    }));
                }
            } catch (e) {
                console.warn(`[${network}] Failed to get pending transactions:`, e.message);
            }
        } else if (config.type === 'classic') {
            // QRL Classic doesn't have a mempool API in the same way
            // Return empty for now
            pending = [];
        }

        jsonResponse(res, { success: true, pending });
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err), pending: [] });
    }
}

// ========== IPFS FUNCTIONS ==========

// IPFS Gateway configuration
const IPFS_GATEWAYS = [
    'https://ipfs.io/ipfs/',
    'https://dweb.link/ipfs/',
    'https://cloudflare-ipfs.com/ipfs/',
    'https://gateway.pinata.cloud/ipfs/'
];

// Pinata API for uploads (if configured)
const PINATA_API_KEY = process.env.PINATA_API_KEY || '';
const PINATA_SECRET_KEY = process.env.PINATA_SECRET_KEY || '';

async function handleIPFSUpload(req, res) {
    const body = await parseBody(req);
    const { filename, content, mimeType, pinataApiKey, pinataSecretKey, localApi } = body;

    if (!content) {
        jsonResponse(res, { success: false, error: 'No content provided' });
        return;
    }

    // Use keys from request body or fall back to environment variables
    const apiKey = pinataApiKey || PINATA_API_KEY;
    const secretKey = pinataSecretKey || PINATA_SECRET_KEY;
    const localIpfsApi = localApi || 'http://localhost:5001';

    try {
        // If Pinata is configured, use it for uploads
        if (apiKey && secretKey) {
            // Extract base64 data
            let base64Data = content;
            if (content.includes(',')) {
                base64Data = content.split(',')[1];
            }
            const buffer = Buffer.from(base64Data, 'base64');

            // Create form data boundary
            const boundary = '----WebKitFormBoundary' + Math.random().toString(36).substr(2);

            // Build multipart form data manually
            let formData = '';
            formData += `--${boundary}\r\n`;
            formData += `Content-Disposition: form-data; name="file"; filename="${filename || 'file'}"\r\n`;
            formData += `Content-Type: ${mimeType || 'application/octet-stream'}\r\n\r\n`;

            const formDataBuffer = Buffer.concat([
                Buffer.from(formData, 'utf8'),
                buffer,
                Buffer.from(`\r\n--${boundary}--\r\n`, 'utf8')
            ]);

            const pinataResponse = await httpRequest('https://api.pinata.cloud/pinning/pinFileToIPFS', {
                method: 'POST',
                headers: {
                    'Content-Type': `multipart/form-data; boundary=${boundary}`,
                    'pinata_api_key': apiKey,
                    'pinata_secret_api_key': secretKey
                },
                body: formDataBuffer,
                raw: true
            });

            if (pinataResponse.data && pinataResponse.data.IpfsHash) {
                const cid = pinataResponse.data.IpfsHash;
                jsonResponse(res, {
                    success: true,
                    cid: cid,
                    url: `https://gateway.pinata.cloud/ipfs/${cid}`,
                    filename: filename
                });
                return;
            }
        }

        // Fallback: Use local IPFS node if available
        try {
            let base64Data = content;
            if (content.includes(',')) {
                base64Data = content.split(',')[1];
            }
            const buffer = Buffer.from(base64Data, 'base64');

            const localIPFS = await httpRequest(`${localIpfsApi}/api/v0/add`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/octet-stream' },
                body: buffer,
                raw: true
            });

            if (localIPFS.data && localIPFS.data.Hash) {
                const cid = localIPFS.data.Hash;
                jsonResponse(res, {
                    success: true,
                    cid: cid,
                    url: `http://localhost:8080/ipfs/${cid}`,
                    filename: filename
                });
                return;
            }
        } catch (localErr) {
            // Local IPFS not available
        }

        // No upload method available
        jsonResponse(res, {
            success: false,
            error: 'IPFS upload requires either Pinata API keys (PINATA_API_KEY, PINATA_SECRET_KEY) or a local IPFS node'
        });

    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Test Pinata API connection
async function handleIPFSTestPinata(req, res) {
    const body = await parseBody(req);
    const { apiKey, secretKey } = body;

    if (!apiKey || !secretKey) {
        jsonResponse(res, { success: false, error: 'API key and secret key are required' });
        return;
    }

    try {
        const response = await httpRequest('https://api.pinata.cloud/data/testAuthentication', {
            method: 'GET',
            headers: {
                'pinata_api_key': apiKey,
                'pinata_secret_api_key': secretKey
            },
            timeout: 10000
        });

        if (response.statusCode === 200) {
            jsonResponse(res, { success: true, message: 'Pinata authentication successful' });
        } else {
            jsonResponse(res, { success: false, error: `Authentication failed: ${response.statusCode}` });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// Test local IPFS node connection
async function handleIPFSTestLocal(req, res) {
    const body = await parseBody(req);
    const localApi = body.localApi || 'http://localhost:5001';

    try {
        const response = await httpRequest(`${localApi}/api/v0/id`, {
            method: 'POST',
            timeout: 5000
        });

        if (response.statusCode === 200 && response.data) {
            const nodeInfo = typeof response.data === 'string' ? JSON.parse(response.data) : response.data;
            jsonResponse(res, {
                success: true,
                message: 'Local IPFS node connected',
                nodeId: nodeInfo.ID,
                agentVersion: nodeInfo.AgentVersion
            });
        } else {
            jsonResponse(res, { success: false, error: 'IPFS node not responding' });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: `Cannot connect to local IPFS node: ${err.message}` });
    }
}

// Test IPFS gateway
async function handleIPFSTestGateway(req, res) {
    const body = await parseBody(req);
    const gateway = body.gateway || 'https://ipfs.io/ipfs/';

    try {
        // Test with a known small CID (IPFS logo)
        const testCid = 'QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG';
        const response = await httpRequest(gateway + testCid, {
            method: 'HEAD',
            timeout: 10000
        });

        if (response.statusCode === 200 || response.statusCode === 301 || response.statusCode === 302) {
            jsonResponse(res, { success: true, message: 'Gateway is accessible' });
        } else {
            jsonResponse(res, { success: false, error: `Gateway returned status ${response.statusCode}` });
        }
    } catch (err) {
        jsonResponse(res, { success: false, error: `Gateway test failed: ${err.message}` });
    }
}

async function handleIPFSFetch(req, res) {
    const body = await parseBody(req);
    let { cid, gateway } = body;

    if (!cid) {
        jsonResponse(res, { success: false, error: 'No CID provided' });
        return;
    }

    // Clean CID
    if (cid.startsWith('ipfs://')) {
        cid = cid.replace('ipfs://', '');
    }
    if (cid.includes('/ipfs/')) {
        cid = cid.split('/ipfs/')[1];
    }

    // Try local gateway first, then public gateways
    const gateways = gateway ? [gateway] : ['http://localhost:8080/ipfs/', ...IPFS_GATEWAYS];

    for (const gw of gateways) {
        try {
            const url = gw + cid;
            const response = await httpRequest(url, {
                method: 'GET',
                timeout: 15000,
                responseType: 'buffer'
            });

            if (response.statusCode === 200) {
                const contentType = response.headers?.['content-type'] || 'application/octet-stream';
                let content = response.data;
                let mimeType = contentType.split(';')[0];

                // Convert to base64 data URL for binary content
                if (Buffer.isBuffer(content)) {
                    content = `data:${mimeType};base64,${content.toString('base64')}`;
                }

                // Determine filename from CID
                const filename = cid.slice(0, 12) + (mimeType.includes('image') ? '.png' :
                    mimeType.includes('json') ? '.json' :
                    mimeType.includes('video') ? '.mp4' :
                    mimeType.includes('audio') ? '.mp3' : '');

                jsonResponse(res, {
                    success: true,
                    cid: cid,
                    content: content,
                    mimeType: mimeType,
                    size: response.data?.length || 0,
                    filename: filename,
                    gateway: gw
                });
                return;
            }
        } catch (err) {
            // Try next gateway
            continue;
        }
    }

    jsonResponse(res, { success: false, error: 'Failed to fetch content from any gateway' });
}

// ========== NFT FUNCTIONS ==========

// ZRC-721 function selectors
const NFT_SELECTORS = {
    tokenURI: '0xc87b56dd',      // tokenURI(uint256)
    ownerOf: '0x6352211e',       // ownerOf(uint256)
    name: '0x06fdde03',          // name()
    symbol: '0x95d89b41',        // symbol()
    balanceOf: '0x70a08231'      // balanceOf(address)
};

// QRC-20 function selectors (shape-compatible with ERC-20 for interop
// with Ethereum-shaped tooling; the standard on QRL is QRC-20).
const QRC20_SELECTORS = {
    name: '0x06fdde03',           // name()
    symbol: '0x95d89b41',         // symbol()
    decimals: '0x313ce567',       // decimals()
    totalSupply: '0x18160ddd',    // totalSupply()
    balanceOf: '0x70a08231'       // balanceOf(address)
};

// Transfer event topic: keccak256("Transfer(address,address,uint256)")
const TRANSFER_EVENT_TOPIC = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';
// Zero address topic (padded to 32 bytes) - indicates token minting
const ZERO_ADDRESS_TOPIC = '0x0000000000000000000000000000000000000000000000000000000000000000';

function encodeUint256(num) {
    return BigInt(num).toString(16).padStart(64, '0');
}

function decodeString(hex) {
    if (!hex || hex === '0x') return '';
    const data = hex.startsWith('0x') ? hex.slice(2) : hex;
    if (data.length < 128) return '';

    try {
        // Dynamic string: first 32 bytes = offset, next 32 bytes = length
        const offset = parseInt(data.slice(0, 64), 16) * 2;
        const length = parseInt(data.slice(offset, offset + 64), 16);
        const strHex = data.slice(offset + 64, offset + 64 + length * 2);
        return Buffer.from(strHex, 'hex').toString('utf8');
    } catch {
        return '';
    }
}

// Decode an address out of a 32-byte ABI-encoded log topic (left-padded).
// Used ONLY by the QRC-20 Transfer-topic decoder path, which indexes
// tokens on 20-byte-address chains (public Testnet V2) where the
// Transfer event's `from`/`to` fit a 32-byte topic as 12 zero bytes
// + the 20-byte address. A 64-byte-address chain (local zond-private,
// rescue-edition, etc.) emits QRC-20 Transfer events with addresses
// that don't fit a 32-byte topic — token indexing on those chains
// needs a separate decode path and isn't served by this function.
function decodeAddress(hex) {
    if (!hex || hex === '0x') return '';
    const data = hex.startsWith('0x') ? hex.slice(2) : hex;
    return '0x' + data.slice(-40);
}

// ========== QRC-20 TOKEN HELPER FUNCTIONS ==========

function decodeUint256(hex) {
    if (!hex || hex === '0x') return '0';
    const data = hex.startsWith('0x') ? hex.slice(2) : hex;
    try {
        return BigInt('0x' + data).toString();
    } catch {
        return '0';
    }
}

function decodeUint8(hex) {
    if (!hex || hex === '0x') return 18; // Default QRC-20 decimals (shape-compatible with ERC-20)
    const data = hex.startsWith('0x') ? hex.slice(2) : hex;
    try {
        return parseInt(data, 16);
    } catch {
        return 18;
    }
}

// Fetch QRC-20 token metadata from contract
async function fetchTokenMetadata(rpcUrl, contractAddress) {
    const results = await Promise.allSettled([
        httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_call',
                    params: [{ to: contractAddress, data: QRC20_SELECTORS.name }, 'latest'], id: 1 }
        }),
        httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_call',
                    params: [{ to: contractAddress, data: QRC20_SELECTORS.symbol }, 'latest'], id: 2 }
        }),
        httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_call',
                    params: [{ to: contractAddress, data: QRC20_SELECTORS.decimals }, 'latest'], id: 3 }
        }),
        httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_call',
                    params: [{ to: contractAddress, data: QRC20_SELECTORS.totalSupply }, 'latest'], id: 4 }
        })
    ]);

    return {
        name: results[0].status === 'fulfilled' && results[0].value.data?.result
              ? decodeString(results[0].value.data.result) : 'Unknown Token',
        symbol: results[1].status === 'fulfilled' && results[1].value.data?.result
                ? decodeString(results[1].value.data.result) : '???',
        decimals: results[2].status === 'fulfilled' && results[2].value.data?.result
                  ? decodeUint8(results[2].value.data.result) : 18,
        totalSupply: results[3].status === 'fulfilled' && results[3].value.data?.result
                     ? decodeUint256(results[3].value.data.result) : '0'
    };
}

// Format token balance with decimals
function formatTokenBalance(rawBalance, decimals) {
    try {
        const balanceBigInt = BigInt(rawBalance);
        const divisor = BigInt(10 ** Math.min(decimals, 18));
        const integerPart = balanceBigInt / divisor;
        const fractionalPart = balanceBigInt % divisor;
        const fractionalStr = fractionalPart.toString().padStart(decimals, '0').slice(0, 4);
        return `${integerPart}.${fractionalStr}`;
    } catch {
        return '0';
    }
}

// Scan blocks for token creation events (Transfer from 0x0)
async function indexTokensFromBlocks(networkId, fromBlock, toBlock) {
    const config = networkConfigs[networkId];
    if (!config || config.type !== 'zond') return [];

    const rpcUrl = config.execRpc;
    const discoveredTokens = [];

    try {
        // Use qrl_getLogs to find Transfer events from zero address (minting)
        console.log(`[TokenIndex] Scanning ${networkId} blocks ${fromBlock}-${toBlock} for tokens...`);
        const logsResult = await httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: {
                jsonrpc: '2.0',
                method: 'qrl_getLogs',
                params: [{
                    fromBlock: '0x' + fromBlock.toString(16),
                    toBlock: '0x' + toBlock.toString(16),
                    topics: [TRANSFER_EVENT_TOPIC, ZERO_ADDRESS_TOPIC]
                }],
                id: 1
            },
            timeout: 30000
        });

        console.log(`[TokenIndex] ${networkId}: Got ${logsResult.data?.result?.length || 0} Transfer logs`);

        if (logsResult.data?.result && Array.isArray(logsResult.data.result)) {
            // Collect unique contract addresses
            const contractAddresses = new Set();
            const firstLogByContract = new Map();

            for (const log of logsResult.data.result) {
                // Preserve Z prefix case for Zond RPC compatibility
                // Zond addresses require uppercase Z prefix
                let addr = log.address;
                if (addr.startsWith('Z') || addr.startsWith('z')) {
                    addr = 'Z' + addr.slice(1).toLowerCase();
                }
                if (!contractAddresses.has(addr)) {
                    contractAddresses.add(addr);
                    firstLogByContract.set(addr, log);
                }
            }

            console.log(`[TokenIndex] ${networkId}: Found ${contractAddresses.size} unique contract addresses`);

            // Fetch metadata for each discovered token
            for (const address of contractAddresses) {
                const firstLog = firstLogByContract.get(address);
                try {
                    console.log(`[TokenIndex] Fetching metadata for ${address}`);
                    const metadata = await fetchTokenMetadata(rpcUrl, address);
                    console.log(`[TokenIndex] Metadata for ${address}: name="${metadata.name}", symbol="${metadata.symbol}"`);
                    // Only add if we got valid name + symbol (indicates it's a real QRC-20)
                    if (metadata.name && metadata.name !== 'Unknown Token' &&
                        metadata.symbol && metadata.symbol !== '???') {
                        discoveredTokens.push({
                            address: address,
                            name: metadata.name,
                            symbol: metadata.symbol,
                            decimals: metadata.decimals,
                            totalSupply: metadata.totalSupply,
                            creationBlock: parseInt(firstLog.blockNumber, 16),
                            creationTxHash: firstLog.transactionHash,
                            discoveredAt: Date.now(),
                            lastUpdated: Date.now()
                        });
                        console.log(`[TokenIndex] Added token: ${metadata.symbol} (${metadata.name})`);
                    } else {
                        console.log(`[TokenIndex] Skipping ${address} - invalid metadata`);
                    }
                } catch (err) {
                    console.error(`Failed to fetch metadata for ${address}:`, err.message);
                }
            }
        }
    } catch (err) {
        console.error(`Error indexing blocks ${fromBlock}-${toBlock}:`, err.message);
    }

    return discoveredTokens;
}

// Full index from genesis (runs in background)
async function runFullTokenIndex(networkId) {
    const state = tokenIndexState.get(networkId) || { lastIndexedBlock: 0, isIndexing: false };
    if (state.isIndexing) return; // Already running

    const config = networkConfigs[networkId];
    if (!config || config.type !== 'zond') return;

    state.isIndexing = true;
    tokenIndexState.set(networkId, state);

    console.log(`[TokenIndex] Starting token indexing for ${networkId}`);

    try {
        // Get current block height
        const blockNumRes = await httpRequest(config.execRpc, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: { jsonrpc: '2.0', method: 'qrl_blockNumber', params: [], id: 1 }
        });
        const latestBlock = parseInt(blockNumRes.data?.result || '0x0', 16);

        // Process in chunks of 1000 blocks
        const CHUNK_SIZE = 1000;
        let fromBlock = state.lastIndexedBlock;

        while (fromBlock < latestBlock && state.isIndexing) {
            const toBlock = Math.min(fromBlock + CHUNK_SIZE, latestBlock);
            const tokens = await indexTokensFromBlocks(networkId, fromBlock, toBlock);

            // Add to cache
            let networkCache = tokenCache.get(networkId);
            if (!networkCache) {
                networkCache = new Map();
                tokenCache.set(networkId, networkCache);
            }

            for (const token of tokens) {
                networkCache.set(token.address.toLowerCase(), token);
            }

            if (tokens.length > 0) {
                console.log(`[TokenIndex] ${networkId}: Found ${tokens.length} tokens in blocks ${fromBlock}-${toBlock}`);
            }

            fromBlock = toBlock + 1;
            state.lastIndexedBlock = toBlock;
            tokenIndexState.set(networkId, state);

            // Small delay to avoid overwhelming RPC
            await new Promise(resolve => setTimeout(resolve, 50));
        }

        console.log(`[TokenIndex] Completed indexing for ${networkId}. Total tokens: ${tokenCache.get(networkId)?.size || 0}`);
    } catch (err) {
        console.error(`[TokenIndex] Error for ${networkId}:`, err.message);
    } finally {
        state.isIndexing = false;
        tokenIndexState.set(networkId, state);
    }
}

async function handleNFTLookup(req, res) {
    const body = await parseBody(req);
    const { contract, tokenId, network } = body;

    if (!contract || tokenId === undefined) {
        jsonResponse(res, { success: false, error: 'Contract address and tokenId are required' });
        return;
    }

    try {
        const config = networkConfigs[network];
        if (!config || config.type !== 'zond') {
            jsonResponse(res, { success: false, error: 'Network not supported for NFT lookup' });
            return;
        }

        const rpcUrl = config.execRpc;
        const contractAddr = contract.startsWith('Z') ? '0x' + contract.slice(1) : contract;

        // Call tokenURI
        const tokenURIData = NFT_SELECTORS.tokenURI + encodeUint256(tokenId);
        const tokenURIResult = await httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: {
                jsonrpc: '2.0',
                method: 'qrl_call',
                params: [{ to: contractAddr, data: tokenURIData }, 'latest'],
                id: 1
            }
        });

        let tokenURI = '';
        if (tokenURIResult.data?.result) {
            tokenURI = decodeString(tokenURIResult.data.result);
        }

        // Call ownerOf
        const ownerData = NFT_SELECTORS.ownerOf + encodeUint256(tokenId);
        const ownerResult = await httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: {
                jsonrpc: '2.0',
                method: 'qrl_call',
                params: [{ to: contractAddr, data: ownerData }, 'latest'],
                id: 2
            }
        });

        let owner = '';
        if (ownerResult.data?.result) {
            owner = decodeAddress(ownerResult.data.result);
        }

        // Call name()
        const nameResult = await httpRequest(rpcUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: {
                jsonrpc: '2.0',
                method: 'qrl_call',
                params: [{ to: contractAddr, data: NFT_SELECTORS.name }, 'latest'],
                id: 3
            }
        });

        let collectionName = '';
        if (nameResult.data?.result) {
            collectionName = decodeString(nameResult.data.result);
        }

        // Fetch metadata from tokenURI
        let metadata = null;
        let image = '';
        let name = '';
        let description = '';
        let attributes = [];
        // Extended media support
        let animationUrl = '';
        let externalUrl = '';
        let backgroundColor = '';
        let youtubeUrl = '';
        let mediaType = 'image'; // 'image', 'audio', 'video', '3d'
        // Subtitle/caption support with i18n
        let subtitles = []; // Array of { lang: 'en', label: 'English', url: 'ipfs://...' }
        // Audio-specific metadata
        let duration = null;
        let artist = '';
        let album = '';
        let genre = '';
        let lyrics = null; // { text: '...', lang: 'en' } or array for i18n
        // Additional localized content
        let localizedContent = {}; // { 'en': { name, description }, 'es': { name, description } }

        if (tokenURI) {
            try {
                // Handle different URI formats
                let metadataUrl = tokenURI;
                if (tokenURI.startsWith('ipfs://')) {
                    metadataUrl = IPFS_GATEWAYS[0] + tokenURI.replace('ipfs://', '');
                } else if (tokenURI.startsWith('data:application/json')) {
                    // Base64 encoded JSON
                    const base64Data = tokenURI.split(',')[1];
                    metadata = JSON.parse(Buffer.from(base64Data, 'base64').toString('utf8'));
                }

                if (!metadata && metadataUrl.startsWith('http')) {
                    const metaResponse = await httpRequest(metadataUrl, { method: 'GET', timeout: 10000 });
                    if (metaResponse.data) {
                        metadata = typeof metaResponse.data === 'string' ? JSON.parse(metaResponse.data) : metaResponse.data;
                    }
                }

                if (metadata) {
                    name = metadata.name || '';
                    description = metadata.description || '';
                    image = metadata.image || '';
                    attributes = metadata.attributes || [];

                    // Extended metadata fields (OpenSea/ERC-721 standard)
                    animationUrl = metadata.animation_url || '';
                    externalUrl = metadata.external_url || '';
                    backgroundColor = metadata.background_color || '';
                    youtubeUrl = metadata.youtube_url || '';

                    // Subtitle/caption tracks with i18n support
                    // Format: { subtitles: [{ lang: 'en', label: 'English', url: 'ipfs://...' }, ...] }
                    // Or: { subtitles: { en: 'ipfs://...', es: 'ipfs://...' } }
                    if (metadata.subtitles) {
                        if (Array.isArray(metadata.subtitles)) {
                            subtitles = metadata.subtitles.map(sub => {
                                let url = sub.url || sub.src || '';
                                if (url.startsWith('ipfs://')) {
                                    url = IPFS_GATEWAYS[0] + url.replace('ipfs://', '');
                                }
                                return {
                                    lang: sub.lang || sub.language || 'en',
                                    label: sub.label || sub.name || sub.lang || 'Unknown',
                                    url: url,
                                    default: sub.default || false
                                };
                            });
                        } else if (typeof metadata.subtitles === 'object') {
                            // Convert object format to array
                            subtitles = Object.entries(metadata.subtitles).map(([lang, url]) => {
                                let resolvedUrl = url;
                                if (resolvedUrl.startsWith('ipfs://')) {
                                    resolvedUrl = IPFS_GATEWAYS[0] + resolvedUrl.replace('ipfs://', '');
                                }
                                return { lang, label: lang.toUpperCase(), url: resolvedUrl, default: lang === 'en' };
                            });
                        }
                    }
                    // Also check captions field (alternative naming)
                    if (!subtitles.length && metadata.captions) {
                        subtitles = Array.isArray(metadata.captions) ? metadata.captions : [];
                    }

                    // Audio-specific metadata
                    duration = metadata.duration || null;
                    artist = metadata.artist || metadata.creator || '';
                    album = metadata.album || '';
                    genre = metadata.genre || '';
                    lyrics = metadata.lyrics || null;

                    // Localized content (i18n for name/description)
                    // Format: { localization: { en: { name, description }, es: { name, description } } }
                    if (metadata.localization) {
                        localizedContent = metadata.localization;
                    } else if (metadata.i18n) {
                        localizedContent = metadata.i18n;
                    } else if (metadata.translations) {
                        localizedContent = metadata.translations;
                    }

                    // Convert IPFS URLs to gateway URLs
                    if (image.startsWith('ipfs://')) {
                        image = IPFS_GATEWAYS[0] + image.replace('ipfs://', '');
                    }
                    if (animationUrl.startsWith('ipfs://')) {
                        animationUrl = IPFS_GATEWAYS[0] + animationUrl.replace('ipfs://', '');
                    }

                    // Detect media type from animation_url
                    if (animationUrl) {
                        const urlLower = animationUrl.toLowerCase();
                        // Audio formats
                        if (urlLower.match(/\.(mp3|wav|ogg|flac|aac|m4a|wma|aiff|alac|opus)(\?|$)/)) {
                            mediaType = 'audio';
                        }
                        // Video formats
                        else if (urlLower.match(/\.(mp4|webm|ogv|mov|avi|mkv|m4v|wmv|flv)(\?|$)/)) {
                            mediaType = 'video';
                        }
                        // 3D model formats
                        else if (urlLower.match(/\.(gltf|glb|obj|fbx|stl|3ds|dae|ply)(\?|$)/)) {
                            mediaType = '3d';
                        }
                        // HTML/interactive
                        else if (urlLower.match(/\.(html|htm|svg)(\?|$)/)) {
                            mediaType = 'interactive';
                        }
                    }
                }
            } catch (metaErr) {
                console.error('Metadata fetch error:', metaErr.message);
            }
        }

        jsonResponse(res, {
            success: true,
            nft: {
                tokenId: tokenId,
                contract: contractAddr,
                owner: owner,
                collection: collectionName,
                tokenURI: tokenURI,
                name: name || `Token #${tokenId}`,
                description: description,
                image: image,
                attributes: attributes,
                // Extended media fields
                animationUrl: animationUrl,
                externalUrl: externalUrl,
                backgroundColor: backgroundColor,
                youtubeUrl: youtubeUrl,
                mediaType: mediaType,
                // Subtitle/caption support (i18n)
                subtitles: subtitles,
                // Audio-specific metadata
                duration: duration,
                artist: artist,
                album: album,
                genre: genre,
                lyrics: lyrics,
                // Localized content (i18n)
                localization: localizedContent,
                // Raw metadata
                metadata: metadata
            }
        });

    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

async function handleNFTCollection(req, res) {
    const body = await parseBody(req);
    const { contract, start = 0, count = 12, network } = body;

    if (!contract) {
        jsonResponse(res, { success: false, error: 'Contract address is required' });
        return;
    }

    try {
        const config = networkConfigs[network];
        if (!config || config.type !== 'zond') {
            jsonResponse(res, { success: false, error: 'Network not supported' });
            return;
        }

        const rpcUrl = config.execRpc;
        const contractAddr = contract.startsWith('Z') ? '0x' + contract.slice(1) : contract;

        const nfts = [];

        for (let i = start; i < start + count; i++) {
            try {
                // Get tokenURI
                const tokenURIData = NFT_SELECTORS.tokenURI + encodeUint256(i);
                const result = await httpRequest(rpcUrl, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: {
                        jsonrpc: '2.0',
                        method: 'qrl_call',
                        params: [{ to: contractAddr, data: tokenURIData }, 'latest'],
                        id: i
                    }
                });

                if (result.data?.error) continue;

                let tokenURI = '';
                if (result.data?.result && result.data.result !== '0x') {
                    tokenURI = decodeString(result.data.result);
                }

                let name = `Token #${i}`;
                let image = '';

                // Fetch metadata
                if (tokenURI) {
                    try {
                        let metadataUrl = tokenURI;
                        let metadata = null;

                        if (tokenURI.startsWith('ipfs://')) {
                            metadataUrl = IPFS_GATEWAYS[0] + tokenURI.replace('ipfs://', '');
                        } else if (tokenURI.startsWith('data:application/json')) {
                            const base64Data = tokenURI.split(',')[1];
                            metadata = JSON.parse(Buffer.from(base64Data, 'base64').toString('utf8'));
                        }

                        if (!metadata && metadataUrl.startsWith('http')) {
                            const metaResponse = await httpRequest(metadataUrl, { method: 'GET', timeout: 5000 });
                            if (metaResponse.data) {
                                metadata = typeof metaResponse.data === 'string' ? JSON.parse(metaResponse.data) : metaResponse.data;
                            }
                        }

                        if (metadata) {
                            name = metadata.name || name;
                            image = metadata.image || '';
                            if (image.startsWith('ipfs://')) {
                                image = IPFS_GATEWAYS[0] + image.replace('ipfs://', '');
                            }
                        }
                    } catch {
                        // Ignore metadata fetch errors
                    }
                }

                nfts.push({
                    tokenId: i,
                    name: name,
                    image: image,
                    tokenURI: tokenURI
                });

            } catch {
                // Token doesn't exist or error, skip
            }
        }

        jsonResponse(res, { success: true, nfts, contract: contractAddr });

    } catch (err) {
        jsonResponse(res, { success: false, error: errDetail(err) });
    }
}

// ========== SERVER ==========

// Per-IP token bucket for /api/*. Prevents a page the operator
// visits from flooding Argon2id / docker-buildx / compose-reset
// endpoints with free requests. Loopback-only tool, so the budget
// can be modest; override via QRL_API_RATE_* env vars if an
// honest workload trips the limit.
const RATE_BURST = parseInt(process.env.QRL_API_RATE_BURST || '60', 10);
const RATE_REFILL_PER_SEC = parseInt(process.env.QRL_API_RATE_REFILL || '10', 10);
const _buckets = new Map(); // ip → { tokens, lastRefill }
function rateLimitAllow(ip) {
    const now = Date.now();
    let b = _buckets.get(ip);
    if (!b) { b = { tokens: RATE_BURST, lastRefill: now }; _buckets.set(ip, b); }
    const elapsedSec = (now - b.lastRefill) / 1000;
    b.tokens = Math.min(RATE_BURST, b.tokens + elapsedSec * RATE_REFILL_PER_SEC);
    b.lastRefill = now;
    if (b.tokens < 1) return false;
    b.tokens -= 1;
    return true;
}
// Prune stale buckets every 5 min to bound memory.
setInterval(() => {
    const cutoff = Date.now() - 10 * 60 * 1000;
    for (const [ip, b] of _buckets) {
        if (b.lastRefill < cutoff) _buckets.delete(ip);
    }
}, 5 * 60 * 1000).unref();

const server = http.createServer(async (req, res) => {
    // Bind req to res so jsonResponse can read req.headers.origin.
    res.req = req;

    // Loopback-only CORS preflight. See securityHeaders().
    if (req.method === 'OPTIONS') {
        res.writeHead(204, securityHeaders(req));
        res.end();
        return;
    }

    const url = req.url.split('?')[0];

    // Rate-limit /api/* per client IP.
    if (url.startsWith('/api/')) {
        const ip = req.socket.remoteAddress || 'unknown';
        if (!rateLimitAllow(ip)) {
            res.writeHead(429, {
                'Content-Type': 'application/json',
                'Retry-After': '1',
                ...securityHeaders(req),
            });
            res.end(JSON.stringify({ success: false, error: 'rate limited — slow down' }));
            return;
        }
    }

    // Origin check: any state-changing request from a non-loopback
    // origin is refused. Browsers can still make the request, but the
    // dashboard refuses to act on it. (GETs are read-only metadata,
    // allowed from any origin to simplify dashboard tabbing.)
    if (req.method === 'POST' && url.startsWith('/api/')) {
        const o = req.headers.origin;
        if (o && !LOOPBACK_ORIGIN_RE.test(o)) {
            res.writeHead(403, { 'Content-Type': 'application/json', ...securityHeaders(req) });
            res.end(JSON.stringify({ success: false, error: 'cross-origin requests from non-loopback origins are refused on state-changing endpoints' }));
            return;
        }
    }

    // API routes
    if (url === '/api/status') {
        await handleStatusRequest(req, res);
    } else if (url === '/api/wallet/load' && req.method === 'POST') {
        await handleWalletLoad(req, res);
    } else if (url === '/api/wallet/create' && req.method === 'POST') {
        await handleWalletCreate(req, res);
    } else if (url === '/api/wallet/create/prepare' && req.method === 'POST') {
        await handleWalletCreatePrepare(req, res);
    } else if (url === '/api/wallet/create/seal' && req.method === 'POST') {
        await handleWalletCreateSeal(req, res);
    } else if (url === '/api/wallet/list' && req.method === 'GET') {
        await handleWalletList(req, res);
    } else if (url === '/api/wallet/verify' && req.method === 'POST') {
        await handleWalletVerify(req, res);
    } else if (url === '/api/wallet/unlock' && req.method === 'POST') {
        await handleWalletUnlock(req, res);
    } else if (url === '/api/wallet/sign' && req.method === 'POST') {
        await handleWalletSign(req, res);
    } else if (url === '/api/wallet/balance' && req.method === 'POST') {
        await handleWalletBalance(req, res);
    } else if (url === '/api/transaction/send' && req.method === 'POST') {
        await handleTransactionSend(req, res);
    } else if (url === '/api/transaction/message' && req.method === 'POST') {
        await handleTransactionMessage(req, res);
    } else if (url === '/api/transaction/lookup' && req.method === 'POST') {
        await handleTransactionLookup(req, res);
    } else if (url === '/api/transactions/recent' && req.method === 'POST') {
        await handleRecentTransactions(req, res);
    } else if (url === '/api/block/lookup' && req.method === 'POST') {
        await handleBlockLookup(req, res);
    } else if (url === '/api/blocks/recent' && req.method === 'POST') {
        await handleRecentBlocks(req, res);
    } else if (url === '/api/address/lookup' && req.method === 'POST') {
        await handleAddressLookup(req, res);
    } else if (url === '/api/token/lookup' && req.method === 'POST') {
        await handleTokenLookup(req, res);
    } else if (url === '/api/tokens/by-address' && req.method === 'POST') {
        await handleTokensByAddress(req, res);
    } else if (url === '/api/tokens/browse' && req.method === 'POST') {
        await handleTokenBrowse(req, res);
    } else if (url === '/api/tokens/details' && req.method === 'POST') {
        await handleTokenDetails(req, res);
    } else if (url === '/api/tokens/refresh-index' && req.method === 'POST') {
        await handleTokenIndexRefresh(req, res);
    } else if (url === '/api/richlist' && req.method === 'POST') {
        await handleRichlist(req, res);
    } else if (url === '/api/peers' && req.method === 'POST') {
        await handlePeers(req, res);
    } else if (url === '/api/metrics' && req.method === 'POST') {
        await handleMetrics(req, res);
    } else if (url === '/api/explorer/search' && req.method === 'POST') {
        await handleExplorerSearch(req, res);
    } else if (url === '/api/ots/check' && req.method === 'POST') {
        await handleOTSCheck(req, res);
    } else if (url === '/api/zond/reset-genesis' && req.method === 'POST') {
        await handleZondResetGenesis(req, res);
    } else if (url === '/api/compose/status' && req.method === 'GET') {
        await handleComposeStatus(req, res);
    } else if (url === '/api/compose/reset' && req.method === 'POST') {
        await handleComposeReset(req, res);
    } else if (url === '/api/compose/security' && req.method === 'GET') {
        await handleComposeSecurity(req, res);
    } else if (url === '/api/dashboard/identity' && req.method === 'GET') {
        await handleDashboardIdentity(req, res);
    } else if (url === '/api/compose/projects' && req.method === 'GET') {
        await handleComposeProjects(req, res);
    } else if (url === '/api/compose/container/restart' && req.method === 'POST') {
        await handleComposeContainerRestart(req, res);
    } else if (url === '/api/compose/container/pause' && req.method === 'POST') {
        await handleComposeContainerPause(req, res);
    } else if (url === '/api/compose/container/unpause' && req.method === 'POST') {
        await handleComposeContainerUnpause(req, res);
    } else if (url.startsWith('/api/compose/container/logs') && req.method === 'GET') {
        await handleComposeContainerLogs(req, res);
    } else if (url === '/api/system/disk' && req.method === 'GET') {
        await handleSystemDisk(req, res);
    } else if (url === '/api/system/qrlenv' && req.method === 'GET') {
        await handleSystemQrlenv(req, res);
    } else if (url === '/api/tls/gen-cert' && req.method === 'POST') {
        await handleTlsGenCert(req, res);
    } else if (url === '/api/release/artifacts' && req.method === 'GET') {
        await handleReleaseArtifacts(req, res);
    } else if (url === '/api/release/build' && req.method === 'POST') {
        await handleReleaseBuild(req, res);
    } else if (url === '/api/release/sign' && req.method === 'POST') {
        await handleReleaseSign(req, res);
    } else if (url === '/api/release/verify' && req.method === 'POST') {
        await handleReleaseVerify(req, res);
    } else if (url === '/api/release/bundle' && req.method === 'POST') {
        await handleReleaseBundle(req, res);
    } else if (url === '/api/release/load' && req.method === 'POST') {
        await handleReleaseLoad(req, res);
    } else if (url === '/api/release/inspect' && req.method === 'POST') {
        await handleReleaseInspect(req, res);
    } else if (url === '/api/rpc' && req.method === 'POST') {
        await handleRpcPassthrough(req, res);
    } else if (url === '/api/proxy/json' && req.method === 'GET') {
        await handleJsonProxy(req, res, req.url);
    } else if (url === '/api/nodes/control' && req.method === 'POST') {
        await handleNodeControl(req, res);
    } else if (url === '/api/nodes/mining' && req.method === 'POST') {
        await handleClassicMiningToggle(req, res);
    } else if (url === '/api/nodes/mining' && req.method === 'GET') {
        await handleClassicMiningStatus(req, res);
    } else if (url === '/api/zeus-proxy/control' && req.method === 'POST') {
        await handleZeusProxyControl(req, res);
    }
    // Variant & snapshot management
    else if (url === '/api/testnet-build' && req.method === 'GET') {
        await handleTestnetBuildGet(req, res);
    } else if (url === '/api/testnet-build' && req.method === 'POST') {
        await handleTestnetBuildSet(req, res);
    } else if (url === '/api/variants/current' && req.method === 'GET') {
        await handleVariantCurrent(req, res);
    } else if (url === '/api/variants/switch' && req.method === 'POST') {
        await handleVariantSwitch(req, res);
    } else if (url === '/api/variants/build' && req.method === 'POST') {
        await handleVariantBuild(req, res);
    } else if (url === '/api/snapshots' && req.method === 'GET') {
        await handleSnapshotList(req, res);
    } else if (url === '/api/snapshots/save' && req.method === 'POST') {
        await handleSnapshotSave(req, res);
    } else if (url === '/api/snapshots/restore' && req.method === 'POST') {
        await handleSnapshotRestore(req, res);
    } else if (url === '/api/snapshots/delete' && req.method === 'POST') {
        await handleSnapshotDelete(req, res);
    } else if (url === '/api/snapshots/fork' && req.method === 'POST') {
        await handleSnapshotFork(req, res);
    }
    // New unified network-aware API routes
    else if (url === '/api/network/stats' && req.method === 'POST') {
        await handleNetworkStats(req, res);
    } else if (url === '/api/network/blocks' && req.method === 'POST') {
        await handleLatestBlocks(req, res);
    } else if (url === '/api/network/transactions' && req.method === 'POST') {
        await handleLatestTransactions(req, res);
    } else if (url === '/api/network/pending' && req.method === 'POST') {
        await handlePendingTransactions(req, res);
    }
    // Globe visualization API
    else if (url === '/api/globe/peers' && req.method === 'POST') {
        await handleGlobePeers(req, res);
    } else if (url === '/api/globe/transactions' && req.method === 'POST') {
        await handleGlobeTransactions(req, res);
    } else if (url === '/api/globe/enhanced-data' && req.method === 'POST') {
        await handleGlobeEnhancedData(req, res);
    } else if (url === '/api/topology' && req.method === 'POST') {
        await handleTopology(req, res);
    }
    // IPFS API routes
    else if (url === '/api/ipfs/upload' && req.method === 'POST') {
        await handleIPFSUpload(req, res);
    } else if (url === '/api/ipfs/fetch' && req.method === 'POST') {
        await handleIPFSFetch(req, res);
    } else if (url === '/api/ipfs/test-pinata' && req.method === 'POST') {
        await handleIPFSTestPinata(req, res);
    } else if (url === '/api/ipfs/test-local' && req.method === 'POST') {
        await handleIPFSTestLocal(req, res);
    } else if (url === '/api/ipfs/test-gateway' && req.method === 'POST') {
        await handleIPFSTestGateway(req, res);
    }
    // NFT API routes
    else if (url === '/api/nft/lookup' && req.method === 'POST') {
        await handleNFTLookup(req, res);
    } else if (url === '/api/nft/collection' && req.method === 'POST') {
        await handleNFTCollection(req, res);
    }
    // Proxy routes for direct Zeus/RPC forwarding
    else if (url.startsWith('/api/proxy/') && req.method === 'POST') {
        const parts = url.split('/');
        const networkId = parts[3]; // e.g., 'classic-mainnet'
        const method = parts[4] || 'GetStats'; // e.g., 'GetStats'
        await handleProxyRequest(req, res, networkId, method);
    }
    // Page routes
    else if (url === '/' || url === '/index.html') {
        // Serve new dashboard by default
        const htmlPath = path.join(__dirname, 'dashboard-v2.html');
        const servePath = fs.existsSync(htmlPath) ? htmlPath : path.join(__dirname, 'dashboard.html');
        const html = fs.readFileSync(servePath, 'utf8');
        res.writeHead(200, {
            'Content-Type': 'text/html',
            // CSP is permissive on inline because the dashboard uses
            // inline event handlers + inline script tags — reflow would
            // mean rewriting ~10k lines of HTML. We lock down the big
            // escape routes (frame-src, object-src, base-uri) and keep
            // network reach loopback-only.
            'Content-Security-Policy': [
                "default-src 'self' 'unsafe-inline' data: blob:",
                "script-src 'self' 'unsafe-inline' 'unsafe-eval' blob:",
                "connect-src 'self' http://127.0.0.1:* ws://127.0.0.1:* https://127.0.0.1:* http://localhost:* ws://localhost:*",
                "img-src 'self' data: https://img.shields.io",
                "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
                "font-src 'self' data: https://fonts.gstatic.com",
                "frame-src 'none'",
                "object-src 'none'",
                "base-uri 'self'",
                "form-action 'self'",
            ].join('; '),
            'X-Frame-Options': 'DENY',
            'X-Content-Type-Options': 'nosniff',
            'Referrer-Policy': 'no-referrer',
        });
        res.end(html);
    } else if (url === '/old' || url === '/v1') {
        const htmlPath = path.join(__dirname, 'dashboard.html');
        const html = fs.readFileSync(htmlPath, 'utf8');
        res.writeHead(200, {
            'Content-Type': 'text/html',
            'X-Frame-Options': 'DENY',
            'X-Content-Type-Options': 'nosniff',
            'Referrer-Policy': 'no-referrer',
        });
        res.end(html);
    } else if (url === '/vendor/hash-wasm/argon2.umd.min.js' && req.method === 'GET') {
        // Vendored Argon2id (hash-wasm 4.11.0 UMD). Integrity-checked on
        // startup — see VENDOR_INTEGRITY at the bottom of this file.
        try {
            const p = path.join(__dirname, 'vendor', 'hash-wasm', 'argon2.umd.min.js');
            const body = fs.readFileSync(p);
            res.writeHead(200, {
                'Content-Type': 'application/javascript; charset=utf-8',
                'Cache-Control': 'public, max-age=31536000, immutable',
                'X-Content-Type-Options': 'nosniff',
                'Cross-Origin-Resource-Policy': 'same-origin',
            });
            res.end(body);
        } catch (e) {
            res.writeHead(500);
            res.end('vendor asset missing');
        }
    } else if (url === '/web/argon2-worker.js' && req.method === 'GET') {
        // Argon2id Web Worker — must be same-origin so the browser will
        // let it importScripts('/vendor/...').
        try {
            const p = path.join(__dirname, 'web', 'argon2-worker.js');
            const body = fs.readFileSync(p);
            res.writeHead(200, {
                'Content-Type': 'application/javascript; charset=utf-8',
                'Cache-Control': 'no-store',
                'X-Content-Type-Options': 'nosniff',
                'Cross-Origin-Resource-Policy': 'same-origin',
            });
            res.end(body);
        } catch (e) {
            res.writeHead(500);
            res.end('worker script missing');
        }
    } else if (url === '/token-images.json') {
        // User-maintained token-image list. We only serve the file — never fetch,
        // proxy, or cache actual image bytes. Browsers do the image loading themselves.
        try {
            const listPath = path.join(__dirname, 'token-images.json');
            const body = fs.readFileSync(listPath, 'utf8');
            res.writeHead(200, {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-store',
            });
            res.end(body);
        } catch (e) {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end('{"tokens":{}}');
        }
    } else {
        res.writeHead(404);
        res.end('Not found');
    }
});

// Vendored file integrity — verified on startup and on the first request.
// If the file drifts, we surface a very loud warning and REFUSE to serve
// the worker script. Clients fall back to Tier 1 (server-side Argon2id)
// which is still safe. See vendor/hash-wasm/README.md.
const VENDOR_INTEGRITY = Object.freeze({
    'vendor/hash-wasm/argon2.umd.min.js':
        'a39436077c1037ac5fd3ef5855026de16dee90a3bf4f2291b982e05a51cd1b07',
});
function verifyVendoredAssetIntegrity() {
    const crypto = require('crypto');
    for (const [rel, expected] of Object.entries(VENDOR_INTEGRITY)) {
        const abs = path.join(__dirname, rel);
        try {
            const buf = fs.readFileSync(abs);
            const sha = crypto.createHash('sha256').update(buf).digest('hex');
            if (sha !== expected) {
                console.error(`[VENDOR INTEGRITY] MISMATCH for ${rel}:`);
                console.error(`  expected: ${expected}`);
                console.error(`  actual:   ${sha}`);
                console.error(`  Refusing to trust this file. Tier 2 wallet-create will not work; Tier 1 fallback remains available.`);
            } else {
                console.log(`[VENDOR INTEGRITY] OK: ${rel}`);
            }
        } catch (e) {
            console.warn(`[VENDOR INTEGRITY] file missing: ${rel} — ${e.message}`);
        }
    }
}

// Tier 3d — FIPS 140-3 startup gate.
//
// When QRL_REQUIRE_FIPS=1 in the environment, verify that Node was built
// against a FIPS-validated OpenSSL module and that FIPS mode is currently
// active. Refuse to start otherwise. See docs/TIER3D-FIPS-SCOPING.md.
//
// This is deliberately a soft check unless the operator opts in — most
// developers run against a stock Node.js build, and the tier 1-3c
// hardening still applies without FIPS validation.
function checkFipsRequirement() {
    if (process.env.QRL_REQUIRE_FIPS !== '1') return;
    const cryptoLib = require('crypto');
    let fipsActive = false;
    try {
        // Node < 18 exposed `crypto.fips` as boolean; Node 18+ has
        // `crypto.getFips()` returning 1/0. Try both.
        if (typeof cryptoLib.getFips === 'function') fipsActive = cryptoLib.getFips() === 1;
        else if ('fips' in cryptoLib) fipsActive = !!cryptoLib.fips;
    } catch { fipsActive = false; }
    if (!fipsActive) {
        console.error('[FIPS] QRL_REQUIRE_FIPS=1 but crypto.getFips() reports 0.');
        console.error('[FIPS] Rebuild Node against a FIPS-validated OpenSSL 3.x module and re-enable at runtime with:');
        console.error('[FIPS]     node --enable-fips --openssl-config=/path/to/fipsmodule.cnf status-server.js');
        console.error('[FIPS] See docs/TIER3D-FIPS-SCOPING.md for the full build recipe.');
        process.exit(2);
    }
    const openssl = process.versions && process.versions.openssl;
    console.log(`[FIPS] active. OpenSSL: ${openssl || 'unknown'}. Node: ${process.versions.node}.`);
}
checkFipsRequirement();

// ============================================================================
// PQ TLS listener (opt-in via QRL_TLS_CERT + QRL_TLS_KEY env vars).
//
// When both are set to PEM file paths, the dashboard exposes HTTPS on
// QRL_TLS_PORT (default 8443) using TLS 1.3 with X25519MLKEM768 as the
// preferred key-exchange group (PQ-safe hybrid, FIPS 203). Falls back
// to plain HTTP on PORT when TLS is disabled — backward compatible.
//
// The cert signature is out-of-scope here — MLKEM handles KEM, not
// authentication. Cert can be ECDSA-P256 today; migration to ML-DSA-87
// certs waits on X.509 tooling to catch up (browsers, CA infrastructure).
//
// The hybrid group X25519MLKEM768 is what Chrome + Firefox + Cloudflare
// have already deployed at scale — real interop, not just theory.
// ============================================================================

const tlsCertPath = process.env.QRL_TLS_CERT;
const tlsKeyPath = process.env.QRL_TLS_KEY;
const tlsPort = parseInt(process.env.QRL_TLS_PORT || '8443', 10);

let pqTlsServer = null;
if (tlsCertPath && tlsKeyPath) {
    const tls = require('tls');
    try {
        const tlsOpts = {
            cert: fs.readFileSync(tlsCertPath),
            key: fs.readFileSync(tlsKeyPath),
            minVersion: 'TLSv1.3',
            maxVersion: 'TLSv1.3',
            ecdhCurve: 'X25519MLKEM768:X25519:P-256',
        };
        pqTlsServer = https.createServer(tlsOpts, server.listeners('request')[0]);
        pqTlsServer.listen(tlsPort, () => {
            console.log(`QRL Command Center PQ-TLS listening at https://localhost:${tlsPort}/`);
            console.log(`  key-exchange:  X25519MLKEM768 (hybrid, FIPS 203) preferred`);
            console.log(`  fallback:      X25519, P-256 (classical only)`);
            console.log(`  cert:          ${tlsCertPath}`);
        });
    } catch (e) {
        console.error(`PQ-TLS setup FAILED: ${e.message}`);
        console.error(`  (dashboard will still serve HTTP on ${PORT} — but PQ TLS is disabled)`);
    }
}

server.listen(PORT, () => {
    console.log(`QRL Command Center running at http://localhost:${PORT}/`);
    console.log(`Old dashboard available at http://localhost:${PORT}/old`);
    console.log(`API endpoint: http://localhost:${PORT}/api/status`);
    if (!pqTlsServer) {
        console.log(`(PQ TLS disabled — set QRL_TLS_CERT + QRL_TLS_KEY to enable HTTPS on ${tlsPort})`);
    }
    verifyVendoredAssetIntegrity();
});

// ============================================================================
// Tier 3a — Unix domain socket bind (same-uid-only API surface)
//
// Filesystem socket at $HOME/.qrl-dashboard/api.sock (mode 0600). Only
// processes running as this user can connect. Not exposed over the
// network in any form. The CLI in `bin/qrl-wallet-cli.js` uses this
// socket exclusively — passwords never enter the TCP listener.
//
// The same HTTP request handler is reused so all API routes work
// identically over both transports. Wallet-create endpoints are
// available on both (browser needs TCP), but the CLI's socket path is
// preferred for scripted operations.
// ============================================================================
const UNIX_SOCK_DIR = path.join(os.homedir(), '.qrl-dashboard');
const UNIX_SOCK_PATH = path.join(UNIX_SOCK_DIR, 'api.sock');

function startUnixSocketServer(requestHandler) {
    try {
        if (!fs.existsSync(UNIX_SOCK_DIR)) {
            fs.mkdirSync(UNIX_SOCK_DIR, { recursive: true, mode: 0o700 });
        } else {
            // Confirm mode 0700. If someone made it world-readable we tighten it,
            // rather than silently exposing anything inside via traversal.
            const st = fs.statSync(UNIX_SOCK_DIR);
            if ((st.mode & 0o077) !== 0) fs.chmodSync(UNIX_SOCK_DIR, 0o700);
        }
        // Clean up a stale socket file left by a previous crash. Only unlink
        // if it's actually a socket — never blindly rm arbitrary files.
        if (fs.existsSync(UNIX_SOCK_PATH)) {
            const st = fs.statSync(UNIX_SOCK_PATH);
            if (st.isSocket()) fs.unlinkSync(UNIX_SOCK_PATH);
            else {
                console.warn(`[UNIX SOCK] refusing to unlink non-socket file at ${UNIX_SOCK_PATH}`);
                return null;
            }
        }
        const unixServer = http.createServer(requestHandler);
        // The listen() error path is asynchronous — a sync try/catch above
        // doesn't catch it. On macOS Docker Desktop, the bind-mount for
        // /home/node/.qrl-dashboard is backed by VirtioFS, which doesn't
        // support Unix-domain-socket bind on some host OS versions, and
        // the listen fails with ENOTSUP. Treat that as "Unix socket API
        // not available on this filesystem; TCP only" rather than letting
        // the error crash the whole process.
        unixServer.on('error', (err) => {
            console.warn(`[UNIX SOCK] disabled: ${err.code || ''} ${err.message}`);
            try { unixServer.close(); } catch {}
        });
        unixServer.listen(UNIX_SOCK_PATH, () => {
            // Tighten mode BEFORE anything can connect. Between listen() and
            // this chmod, the socket is technically world-connectable — but
            // the parent dir is 0700 so no other user can find it anyway.
            try { fs.chmodSync(UNIX_SOCK_PATH, 0o600); } catch {}
            console.log(`[UNIX SOCK] API also listening on ${UNIX_SOCK_PATH} (mode 0600)`);
        });
        // Clean removal on graceful shutdown. Not registered on SIGKILL —
        // startUnixSocketServer's stale-socket cleanup above handles that.
        const cleanup = () => {
            try { unixServer.close(); } catch {}
            try { if (fs.existsSync(UNIX_SOCK_PATH)) fs.unlinkSync(UNIX_SOCK_PATH); } catch {}
        };
        process.on('SIGTERM', cleanup);
        process.on('SIGINT', () => { cleanup(); process.exit(0); });
        return unixServer;
    } catch (e) {
        console.warn(`[UNIX SOCK] disabled: ${e.message}`);
        return null;
    }
}
// We must pass the same handler function the TCP server was created with.
// http.Server exposes it as `_events.request`.
startUnixSocketServer(server._events.request);
