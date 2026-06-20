# RBAC Hardening Vue Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the Go + Vue3 quick-start template so backend authorization is enforced centrally, system records are protected, Vue JSX/TSX is removed, and both backend and frontend have a reliable verification path.

**Architecture:** Add a focused Gin auth/RBAC middleware that resolves the current principal once per request, checks active user/roles/permissions against an explicit route permission table, and exposes the principal through context for handlers. Keep existing handlers mostly intact, but replace repeated token parsing with context helpers and add business guards for system records. Convert the small Vue JSX/TSX surface to SFC/`h()` based plain TS and then fix type/build blockers from the current dependency set.

**Tech Stack:** Go 1.x, Gin, PostgreSQL, golang-jwt/jwt/v5, Vue 3, Vite 7, TypeScript 5.9, pnpm 8.

## Global Constraints

- Work in the current repository at `/Users/yusenlin/Avalon/freedom/github/template-rbac-go`.
- Do not create new Docker images; runtime verification may use existing PostgreSQL/Redis/MinIO containers only.
- Remove React/Next/JSX adaptation from the Vue app, including Vue JSX/TSX usage.
- Keep changes scoped to `backend`, `admin`, and project docs.
- Preserve the existing response envelope (`code`, `data`, `msg`, `success`, `timestamp`) unless a caller explicitly expects a raw download.
- Verification must include `go test ./...` and frontend type/build checks.

---

### Task 1: Backend Auth/RBAC Middleware

**Files:**
- Create: `backend/internal/interfaces/http/auth_context.go`
- Create: `backend/internal/interfaces/http/rbac_middleware.go`
- Modify: `backend/internal/infrastructure/security/jwt.go`
- Modify: `backend/cmd/admin/main.go`
- Test: `backend/internal/interfaces/http/rbac_middleware_test.go`

**Interfaces:**
- Produces: `CurrentPrincipal(c *gin.Context) (*Principal, bool)`, `CurrentUserID(c *gin.Context) int64`, `RequireRBAC(db *sql.DB, tokenSvc *security.TokenService, routes []RoutePermission) gin.HandlerFunc`, and `DefaultRoutePermissions() []RoutePermission`.
- Consumes: `security.TokenService.Parse`, `sys_user`, `sys_role`, `sys_user_role`, `sys_role_menu`, `sys_menu`.

- [x] **Step 1: Write failing middleware tests**

Add tests proving:
- unauthenticated `/system/user` returns business `401`;
- disabled users with a valid token return `401`;
- users without the mapped permission return `403`;
- users with the mapped permission pass through;
- role code `admin` bypasses route permission checks.

Run: `go test ./internal/interfaces/http -run TestRequireRBAC -count=1`
Expected: FAIL because `RequireRBAC` and route mapping do not exist.

- [x] **Step 2: Harden JWT parsing**

Change `TokenService.Parse` to call `jwt.ParseWithClaims` with `jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})`.

Run: `go test ./internal/infrastructure/security ./internal/interfaces/http -count=1`
Expected: PASS after implementation.

- [x] **Step 3: Implement principal and middleware**

Implement `Principal` with `UserID`, `Username`, `Roles`, `Permissions`, `IsAdmin`, `DeptID`, and `IsSystem`. Implement token parsing, active user check, active role check, distinct permission loading, and route matching by method + Gin full path. Abort with `Fail(c, "401", "未授权，请重新登录")` or `Fail(c, "403", "没有访问权限")`.

Run: `go test ./internal/interfaces/http -run TestRequireRBAC -count=1`
Expected: PASS.

- [x] **Step 4: Wire middleware**

Register public routes first, then add RBAC middleware before protected routes. Keep `/auth/login`, `/captcha/image`, `/common/dict/option/site`, `/common/dict/:code`, `/file/*`, and optional swagger public; protect `/auth/logout`, `/auth/user/*`, `/common/tree/*`, `/common/dict/user`, `/common/dict/role`, `/common/file`, `/system/*`, and `/monitor/*`.

Run: `go test ./...`
Expected: PASS.

### Task 2: Handler Context And System Record Protection

**Files:**
- Modify: `backend/internal/interfaces/http/*_handler.go`
- Test: `backend/internal/interfaces/http/system_guard_test.go`

**Interfaces:**
- Consumes: `CurrentUserID(c)` and `CurrentPrincipal(c)` from Task 1.
- Produces: consistent system record guards for roles, users, departments, dicts, storage, clients, menus, and options.

- [x] **Step 1: Write failing guard tests**

Add handler-level tests for:
- deleting system role is rejected;
- updating system user role/password by a non-admin is rejected;
- deleting a system department is rejected;
- deleting a system dictionary is rejected.

Run: `go test ./internal/interfaces/http -run 'TestSystem.*Guard' -count=1`
Expected: FAIL where guards are currently missing.

- [x] **Step 2: Replace repeated token parsing**

Change handler `currentUserID` methods to use `CurrentUserID(c)` and keep existing `0` fallback behavior for safety.

Run: `go test ./internal/interfaces/http -count=1`
Expected: PASS.

- [x] **Step 3: Add concrete guards**

