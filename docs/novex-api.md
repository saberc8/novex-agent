# Novex demo API contract

Base `/api/novex`. JSON success follows existing template: `{ "code":"200", "success":true, "data":..., "msg":"操作成功" }`; errors use real HTTP status and `{code:"400|401|403|409|500",success:false,msg:"...",data:null}`. Auth header `Authorization: Bearer <token>`. All ids are JSON strings except `userId`, `roleId`, `policyVersion` (numbers). List endpoints return arrays (not `{list,total}`). JSON camelCase. Never send database passwords back.

## Auth and chat (same sys_user + sys_user_role)

- `POST /login` `{username,password}` → `{token,user:{id,username,nickname,tenantId,departmentId,isAdmin,globalDataAccess,roles:[{id,name,code}],policyVersion}}`.
- `GET /me` → same `user` object.
- `POST /logout` → `{loggedOut:true}`, revokes the current Redis-backed session.
- `GET /runtimes` → `[{id:"pi",name:"Pi",available:true},{id:"dsh",name:"DeepSeek Harness",available:true}]` (availability probes runner).
- `GET /conversations` → `[{id,title,runtime,policyVersion,createdAt,locked}]`.
- `POST /conversations` `{title?,runtime:"pi"|"dsh"}` → conversation.
- `GET /conversations/:id/messages` → `[{id,role:"user"|"assistant",content,createdAt,runId?}]`. Policy-changed conversation returns 409; never replay old sensitive history.
- `POST /conversations/:id/messages` `{content}` → `{runId,conversationId,status:"queued"}`; persists user input, starts asynchronous real runner.
- `GET /runs/:id` → `{id,conversationId,status,policyVersion,answer?,error?,createdAt}`; answer only after completion and fresh auth.
- `GET /runs/:id/events` → SSE (use fetch with Authorization, not unauthenticated EventSource): `event: status`, data `{message,status?}`; `event: completed`, data `{text,runId}`; `event: error`, data `{message}`. No sensitive token streaming. Replay includes current terminal state and closes.
- `POST /runs/:id/cancel` `{}` → `{cancelled:true}`.
- `POST /embed-tickets` `{origin:"http://localhost:8202"}` → `{ticket,expiresIn:60}`. Authenticated issuer, explicit allowed embed origin only.
- `POST /embed-exchange` `{ticket,origin}` → login result. One use, expires in 60s, HTTP Origin must be in NOVEX_CHAT_ORIGINS (default localhost/127.0.0.1:8312); body origin is the bound parent origin. Chat must check postMessage event.origin and event.source before exchanging. Never put long-lived token in iframe URL.

## Admin (requires real system admin role)

The ordinary `admin` system role does not imply business or private-file access. Explicit `novex_super_admin` grants global read access to every registered business resource and knowledge document, including across tenants. Only the local `admin` account has been assigned this additional role. `globalDataAccess` is derived from server-loaded roles, never accepted from the client. The role does not enable business writes, arbitrary SQL or another user's conversation history. Built-in account provisioning uses `scripts/enable_global_admin.py`; ordinary role administration retains its existing governance checks.

All below under `/api/novex/admin` unless stated otherwise.

- `GET /overview` → `{datasources,resources,documents,users,runs,policyVersion}`.
- `GET /users` → shared RBAC user array, including `roles`, `novexEnabled`, `systemDeptId`, `tenantId`, and `departmentId`. Users without a Novex business profile remain visible with `novexEnabled:false`; external identities are returned separately by `/identities`.
- `PUT /users/:id/profile` `{tenantId,departmentId}` → explicitly provisions/updates the business profile for an existing `sys_user`. Requires admin and governance plus the `novex:user:enable` route permission. System department and business department are distinct. Tenant changes return 409 while external identity mappings, owned documents, or conversations exist.
- `POST /users` `{username,nickname,password,tenantId,departmentId,roleIds:[number]}` → `{id}`.
- `PUT /users/:id/roles` `{roleIds:[number]}` → `{updated:true}`.
- `GET /roles` → `[{id,name,code}]`.
- `POST /roles` `{name,code}` → `{id}`.

