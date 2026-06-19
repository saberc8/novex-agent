# RBAC ToC Quick Start Template Design

## Goal

Upgrade the repository from a Go + Vue admin RBAC template into a three-part quick-start template:

- `backend`: Go + Gin API service.
- `admin`: Vue 3 PC admin console with RBAC.
- `client`: React H5 consumer client.

The template must keep the PC admin user system and the H5 consumer user system separate, while reusing the existing client configuration concept for login client metadata.

## Current Baseline

- The existing backend directory is `backend`.
- The existing admin directory is `admin`.
- Backend RBAC middleware and system-record guards already exist in the working tree.
- Backend `go test ./...` passes.
- Admin `vue-tsc --noEmit` passes.
- `sys_client` exists as a configurable login-client table for client ID, client type, auth types, status, and timeout settings.

## Architecture

### Directory Layout

- Rename `backend` to `backend`.
- Rename `admin` to `admin`.
- Add a new `client` Vite app using React, TypeScript, and Ant Design Mobile.

### Backend Boundaries

The backend keeps two separate authentication surfaces:

- Admin APIs use `/auth/*`, `/system/*`, `/monitor/*`, `/common/*`, `sys_user`, `sys_role`, `sys_menu`, and RBAC middleware.
- H5 APIs use `/client/*`, `client_user`, and a client-user authentication middleware.

`sys_client` is reused as client metadata. It must include a seeded H5 client row, but it must not store H5 users.

JWTs must include enough subject information to prevent cross-use:

- Admin tokens are accepted by admin RBAC middleware only.
- Client tokens are accepted by client middleware only.
- A client token must not access `/system/*`, `/monitor/*`, `/auth/user/*`, or other admin-protected APIs.

### H5 User System

Create a `client_user` table for consumer users. The first version supports:

- phone number as the login identifier;
- password authentication;
- nickname;
- avatar URL;
- status;
- created and updated timestamps.

The H5 auth API supports:

- `POST /client/auth/register`
- `POST /client/auth/login`
- `POST /client/auth/logout`
- `GET /client/user/profile`

Password hashing reuses the backend password hasher/verifier. Token generation reuses the JWT infrastructure, extended with subject type.

### Client App

The H5 app is a real starter app, not a landing page. It uses React mobile components and avoids native browser-only UI primitives for primary controls.

Required first version:

- React + Vite + TypeScript.
- Ant Design Mobile components.
- TabBar with `Home` and `Mine`.
- Login page with phone and password.
- Register page with phone, password, and nickname.
- Authenticated profile card in `Mine`.
- Separate `client_token` storage key.
- API base config compatible with local backend proxy.

### Admin App

The Vue admin app remains the PC RBAC console. Its login continues to use the existing PC client ID and admin auth API.

After renaming to `admin`, docs, env examples, and verification commands must reference the new directory name.

## RBAC Review Scope

After implementation, run a focused RBAC vulnerability review covering:

- route-to-permission coverage for all registered protected admin routes;
- admin versus H5 token separation;
- public route exposure;
- system-record mutation guards;
- super-admin bypass behavior;
- obvious privilege escalation paths in user, role, menu, and client configuration handlers.

The review should report concrete findings with file and line references. Fix critical implementation bugs found during this work if they are directly in scope.

## Verification

Required checks:

- `go test ./...` in `backend`.
- `pnpm typecheck` and `pnpm build` in `admin`.
- `pnpm typecheck` and `pnpm build` in `client`.
- Directory-name references in README and local commands point to `backend`, `admin`, and `client`.

## Decisions

- The H5 client reuses `sys_client` only for client configuration.
- The H5 user system uses a new `client_user` table.
- The first H5 credential method is phone + password.
- The H5 UI uses Ant Design Mobile.
- The H5 API prefix is `/client`.