Before destructive or privilege-changing writes:
- reject system role delete/update/permission/user assignment unless admin;
- reject system user delete/update/reset/update-role unless admin;
- reject self role escalation/removal mistakes;
- reject system menu/dept/dict/client/storage deletion unless admin where the table has `is_system` or equivalent immutable defaults.

Run: `go test ./internal/interfaces/http -run 'TestSystem.*Guard|TestRequireRBAC' -count=1`
Expected: PASS.

### Task 3: Frontend RBAC Alignment And Vue JSX Removal

**Files:**
- Create: `admin/src/components/GiDot/index.vue`
- Create: `admin/src/components/GiTag/index.vue`
- Delete: `admin/src/components/GiDot/index.tsx`
- Delete: `admin/src/components/GiTag/index.tsx`
- Modify: `admin/config/plugins/index.ts`
- Modify: `admin/config/plugins/components.ts`
- Modify: `admin/tsconfig.json`
- Modify: `admin/package.json`
- Modify: `admin/src/types/components.d.ts`
- Modify: Vue files currently using `<script lang="tsx">`
- Modify: `admin/src/utils/has.ts`
- Modify: `admin/src/directives/permission/hasRole.ts`
- Modify: `admin/src/stores/modules/user.ts`

**Interfaces:**
- Produces: Vue-only components and permission helpers that treat role code `admin` as super admin.

- [x] **Step 1: Confirm current TSX surface**

Run: `rg -n "lang=['\"]tsx|\\.tsx|@vitejs/plugin-vue-jsx|jsxFactory|jsxFragmentFactory|React|@next" admin -g '!node_modules'`
Expected: existing TSX and JSX config are listed.

- [x] **Step 2: Convert components and scripts**

Convert `GiDot` and `GiTag` to `.vue`. Replace TSX render callbacks in Vue files with `h(...)`, slots, or template markup. Remove `@vitejs/plugin-vue-jsx`, `vueJsx()`, component resolver `tsx` extension, and JSX compiler options.

Run: `rg -n "lang=['\"]tsx|\\.tsx|@vitejs/plugin-vue-jsx|jsxFactory|jsxFragmentFactory|React|@next" admin -g '!node_modules' -g '!pnpm-lock.yaml'`
Expected: no source/config matches.

- [x] **Step 3: Align frontend RBAC**

Use `admin` as the super-admin role code, keep `*:*:*` support for backend compatibility, and do not rely on persisted permissions as a backend security boundary.

Run: `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck`
Expected: it may still fail on unrelated existing type issues until Task 4, but TSX/React-related failures should be gone.

### Task 4: Frontend Typecheck And Build Reliability

**Files:**
- Modify: `admin/tsconfig.json`
- Modify: `admin/config/plugins/mock.ts`
- Modify: `admin/src/apis/index.ts`
- Modify: Type and component files reported by `vue-tsc`.
- Modify: `admin/vite.config.ts`
- Modify: `admin/pnpm-lock.yaml`

**Interfaces:**
- Produces: `pnpm typecheck` and `pnpm build` commands that pass under the checked-in dependency set.

- [x] **Step 1: Use the current failure list as regression baseline**

Run: `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck`
Expected: FAIL with the known list of TypeScript/build blockers.

- [x] **Step 2: Fix configuration and dependency API mismatches**

Use `moduleResolution: "bundler"`, remove unsupported `vite-plugin-mock` options, fix `cron-parser` calls for v5, remove invalid generic download calls, and make Vite terser config compatible.

Run: `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck`
Expected: remaining failures are component/type shape issues only.

- [x] **Step 3: Fix strict TypeScript shape issues**

Tighten local types, provide defaults for optional values, repair API exports, and keep runtime behavior unchanged.

Run: `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck`
Expected: PASS.

- [x] **Step 4: Build frontend**

Run: `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build`
Expected: PASS and `dist/` generated locally.

### Task 5: Docs And End-To-End Verification

**Files:**
- Modify: `README.md`
- Optional Create: `backend/.env.example`
- Optional Create: `admin/.env.local.example`

**Interfaces:**
- Produces: clear startup instructions using existing Docker PostgreSQL/Redis/MinIO services and frontend port `8202`.

- [x] **Step 1: Update quick-start docs**

Document backend env vars, frontend env vars, default ports `8201` and `8202`, existing Docker service assumptions, MinIO bucket setup, and verification commands.

- [x] **Step 2: Run full verification**

Run:
- `go test ./...` in `backend`
- `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck` in `admin`
- `COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build` in `admin`
- optionally start backend/frontend against existing Docker services and probe login/captcha/public/protected endpoints

Expected: all required checks pass; any optional startup gaps are reported with evidence.

## Self-Review

- Spec coverage: backend RBAC, system guards, React/Vue JSX removal, broader issue checks, and startup/build reliability all have tasks.
- Placeholder scan: no TODO/TBD steps remain; each task has concrete files and commands.
- Type consistency: `Principal`, `CurrentUserID`, `RequireRBAC`, and `DefaultRoutePermissions` names are consistent across tasks.
