# RBAC Security Review - 2026-06-19

## Scope

Reviewed the current RBAC and auth surfaces after adding the H5 client:

- backend route registration and middleware order;
- admin RBAC route permission mapping;
- admin token versus H5 client token separation;
- system-record guards;
- frontend permission helpers.

## Fixed During Review

### High: `PUT /system/user/:id` could rewrite roles without `system:user:updateRole`

Before this review, the user update handler accepted `roleIds` and rewrote `sys_user_role` while the route only required `system:user:update`. A user with basic user-edit permission could therefore change role assignments without the dedicated role-assignment permission.

Fix:

- `UpdateUser` now updates profile fields only: `backend/internal/interfaces/http/system_user_handler.go:650`.
- Role mutation remains isolated in `UpdateUserRole`: `backend/internal/interfaces/http/system_user_handler.go:860`.
- Regression test: `backend/internal/interfaces/http/system_guard_test.go:106`.

### Medium: default PC/H5 client configs were mutable by non-admin client managers

The seeded PC admin client and H5 client are login-critical configuration. A non-admin user with `system:client:update` or `system:client:delete` could previously disable or delete them.

Fix:

- Added default client guard: `backend/internal/interfaces/http/system_guard.go:52`.
- Guarded update: `backend/internal/interfaces/http/client_handler.go:373`.
- Guarded delete: `backend/internal/interfaces/http/client_handler.go:440`.
- Regression tests: `backend/internal/interfaces/http/system_guard_test.go:72` and `backend/internal/interfaces/http/system_guard_test.go:92`.

### Medium: seeded menu records were mutable by non-admin menu managers

Seeded `sys_menu` records define the default permission and navigation skeleton. A non-admin user with `system:menu:update` or `system:menu:delete` could previously alter or remove core menu and permission seed data. Backend route checks are hardcoded, so this did not directly bypass API RBAC, but it could break role permission assignment and admin navigation.

Fix:

- Added `sys_menu.is_system` to new and existing databases: `backend/internal/infrastructure/db/migrate.go:245`.
- Seeded template menu IDs are marked as system records: `backend/internal/infrastructure/db/migrate.go:906`.
- Added default menu guard: `backend/internal/interfaces/http/system_guard.go:67`.
- Guarded menu update/delete: `backend/internal/interfaces/http/menu_handler.go:389` and `backend/internal/interfaces/http/menu_handler.go:560`.
- Regression tests: `backend/internal/interfaces/http/system_guard_test.go:108` and `backend/internal/interfaces/http/system_guard_test.go:128`.

## Verified Protections

### Admin/H5 token separation is enforced

- Admin RBAC rejects non-admin token subjects: `backend/internal/interfaces/http/rbac_middleware.go:84`.
- H5 client middleware requires `SubjectClient`: `backend/internal/interfaces/http/client_auth_middleware.go:54`.
- Client protected routes are registered before admin RBAC is applied: `backend/cmd/admin/main.go:136`.
- Regression tests cover both directions: `backend/internal/interfaces/http/rbac_middleware_test.go:112` and `backend/internal/interfaces/http/client_auth_handler_test.go:47`.

### Protected admin route coverage is tested

`TestDefaultRoutePermissionsCoverRegisteredProtectedRoutes` builds registered protected routes and compares them against `DefaultRoutePermissions`, catching missing RBAC mappings: `backend/internal/interfaces/http/rbac_middleware_test.go:134`.

### System user, role, dict, client, and menu guards exist

System record guards block non-admin mutation of immutable seed records: `backend/internal/interfaces/http/system_guard.go:10`.

## Remaining Risks

### Low: Swagger and uploaded files are public

Swagger is registered before RBAC: `backend/cmd/admin/main.go:132`. Static `/file` access is also public: `backend/cmd/admin/main.go:128`. This is acceptable for a quick-start template, but production deployments should either disable Swagger or put it behind admin auth, and should treat `/file` as public-object storage only.

### Low: frontend permission checks are UI-only

The Vue permission directive and helper correctly use `admin` and `*:*:*` for display gating, but these checks are not security boundaries. Backend RBAC remains the source of truth.

References:

- `admin/src/utils/has.ts:3`
- `admin/src/directives/permission/hasPerm.ts:8`
- `admin/src/directives/permission/hasRole.ts:8`

## Verification Commands

Executed after fixes:

```bash
cd backend
GOCACHE=/private/tmp/template-rbac-go-gocache go test ./...

cd ../admin
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build

cd ../client
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build
```
