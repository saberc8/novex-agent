# RBAC ToC Quick Start Template Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the existing Go/Vue RBAC template to `backend` and `admin`, add a React H5 `client`, and introduce a separate ToC user system that reuses `sys_client` only for client metadata.

**Architecture:** Keep admin authentication and RBAC on `/auth`, `/system`, and `sys_user`. Add `/client` APIs backed by a new `client_user` table and subject-aware JWTs so admin and H5 tokens cannot be used interchangeably. Scaffold the H5 app as a Vite React TypeScript app using Ant Design Mobile.

**Tech Stack:** Go 1.24, Gin, PostgreSQL, JWT HS256, Vue 3 admin, React 18 H5 client, Vite, TypeScript, Ant Design Mobile, pnpm 8.

## Global Constraints

- Work in `/Users/yusenlin/Avalon/freedom/github/template-rbac-go`.
- Preserve existing uncommitted RBAC/admin fixes; do not revert unrelated changes.
- Rename `backend` to `backend` and `admin` to `admin`.
- Use `sys_client` for client metadata only; do not store H5 users in `sys_user`.
- Store H5 users in `client_user`.
- H5 APIs use `/client`.
- H5 UI uses React mobile components from Ant Design Mobile.
- H5 auth token storage key is `client_token`.
- Admin tokens must not authenticate H5 APIs, and H5 tokens must not authenticate admin RBAC APIs.

---

### Task 1: Directory Rename And Reference Update

