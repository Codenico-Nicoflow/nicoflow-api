# nicoflow-api

Go REST API for the Nicoflow platform.

**Base URL:** `https://api.nicoflow.com/v1`
**WebSocket:** `wss://api.nicoflow.com/v1/ws`

---

## Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| Router | Chi v5 |
| Database | PostgreSQL 15 (Render Managed) |
| Migrations | golang-migrate |
| File storage | AWS S3 (`nicoflow-attachments`) |
| Billing | Lemon Squeezy |
| Hosting | Render Web Service |

---

## Prerequisites

- Go 1.25+
- PostgreSQL 15
- [Air](https://github.com/air-verse/air) for hot-reload (`go install github.com/air-verse/air@latest`)
- [golangci-lint](https://golangci-lint.run) v2.11.4+
- [golang-migrate CLI](https://github.com/golang-migrate/migrate)

---

## Environment Variables

Copy and fill in before running locally:

Copy `.env.example` to `.env` and fill in secrets:

```bash
DATABASE_URL=            # Postgres connection URL (pgx DSN)
JWT_SECRET=              # HS256 signing secret — min 32 bytes
JWT_EXPIRY=15m
REFRESH_TOKEN_EXPIRY=168h
PORT=8080
APP_ENV=development
LOG_LEVEL=info
CORS_ORIGINS=            # Comma-separated allowed origins
LS_WEBHOOK_SECRET=       # Lemon Squeezy HMAC webhook secret
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=       # S3 access key
AWS_SECRET_ACCESS_KEY=   # S3 secret key
S3_BUCKET_NAME=nicoflow-attachments
```

---

## Running Locally

```bash
# Hot-reload (recommended)
make dev

# Single build + run
make build
./bin/server

# Run tests
make test

# Lint
make lint
```

---

## Local Development with Docker

The full stack (API + Postgres + MinIO) via Docker Compose:

```bash
cp .env.example .env    # fill in secrets
make docker-up          # starts postgres + minio + api on :8080
curl http://localhost:8080/v1/health   # → 200 {"status":"ok"}
make docker-migrate-up  # apply all migrations against docker postgres
make docker-down        # tear down
```

`DATABASE_URL` inside the container is automatically set to `postgres://nicoflow:nicoflow@postgres:5432/nicoflow?sslmode=disable` by docker-compose.yml — no manual editing required.

---

## Database Migrations

```bash
# Apply all pending migrations
make migrate-up

# Roll back ONE step (safe for production)
make migrate-down

# Roll back ALL steps (dev/reset only)
make migrate-down-all

# Show current version + dirty state
make migrate-version

# Create new migration pair
make migrate-create name=add_notes_to_users

# Force-set version (use when a migration fails mid-way in dev)
make migrate-force version=5

# Run against the local Docker Compose postgres
make docker-migrate-up
```

Migration files live in `migrations/`. Convention: `{seq}_{description}.up.sql` / `.down.sql`.

> **Golden Rule: never modify a deployed migration file.**
> Once a migration has been applied to any environment (staging or production), it is immutable.
> Always add a new numbered `.up.sql` / `.down.sql` pair — never edit an existing one.

---

## Project Structure

```
nicoflow-api/
├── cmd/
│   └── api/
│       └── main.go          # Thin entrypoint — load config, init DB, start server
│
├── internal/
│   ├── config/
│   │   └── config.go        # Typed config loaded from env vars
│   │
│   ├── db/                  # pgxpool init
│   │
│   ├── handler/
│   │   ├── health.go        # GET /v1/health
│   │   └── router.go        # Chi router wiring — all routes registered here
│   │
│   ├── domain/              # Business logic grouped by domain
│   │   ├── auth/            # handler.go, service.go, repository.go, types.go
│   │   ├── area/
│   │   ├── project/
│   │   ├── task/
│   │   ├── bucket/
│   │   ├── ai/
│   │   └── billing/
│   │
│   ├── middleware/
│   │   ├── auth.go          # JWT → userID + plan into context
│   │   ├── cors.go
│   │   ├── logger.go        # zerolog structured request log
│   │   ├── plan_enforcer.go
│   │   ├── ratelimit.go     # IP + user token-bucket limiters
│   │   ├── recover.go       # panic → 500
│   │   └── request_id.go    # X-Request-ID injection
│   │
│   ├── apperror/
│   │   └── errors.go        # AppError type + 30 typed error code constants
│   │
│   ├── storage/
│   │   └── s3.go            # S3 presigned URL client (stub until E-024)
│   │
│   ├── ws/
│   │   ├── hub.go
│   │   ├── client.go
│   │   └── events.go        # Typed WS event constants + Event envelope
│   │
│   └── testutil/            # Shared test helpers + DB setup
│
├── pkg/
│   ├── respond/             # JSON envelope helpers (respond.JSON, respond.Error)
│   ├── jwtutil/             # JWT HS256 sign/verify (stub until E-009)
│   └── hashutil/            # bcrypt helpers (stub until E-009)
│
├── migrations/              # golang-migrate SQL files (001–013)
├── .github/workflows/       # CI — lint, test, security, build, docker-build
├── .golangci.yml
├── .air.toml
├── Makefile
├── Dockerfile
└── go.mod
```

---

## Developer Rules of Thumb

### Migrations
- One file per schema change — never edit an existing migration file, always add a new one
- Model + repo + migration always move together: if you add a column, update the struct, update the scan, run the migration
- If the column doesn't exist in the DB yet, any query referencing it will error at runtime

### Logging
- `slog` → application-level events (startup, shutdown, fatal errors — no HTTP request exists yet)
- `middleware.Logger()` → HTTP request/response lifecycle only (method, path, status, latency)
- Don't log inside repos/services — return the error and let it bubble up to the HTTP layer

### Error handling
- `errors.Is(err, pgx.ErrNoRows)` → not found, return `nil, nil` → handler returns 404
- `err != nil` (after the above check) → real DB error → handler returns 500
- Repos wrap errors with `fmt.Errorf("RepoName.Method: %w", err)` and return them — they never log

### Context
- `context.Context` — always the first parameter for any function doing I/O
- Extract from request via `r.Context()` in handlers; propagate down to service and repo
- DB queries abort automatically if the client disconnects

### SQL
- Always list columns explicitly in SELECT — never `SELECT *`
- Column order in the query must match field order in `rows.Scan(...)` — pgx maps by position not name

---

## Layer Rules

**Non-negotiable.** Violating these makes the codebase unmaintainable.

| Layer | May call | Never |
|---|---|---|
| `handler` | `service` | `repository` directly, `*sql.DB` |
| `service` | `repository` | `http.ResponseWriter`, HTTP status codes |
| `repository` | DB only | Business logic, call `service` |
| `middleware` | `service` (sparingly) | Business logic |

---

## API Routes

### Public
```
GET  /v1/health
POST /v1/auth/register
POST /v1/auth/login
POST /v1/auth/refresh
POST /v1/auth/logout
POST /v1/billing/webhook
```

### Authenticated (`Authorization: Bearer <token>`)
```
GET  /v1/ws

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

GET    /v1/tasks/:taskId/subtasks
POST   /v1/tasks/:taskId/subtasks
PUT    /v1/tasks/:taskId/subtasks/:id
DELETE /v1/tasks/:taskId/subtasks/:id

POST   /v1/inbox/capture
GET    /v1/inbox

GET    /v1/time-spread

GET    /v1/user/plan

GET    /v1/ai/sessions
POST   /v1/ai/sessions
DELETE /v1/ai/sessions/:id
GET    /v1/ai/sessions/:sessionId/messages
POST   /v1/ai/sessions/:sessionId/messages

GET    /v1/billing/checkout-url
GET    /v1/billing/portal-url

POST   /v1/attachments/upload-url
POST   /v1/attachments/download-url
```

---

## Response Envelope

Every response uses this shape:

```json
// Success
{ "data": { ... }, "error": null }

// Error
{ "data": null, "error": { "code": "TASK_NOT_FOUND", "message": "..." } }
```

### Error Codes

| HTTP | Code | Meaning |
|---|---|---|
| 400 | `INVALID_INPUT` | Validation failure |
| 401 | `INVALID_TOKEN` | Missing or invalid JWT |
| 401 | `UNAUTHORIZED` | Authentication required |
| 403 | `FORBIDDEN` | Authenticated but not authorized |
| 403 | `PLAN_LIMIT_EXCEEDED` | Free tier limit reached |
| 404 | `RESOURCE_NOT_FOUND` | Generic not found |
| 404 | `TASK_NOT_FOUND` | Task-specific |
| 404 | `PROJECT_NOT_FOUND` | Project-specific |
| 404 | `AREA_NOT_FOUND` | Area-specific |
| 409 | `DUPLICATE_NAME` | Name already exists for this user |
| 409 | `IDEMPOTENCY_CONFLICT` | Webhook already processed |
| 429 | `RATE_LIMITED` | Too many requests |
| 500 | `DATABASE_ERROR` | DB or unexpected server error |

---

## Authentication

- **Algorithm:** HS256 JWT
- **Access token TTL:** 15 minutes
- **Refresh token TTL:** 7 days (single-use rotation — old token deleted on refresh)
- **JWT payload:** `{ userId, plan, exp, iat }`
- **Header:** `Authorization: Bearer <token>`

---

## Plan Enforcement

```
Free tier limits:
  Areas:       max 3
  Projects:    max 5
  AI requests: max 10/month

Enforcement:
  - Plan read from JWT (no extra DB query per request)
  - PlanEnforcer middleware applied per route
  - Pro users bypass immediately
  - Returns 403 PLAN_LIMIT_EXCEEDED
```

---

## Database Tables

| Table | Purpose |
|---|---|
| `users` | Email + bcrypt password hash |
| `refresh_tokens` | Active tokens for rotation/revocation |
| `areas` | Top-level organisation (Work, Health, etc.) |
| `projects` | Containers for tasks, linked to an area |
| `tasks` | Individual tasks (`project_id = NULL` = inbox) |
| `subtasks` | Checklist items within a task |
| `user_plans` | Current plan + Lemon Squeezy subscription |
| `webhook_events` | Idempotency log for LS webhooks |
| `ai_sessions` | AI conversation sessions |
| `ai_messages` | Messages within a session |
| `ai_usage_monthly` | Monthly AI request counter (free tier enforcement) |

Full schema: [Confluence — Database Schema](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/950273)

---

## Branching

```
feature/* → staging (PR)
staging   → main (release)
```

Branch from `staging`. PR to `staging`. `staging` → `main` for releases.

---

## CI

GitHub Actions on every push/PR to `staging` and `main`:

1. `golangci-lint` — static analysis
2. `go test ./... -race` — tests with race detector
3. `gosec` + `govulncheck` — security scans
4. `go build ./...` — binary compilation
5. `docker build` — distroless image smoke test (< 50MB assertion)

See `.github/workflows/ci-backend.yml`.

---

## Useful Links

- [Confluence Space](https://nicoflow.atlassian.net/wiki/spaces/NI)
- [Database Schema](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/950273)
- [API Design Principles](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/1048577)
- [Security Architecture](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/589846)
- [Jira Board](https://nicoflow.atlassian.net) — project key `NIC`
