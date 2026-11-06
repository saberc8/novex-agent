import test from 'node:test';
import assert from 'node:assert/strict';
import { runtimeFailure, safeFailure } from '../src/runtime-errors.js';

test('runtime diagnostics classify failures without retaining provider messages or arbitrary properties', () => {
  const privateText = 'private-salary-and-provider-token';
  for (const [message, expected] of [
    [`HTTP 429 ${privateText}`, 'MODEL_HTTP_ERROR'],
    [`fetch failed: ${privateText}`, 'MODEL_CONNECTION_ERROR'],
    [`context length exceeded ${privateText}`, 'MODEL_CONTEXT_LIMIT'],
    [`Tool call budget exceeded ${privateText}`, 'TOOL_BUDGET'],
    [`Pi returned no final answer ${privateText}`, 'EMPTY_ANSWER'],
    [`unknown ${privateText}`, 'AGENT_ERROR'],
  ]) {
    const result = runtimeFailure(Object.assign(new Error(message), { toolCalls: 3, detail: privateText }));
    assert.equal(result.code, expected);
    assert.equal(result.toolCalls, 3);
    assert.ok(!JSON.stringify(result).includes(privateText));
    assert.equal(result.httpStatus, expected === 'MODEL_HTTP_ERROR' ? 429 : undefined);
  }
  assert.equal(runtimeFailure({ stopReason: 'length', message: privateText }).code, 'OUTPUT_LIMIT');
  assert.equal(runtimeFailure({ stopReason: 'aborted', message: privateText }).code, 'CANCELLED');
  assert.deepEqual(safeFailure({ code: privateText, message: privateText, httpStatus: privateText, toolCalls: -1 }), { code: 'AGENT_ERROR', message: 'Agent execution failed' });
  assert.equal(safeFailure(null).code, 'AGENT_ERROR');
});
