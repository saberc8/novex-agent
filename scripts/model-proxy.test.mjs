import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createModelProxy } from './model-proxy.mjs';

test('gateway confines upstream, injects key privately and revokes run capabilities', async () => {
  const seen = [];
  const server = createModelProxy({ runnerSecret: 'control-secret', apiKey: 'provider-secret', model: 'demo-model', upstream: 'https://api.b.ai/v1', guestBaseUrl: 'http://guest/v1' }, async (url, options) => {
    seen.push({ url, ...options });
    return new Response(JSON.stringify({ choices: [{ message: { content: 'OK' } }] }), { headers: { 'Content-Type': 'application/json' } });
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const request = (path, token, input, method = 'POST') => fetch(base + path, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: input ? JSON.stringify(input) : undefined });
  try {
    assert.equal((await request('/internal/tokens', 'wrong', { runId: 'run1' })).status, 401);
    const issued = await (await request('/internal/tokens', 'control-secret', { runId: 'run1' })).json();
    assert.ok(issued.token && !JSON.stringify(issued).includes('provider-secret'));
    assert.equal((await request('/v1/models', issued.token, {})).status, 404);
    assert.equal((await request('/v1/chat/completions', issued.token, { model: 'other', messages: [] })).status, 400);
    const response = await request('/v1/chat/completions', issued.token, { model: 'demo-model', messages: [], max_tokens: 90000 });
    assert.equal(response.status, 200);
    assert.equal((await response.json()).choices[0].message.content, 'OK');
    assert.equal(seen[0].url, 'https://api.b.ai/v1/chat/completions');
    assert.equal(seen[0].headers.Authorization, 'Bearer provider-secret');
    assert.equal(JSON.parse(seen[0].body).max_tokens, 8192);
    await request('/internal/tokens/run1', 'control-secret', null, 'DELETE');
    assert.equal((await request('/v1/chat/completions', issued.token, { model: 'demo-model', messages: [] })).status, 401);
    assert.equal(seen.length, 1);
  } finally { await new Promise(resolve => server.close(resolve)); }
});
