#!/usr/bin/env node
// Enhanced QRL Method Proxy with JWT Authentication
// Handles both basic methods (HTTP) and engine API methods (Auth)

const http = require('http');
const crypto = require('crypto');

const PROXY_PORT = 9552; // Proxy port for beacon chain
const HTTP_PORT = 9545; // Regular gqrl HTTP port (no auth)
const AUTH_PORT = 9551; // gqrl authenticated port (JWT required)

// JWT secret from gqrl
const JWT_SECRET = '0x82ef131461b14597adaaa8302e3543460a78f7a1cce8bc4fa02d276e35dd09a0';
const JWT_SECRET_BYTES = Buffer.from(JWT_SECRET.slice(2), 'hex'); // Remove 0x prefix

// Method routing: engine_ methods go to auth port, others to HTTP port
const ENGINE_METHODS = [
    'engine_newPayloadV1', 'engine_newPayloadV2', 'engine_newPayloadV3',
    'engine_forkchoiceUpdatedV1', 'engine_forkchoiceUpdatedV2', 'engine_forkchoiceUpdatedV3',
    'engine_getPayloadV1', 'engine_getPayloadV2', 'engine_getPayloadV3',
    'engine_getPayloadBodiesByHashV1', 'engine_getPayloadBodiesByRangeV1'
];

// Method translation map
const METHOD_TRANSLATIONS = {
    'zond_chainId': 'qrl_chainId',
    'zond_getBlockByNumber': 'qrl_getBlockByNumber',
    'zond_getBlockByHash': 'qrl_getBlockByHash',
    'zond_syncing': 'qrl_syncing',
    'zond_getBalance': 'qrl_getBalance',
    'zond_getTransactionByHash': 'qrl_getTransactionByHash',
    'zond_getTransactionCount': 'qrl_getTransactionCount',
    'zond_call': 'qrl_call'
};

function createJWTToken() {
    // Create JWT token for engine API authentication
    // Standard JWT: header.payload.signature
    const header = {
        alg: 'HS256',
        typ: 'JWT'
    };

    const payload = {
        iat: Math.floor(Date.now() / 1000), // Issued at
        exp: Math.floor(Date.now() / 1000) + 60 // Expires in 60 seconds
    };

    const encodedHeader = Buffer.from(JSON.stringify(header)).toString('base64url');
    const encodedPayload = Buffer.from(JSON.stringify(payload)).toString('base64url');

    const signatureInput = `${encodedHeader}.${encodedPayload}`;
    const signature = crypto.createHmac('sha256', JWT_SECRET_BYTES)
                           .update(signatureInput)
                           .digest('base64url');

    return `${signatureInput}.${signature}`;
}

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
            const originalMethod = request.method;

            // Translate method name if needed
            if (METHOD_TRANSLATIONS[request.method]) {
                console.log(`[PROXY] Translating ${request.method} → ${METHOD_TRANSLATIONS[request.method]}`);
                request.method = METHOD_TRANSLATIONS[request.method];
            }

            // Route to appropriate port based on method type
            const isEngineMethod = ENGINE_METHODS.includes(originalMethod) ||
                                   ENGINE_METHODS.includes(request.method);
            const targetPort = isEngineMethod ? AUTH_PORT : HTTP_PORT;

            console.log(`[PROXY] ${originalMethod} → ${request.method} (port ${targetPort})`);

            // Prepare headers
            const proxyHeaders = {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(JSON.stringify(request))
            };

            // Add JWT authentication for engine API methods
            if (isEngineMethod) {
                const jwtToken = createJWTToken();
                proxyHeaders['Authorization'] = `Bearer ${jwtToken}`;
                console.log(`[PROXY] Added JWT auth for engine method`);
            }

            // Forward to appropriate gqrl port
            const proxyReq = http.request({
                hostname: 'localhost',
                port: targetPort,
                method: 'POST',
                headers: proxyHeaders
            }, (proxyRes) => {
                // Copy response headers
                res.writeHead(proxyRes.statusCode, proxyRes.headers);
                proxyRes.pipe(res);
            });

            proxyReq.on('error', (err) => {
                console.error(`[PROXY] Error connecting to port ${targetPort}:`, err.message);
                res.writeHead(500, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({
                    jsonrpc: '2.0',
                    id: request.id,
                    error: { code: -32603, message: `Proxy error: ${err.message}` }
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
    console.log(`🔄 Enhanced QRL Method Proxy running on port ${PROXY_PORT}`);
    console.log(`📡 Basic methods → HTTP port ${HTTP_PORT}`);
    console.log(`🔐 Engine methods → Auth port ${AUTH_PORT} (JWT)`);
    console.log(`🎯 Method translation: zond_* → qrl_*`);
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