#!/usr/bin/env node
// QRL Method Proxy - Translates zond_* methods to qrl_* methods
// This allows the beacon chain to communicate with execution layer

const http = require('http');
const { spawn } = require('child_process');

const PROXY_PORT = 9552; // Proxy port for beacon chain
const TARGET_PORT = 9545; // Actual gqrl HTTP port (no auth required)

// Method translation map
const METHOD_TRANSLATIONS = {
    'zond_chainId': 'qrl_chainId',
    'zond_getBlockByNumber': 'qrl_getBlockByNumber',
    'zond_getBlockByHash': 'qrl_getBlockByHash',
    'zond_syncing': 'qrl_syncing',
    'zond_getBalance': 'qrl_getBalance',
    // Engine API methods - check if they exist on regular port or pass through
    'engine_newPayloadV1': 'engine_newPayloadV1',
    'engine_newPayloadV2': 'engine_newPayloadV2',
    'engine_forkchoiceUpdatedV1': 'engine_forkchoiceUpdatedV1',
    'engine_forkchoiceUpdatedV2': 'engine_forkchoiceUpdatedV2',
    'engine_getPayloadV1': 'engine_getPayloadV1',
    'engine_getPayloadV2': 'engine_getPayloadV2',
    // Also try QRL-prefixed versions if needed
    'zond_newPayload': 'qrl_newPayload',
    'zond_forkchoiceUpdated': 'qrl_forkchoiceUpdated',
};

const server = http.createServer(async (req, res) => {
    if (req.method !== 'POST') {
        res.writeHead(405, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Method not allowed' }));
        return;
    }

    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', async () => {
        try {
            const request = JSON.parse(body);

            // Translate method name if needed
            if (METHOD_TRANSLATIONS[request.method]) {
                console.log(`[PROXY] Translating ${request.method} → ${METHOD_TRANSLATIONS[request.method]}`);
                request.method = METHOD_TRANSLATIONS[request.method];
            }

            // Forward to actual gqrl node (with JWT authentication passthrough)
            const proxyHeaders = {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(JSON.stringify(request))
            };

            // Pass through Authorization header if present
            if (req.headers.authorization) {
                proxyHeaders['Authorization'] = req.headers.authorization;
            }

            const proxyReq = http.request({
                hostname: 'localhost',
                port: TARGET_PORT,
                method: 'POST',
                headers: proxyHeaders
            }, (proxyRes) => {
                // Copy response headers
                res.writeHead(proxyRes.statusCode, proxyRes.headers);
                proxyRes.pipe(res);
            });

            proxyReq.on('error', (err) => {
                console.error('[PROXY] Error:', err);
                res.writeHead(500, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({
                    jsonrpc: '2.0',
                    id: request.id,
                    error: { code: -32603, message: 'Proxy error: ' + err.message }
                }));
            });

            proxyReq.write(JSON.stringify(request));
            proxyReq.end();

        } catch (err) {
            console.error('[PROXY] Parse error:', err);
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: 'Invalid JSON' }));
        }
    });
});

server.listen(PROXY_PORT, () => {
    console.log(`🔄 QRL Method Proxy running on port ${PROXY_PORT}`);
    console.log(`📡 Translating zond_* → qrl_* methods`);
    console.log(`🎯 Forwarding to gqrl node on port ${TARGET_PORT}`);
});

// Graceful shutdown
process.on('SIGTERM', () => {
    console.log('🛑 Shutting down proxy...');
    server.close(() => process.exit(0));
});

process.on('SIGINT', () => {
    console.log('\n🛑 Shutting down proxy...');
    server.close(() => process.exit(0));
});