The admin UI links to the original system user/role pages for account creation and role maintenance. The above create-user/create-role endpoints remain compatible with existing integrations; they write the same RBAC tables. Novex administration is registered in `sys_menu` and checked with the existing RBAC middleware in addition to its business governance checks. Chat/MCP authorization remains bound to the authenticated user/run and does not grant administration rights.

- `GET /datasources` → `[{id,name,host,port,database,username,tenantId,sslMode,hasPassword,createdAt}]`.
- `POST /datasources` `{name,host,port:5432,database,username,password,tenantId?:"internal-single-tenant",sslMode:"disable"|"require"|"verify-full"}` → datasource (encrypted password omitted).
- `PUT /datasources/:id` → same body; empty/omitted password keeps existing password.
- `POST /datasources/test` → same connection body; returns `{ok:true,readOnly:true,serverVersion}`. An optional `id` loads saved credentials; explicit fields override.
- `POST /datasources/:id/introspect` `{}` → `{tables:[{schema,name,columns:[{name,dataType,nullable}]}]}`. Also persists allowlisted schema snapshot.
- `GET /resources` → resource array.
- `POST /resources` and `PUT /resources/:id` body `{name,key,datasourceId,schema,table,tenantColumn,ownerColumn?,departmentColumn?,warehouseColumn?,identityRequired:true,columns:["employee_id",...]}` → resource `{id,...body}`. Identifiers must exist in introspected snapshot. tenantColumn may be empty only when datasource.tenantId explicitly binds a single internal tenant; otherwise it is required. owner/dept/warehouse fields map to trusted external identity values.
- `GET /identities` → `[{id,userId,datasourceId,tenantId,externalTenantId,employeeId,departmentId,warehouseIds:["W1"]}]`.
- `POST /identities` and `PUT /identities/:id` → same body (id ignored on create); upsert user+datasource. tenantId must match the user's internal tenant; externalTenantId is the business DB company/tenant identifier. Default externalTenantId=tenantId.
- `GET /grants` → `[{id,roleId,resourceId,scope:"self"|"department"|"warehouse"|"tenant",fields:{column:"plain"|"masked"}}]`.
- `POST /grants` → same body without id. Each grant keeps row scope and fields coupled. No explicit deny.
- `DELETE /grants/:id` → `{deleted:true}`. All auth-impacting mutations increment tenant/global policyVersion and cancel active runs.
- `POST /query/preview` `{userId,resourceId,fields?:["total"],filters?:[{field:"employee_id",op:"eq",value:"E001"}],sort?:{field:"employee_id",direction:"asc"},limit?:20}` → query result with `rows`, `columns`, `resource`, `policyVersion`, `explanation`. Requires `novex_governance`; all business values are redacted to `[可读]` or `***` so preview cannot bypass payroll authorization.
- `GET /audit?limit=50` → `[{id,userId,action,resourceId,allowed,detail,createdAt}]`. No secrets or raw sensitive rows.

Authenticated end-user `GET /resources` returns visible resource metadata only. `POST /query` takes preview body excluding userId; authenticated caller is immutable. Result `{rows:[{...}],columns:[string],resource:{id,key,name},policyVersion,explanation}`. Hidden fields may not be used in filter/sort. Page limits maximum 100. `eq`, `contains`, `gte`, `lte` allowed; no SQL strings.

For the explicit global role, `/resources` includes all registered resources with `scopes:["global"]`, `actor:{accessMode:"global",identityRequired:false}`, `datasourceId`, `datasourceName` and `tenantColumn`. No employee identity is invented. Global queries allow all registered fields plus the registered tenant column, with unmasked values; use that column to distinguish/filter companies with identical business IDs. The normal read-only transaction, schema validation, parameter binding, timeout, limit and `hasMore` protections still apply.

## Knowledge

