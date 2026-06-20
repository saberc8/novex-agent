# Template RBAC Go + Vue3 + React H5

This repository is a trimmed Go + Vue3 import from
[saberc8/avalon-template-ex](https://github.com/saberc8/avalon-template-ex).
It is shaped as a quick-start template for an RBAC admin console plus a ToC H5
client.

## Project Layout

- `backend`: Go + Gin backend service.
- `admin`: Vue3 admin frontend based on Vite and Arco Design.
- `client`: React H5 frontend based on Vite and Ant Design Mobile.

The admin and H5 client use different user systems:

- Admin login uses `/auth/*`, `sys_user`, roles, menus, and RBAC permissions.
- H5 login uses `/client/*` and `client_user`.
- `sys_client` is shared only as login client metadata.

## Backend

```bash
cd backend
go run ./cmd/admin
```

The backend defaults to port `8201` and reads database/auth settings from
environment variables such as `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PWD`,
`DB_NAME`, `AUTH_RSA_PRIVATE_KEY`, and `AUTH_JWT_SECRET`.

Use your existing Docker PostgreSQL, Redis, and MinIO services. This template
does not require building a new Docker image for local development.

Common backend environment variables:

```env
HTTP_PORT=8201
HTTP_CORS_ALLOWED_ORIGINS=http://localhost:8202,http://127.0.0.1:8202,http://localhost:8203,http://127.0.0.1:8203
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PWD=postgres
DB_NAME=avalon
REDIS_HOST=localhost
REDIS_PORT=6379
FILE_STORAGE_DIR=./data/file
FILE_BASE_URL=/file
```

If you use MinIO for file storage, configure the storage row in the admin UI to
point at the existing MinIO endpoint, access key, secret key, bucket, and public
domain.

## Admin Frontend

```bash
cd admin
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 install --ignore-scripts
pnpm dev
```

The frontend defaults to port `8202`. Configure the API endpoint in
`admin/.env.development`, for example:

```env
VITE_API_BASE_URL=http://localhost:8201
VITE_PORT=8202
```

Default admin client ID:

```env
VITE_CLIENT_ID=ef51c9a3e9046c4f2ea45142c8a8344a
```

## H5 Client

```bash
cd client
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 install --ignore-scripts
pnpm dev
```

The H5 client defaults to port `8203` and calls `/client/*` APIs through the
same backend service.

```env
VITE_API_BASE_URL=http://localhost:8201
VITE_PORT=8203
VITE_CLIENT_ID=h5-client
```

## Verification

```bash
cd backend
GOCACHE=/private/tmp/template-rbac-go-gocache go test ./...

cd ../admin
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 lint
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build

cd ../client
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 typecheck
COREPACK_ENABLE_AUTO_PIN=0 corepack pnpm@8.15.9 build
```

The admin frontend is Vue SFC only. React is used only in the H5 `client`.

## Default Admin Account

- Username: `admin`
- Password: `admin123`

## Default H5 Account

Use the H5 registration page to create a `client_user` account. The default H5
client ID is seeded as `h5-client`.
