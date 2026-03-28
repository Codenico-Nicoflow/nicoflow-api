# nicoflow-api

Go + Gin REST API for the Nicoflow platform.

**Base URL:** `https://api.nicoflow.com/v1`
**WebSocket:** `wss://api.nicoflow.com/v1/ws`

---

## Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| Framework | Gin |
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

```bash
DB_URL=                  # Postgres connection URL
JWT_SECRET=              # 256-bit HS256 signing secret
LS_WEBHOOK_SECRET=       # Lemon Squeezy HMAC webhook secret
AWS_ACCESS_KEY_ID=       # S3 access
AWS_SECRET_ACCESS_KEY=   # S3 access
S3_BUCKET_NAME=nicoflow-attachments
CORS_ORIGINS=            # Comma-separated allowed origins
APP_ENV=development      # development | staging | production
PORT=8080
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

## Database Migrations

```bash
# Apply all pending migrations
make migrate

# Roll back last migration
make rollback

# Create new migration files
migrate create -ext sql -dir migrations -seq [example_migration_name]
```

Migration files live in `migrations/`. Convention: `{seq}_{description}.up.sql` / `.down.sql`.
**Never modify existing migration files** — always add a new one.

---

## Project Structure

```
nicoflow-api/
├── cmd/
│   └── server/
│       ├── main.go          # Entry point — wires repos → services → handlers → router
│       └── stubs.go         # Nil repo stubs for compilation (replace with pgx impls)
│
├── internal/
│   ├── config/
│   │   └── config.go        # Typed config loaded from env vars
│   │
│   ├── model/               # Shared Go structs (1:1 with DB tables)
│   │   ├── user.go
│   │   ├── refresh_token.go
│   │   ├── area.go
│   │   ├── project.go
│   │   ├── task.go
│   │   ├── subtask.go
│   │   ├── user_plan.go
│   │   ├── webhook_event.go
│   │   ├── ai_session.go
│   │   ├── ai_message.go
│   │   └── ai_usage_monthly.go
│   │
│   ├── repository/          # Data access interfaces (DB only — no business logic)
│   │   ├── user_repo.go
│   │   ├── refresh_token_repo.go
│   │   ├── area_repo.go
│   │   ├── project_repo.go
│   │   ├── task_repo.go
│   │   ├── subtask_repo.go
│   │   ├── user_plan_repo.go
│   │   ├── webhook_event_repo.go
│   │   ├── ai_session_repo.go
│   │   ├── ai_message_repo.go
│   │   └── ai_usage_monthly_repo.go
│   │
│   ├── service/             # Business logic (no HTTP, no *gin.Context)
│   │   ├── auth_service.go
│   │   ├── area_service.go
│   │   ├── project_service.go
│   │   ├── task_service.go
│   │   ├── subtask_service.go
│   │   ├── inbox_service.go
│   │   ├── time_spread_service.go
│   │   ├── user_plan_service.go
│   │   ├── billing_service.go
│   │   ├── attachment_service.go
│   │   ├── ai_session_service.go
│   │   ├── ai_message_service.go
│   │   └── ws_service.go
│   │
│   ├── handler/             # HTTP layer only — bind request, call service, return response
│   │   ├── health.go
│   │   ├── auth.go
│   │   ├── areas.go
│   │   ├── projects.go
│   │   ├── tasks.go
│   │   ├── subtasks.go
│   │   ├── inbox.go
│   │   ├── time_spread.go
│   │   ├── user_plan.go
│   │   ├── billing.go
│   │   ├── attachments.go
│   │   ├── ai_session.go
│   │   ├── ai_message.go
│   │   └── ws.go
│   │
│   ├── middleware/
│   │   ├── request_id.go    # Attach X-Request-ID
│   │   ├── logger.go        # Structured JSON request log
│   │   ├── cors.go          # CORS policy per environment
│   │   ├── auth.go          # JWT validation → inject userID + plan into context
│   │   └── plan_enforcer.go # Free-tier limit enforcement on write routes
│   │
│   ├── response/
│   │   └── response.go      # RespondOK, RespondCreated, RespondError + error code constants
│   │
│   └── ws/
│       ├── hub.go           # Connection registry + broadcast loop
│       └── client.go        # Per-connection read/write pumps
│
├── migrations/              # golang-migrate SQL files
├── api/                     # OpenAPI spec
├── .github/workflows/       # CI — lint + test + security
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
- `gin.Context` — handlers only. Use it for: request body, headers, URL params, writing responses
- `context.Context` — services and repos. Use it for: DB queries, cancellation, timeouts
- Bridge between them: `c.Request.Context()` — pass this from handler into service/repo so DB queries abort if the client disconnects

### SQL
- Always list columns explicitly in SELECT — never `SELECT *`
- Column order in the query must match field order in `rows.Scan(...)` — pgx maps by position not name

---

## Layer Rules

**Non-negotiable.** Violating these makes the codebase unmaintainable.

| Layer | May call | Never |
|---|---|---|
| `handler` | `service` | `repository` directly, `*sql.DB` |
| `service` | `repository` | `*gin.Context`, HTTP status codes |
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
3. `gosec` — security scan

See `.github/workflows/ci-backend.yml`.

---

## Useful Links

- [Confluence Space](https://nicoflow.atlassian.net/wiki/spaces/NI)
- [Database Schema](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/950273)
- [API Design Principles](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/1048577)
- [Security Architecture](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/589846)
- [Jira Board](https://nicoflow.atlassian.net) — project key `NIC`
