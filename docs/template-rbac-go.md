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

The backend defaults to port `8201`. PostgreSQL and Redis must both be
available: Redis stores revocable JWT sessions, idle-timeout state, login
failure counters, the cross-instance online-user view, and pending imports.

Use your existing Docker PostgreSQL, Redis, and MinIO services. This template
does not require building a new Docker image for local development.

Common backend environment variables:

```env
HTTP_PORT=8201
APP_ENV=development
HTTP_CORS_ALLOWED_ORIGINS=http://localhost:8202,http://127.0.0.1:8202,http://localhost:8203,http://127.0.0.1:8203
HTTP_MAX_REQUEST_BODY_BYTES=33554432
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PWD=postgres
DB_NAME=avalon
REDIS_HOST=localhost
REDIS_PORT=6379
FILE_STORAGE_DIR=./data/file
FILE_BASE_URL=/file
AUTH_JWT_SECRET=replace-with-at-least-32-random-characters
AUTH_RSA_PRIVATE_KEY=base64-pkcs8-private-key
BOOTSTRAP_ADMIN_PASSWORD=replace-this-on-first-start
```

`AUTH_RSA_PRIVATE_KEY` is always required and must be at least 2048 bits.
`AUTH_JWT_SECRET` and `BOOTSTRAP_ADMIN_PASSWORD` are mandatory in production;
the bootstrap password is only used when the initial `admin` row is absent.
Generate a matching RSA pair, then put the private value in the backend
environment and the public value in `VITE_RSA_PUBLIC_KEY` when building the
admin frontend:

```bash
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out rsa-private.pem
openssl pkcs8 -topk8 -nocrypt -in rsa-private.pem -outform DER | openssl base64 -A
openssl pkey -in rsa-private.pem -pubout -outform DER | openssl base64 -A
```

Do not commit the generated private key or production secrets.

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
VITE_RSA_PUBLIC_KEY=base64-subject-public-key-info
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

For local development only, if `BOOTSTRAP_ADMIN_PASSWORD` is omitted while the
initial database is created, the bootstrap credentials are `admin` /
`admin123`. Production startup refuses to create that account without an
explicit bootstrap password.

## User Import

The admin user-import screen downloads and accepts CSV. Parsing validates the
configured password policy and duplicate fields, stores the pending data in
Redis for 15 minutes, and applies the chosen duplicate strategy in one database
transaction. Use the downloaded template rather than an XLS/XLSX file.

## Default H5 Account

Use the H5 registration page to create a `client_user` account. The default H5
client ID is seeded as `h5-client`.
