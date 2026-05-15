# CLAUDE.md — nicoflow-api

We're building the backend described in `SPEC.md`. Read §8 (Backend Architecture) for canonical decisions. Read §3 (API Endpoint Reference) for exact contract shapes. Read §4 for error codes.

Keep replies concise. No fluff. For any third-party library, use the DocsExplorer subagent to fetch current docs before writing code.

---

## Branching

- Branch from `staging`. PR to `staging`. `staging` → `main` for releases.
- Branch naming: `NIC-<ticket>-<short-description>` (e.g. `NIC-945-auth-tests`).

---

## Tech Stack

| Layer      | Choice                                                    |
| ---------- | --------------------------------------------------------- |
| Language   | Go (latest stable)                                        |
| Router     | `chi` v5                                                  |
| Database   | PostgreSQL 15 via `pgx/v5` + `pgxpool`                    |
| Migrations | `golang-migrate` — files in `migrations/`                 |
| Auth       | JWT HS256 (15 min) + bcrypt refresh tokens + dual-hash    |
| Email      | stdlib `net/smtp` via `SMTP_DSN` env var (Mailtrap)       |
| Logging    | `zerolog`                                                 |
| Config     | env vars (loaded at startup via `internal/config/`)       |
| Hosting    | Render.com (port `3001` via `PORT` env var)               |

---

## Package Structure (SPEC §8.2.1)

Domain-grouped layout — **currently active**:

```
internal/
├── config/         ← env-based Config struct (JWTExpiry, RefreshTokenExpiry, SMTPDsn, AppBaseURL, …)
├── db/             ← pgxpool setup
├── middleware/     ← auth, cors, ratelimit, logging, requestid, recover
├── domain/
│   ├── auth/       ← handler, service, repository, types (FULLY IMPLEMENTED — E-009)
│   ├── area/       ← stub
│   ├── project/    ← stub
│   ├── task/       ← stub
│   ├── bucket/     ← stub
│   ├── ai/         ← stub
│   └── billing/    ← stub
├── ws/             ← WebSocket hub + client (stub)
├── storage/        ← S3 presigned URL logic (stub)
└── apperror/       ← AppError type + error code constants
pkg/
├── jwtutil/        ← Issue / Parse JWT HS256 (IMPLEMENTED)
├── hashutil/       ← Hash / Compare bcrypt cost 12 (IMPLEMENTED)
├── emailutil/      ← SendPasswordReset via SMTP (IMPLEMENTED)
└── respond/        ← JSON response envelope
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

- JWT HS256, TTL from `JWT_EXPIRY` env (default 15 min), signed with `JWT_SECRET` env var (min 32 bytes).
- JWT Claims: `{ sub: userID (string), email, plan ("free"|"pro"), iss: "nicoflow-api", exp, iat }`. Plan is read from the claim — **no DB call per request**.
- **Refresh tokens — dual-hash pattern:**
  - Generate 32-byte `crypto/rand` → hex string (64 chars) = raw token.
  - `token_fingerprint` = `SHA-256(rawToken)` hex — stored in DB for O(1) lookup.
  - `token_hash` = `bcrypt(rawToken, cost=12)` — stored in DB for tamper verification.
  - Raw token returned to client in `AuthResponse.refreshToken` and as HttpOnly cookie.
  - On refresh: lookup by fingerprint, bcrypt-compare, atomic delete-old/insert-new. 0 rows deleted → reuse detected → revoke all tokens for user.
- **Password reset tokens** — same dual-hash pattern. 1-hour TTL. Marked `used_at` after consumption. Storing a new token purges prior unused tokens for the user.
- Refresh cookie: `HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token; Max-Age=604800`.
- Password change (`reset-password`) revokes all active refresh tokens.
- Soft delete (`DELETE /v1/users/me`) sets `deleted_at` and revokes all refresh tokens.
- Row-level isolation: every repo query that touches user data must filter by `user_id` or `deleted_at IS NULL`.

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

## Per-Endpoint Security Checklist (apply to every story)

Every endpoint — public or protected — must go through this before it ships:

| Check | Detail |
| ----- | ------ |
| **Rate limiting** | Public write endpoints: `r.With(mw.RateLimitIP(n, n)).Post(...)`. Protected: `RateLimitUser` is global already. |
| **Input validation** | Validate before any DB call. Return typed `apperror` code, never raw errors. |
| **No user enumeration** | Login / forgot-password return the same 401/200 regardless of whether the user exists. |
| **SQL injection** | All queries use `pgx.NamedArgs{}` — never string-concatenate SQL. |
| **No raw errors to client** | Always `respond.Error(w, status, apperror.ErrXxx, msg)`. |
| **Context propagation** | `ctx context.Context` is always the first param on every I/O function. |
| **Row-level isolation** | Every repo query that touches user data must include `WHERE user_id = @userID`. |
| **Bcrypt cost** | All passwords and opaque tokens hashed at cost 12. |
| **Dual-hash pattern** | Opaque tokens (refresh, reset): SHA-256 fingerprint for DB lookup + bcrypt hash for verification. |
| **Refresh token rotation** | On every refresh: delete old row, insert new. 0 rows deleted = reuse → revoke all. |
| **Cookie security** | Refresh cookie: `HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token`. |
| **Password change** | Must revoke all active refresh tokens. |
| **Soft delete** | `deleted_at IS NULL` in all user queries. |

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

| Variable               | Required | Description                                                                 |
| ---------------------- | -------- | --------------------------------------------------------------------------- |
| `DATABASE_URL`         | Yes      | PostgreSQL connection string                                                |
| `JWT_SECRET`           | Yes      | HS256 signing secret (min 32 bytes, cryptographically random)               |
| `PORT`                 | Yes      | HTTP port (set by Render automatically, e.g. `3001`)                        |
| `JWT_EXPIRY`           | No       | Access token TTL (default `15m`)                                            |
| `REFRESH_TOKEN_EXPIRY` | No       | Refresh token TTL (default `168h` = 7 days)                                 |
| `SMTP_DSN`             | No       | SMTP connection string for password-reset email, e.g. `smtp://user:pass@smtp.mailtrap.io:587` |
| `APP_BASE_URL`         | No       | Frontend URL for reset-password links, e.g. `https://app.nicoflow.app`     |
| `CORS_ORIGINS`         | No       | Comma-separated allowed CORS origins                                        |
| `APP_ENV`              | No       | `development` \| `staging` \| `production`                                  |
| `LOG_LEVEL`            | No       | `debug` \| `info` \| `warn` \| `error` (default `info`)                     |
| `AWS_REGION`           | No       | S3 region                                                                   |
| `AWS_ACCESS_KEY_ID`    | No       | IAM key for S3                                                              |
| `AWS_SECRET_ACCESS_KEY`| No       | IAM secret for S3                                                           |
| `S3_BUCKET_NAME`       | No       | S3 bucket name (`nicoflow-attachments`)                                     |
| `LS_WEBHOOK_SECRET`    | No       | HMAC-SHA256 secret for Lemon Squeezy webhook                                |

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
