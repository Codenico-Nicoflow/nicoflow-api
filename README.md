# nicoflow-api

Go REST API for the Nicoflow platform.

**Base URL:** `https://api.nicoflow.com/v1`  
**WebSocket:** `wss://api.nicoflow.com/v1/ws`

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Local Development (recommended — Docker)](#local-development-recommended--docker)
- [Local Development (no Docker)](#local-development-no-docker)
- [Staging](#staging)
- [Production](#production)
- [Testing](#testing)
- [Migrations](#migrations)
- [Make Targets](#make-targets)
- [Project Structure](#project-structure)
- [API Reference](#api-reference)

---

## Prerequisites

| Tool                    | Install                                      |
| ----------------------- | -------------------------------------------- |
| Go 1.25+                | https://go.dev/dl                            |
| Docker + Docker Compose | https://docs.docker.com/get-docker           |
| Air (hot-reload)        | `go install github.com/air-verse/air@latest` |
| golang-migrate CLI      | `brew install golang-migrate`                |
| golangci-lint           | `brew install golangci-lint`                 |

---

## Local Development (recommended — Docker)

Docker Compose spins up **Postgres 16** and **MinIO** (local S3-compatible storage) for you. You run the API binary directly on your machine with hot-reload via Air. You do **not** need a local Postgres installation.

**Step 1 — Copy the env file**

```bash
cp .env.example .env
```

Open `.env` and fill in the two values that have no usable default:

```bash
JWT_SECRET=any-32-char-random-string-for-local-dev!!
SMTP_DSN=smtp://your-mailtrap-user:your-mailtrap-pass@smtp.mailtrap.io:587
```

Everything else already has working defaults for local dev (ports, DB credentials, MinIO keys).

**Step 2 — Start Postgres + MinIO**

```bash
make docker-up
```

This starts:

- **Postgres 16** on `localhost:5432` — credentials: `nicoflow / nicoflow / nicoflow`
- **MinIO** on `localhost:9000` — S3-compatible object storage; web console at `localhost:9001` (login: `minioadmin / minioadmin`)

**Step 3 — Apply all migrations**

```bash
make docker-migrate-up
```

**Step 4 — Start the API with hot-reload**

```bash
make dev
```

The API is now running at `http://localhost:8080`. Air watches for file changes and automatically rebuilds.

```bash
curl http://localhost:8080/health
# → {"status":"ok","version":"<git_sha>"}
```

**API docs (Swagger):** with the server running (non-production), the auth API is browsable at
`http://localhost:8080/v1/swagger/index.html` (raw spec at `/v1/swagger/doc.json`). Regenerate after
changing handler annotations with `make swagger`.

**Postman:** import `docs/postman/Nicoflow-API.postman_collection.json` (the full API surface) plus
**one** environment for your target:
- `Nicoflow-Local.postman_environment.json` — `http://localhost:8080`
- `Nicoflow-Staging.postman_environment.json` — `https://nicoflow-api-staging.onrender.com`
- `Nicoflow-Production.postman_environment.json` — `https://api.nicoflow.app`

Select the environment top-right, then run **Auth › Login** (it captures the access token into
`{{accessToken}}`) — protected requests then authenticate automatically; create requests auto-capture
`{{areaId}}` / `{{projectId}}` / `{{taskId}}` / `{{subtaskId}}` for chaining. Paste reset/verify tokens
(from the emails) into `{{resetToken}}` / `{{verifyToken}}`. **`baseUrl` must NOT include `/v1`** — the
collection paths already carry it; a `/v1` in `baseUrl` yields `/v1/v1/...` and 404s. Staging/production
envs ship with placeholder creds — fill your own; passwords are `secret`-typed so they aren't persisted.
Folders tagged **⚠ STUB 501** hit not-yet-implemented routes (bucket, search, attachments, ai,
billing, notifications). Keep the collection in sync when endpoints change (see `SPEC.md` §3).

**Tear down**

```bash
make docker-down   # stops containers, data volumes are kept
```

To also wipe the DB and MinIO data (clean slate):

```bash
docker compose down -v
```

---

## Local Development (no Docker)

If you already have Postgres running locally (e.g. via Homebrew), you can skip Docker.

**Step 1 — Create the database**

```bash
createdb nicoflow
```

**Step 2 — Copy and fill env**

```bash
cp .env.example .env
```

Update `DATABASE_URL` to point at your local Postgres:

```bash
DATABASE_URL=postgres://localhost:5432/nicoflow?sslmode=disable
JWT_SECRET=any-32-char-random-string-for-local-dev!!
```

**Step 3 — Apply migrations**

```bash
make migrate-up
```

**Step 4 — Start the API**

```bash
make dev         # hot-reload via Air (recommended)
# or
make build && ./bin/api
```

---

## Staging

Staging runs on **Render** and deploys automatically on every merge to the `staging` branch via GitHub Actions.

**Migrations** are applied as a Render pre-deploy command — no manual step needed. If you ever need to run them manually from your machine:

```bash
DATABASE_URL=<staging-postgres-url> make migrate-up
```

**Branch workflow:**

```
feature/NIC-xxx-description  →  PR to staging  →  review + merge
staging                      →  PR to main     →  release
```

Never push directly to `staging` or `main`.

---

## Production

Production runs on **Render** and deploys automatically on every merge from `staging` → `main`.

All configuration is set as environment variables in the Render dashboard:

| Variable                | Required | Description                                                      |
| ----------------------- | -------- | ---------------------------------------------------------------- |
| `DATABASE_URL`          | Yes      | Render Managed Postgres connection string                        |
| `JWT_SECRET`            | Yes      | HS256 signing secret — min 32 bytes, cryptographically random    |
| `PORT`                  | Auto     | Set by Render — do not override                                  |
| `JWT_EXPIRY`            | No       | Access token TTL (default `15m`)                                 |
| `REFRESH_TOKEN_EXPIRY`  | No       | Refresh token TTL (default `168h`)                               |
| `SMTP_DSN`              | No       | `smtp://user:pass@host:587` — required for password reset emails |
| `REQUIRE_EMAIL_VERIFICATION` | No  | Gate login on a verified email (`true`/`false`, default `false`). **Recommended `true` on both staging and prod** so the reject path (`EMAIL_NOT_VERIFIED`) is exercised before production; leave `false` only in local dev where no SMTP is configured. |
| `APP_BASE_URL`          | No       | Frontend URL for reset links, e.g. `https://app.nicoflow.app`    |
| `CORS_ORIGINS`          | No       | Comma-separated allowed origins, e.g. `https://app.nicoflow.app` |
| `APP_ENV`               | No       | `production`                                                     |
| `LOG_LEVEL`             | No       | `info`                                                           |
| `AWS_REGION`            | No       | S3 region, e.g. `us-east-1`                                      |
| `AWS_ACCESS_KEY_ID`     | No       | IAM key for S3                                                   |
| `AWS_SECRET_ACCESS_KEY` | No       | IAM secret for S3                                                |
| `S3_BUCKET_NAME`        | No       | `nicoflow-attachments`                                           |
| `LS_WEBHOOK_SECRET`     | No       | Lemon Squeezy HMAC webhook secret                                |

Migrations run automatically as a Render pre-deploy command:

```bash
migrate -path migrations -database "$DATABASE_URL" up
```

> Never run `migrate down` or `migrate down-all` against staging or production.

---

## Testing

### Unit tests

Unit tests use mocked dependencies — no database required. Run them any time:

```bash
make test
```

This runs all `*_test.go` files (excluding integration), with the race detector and coverage output to `coverage.out`.

To view coverage in the browser:

```bash
go tool cover -html=coverage.out
```

---

### Integration tests

Integration tests hit a **real PostgreSQL database**. They are gated behind the `integration` build tag and a separate `TEST_DATABASE_URL` env var so they never run accidentally against your dev or staging DB.

**Step 1 — Create a dedicated test database**

If you're using Docker Compose (recommended):

```bash
make docker-up   # starts Postgres on localhost:5432
docker exec -it nicoflow-api-postgres-1 psql -U nicoflow -c "CREATE DATABASE nicoflow_test;"
```

If you're using a local Postgres (Homebrew / native):

```bash
createdb nicoflow_test
```

**Step 2 — Apply migrations to the test database**

Docker Compose Postgres (password is in your `.env` as `POSTGRES_PASSWORD`):

```bash
migrate -path migrations -database "postgres://nicoflow:<POSTGRES_PASSWORD>@localhost:5432/nicoflow_test?sslmode=disable" up
```

Local Postgres (no password, trust auth):

```bash
migrate -path migrations -database "postgres://localhost/nicoflow_test?sslmode=disable" up
```

**Step 3 — Run integration tests**

Docker Compose Postgres:

```bash
TEST_DATABASE_URL="postgres://nicoflow:<POSTGRES_PASSWORD>@localhost:5432/nicoflow_test?sslmode=disable" make test-integration
```

Local Postgres (no password):

```bash
TEST_DATABASE_URL="postgres://localhost/nicoflow_test?sslmode=disable" make test-integration
```

Replace `<POSTGRES_PASSWORD>` with the value of `POSTGRES_PASSWORD` from your `.env` file. URL-encode any special characters (e.g. `$` → `%24`).

Each test truncates the tables it needs before and after running — they are fully isolated and safe to re-run repeatedly.

If `TEST_DATABASE_URL` is not set, integration tests are **skipped** (not failed), so `make test` is always safe to run without a test DB.

---

## Migrations

Migration files live in `migrations/`. Format: `{seq}_{description}.up.sql` / `.down.sql`.

```bash
# Apply all pending migrations (uses DATABASE_URL env var)
make migrate-up

# Roll back ONE step — safe for any environment
make migrate-down

# Roll back ALL steps — dev only, never against staging or prod
make migrate-down-all

# Show current migration version + dirty state
make migrate-version

# Create a new migration pair
make migrate-create name=add_notes_to_tasks
# → creates migrations/017_add_notes_to_tasks.up.sql + .down.sql

# Force-set version when a migration fails mid-way in dev
make migrate-force version=5

# Apply migrations against local Docker Compose Postgres
make docker-migrate-up
```

> **Golden rule:** Never edit a migration file that has already been applied to any environment. Always add a new numbered pair.

### How `DATABASE_URL` is resolved

The `migrate-*` targets pick up `DATABASE_URL` differently per environment — you don't have to think about it, but here's the model:

| Environment      | Source of `DATABASE_URL`                        | How migrations run                                  |
| ---------------- | ----------------------------------------------- | --------------------------------------------------- |
| **Local**        | Read from `.env` at recipe time                 | `make migrate-up` (just works)                      |
| **Staging/Prod** | Injected by Render as a real env var            | **Automatic** Render pre-deploy command (no manual step) |

- **Local:** the Makefile reads `DATABASE_URL` straight out of `.env` *without* `include`/`source`, so a literal `$` in the local DB password survives un-mangled. An already-exported env var still wins over `.env`.
- **Staging/Prod:** Render sets `DATABASE_URL` in the environment, so it takes precedence and `.env` is never consulted (it doesn't exist on Render). Migrations apply automatically via the Render **pre-deploy command**:
  ```bash
  migrate -path migrations -database "$DATABASE_URL" up
  ```
  Use `sslmode=require` for the managed Postgres URL. To run a migration against staging/prod manually from your machine, pass the URL inline:
  ```bash
  DATABASE_URL='<render-staging-or-prod-url>' make migrate-up
  ```

> **Production strategy:** there is **no separate prod migration step to wire up** — it's the same `up` command, just with Render's injected `DATABASE_URL`. Never run `make migrate-down` / `migrate-down-all` against staging or production (roll forward with a new migration instead).
>
> **Gotcha:** if you `source .env` in a plain shell, a raw `$` in the local password breaks it (`too many colons in address`). URL-encode it (`$` → `%24`) or just use the `make migrate-*` targets, which read the value literally.

---

## Make Targets

| Target                         | Description                                            |
| ------------------------------ | ------------------------------------------------------ |
| `make dev`                     | Start API with Air hot-reload                          |
| `make build`                   | Compile binary to `bin/api`                            |
| `make test`                    | Run all tests with race detector + coverage            |
| `make lint`                    | Run golangci-lint                                      |
| `make swagger`                 | Regenerate OpenAPI docs into `docs/` from annotations  |
| `make docker-up`               | Start Postgres + MinIO via Docker Compose              |
| `make docker-down`             | Stop containers (keeps data volumes)                   |
| `make docker-migrate-up`       | Apply migrations against Docker Compose Postgres       |
| `make migrate-up`              | Apply all pending migrations (`DATABASE_URL` required) |
| `make migrate-down`            | Roll back one migration                                |
| `make migrate-version`         | Show current migration version                         |
| `make migrate-create name=x`   | Create a new migration pair                            |
| `make migrate-force version=n` | Force-set migration version (dev only)                 |

---

## Project Structure

```
nicoflow-api/
├── cmd/api/main.go          # Entrypoint — load config, init DB, wire deps, start server
│
├── internal/
│   ├── config/              # Typed config loaded from env vars at startup
│   ├── db/                  # pgxpool connection setup
│   ├── apperror/            # AppError type + typed error code constants
│   │
│   ├── handler/
│   │   ├── router.go        # Chi router — all routes registered here
│   │   └── health.go        # GET /health
│   │
│   ├── domain/              # Business logic grouped by domain (handler + service + repository + types per domain)
│   │   ├── auth/            # IMPLEMENTED — register, login, logout, refresh, forgot/reset password, profile
│   │   ├── area/            # stub
│   │   ├── project/         # stub
│   │   ├── task/            # stub
│   │   ├── bucket/          # stub
│   │   ├── ai/              # stub
│   │   └── billing/         # stub
│   │
│   ├── middleware/
│   │   ├── auth.go          # JWT validation → userID + email + plan into context
│   │   ├── cors.go          # CORS with credentials, Vary, preflight cache
│   │   ├── logger.go        # zerolog structured request logging
│   │   ├── ratelimit.go     # IP + user token-bucket rate limiters
│   │   ├── recover.go       # panic → 500
│   │   └── request_id.go    # X-Request-ID header injection
│   │
│   ├── storage/             # S3 presigned URL client (stub)
│   └── ws/                  # WebSocket hub + client (stub)
│
├── pkg/
│   ├── respond/             # respond.JSON / respond.Error envelope helpers
│   ├── jwtutil/             # JWT HS256 Issue / Parse
│   ├── hashutil/            # bcrypt Hash / Compare (cost 12)
│   └── emailutil/           # SendPasswordReset via stdlib net/smtp
│
├── migrations/              # golang-migrate SQL files (001–016)
├── .github/workflows/       # CI — lint, test, security scan, build, docker build
├── docker-compose.yml       # Local dev stack (Postgres + MinIO + API)
├── Dockerfile               # Distroless production image
├── Makefile
├── .air.toml                # Air hot-reload config
└── .golangci.yml
```

---

## API Reference

### Public endpoints (no auth required)

```
GET  /health
POST /v1/auth/register
POST /v1/auth/login
POST /v1/auth/refresh-token
POST /v1/auth/logout
POST /v1/auth/forgot-password
POST /v1/auth/reset-password
POST /v1/auth/biometric/verify    (501 — reserved for FIDO2/WebAuthn v2)
POST /v1/billing/webhook
```

### Protected endpoints (`Authorization: Bearer <token>`)

```
GET  /v1/auth/logout-all
POST /v1/auth/biometric/register  (501 — reserved)
POST /v1/auth/push-token

GET    /v1/users/me
PATCH  /v1/users/me
DELETE /v1/users/me

GET    /v1/areas
POST   /v1/areas
PUT    /v1/areas/:id
DELETE /v1/areas/:id

GET    /v1/projects
POST   /v1/projects
PUT    /v1/projects/:id
DELETE /v1/projects/:id

GET    /v1/tasks
POST   /v1/tasks
PUT    /v1/tasks/:id
DELETE /v1/tasks/:id

GET    /v1/ws
```

### Response envelope

Every response — success or error — uses this shape:

```json
{ "data": { ... }, "error": null }
{ "data": null, "error": { "code": "RESOURCE_NOT_FOUND", "message": "..." } }
```

### Auth flow

```
1. Register / Login
   POST /v1/auth/register  or  POST /v1/auth/login
   ← { token, refreshToken, user }
   ← Set-Cookie: refresh_token=<value>; HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token

2. Use the API
   All protected routes require:  Authorization: Bearer <token>
   Access token expires in 15 minutes.

3. Refresh the access token (browser sends cookie automatically)
   POST /v1/auth/refresh-token
   ← { token, refreshToken, user }
   Old refresh token is deleted — reuse of a rotated token triggers full session revocation.

4. Logout (current device)
   POST /v1/auth/logout
   ← 204 No Content, cookie cleared

5. Logout (all devices)
   GET /v1/auth/logout-all
   ← 204 No Content, all refresh tokens for the user are deleted
```

### Error codes

| HTTP | Code                   | Meaning                                               |
| ---- | ---------------------- | ----------------------------------------------------- |
| 400  | `INVALID_INPUT`        | Validation failure                                    |
| 400  | `INVALID_EMAIL`        | Malformed email address                               |
| 400  | `WEAK_PASSWORD`        | Password does not meet strength requirements          |
| 401  | `UNAUTHORIZED`         | Authentication required or credentials invalid        |
| 401  | `INVALID_TOKEN`        | JWT or refresh token is missing, expired, or tampered |
| 403  | `FORBIDDEN`            | Authenticated but not authorised                      |
| 403  | `PLAN_LIMIT_EXCEEDED`  | Free tier limit reached                               |
| 404  | `RESOURCE_NOT_FOUND`   | Generic not found                                     |
| 409  | `EMAIL_ALREADY_EXISTS` | Email is already registered                           |
| 429  | `RATE_LIMITED`         | Too many requests — check `Retry-After` header        |
| 500  | `DATABASE_ERROR`       | Unexpected server error                               |

---

## Useful Links

- [Confluence Space](https://nicoflow.atlassian.net/wiki/spaces/NI)
- [Jira Board](https://nicoflow.atlassian.net/jira/software/projects/NIC/boards/2)