**Files:**
- Move: `backend/` to `backend/`
- Move: `admin/` to `admin/`
- Modify: `.gitignore`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-06-19-rbac-toc-template-design.md`
- Modify: `docs/superpowers/plans/*.md`

**Interfaces:**
- Produces: repository layout with `backend`, `admin`, and later `client`.
- Consumes: existing Go module path `voc-go-backend` and Vue admin internals unchanged.

- [ ] **Step 1: Verify old directory names exist**

Run: `test -d backend && test -d admin`
Expected: exit code `0`.

- [ ] **Step 2: Rename directories**

Run:

```bash
mv backend backend
mv admin admin
```

Expected: `backend/go.mod` and `admin/package.json` exist.

- [ ] **Step 3: Update path references**

Replace path references in docs and ignore rules:

```text
backend -> backend
admin -> admin
```

Keep historical text only where explicitly describing the pre-rename baseline.

- [ ] **Step 4: Verify no active old-path references remain**

Run: `rg -n "backend|admin" README.md .gitignore docs backend admin -g '!admin/node_modules'`
Expected: only historical baseline notes in the design spec, or no matches.

- [ ] **Step 5: Run baseline checks after rename**

Run:

```bash
cd backend && GOCACHE=/private/tmp/template-rbac-go-gocache go test ./...
cd ../admin && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
```

Expected: both commands exit `0`.

### Task 2: Subject-Aware JWT And Client User Migration

**Files:**
- Modify: `backend/internal/infrastructure/security/jwt.go`
- Modify: `backend/internal/infrastructure/db/migrate.go`
- Test: `backend/internal/infrastructure/security/jwt_test.go`
- Test: `backend/internal/infrastructure/db/migrate_test.go`

**Interfaces:**
- Produces: `SubjectAdmin`, `SubjectClient`, `GenerateForSubject(userID int64, subject string)`, and `Parse` returning claims with `Subject`.
- Produces: `client_user` table and seeded H5 `sys_client` row.
- Consumes: existing `Generate(userID)` as admin-token compatibility wrapper.

- [ ] **Step 1: Write failing JWT subject tests**

Add tests:

```go
func TestTokenServiceGenerateDefaultsToAdminSubject(t *testing.T) {
    svc := NewTokenService("secret", time.Hour)
    token, err := svc.Generate(10)
    if err != nil { t.Fatalf("Generate: %v", err) }
    claims, err := svc.Parse(token)
    if err != nil { t.Fatalf("Parse: %v", err) }
    if claims.Subject != SubjectAdmin { t.Fatalf("subject = %q", claims.Subject) }
}

func TestTokenServiceGenerateForClientSubject(t *testing.T) {
    svc := NewTokenService("secret", time.Hour)
    token, err := svc.GenerateForSubject(20, SubjectClient)
    if err != nil { t.Fatalf("GenerateForSubject: %v", err) }
    claims, err := svc.Parse(token)
    if err != nil { t.Fatalf("Parse: %v", err) }
    if claims.Subject != SubjectClient { t.Fatalf("subject = %q", claims.Subject) }
}
```

Run: `cd backend && go test ./internal/infrastructure/security -run TestTokenService -count=1`
Expected: FAIL because subject constants and methods are missing.

- [ ] **Step 2: Implement subject-aware JWT**

Add constants:

```go
const (
    SubjectAdmin = "admin"
    SubjectClient = "client"
)
```

Add `Subject string json:"subType"` to `Claims`. Keep `Generate(userID)` as `GenerateForSubject(userID, SubjectAdmin)`. Validate unknown subjects in `GenerateForSubject`.

- [ ] **Step 3: Run JWT tests**

Run: `cd backend && go test ./internal/infrastructure/security -run TestTokenService -count=1`
Expected: PASS.

- [ ] **Step 4: Add migration tests for client_user and H5 client seed**

Extend migration SQL recording tests to assert calls include `client_user` and an H5 client seed.

Run: `cd backend && go test ./internal/infrastructure/db -run TestAutoMigrate -count=1`
Expected: FAIL until migration code is added.

- [ ] **Step 5: Implement migration**

Add `ensureClientUser` before `ensureSysClient` returns. Add H5 row to `ensureSysClient` with:

```text
client_id = h5-client
client_type = H5
auth_type = ["ACCOUNT"]
active_timeout = 1800
timeout = 86400
status = 1
```

Add `client_type` dict item `H5`.

- [ ] **Step 6: Run migration tests**

Run: `cd backend && go test ./internal/infrastructure/db -count=1`
Expected: PASS.

### Task 3: Client Auth API

**Files:**
- Create: `backend/internal/interfaces/http/client_auth_handler.go`
- Create: `backend/internal/interfaces/http/client_auth_middleware.go`
- Test: `backend/internal/interfaces/http/client_auth_handler_test.go`
- Test: `backend/internal/interfaces/http/rbac_middleware_test.go`
- Modify: `backend/cmd/admin/main.go`
- Modify: `backend/internal/interfaces/http/rbac_middleware.go`

**Interfaces:**
- Produces: `RegisterClientAuthRoutes(r *gin.Engine)`.
- Produces: `RequireClientAuth(db *sql.DB, tokenSvc *security.TokenService) gin.HandlerFunc`.
- Produces: `CurrentClientUserID(c *gin.Context) int64`.
- Consumes: `client_user`, `sys_client`, `security.BcryptHasher`, `security.BcryptVerifier`, and subject-aware JWTs.

- [ ] **Step 1: Write failing client auth tests**

Add tests that register, login, fetch profile, reject admin token on `/client/user/profile`, and reject client token on `/system/user`.

Run: `cd backend && go test ./internal/interfaces/http -run 'TestClientAuth|TestRequireRBACRejectsClientSubject' -count=1`
Expected: FAIL because client handler/middleware is missing.

- [ ] **Step 2: Reject non-admin subject in RBAC middleware**

In `loadPrincipal`, reject claims where `Subject != "" && Subject != security.SubjectAdmin`.

- [ ] **Step 3: Implement client auth handler**

Implement request/response types:

```go
type clientRegisterReq struct { Phone string; Password string; Nickname string; ClientID string }
type clientLoginReq struct { Phone string; Password string; ClientID string }
type clientLoginResp struct { Token string; UserID int64; Phone string; Nickname string }
type clientProfileResp struct { ID int64; Phone string; Nickname string; Avatar string }
```

Validate active H5 client by `sys_client.client_id`, `client_type='H5'`, `status=1`, and `auth_type` containing `ACCOUNT`.

- [ ] **Step 4: Implement client auth middleware**

Parse token, require `SubjectClient`, load active `client_user`, and store client principal in Gin context.

- [ ] **Step 5: Wire routes**

Register public:

```text
POST /client/auth/register
POST /client/auth/login
POST /client/auth/logout
```

Register protected:

```text
GET /client/user/profile
```

Use `RequireClientAuth` only for protected client routes.

- [ ] **Step 6: Run backend client auth tests**

Run: `cd backend && go test ./internal/interfaces/http -run 'TestClientAuth|TestRequireRBAC' -count=1`
Expected: PASS.

### Task 4: React H5 Client App

**Files:**
- Create: `client/package.json`
- Create: `client/pnpm-lock.yaml`
- Create: `client/index.html`
- Create: `client/vite.config.ts`
- Create: `client/tsconfig.json`
- Create: `client/src/*`
- Modify: `README.md`

**Interfaces:**
- Consumes: `/client/auth/register`, `/client/auth/login`, `/client/auth/logout`, `/client/user/profile`.
- Produces: H5 app with `client_token`, TabBar, login, register, and profile flows.

- [ ] **Step 1: Scaffold dependency manifest**

Create a Vite React TypeScript app using dependencies:

```json
"@vitejs/plugin-react": "^4.3.4",
"vite": "^5.4.11",
"typescript": "^5.6.3",
"react": "^18.3.1",
"react-dom": "^18.3.1",
"antd-mobile": "^5.39.0",
"axios": "^1.7.9"
```

- [ ] **Step 2: Install lockfile**

Run: `cd client && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 install --ignore-scripts`
Expected: `pnpm-lock.yaml` created and install exits `0`.

- [ ] **Step 3: Implement API and auth state**

Create `src/api/http.ts`, `src/api/clientAuth.ts`, and `src/auth/token.ts`. Use `client_token`, attach `Authorization: Bearer <token>`, and clear token on business code `401`.

- [ ] **Step 4: Implement routes and mobile screens**

Create `src/App.tsx` with browser-free internal navigation state:

- unauthenticated users can switch between login and register;
- authenticated users see TabBar;
- `Home` shows quick-start feature tiles;
- `Mine` shows profile and logout.

Use Ant Design Mobile `Form`, `Input`, `Button`, `TabBar`, `NavBar`, `Card`, `Toast`, `Avatar`, and `CapsuleTabs` where appropriate.

- [ ] **Step 5: Style mobile shell**

Create `src/styles.css` with stable mobile dimensions, safe-area padding, and no marketing hero.

- [ ] **Step 6: Verify client app**

Run:

```bash
cd client
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build
```

Expected: both commands exit `0`.

### Task 5: Docs, Full Verification, And RBAC Review

**Files:**
- Modify: `README.md`
- Create: `docs/rbac-review-2026-06-19.md`

**Interfaces:**
- Produces: clear startup commands and a concrete RBAC vulnerability review.

- [ ] **Step 1: Update README**

Document:

```text
backend: go run ./cmd/admin
admin: pnpm dev
client: pnpm dev
ports: backend 4398, admin 5173, client 5174
client H5 clientId: h5-client
admin PC clientId: ef51c9a3e9046c4f2ea45142c8a8344a
```

- [ ] **Step 2: Run full verification**

Run:

```bash
cd backend && GOCACHE=/private/tmp/template-rbac-go-gocache go test ./...
cd ../admin && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build
cd ../client && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck && COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build
```

Expected: all commands exit `0`.

- [ ] **Step 3: Review RBAC vulnerabilities**

Inspect:

- `backend/internal/interfaces/http/rbac_middleware.go`
- `backend/internal/interfaces/http/rbac_middleware_test.go`
- `backend/internal/interfaces/http/system_guard.go`
- `backend/internal/interfaces/http/*_handler.go`
- `backend/cmd/admin/main.go`
- `admin/src/directives/permission/*`
- `admin/src/utils/has.ts`

Write findings to `docs/rbac-review-2026-06-19.md` with severity, file, line, impact, and recommendation.

- [ ] **Step 4: Fix critical in-scope RBAC bugs**

If a critical finding is caused by the current implementation and can be fixed without a new feature, add a failing test, implement the fix, and rerun backend tests.

## Self-Review

- Spec coverage: all directory, backend, H5 client, docs, verification, and RBAC review requirements are mapped to tasks.
- Placeholder scan: no TBD or TODO placeholders are present.
- Type consistency: subject names, API prefixes, `client_user`, and `client_token` are consistent across tasks.
