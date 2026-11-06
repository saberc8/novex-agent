/** Fixed-upstream model gateway. Run with Node >=24 and --use-env-proxy.
 * The trusted host retains the provider key; guests only receive a revocable
 * per-run capability that permits this configured model's chat completions.
 */
import http from 'node:http';
import { randomBytes, timingSafeEqual } from 'node:crypto';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { fileURLToPath } from 'node:url';

const equal = (a, b) => {
  const x = Buffer.from(a || ''); const y = Buffer.from(b || '');
  return x.length === y.length && x.length > 0 && timingSafeEqual(x, y);
};
const bearer = (req) => (req.headers.authorization || '').replace(/^Bearer\s+/i, '');
const json = (res, status, data) => {
  res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(data));
};
async function body(req) {
  const chunks = []; let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 4 * 1024 * 1024) throw new Error('body limit');
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}');
}

export function createModelProxy(config, upstreamFetch = fetch) {
  const capabilities = new Map();
  const active = new Map();
  const admin = (req) => equal(bearer(req), config.runnerSecret);
  const revoke = (runId) => {
    for (const [token, cap] of capabilities) if (cap.runId === runId) capabilities.delete(token);
    for (const [controller, id] of active) if (id === runId) controller.abort();
  };
  const server = http.createServer(async (req, res) => {
    try {
      const path = new URL(req.url, 'http://proxy.invalid').pathname;
      if (path === '/health' && req.method === 'GET') return json(res, 200, { ok: true, model: config.model });
      if (path === '/internal/tokens' && req.method === 'POST') {
        if (!admin(req)) return json(res, 401, { error: 'unauthorized' });
        const input = await body(req);
        if (typeof input.runId !== 'string' || !/^[a-zA-Z0-9_-]{1,100}$/.test(input.runId)) return json(res, 400, { error: 'invalid run' });
        revoke(input.runId);
        const token = randomBytes(32).toString('hex');
        const seconds = Math.min(1800, Math.max(60, Number(input.ttlSeconds) || 900));
        capabilities.set(token, { runId: input.runId, expires: Date.now() + seconds * 1000, requests: 0 });
        return json(res, 201, { token, baseUrl: config.guestBaseUrl, expiresIn: seconds });
      }
      if (path.startsWith('/internal/tokens/') && req.method === 'DELETE') {
        if (!admin(req)) return json(res, 401, { error: 'unauthorized' });
        revoke(decodeURIComponent(path.slice('/internal/tokens/'.length)));
        return json(res, 200, { revoked: true });
      }
      // No arbitrary HTTP proxying, models, account endpoints, or redirects.
      if (path !== '/v1/chat/completions' || req.method !== 'POST') return json(res, 404, { error: 'unsupported endpoint' });
      const token = bearer(req);
      const cap = capabilities.get(token);
      if (!cap || cap.expires <= Date.now()) {
        capabilities.delete(token);
        return json(res, 401, { error: 'expired or invalid run capability' });
      }
      if (++cap.requests > 40) return json(res, 429, { error: 'run model request budget exceeded' });
      const input = await body(req);
      if (input.model !== config.model || !Array.isArray(input.messages)) return json(res, 400, { error: 'model or messages not allowed' });
      if (capabilities.get(token) !== cap || cap.expires <= Date.now()) return json(res, 401, { error: 'run capability revoked' });
      // Avoid accidentally accepting an agent's effectively unbounded output budget.
      if (input.max_completion_tokens !== undefined) {
        input.max_completion_tokens = Math.max(1, Math.min(8192, Number(input.max_completion_tokens) || 8192));
      } else {
        input.max_tokens = Math.max(1, Math.min(8192, Number(input.max_tokens) || 8192));
      }
      const controller = new AbortController();
      active.set(controller, cap.runId);
      const timer = setTimeout(() => controller.abort(), 240_000);
      res.on('close', () => { if (!res.writableEnded) controller.abort(); });
      try {
        const upstream = await upstreamFetch(config.upstream + '/chat/completions', {
          method: 'POST', redirect: 'error', signal: controller.signal,
          headers: { Authorization: 'Bearer ' + config.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify(input),
        });
        if (!upstream.ok) {
          // Do not forward upstream diagnostics that could contain request data.
          await upstream.body?.cancel();
          return json(res, upstream.status, { error: { message: `模型服务返回 HTTP ${upstream.status}`, type: 'upstream_error' } });
        }
        res.writeHead(upstream.status, {
          'Content-Type': upstream.headers.get('content-type') || 'application/json',
          'Cache-Control': 'no-store', 'X-Accel-Buffering': 'no',
        });
        if (upstream.body) await pipeline(Readable.fromWeb(upstream.body), res);
        else res.end();
      } catch (error) {
        if (!res.headersSent) json(res, 502, { error: { message: controller.signal.aborted ? '模型请求已取消或超时' : '模型服务连接失败', type: 'upstream_error' } });
        else res.destroy();
      } finally {
        clearTimeout(timer); active.delete(controller);
      }
    } catch {
      if (!res.headersSent) json(res, 400, { error: 'invalid request' });
      else res.destroy();
    }
  });
  server.requestTimeout = 300_000;
  server.headersTimeout = 15_000;
  const cleanup = setInterval(() => {
    for (const [token, cap] of capabilities) if (cap.expires <= Date.now()) {
      revoke(cap.runId); capabilities.delete(token);
    }
  }, 30_000);
  cleanup.unref();
  server.on('close', () => clearInterval(cleanup));
  return server;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const env = process.env;
  if (!env.NOVEX_MODEL_API_KEY || !env.NOVEX_RUNNER_SECRET) throw new Error('Model gateway credentials are required');
  const upstream = env.NOVEX_MODEL_BASE_URL || 'https://api.b.ai/v1';
  const parsed = new URL(upstream);
  if (parsed.protocol !== 'https:' || parsed.hostname !== 'api.b.ai' || parsed.pathname.replace(/\/$/, '') !== '/v1') throw new Error('This demo only permits the configured api.b.ai upstream');
  const port = Number(env.NOVEX_MODEL_PROXY_PORT || 8330);
  const server = createModelProxy({
    apiKey: env.NOVEX_MODEL_API_KEY, runnerSecret: env.NOVEX_RUNNER_SECRET,
    model: env.NOVEX_MODEL_NAME || 'deepseek-v4.1-flash', upstream: upstream.replace(/\/$/, ''),
    guestBaseUrl: env.NOVEX_MODEL_GUEST_URL || `http://host.docker.internal:${port}/v1`,
  });
  server.listen(port, env.NOVEX_MODEL_PROXY_HOST || '0.0.0.0', () => console.log(`Novex model gateway listening on port ${port}; per-run capabilities required.`));
}
