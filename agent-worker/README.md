# Novex external Agent worker

This service adapts two **real external harnesses**. It does not implement an LLM tool-calling loop.

| Runtime | Locked package | Execution interface |
|---|---|---|
| Pi | `@earendil-works/pi-coding-agent@1.5.0` | Official `createAgentSession()` SDK and native read/grep/find/ls implementations |
| DeepSeek Harness | `@deepseek-ai/dsh@0.2.2-rc.1`, matching SDK client | Official `DeepSeekHarness` SDK, which starts `dsh --profile sdk-minimal` over stdio JSON-RPC |

DeepSeek Harness uses its official `dsh-llm-pi-ai` provider plugin to reach an OpenAI-compatible endpoint. The agent loop, session, tool dispatcher, and lifecycle remain DeepSeek Harness's own. Its direct DeepSeek provider uses a different Messages protocol, so it is deliberately not pointed at a Chat Completions gateway.

All packages and Node 24.22.2 are installed inside this folder. Nothing is installed globally. The root project API owns user/tenant identity, policies, conversation history, authorized Markdown workspaces, MCP data filtering, and audit records.

## Run

```sh
cd agent-worker
npm ci
docker build -t novex-agent-runtime:local .
```

Start the root model proxy first (`scripts/model-proxy.mjs`, port 8330), then start this trusted host supervisor with the project-local Node:

```sh
NOVEX_EXECUTION_MODE=docker \
NOVEX_WORKSPACE_ROOT=/absolute/path/to/novex-data/workspaces \
NOVEX_MCP_ALLOWED_ORIGIN=http://host.docker.internal:8310 \
NOVEX_MODEL_PROXY_URL=http://127.0.0.1:8330 \
./node_modules/.bin/node --env-file=../.env.local src/server.js
```

The workspace root must already exist. The backend supplies one immutable, newly materialized child directory per run. The backend public MCP URL must be reachable from containers, normally `http://host.docker.internal:8310/api/novex/mcp` on Docker Desktop. Use a dedicated Docker network and the appropriate host gateway on Linux. Port 8320 stays on loopback by default; the browser never receives its bearer secret.

For local debugging only, `NOVEX_EXECUTION_MODE=subprocess` starts each harness in its own process, with separate temporary homes. This is **not OS sandboxing**. The native file tool guards still reject paths outside the authorized workspace. Use Docker mode for the demo's isolation guarantee.

## HTTP contract

`GET /health` returns runtime availability/version, execution mode, `osSandbox`, and active run count. Availability identifies installed runtime support; it is not a claim that an upstream model request has just succeeded.

`POST /run` requires `Authorization: Bearer <NOVEX_RUNNER_SECRET>` and JSON:

```json
{
  "runId": "a-server-generated-run-id",
  "runtime": "pi",
  "prompt": "我的工资是多少？",
  "history": [{ "role": "user", "content": "先前对话" }],
  "workspacePath": "/absolute/authorized/workspaces/run-id",
  "mcpUrl": "http://host.docker.internal:8310/api/novex/mcp",
  "mcpToken": "short-lived-run-scoped-token"
}
```

The response is SSE with `status {message}`, `completed {text,usage}`, or `error {code,message,httpStatus?,toolCalls?}`. A heartbeat is a comment frame. No raw model or tool result is streamed to the browser by this layer. The Go service can revalidate policy before releasing `completed.text`. Pi completion waits for the official SDK's `prompt()` to finish and checks the final stop reason. DSH completion waits for the SDK's whole-agent idle result and requires a successful finish reason. A received/queued command is never reported as completion.

Runtime failures are recorded in the host worker log as `agent_failure` with the run ID, runtime, elapsed time, and an allowlisted classification (for example, output limit, tool budget, HTTP failure, connection failure, or unknown runtime error). Provider error bodies, prompts, business values and credentials are never logged. Unknown failures retain the generic classification rather than exposing arbitrary exception text. This improves diagnosis; it does not add a custom Agent loop or silently retry a failed conversation.

`POST /runs/:runId/cancel` requires the same secret. It revokes the model capability, terminates the process/container, and cleans up the run. HTTP disconnects also cancel work. Only validated runtime IDs, histories, workspace paths, and the configured MCP origin are accepted.

## Isolation and authority

