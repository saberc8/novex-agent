# Template RBAC Go + Vue3

This repository is a trimmed Go + Vue3 import from
[saberc8/avalon-template-ex](https://github.com/saberc8/avalon-template-ex).

## Project Layout

- `backend-go`: Go + Gin backend service.
- `pc-admin-vue3`: Vue3 admin frontend based on Vite and Arco Design.

## Backend

```bash
cd backend-go
go run ./cmd/admin
```

The backend defaults to port `4398` and reads database/auth settings from
environment variables such as `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PWD`,
`DB_NAME`, `AUTH_RSA_PRIVATE_KEY`, and `AUTH_JWT_SECRET`.

## Frontend

```bash
cd pc-admin-vue3
pnpm install
pnpm dev
```

The frontend defaults to port `4399`. Configure the API endpoint in
`pc-admin-vue3/.env.development`, for example:

```env
VITE_API_BASE_URL=http://localhost:4398
```

## Default Account

- Username: `admin`
- Password: `admin123`
