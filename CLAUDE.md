# CLAUDE.md — nicoflow-api

We're building the backend described in `SPEC.md`. Read §8 (Backend Architecture) for canonical decisions. Read §3 (API Endpoint Reference) for exact contract shapes. Read §4 for error codes.

Keep replies concise. No fluff. For any third-party library, use the DocsExplorer subagent to fetch current docs before writing code.

---

## Branching

- Branch from `staging`. PR to `staging`. `staging` → `main` for releases.
- Branch naming: `NIC-<ticket>-<short-description>` (e.g. `NIC-945-auth-tests`).

---

## Tech Stack

| Layer      | Choice                                       |
| ---------- | -------------------------------------------- |
| Language   | Go (latest stable)                           |
| Router     | `gin` → migrating to `chi` (pending refactor) |
| Database   | PostgreSQL 15 via `pgx/v5` + `pgxpool`       |
| Migrations | `golang-migrate` — files in `migrations/`    |
| Auth       | JWT HS256 (15 min) + bcrypt refresh tokens   |
| Logging    | `zerolog`                                    |
| Config     | env vars (loaded at startup)                 |
| Hosting    | Render.com (port `3001` via `PORT` env var)  |

---

## Current Package Structure (pre-refactor flat layout)

The repo currently uses a **flat layout** under `internal/`:

```
internal/
├── config/
├── db/
├── dto/          ← request/response DTOs
├── handler/      ← HTTP handlers (one file per domain)
├── middleware/   ← auth, cors, ratelimit, logging, requestid, recover
├── model/        ← DB models / domain types
├── repository/   ← SQL queries (one file per domain)
├── response/     ← respond.JSON / respond.Error helpers
├── service/      ← business logic (one file per domain)
├── validations/  ← reusable input validation helpers
└── ws/           ← WebSocket hub + client
```

## Target Package Structure (SPEC §8.2.1)

The refactor will move to a **domain-grouped layout**:

```
internal/
├── config/
├── db/
├── middleware/
├── domain/
│   ├── auth/       (handler, service, repository, types)
│   ├── user/
│   ├── area/
│   ├── project/
│   ├── task/
│   ├── bucket/
│   ├── ai/
│   └── billing/
├── ws/
├── storage/        ← S3 presigned URL logic
└── apperror/       ← AppError type + error code constants
pkg/
├── jwtutil/
├── hashutil/
└── respond/
```

---

## Layer Rules

**Handler** — parse & validate request, extract Claims from ctx, call Service, write response via `respond.JSON` / `respond.Error`. No business logic.

**Service** — all business logic, plan limit enforcement, WS event emission. Depends on Repository **interface**, never concrete type.

**Repository** — SQL only via parameterised pgx queries. Returns domain structs or `*apperror.AppError`. Zero business logic. Every user-scoped query must include `AND user_id = $1`.

Dependency direction: Handler → Service interface → Repository interface. Outer layers never import concrete inner types.

---

## Database

- All PKs are `TEXT NOT NULL PRIMARY KEY` (application-generated UUIDs/NanoIDs). Go uses `string`, never `int64`.
- All timestamps are `TIMESTAMPTZ`. Never `TIMESTAMP WITHOUT TIME ZONE`.
- `deleted_at` soft-delete exists **only** on `users`. Everything else uses hard delete with FK cascade/set-null per SPEC §8.1.
- Never modify a deployed migration — always add a new numbered `.up.sql` / `.down.sql` pair.
- `display_order` / `position` use `INT DEFAULT 0`. Sparse ordering is intentional.

### Migration naming

```
migrations/
  001_create_users.up.sql / .down.sql
  002_create_refresh_tokens.up.sql / .down.sql
  ...
```

Run with `make migrate-up` / `make rollback`.

---

## Error Handling

All errors use constants from `internal/apperror/errors.go` (or `pkg/apperror/errors.go` after refactor). Return `*apperror.AppError` from service/repo; handlers convert via `respond.Error(w, err)`.

Standard envelope:
```json
{ "data": null, "error": { "code": "RESOURCE_NOT_FOUND", "message": "..." } }
```

Never return raw Go errors to the client. Never use HTTP status codes directly as the error signal — always pair with a typed code string.

---

## Auth & Security

- JWT HS256, 15-min TTL, signed with `JWT_SECRET` env var (min 32 bytes).
- Refresh tokens: 32-byte crypto/rand → hex string → **bcrypt hash stored** (cost 12). Raw token returned to client; hash stored in DB. Token rotation on every refresh; reuse detection revokes all tokens for the user.
- Refresh cookie: `HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token; Max-Age=604800`.
- JWT Claims: `{ sub: userID (string), email, plan ("free"|"pro"), iss: "nicoflow-api", exp, iat }`.
- Plan is read from the JWT `plan` claim — **no DB call per request**.
- Row-level isolation: every repo query that touches user data must filter by `user_id`.

