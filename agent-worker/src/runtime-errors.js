// Keep operational diagnostics useful without logging provider text, prompts,
// tool results, credentials, or arbitrary error properties.
const messages = Object.freeze({
  OUTPUT_LIMIT: 'Agent answer exceeded the output limit',
  TOOL_BUDGET: 'Agent reached the tool call limit',
  MODEL_HTTP_ERROR: 'Model service returned an HTTP error',
  MODEL_CONTEXT_LIMIT: 'Model input exceeded its context limit',
  MODEL_CONNECTION_ERROR: 'Model connection or stream failed',
  INVALID_MODEL_RESPONSE: 'Model response could not be parsed',
  ACCESS_DENIED: 'Agent attempted an unavailable tool or file',
  CANCELLED: 'Agent run was cancelled',
  TIMEOUT: 'Agent run timed out',
  EMPTY_ANSWER: 'Agent returned no final answer',
  RUNTIME_START_ERROR: 'Agent runtime could not start',
  RUNTIME_EXIT: 'Agent runtime exited before completion',
  AGENT_ERROR: 'Agent execution failed',
});

export function safeFailure(value = {}) {
  if (!value || typeof value !== 'object') value = {};
  const code = Object.hasOwn(messages, value.code) ? value.code : 'AGENT_ERROR';
  const result = { code, message: messages[code] };
  if (Number.isInteger(value.httpStatus) && value.httpStatus >= 400 && value.httpStatus <= 599) result.httpStatus = value.httpStatus;
  if (Number.isInteger(value.toolCalls) && value.toolCalls >= 0 && value.toolCalls <= 100000) result.toolCalls = value.toolCalls;
  return result;
}

export function runtimeFailure(error) {
  const text = String(error?.message || '');
  const status = /(?:HTTP\s*|status(?:\s+code)?[\s:=]*)([45]\d{2})\b/i.exec(text);
  let code = 'AGENT_ERROR';
  if (['length', 'max_tokens', 'max_output_tokens'].includes(error?.stopReason) || /(?:output|max_tokens|max tokens).*(?:limit|exceed)|did not complete: length/i.test(text)) code = 'OUTPUT_LIMIT';
  else if (/tool call budget|tool.*call.*limit/i.test(text)) code = 'TOOL_BUDGET';
  else if (error?.stopReason === 'aborted' || error?.name === 'AbortError') code = 'CANCELLED';
  else if (/context.{0,40}(?:limit|exceed|length)|too many (?:input )?tokens/i.test(text)) code = 'MODEL_CONTEXT_LIMIT';
  else if (status) code = 'MODEL_HTTP_ERROR';
  else if (/timeout|timed out/i.test(text)) code = 'TIMEOUT';
  else if (/fetch failed|ECONN|network|socket|terminated|premature|stream.*(?:fail|error|clos)/i.test(text)) code = 'MODEL_CONNECTION_ERROR';
  else if (/access denied|tool.*denied|outside.*workspace/i.test(text)) code = 'ACCESS_DENIED';
  else if (/parse|invalid json|unexpected token|malformed/i.test(text)) code = 'INVALID_MODEL_RESPONSE';
  else if (/no final (?:answer|response)|no response/i.test(text)) code = 'EMPTY_ANSWER';
  return safeFailure({ code, httpStatus: status ? Number(status[1]) : undefined, toolCalls: error?.toolCalls });
}