- `GET /knowledge` → authorized `[{id,title,fileName,kind,scope,ownerId,tenantId,departmentId,createdAt,characters,sheetCount?,rowCount?,warnings?}]`. `kind` is `markdown` or `spreadsheet`; this list does not fetch full Markdown or workbook cells. The explicit global data role sees all documents without changing their original ACL; list/detail/search, native workspaces, spreadsheet queries and original downloads use the same global-read capability.
- `POST /knowledge` multipart: `file`, `title?`, `scope=self|department|tenant` → document metadata. Authenticated user may upload self; admin may select wider scope. MD/TXT/DOCX/text PDF up to 16 MiB become Markdown. XLSX/UTF-8 CSV up to 32 MiB retain their original bytes and are indexed as structured rows, never flattened to Markdown. Workbook limits: 200,000 data rows, 2,000,000 populated/formula cells, 128 columns, 32 sheets, 256 MiB expanded ZIP. First non-empty row is the header. Formulas/macros are never executed; saved formula values are used and missing caches generate warnings. Legacy XLS and XLSM are unsupported.
- `GET /knowledge/:id` → authorized metadata plus `{markdown}` for text, or `{sheets:[{name,headerRow,rowCount,columnCount,columns:[{key:"A",label:"SKU",type:"string|number|date|boolean|mixed"}]}]}` for a workbook. Data row counts exclude headers.
- `POST /knowledge/:id/search` `{sheet?,query?,filters?:[{column:"A",op:"eq|contains|gte|lte",value}],limit?:20,offset?:0}` → `{documentId,sheet,columns,rows:[{sheet,row,cells:{A:...},formulas:{F:"=..."},citation}],hasMore,warnings}`. Original Excel row numbers and cell ranges are preserved. Column filters require a sheet, size comparisons require numeric columns; CSV values remain text to preserve identifiers such as `0012`. Max 100 rows, 200,000 offset, 8 scalar filters, 200 bytes per query/filter string, 64 KiB HTTP request and 512 KiB raw cell result. SQL uses parameterized values and registered column keys. Keyword `%`/`_` are literal characters, not SQL wildcards.
- `GET /knowledge/:id/file` → authenticated original workbook/CSV download with `Cache-Control: private, no-store`; no token in the URL. Text originals are still temporary conversion inputs.
- `DELETE /knowledge/:id` → owner or admin; cancels runs and bumps policy.
- `PUT /knowledge/:id/acl` `{scope,departmentId?,ownerId?}` → admin only; tenant immutable; bumps policy.

Workbook ACL applies to the whole file and every sheet, including its original download. It does not inherit row/column grants from external business databases. For employee/department/column isolation within a mixed workbook, import the data into an authorized business resource or split the files into separate ACL units.

## Runner and MCP

Go calls runner `POST ${NOVEX_RUNNER_URL:-http://127.0.0.1:8320}/run`, `Authorization: Bearer ${NOVEX_RUNNER_SECRET}` with `{runId,runtime,prompt,history:[{role,content}],workspacePath,mcpUrl,mcpToken}`. SSE `status {message}`, `completed {text,usage?}`, `error {message}`. Optional delta buffered only. Cancellation `POST /runs/:id/cancel`. Health `GET /health` may return `{ok:true,runtimes:{pi:true,dsh:true}}`.

MCP endpoint `POST /api/novex/mcp`, JSON-RPC 2.0 Streamable HTTP stateless, run-scoped bearer auth, supports initialize, ping, notifications/initialized, tools/list, tools/call. Tools:
- `list_resources {}` (business resource catalog, no arbitrary tables)
- `query_resource {resourceId,fields?,filters?,sort?,limit?}` (same service as REST)
- `search_documents {query,limit?}` (authorized files only)
- `read_document {documentId}`
- `query_spreadsheet {documentId,sheet?,query?,filters?,limit?,offset?}` uses the same authorized structured query as the HTTP endpoint. `search_documents` now searches Markdown/title/Excel cell indexes; `read_document` returns workbook schema instead of its whole content.

`query_resource` includes `hasMore` and `rowLimit`. When `hasMore` is true the returned rows are incomplete, and the Agent must not describe their sum/count as a full company/period statistic. Narrow the filters; arbitrary SQL and full-set aggregation are not provided by this demo.

All tool results use text content containing JSON. Tool authorization or query errors set `isError:true`. Tenant/identity/policyVersion are resolved server-side from immutable run. Runner must not expose MCP token or unrestricted filesystem. Authorized Markdown workspace contains only permitted docs and index. Cross-user native agent sessions must never be reused.
