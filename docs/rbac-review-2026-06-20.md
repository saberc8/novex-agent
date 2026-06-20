# RBAC / Admin / Client Review - 2026-06-20

## Fixed

- Port defaults are now aligned to `backend=8201`, `admin=8202`, `client=8203`.
- Frontend permission literals, seeded menu permissions, and backend `DefaultRoutePermissions()` are aligned. A regression test now fails if seeded permissions do not have a backend RBAC contract.
- Removed stale file permissions `system:file:get` and `system:file:download` from new seeds, and added migration cleanup for existing databases. File detail/download actions now use `system:file:list`, matching the data that the backend actually returns.
- Admin dev mock no longer imports `random` as a named ESM export from the CommonJS `xe-utils` package.
- System operation logs now redact sensitive request/response payload fields and headers such as `password`, `token`, `secret`, `Authorization`, and omit multipart bodies.
- `data_scope_enum` is now seeded so the role data-permission selector has default options in a fresh database.

## Verified

- `backend`: `GOCACHE=/private/tmp/template-rbac-go-gocache go test ./...`
- `admin`: `pnpm typecheck`, `pnpm build`
- `client`: `pnpm typecheck`, `pnpm build`
- Static permission diff: frontend permission codes, seed menu codes, and backend route permission codes have empty diffs.

## Remaining Hardening

- `data_scope` is stored and loaded into `Principal`, but it is not yet applied to business queries. User/dept related list and export APIs should apply department/self/custom scope filters before this is treated as full data-permission RBAC.
- `/file/*` remains a public static route. This is fine for public object storage, but private files need a protected download/preview endpoint and non-public storage URLs.
- Swagger is public in development. For production, disable it or put it behind admin auth.