Each run is a fresh `docker run --rm`, non-root, with read-only root filesystem, dropped Linux capabilities, no-new-privileges, bounded memory/CPU/PIDs, and a temporary home. **Only that run's authorized workspace is mounted read-only.** The runtime container has no Docker socket, full document store, database connection, server environment, shared CLI home, or another user's session. The trusted host supervisor owns Docker access.

The supervisor obtains a short-lived model capability from the root model proxy and revokes it on completion/cancellation. The real model API key remains in the proxy; it never enters the harness environment, prompts, Docker arguments, or files. DSH receives only the run capability in its provider credential variable. The MCP bearer is a separate run-scoped capability validated by Go on every business request.

Pi gets only wrappers around its native file tools and MCP tools discovered from this run's server. The wrappers resolve real paths and reject traversal or symlink escape. DSH's minimal composition disables shell, terminal, edits/writes, and working-directory mutation; it adds only native `read/glob/grep`, the official MCP plugin, and a pre-execution policy plugin. No agent tool can install a plugin or change configuration. Uploaded Markdown does not participate in trusted skill/config discovery. `skills/customer-service.md` is the platform-owned skill, mounted with the immutable application.

These controls supplement, not replace, backend authorization. Prompts and tool visibility are not permission enforcement. Docker here isolates processes and files; it does not itself implement network egress restrictions. The model proxy pins the model destination, and the runtime receives only one MCP endpoint. For an adversarial production environment, also apply host firewall/network policy to the runner subnet. The demo provides read-only capabilities and omits arbitrary execution tools.

## Configuration

| Variable | Default / purpose |
|---|---|
| `NOVEX_RUNNER_SECRET` | Required control-plane bearer |
| `NOVEX_RUNNER_HOST`, `NOVEX_RUNNER_PORT` | `127.0.0.1`, `8320` |
| `NOVEX_EXECUTION_MODE` | `subprocess` for explicit local development; set `docker` for isolated execution |
| `NOVEX_RUNNER_IMAGE` | `novex-agent-runtime:local` |
| `NOVEX_WORKSPACE_ROOT` | Absolute backend materialization root |
| `NOVEX_MCP_ALLOWED_ORIGIN` | `http://127.0.0.1:8310`; set container-reachable backend origin |
| `NOVEX_MODEL_PROXY_URL` | Host control endpoint `http://127.0.0.1:8330` |
| `NOVEX_MODEL_NAME` | `deepseek-v4.1-flash` |
| `NOVEX_RUN_TIMEOUT_MS` | `180000` |
| `NOVEX_MAX_OUTPUT_TOKENS` | `4096` per model request, also bounded by proxy |
| `NOVEX_MAX_TOOL_CALLS` | `20` per run |
| `NOVEX_MAX_CONCURRENT_RUNS` | `4` |
| `NOVEX_DOCKER_NETWORK` | Optional preconfigured network name |

The model base URL and real key are configured in the root proxy (`NOVEX_MODEL_BASE_URL`, `NOVEX_MODEL_API_KEY`), not in the runner. Keep `.env.local` outside images and source control. Token caps are per-request output caps, not a guaranteed whole-run monetary budget; the proxy additionally bounds request count and expiry.

## Verification

```sh
npm test
./node_modules/.bin/node --env-file=../.env.local test/runtime-smoke.mjs pi dsh
NOVEX_SMOKE_DOCKER=1 ./node_modules/.bin/node --env-file=../.env.local test/runtime-smoke.mjs pi dsh
```

`npm test` checks path traversal, symlink escape, DSH forbidden-tool execution, worker HTTP authentication and safe error diagnostics. The smoke script makes **real model requests** through the proxy. A fixture MCP server records actual `list_resources/query_resource` calls; a temporary Markdown workspace supplies a fictional 7-day leave policy. Success requires a completed harness response with the queried fictional salary and the document's answer. No mock assistant or direct substitute model loop is used. The root integration tests exercise the real RBAC database/MCP service.

Upstream references: [Pi SDK](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sdk.md), [Pi safety](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/security.md), [DSH SDK client](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sdk/client), [DSH minimal profile](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/bundle/sdk-minimal), [DSH gateway adapter](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/llm/llm-pi-ai). DSH is developer preview; the locked version is part of this integration contract.