### Middleware chain order

```
recover → logging → request_id → cors → ratelimit_ip
  ├─ public routes (no JWT): login, register, forgot-password, reset-password, refresh-token, billing/webhook, /v1/ws, /health
  └─ protected routes: auth → ratelimit_user → all /v1/* handlers
```

---

## Rate Limits

| Limiter    | Scope   | Limit        | Burst |
| ---------- | ------- | ------------ | ----- |
| IP-based   | IP      | 100 req/min  | 20    |
| User-based | UserID  | 1000 req/min | 100   |

Auth-specific (stricter per-IP buckets):
- `POST /v1/auth/login` — 10 req/min/IP
- `POST /v1/auth/register` — 5 req/min/IP
- `POST /v1/auth/forgot-password` — 3 req/min/IP

Exceeded → 429 with `RATE_LIMITED` + `Retry-After` header.

---

## Plan Limits (enforced in Service layer)

| Resource     | Free      | Pro       | Error on exceed      |
| ------------ | --------- | --------- | -------------------- |
| Areas        | 3         | Unlimited | `PLAN_LIMIT_EXCEEDED` |
| Projects     | 5 total   | Unlimited | `PLAN_LIMIT_EXCEEDED` |
| AI requests  | 10/month  | Unlimited | `AI_LIMIT_REACHED`   |
| File uploads | 5/task    | 20/task   | `PLAN_LIMIT_EXCEEDED` |

Check via `COUNT(*)` before insert. Plan is read from `ctx` Claims — no DB call.

---

## WebSocket

- Hub is in-process (no Redis in v1). Single Render instance.
- Auth: JWT from `?token=` query param (browsers can't set headers on WS upgrade).
- Invalid JWT → close with code `1008 Policy Violation`.
- Heartbeat: server pings every 30s; pong timeout 10s; write timeout 10s; read timeout 60s.
- Events are full-payload (no diffs), broadcast only to the owning user's connections.
- Services emit events via `hub.BroadcastToUser(userID, event)` after every successful mutation.

---

## Testing

- Table-driven tests for all service + repository layers.
- Use `testutil/` for shared test helpers and DB setup.
- Coverage targets: 90%+ utility functions, 80%+ service layer.
- Never test HTTP status codes in isolation — assert on the `error.code` string in the response body.
- Integration tests must use a real DB (no mocks for the DB layer).

---

## Go Conventions

- No `any` type — ever. Use typed structs or generics.
- All errors must be handled — no blank `_` on error returns.
- Context must flow through every function that does I/O: `ctx context.Context` is always the first param.
- Use `pgx/v5` named arguments (`@param_name`) for all queries — never positional `$1` in complex multi-param queries where it reduces clarity.
- No global state except the DB pool and the WS hub (both wired at startup in `main.go`).
- Interfaces are defined in the **consumer's** package (handler defines `ServiceInterface`; service defines `RepositoryInterface`).

---

## Environment Variables

| Variable                       | Description                                   |
| ------------------------------ | --------------------------------------------- |
| `DATABASE_URL`                 | PostgreSQL connection string                  |
| `JWT_SECRET`                   | HS256 signing secret (min 32 bytes)           |
| `JWT_EXPIRY`                   | e.g. `15m`                                    |
| `REFRESH_TOKEN_EXPIRY`         | e.g. `168h`                                   |
| `ALLOWED_ORIGINS`              | Comma-separated CORS origins                  |
| `AWS_REGION`                   | S3 region                                     |
| `AWS_ACCESS_KEY_ID`            | IAM key                                       |
| `AWS_SECRET_ACCESS_KEY`        | IAM secret                                    |
| `S3_BUCKET`                    | `nicoflow-attachments`                        |
| `LEMON_SQUEEZY_WEBHOOK_SECRET` | HMAC secret for billing webhook               |
| `APP_ENV`                      | `staging` \| `production`                     |
| `PORT`                         | `3001` (set by Render automatically)          |

---

## Makefile Targets

| Target          | Action                             |
| --------------- | ---------------------------------- |
| `make run`      | Start dev server with air (hot reload) |
| `make migrate-up` | Apply all pending migrations     |
| `make rollback` | Roll back one migration step       |
| `make test`     | Run all Go tests                   |
| `make lint`     | Run golangci-lint                  |
| `make build`    | Compile binary to `bin/`           |

---

## Health Check

`GET /health` → `200 OK` with `{"status":"ok","version":"<git_sha>"}`. No auth required.
