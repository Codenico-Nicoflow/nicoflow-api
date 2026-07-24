# CLAUDE.md — nicoflow-api

Go REST API + WebSocket for the Nicoflow platform. We're building the backend described in `SPEC.md`. Read §8 (Backend Architecture) for canonical decisions, §3 (API Endpoint Reference) for exact contract shapes, §4 for error codes.

> **Umbrella context:** this repo sits under `../CLAUDE.md` (the Nicoflow workspace root), which owns the **cross-repo contract** with the frontend (`nicoflow-monorepo`). When something here affects the API↔frontend contract (response shape, endpoint, error code, entity field), update the root file and `SPEC.md` §3/§4 too.

Keep replies concise. No fluff. For any third-party library, fetch current docs via **Context7 MCP** before writing code.

---

## Branching

- Branch from `staging`. PR to `staging`. `staging` → `main` for releases.
- Branch naming (unified with the frontend repo): `<type>/NIC-<ticket>-<short-desc>`, `<type>` ∈ `feature | bugfix | hotfix | chore | refactor` (e.g. `feature/NIC-945-auth-tests`). `hotfix/*` branches from `main`; the rest from `staging`. Multiple tickets allowed (`feature/NIC-1073-1074-...`). Note: older branches used bare `NIC-...` with no prefix — new branches use the prefixed form.

---

## Tech Stack

| Layer      | Choice                                                    |
| ---------- | --------------------------------------------------------- |
| Language   | Go 1.26.4                                                 |
| Router     | `chi` v5                                                  |
| Database   | PostgreSQL 16 via `pgx/v5` + `pgxpool`                   |
| Migrations | `golang-migrate` — files in `migrations/`                 |
| Auth       | JWT HS256 (15 min) + bcrypt refresh tokens + dual-hash    |
| Email      | stdlib `net/smtp` via `SMTP_DSN` env var (Mailtrap)       |
| Logging    | `zerolog`                                                 |
| Config     | env vars (loaded at startup via `internal/config/`)       |
| Storage    | AWS S3 in prod; **MinIO** locally via docker compose      |
| Hot reload | `air` (`make dev`)                                        |
| Hosting    | Render.com (port `8080` via `PORT` env var)               |

---

## Package Structure (SPEC §8.2.1)

Domain-grouped layout — **currently active** (verified against the tree):

```
cmd/
└── api/main.go     ← entrypoint; wires config, pool, hub, handlers
internal/
├── config/         ← env-based Config struct (see Environment Variables below)
├── db/             ← pgxpool setup
├── apperror/       ← AppError type + error code constants  (NOTE: internal/, not pkg/)
├── handler/        ← router.go (the chi mux + middleware chain) + health.go
├── middleware/     ← recover, request_id, logger, security_headers, cors,
│                     ratelimit (IP + user), auth, plan_enforcer
├── domain/
│   ├── auth/       ← handler, service, repository, types (most complete; has handler_test + integration_test + service_test)
│   ├── area/       ← handler, service, repository, types (+ service_test)
│   ├── project/    ← handler, service, repository, types (+ service_test)
│   ├── task/       ← handler, service, repository, types (also serves subtasks, attachments, time-spread, search)
│   ├── bucket/     ← handler, service, repository, types
│   ├── ai/         ← handler, service, repository, types (also serves nlp/parse)
│   └── billing/    ← handler, service, repository, types
├── ws/             ← hub.go, client.go, events.go, handler.go, broadcaster.go (LIVE /v1/ws hub — NIC-1587/1588)
├── storage/        ← s3.go (presigned URLs)
└── testutil/       ← db.go (shared test DB helpers)
pkg/
├── jwtutil/        ← Issue / Parse JWT HS256
├── hashutil/       ← Hash / Compare bcrypt cost 12
├── emailutil/      ← SendPasswordReset via SMTP
└── respond/        ← JSON response envelope (respond.JSON / respond.Error)
```

> **Implementation reality:** every domain has handler/service/repository/types wired and routed in `internal/handler/router.go` — none are bare stubs. Test coverage is uneven (auth richest; ai/bucket/billing/task have no `*_test.go` yet). The **WebSocket route is now LIVE** (`/v1/ws` is a real gorilla hub with instant `notification.created` delivery — E-022 / NIC-1587/1588); `ai` and `billing` handlers are still thin. Always confirm a given handler's depth against the code before assuming it's done.

---

## Layer Rules

**Handler** — parse & validate request, extract Claims from ctx, call Service, write response via `respond.JSON` / `respond.Error`. No business logic.

**Service** — all business logic, plan limit enforcement, (future) WS event emission. Depends on Repository **interface**, never the concrete type.

**Repository** — SQL only via parameterised pgx queries. Returns domain structs or `*apperror.AppError`. Zero business logic. Every user-scoped query must filter by `user_id`.

Dependency direction: Handler → Service interface → Repository interface. Interfaces are defined in the **consumer's** package (handler defines its `ServiceInterface`; service defines its `RepositoryInterface`). Outer layers never import concrete inner types.

---

## Database

- All PKs are `TEXT NOT NULL PRIMARY KEY` — **application-generated string IDs** (UUID/NanoID). Go uses `string`, never `int64`. ⚠️ The frontend interfaces currently type these as `number` — that's known drift (see `../CLAUDE.md` §3). IDs over the wire are strings.
- All timestamps are `TIMESTAMPTZ`. Never `TIMESTAMP WITHOUT TIME ZONE`.
- `deleted_at` soft-delete exists **only** on `users` (migration `012_users_soft_delete`). Everything else uses hard delete with FK cascade/set-null per SPEC §8.1.
- Never modify a deployed migration — always add a new numbered `.up.sql` / `.down.sql` pair.
- `display_order` / `sort_order` use `INT DEFAULT 0`. Sparse ordering is intentional.

### Migrations (001–036 applied)

```
001 create_users                     019 enrich_areas_projects
002 create_refresh_tokens            020 email_verification
003 create_areas                     021 users_username_partial_unique
004 create_projects                  022 drop_folder_icon_check
005 create_tasks                     023 users_add_language
006 create_subtasks                  024 projects_area_cascade
007 create_user_plans                025 tasks_energy_aware
008 create_webhook_events            026 tasks_drop_due_date
009 create_ai_sessions               027 projects_area_required
010 create_ai_messages               028 create_bucket
011 create_ai_usage_monthly          029 search_vectors
012 users_soft_delete                030 search_vectors_simple
013 folder_icons_project             031 create_notifications
014 alter_users_add_profile_fields   032 notification_preferences
015 create_password_reset_tokens     033 create_push_subscriptions
016 create_biometric_credentials     034 notification_prefs_families
017 users_login_lockout              035 notification_prefs_reminder_hours
018 users_email_partial_unique       036 create_file_attachments
```
(031–035 are the notification stack: notifications table → preferences → Web Push subscriptions → per-family toggles → reminder-hours. 036 is the E-024 file-attachments table: polymorphic owner `{type,id}` (no FK), `user_id` cascade, unique `s3_key`; quota enforced by the repo's atomic guarded insert — NIC-1638.)

> **027 `projects_area_required`** makes `projects.area_id NOT NULL` — a project must always belong to an area (matches the `Area › Project › Task` hierarchy and 024's cascade). Area-less projects are deleted by the migration. Create/Update source `area_id` from a **user-scoped `SELECT`** so a project can only live in an area the caller owns; a foreign/missing area → `AREA_NOT_FOUND`.

Run with `make migrate-up` / `make migrate-down` (one step) / `make docker-migrate-up` (against the docker Postgres). New pair: `make migrate-create name=<desc>`.

---

## SQL Style — IMPORTANT (matches the actual code)

Repositories use **positional placeholders** (`$1`, `$2`, …) with `pgx`, not named args. `pgx.NamedArgs` appears in only a couple of places. Whichever you use:

- **Never string-concatenate SQL.** Always parameterise.
- Every user-scoped query must include `user_id = $N` (or equivalent) for row-level isolation.
- Soft-deleted users: include `deleted_at IS NULL` where relevant.

(The older revision of this file claimed "always NamedArgs / never positional" — that was wrong. Positional `$N` is the prevailing style.)

---

## Error Handling

All errors use constants from `internal/apperror/errors.go`. Service/repo return `*apperror.AppError{ Code, Message, Status }`; handlers convert via `respond.Error(w, status, code, message)`.

Standard envelope (`pkg/respond`):
```json
// success
{ "data": <T>, "error": null }
// error
{ "data": null, "error": { "code": "RESOURCE_NOT_FOUND", "message": "..." } }
```

`error` is a **structured object** `{ code, message }` — not a string. (The frontend's `ApiEnvelope.error` is still typed `string | null`; that's a contract bug to fix on the frontend, tracked in `../CLAUDE.md` §3.)

Never return raw Go errors to the client. Never use the HTTP status alone as the error signal — always pair it with a typed `code` string from §4.

---

## Auth & Security

- JWT HS256, TTL from `JWT_EXPIRY` (default 15 min), signed with `JWT_SECRET` (min 32 bytes).
- JWT Claims: `{ sub: userID (string), email, plan ("free"|"pro"), iss: "nicoflow-api", exp, iat }`. Plan is read from the claim — **no DB call per request**.
- **Refresh tokens — dual-hash pattern:**
  - 32-byte `crypto/rand` → 64-char hex = raw token (returned to client + HttpOnly cookie).
  - `token_fingerprint = SHA-256(rawToken)` hex — stored for O(1) lookup.
  - `token_hash = bcrypt(rawToken, cost 12)` — stored for tamper verification.
  - On refresh: lookup by fingerprint → bcrypt-compare → atomic delete-old/insert-new. 0 rows deleted ⇒ reuse detected ⇒ revoke all of the user's tokens.
- **Password reset tokens** (`015_create_password_reset_tokens`) — same dual-hash pattern, 1-hour TTL, single-use (`used_at`). Storing a new token purges prior unused tokens for the user.
- Refresh cookie: `HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token; Max-Age=604800`.
- Password change (`reset-password`) revokes all active refresh tokens.
- Soft delete (`DELETE /v1/users/me`) sets `deleted_at` and revokes all refresh tokens.
- **Login lockout** (`017_users_login_lockout`): failed-attempt tracking / lockout fields on `users`.
- **Biometric credentials** (`016_create_biometric_credentials`): supports `POST /v1/auth/biometric/register` + `/v1/biometric/verify` (mobile phase).
- Row-level isolation: every repo query touching user data filters by `user_id` (and `deleted_at IS NULL` for users).

### Middleware chain order (as wired in `internal/handler/router.go`)

```
recover → request_id → logger → security_headers → cors → ratelimit_ip(100,20)
  ├─ public (no JWT): register, login, refresh-token, forgot-password, reset-password,
  │                    biometric/verify, billing/webhook, /v1/ws, /v1/health
  └─ protected:        auth(JWT) → ratelimit_user(1000,100) → all other /v1/* handlers
```

(`plan_enforcer` middleware exists for plan-gating; plan limits are also enforced in the service layer.)

---

## Rate Limits (per `router.go`)

| Limiter             | Scope  | Limit         | Burst |
| ------------------- | ------ | ------------- | ----- |
| IP-based (global)   | IP     | 100 req/min   | 20    |
| User-based (global) | UserID | 1000 req/min  | 100   |

Auth-specific stricter per-IP buckets (via `r.With(mw.RateLimitIP(n, n, trustedProxies))`):
- `POST /v1/register` — 5
- `POST /v1/login` — 10
- `POST /v1/forgot-password` — 3
- `POST /v1/reset-password` — 5

Client IP is resolved through `TRUSTED_PROXY` / `TrustedProxyCIDRs`. Exceeded → 429 with `RATE_LIMITED` + `Retry-After`.

---

## Plan Limits (enforced in Service layer)

| Resource     | Free      | Pro       | Error on exceed       |
| ------------ | --------- | --------- | --------------------- |
| Areas        | 3         | Unlimited | `PLAN_LIMIT_EXCEEDED` |
| Projects     | 5 total   | Unlimited | `PLAN_LIMIT_EXCEEDED` |
| AI requests  | 10/month  | Unlimited | `AI_LIMIT_REACHED`    |
| Attachments  | ❌ (Pro-only write) | 20/owner · 100 MB/user | `PLAN_LIMIT_EXCEEDED` · `STORAGE_LIMIT_EXCEEDED` |
| NLP parse    | ❌        | ✅        | `PLAN_LIMIT_EXCEEDED` |

Check via `COUNT(*)` before insert. Plan is read from `ctx` Claims — no DB call. Downgrade is graceful (excess resources read-only, never deleted). Canonical numbers: `SPEC.md` §5.

---

## WebSocket (E-022 — LIVE, NIC-1587/1588)

`GET /v1/ws?token=<jwt>` is a live gorilla hub (route wired in `router.go`, no stub). **FREE on every plan** — the JWT identifies the user, it does not gate.
- Hub in-process (`internal/ws`, no Redis in v1). Single Render instance. `Hub.BroadcastToUser(userID, ws.Event)` marshals once + fans out to all of a user's connections; a full send buffer drops the slow client (never blocks the hub). `CloseAll` on graceful shutdown.
- Auth: JWT from `?token=` query param (via `pkg/jwtutil`). Invalid/expired → upgrade then close `1008 Policy Violation`. `CheckOrigin` bound to `CORS_ORIGINS`.
- Heartbeat: server pings every 30s; read deadline 60s (missed pong closes); write deadline 10s; inbound frames capped 512B (receive-only — discarded). Multi-connection per user.
- Envelope `ws.Event{ Event, Payload, Timestamp }` — full payloads, no diffs. Event names in `events.go`: `task.created|updated|deleted|status_changed`, `project.*`, `area.*`, `bucket.processed`, `notification.created`.
- **Notification delivery:** `internal/ws/broadcaster.go` (`NotificationBroadcaster`) adapts the hub to `notification.Broadcaster` and is injected into `notification.NewService` in `main.go`. Every `Create` broadcasts `notification.created` (fire-and-forget — a broadcast never fails/blocks the insert). Adapter lives in `ws` (imports `notification`), never the reverse.

## Web Push (E-025 — Pro, NIC-1580)

- `push_subscriptions` table (migration 033, `UNIQUE(user_id, endpoint)`, FK cascade). `POST /v1/notifications/push/subscribe` (Pro-only → `PLAN_LIMIT_EXCEEDED` on free; `422` on missing keys; upsert → `201`) + `DELETE …/subscribe` (idempotent, no plan gate → `204`).
- VAPID via `pkg/pushutil` (`VAPID_PUBLIC_KEY/PRIVATE_KEY/SUBJECT`); any unset ⇒ no-op sender (safe local dev, mirrors `SMTP_DSN`). `NewPushSender` fans a created notification to all of a user's subscriptions, pruning 404/410 (expired) endpoints.

## Notification preferences (E-020/E-025)

`notification_preferences` (migration 032 + 034): `email_digest`, `push_enabled`, `sms_enabled`, `before_due_minutes`, `after_due_minutes`, and the four per-family toggles `overdue_enabled` / `daily_summary_enabled` / `inbox_nudges_enabled` / `streaks_enabled` (all `DEFAULT TRUE`). Lazy upsert; an absent row = all defaults. Each proactive sweep reads its toggle (COALESCE to default) alongside plan before emitting.

---

## Testing

- Table-driven tests for service + repository layers. Real DB for integration (no DB mocks); helpers in `internal/testutil/db.go`. Integration tests are behind the `integration` build tag (`make test-integration`).
- Coverage targets: 90%+ utilities, 80%+ service. Current coverage is uneven — `ai`, `bucket`, `billing`, `task` lack `*_test.go`; closing those is open work (E-018).
- Never assert HTTP status in isolation — assert on the `error.code` string in the body.

---

## Per-Endpoint Security Checklist (apply to every story)

| Check | Detail |
| ----- | ------ |
| **Rate limiting** | Public write endpoints wrap with `r.With(mw.RateLimitIP(n, n, trustedProxies))`. Protected: `RateLimitUser` is global. |
| **Input validation** | Validate before any DB call. Return typed `apperror` code, never raw errors. |
| **No user enumeration** | Login / forgot-password return the same response regardless of whether the user exists. |
| **SQL injection** | Always parameterise (`$N`). Never string-concatenate SQL. |
| **No raw errors to client** | Always `respond.Error(w, status, apperror.ErrXxx, msg)`. |
| **Context propagation** | `ctx context.Context` is always the first param on every I/O function. |
| **Row-level isolation** | Every repo query touching user data filters by `user_id`. |
| **Bcrypt cost** | All passwords and opaque tokens hashed at cost 12. |
| **Dual-hash pattern** | Opaque tokens (refresh, reset): SHA-256 fingerprint for lookup + bcrypt hash for verification. |
| **Refresh token rotation** | On refresh: delete old row, insert new. 0 rows deleted = reuse → revoke all. |
| **Cookie security** | Refresh cookie: `HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh-token`. |
| **Password change** | Must revoke all active refresh tokens. |
| **Soft delete** | `deleted_at IS NULL` in all user queries. |

---

## Go Conventions

- **No `any` type — ever.** Use typed structs or generics.
- All errors must be handled — no blank `_` on error returns.
- `ctx context.Context` is always the first param on every I/O function.
- Parameterised pgx queries only (positional `$N` is the prevailing style).
- No global state except the DB pool and the WS hub (wired at startup in `cmd/api/main.go`).
- Interfaces are defined in the **consumer's** package.

---

## Environment Variables (from `.env.example` / `internal/config`)

| Variable               | Required | Description                                                                 |
| ---------------------- | -------- | --------------------------------------------------------------------------- |
| `DATABASE_URL`         | Yes      | PostgreSQL connection string                                                |
| `JWT_SECRET`           | Yes      | HS256 signing secret (min 32 bytes, cryptographically random)               |
| `PORT`                 | Yes      | HTTP port (Render sets it; **8080** locally)                                |
| `JWT_EXPIRY`           | No       | Access token TTL (default `15m`)                                            |
| `REFRESH_TOKEN_EXPIRY` | No       | Refresh token TTL (default `168h` = 7 days)                                 |
| `APP_ENV`              | No       | `development` \| `staging` \| `production`                                  |
| `LOG_LEVEL`            | No       | `debug` \| `info` \| `warn` \| `error` (default `info`)                     |
| `CORS_ORIGINS`         | No       | Comma-separated allowed CORS origins (e.g. `http://localhost:5173`)         |
| `APP_BASE_URL`         | No       | Frontend URL for reset-password links (e.g. `http://localhost:5173`)        |
| `TRUSTED_PROXY`        | No       | Trusted proxy CIDR(s) for client-IP resolution behind a proxy              |
| `SMTP_DSN`             | No       | SMTP DSN for password-reset email, e.g. `smtp://user:pass@smtp.mailtrap.io:587` |
| `TEST_DATABASE_URL`    | No       | DB connection string for integration tests                                  |
| `LS_WEBHOOK_SECRET`    | No       | HMAC-SHA256 secret for Lemon Squeezy webhook                                |
| `STORAGE_REGION`         | No*      | S3-compatible region. `auto` for Cloudflare R2, a real region for AWS S3, anything for MinIO. *One of the four storage vars — all four (REGION/ACCESS_KEY_ID/SECRET_ACCESS_KEY/BUCKET) must be set to enable file attachments; any unset ⇒ feature disabled (`/attachments*` → typed `503`), never a silent no-op. |
| `STORAGE_ACCESS_KEY_ID`  | No*      | Access key for the object store (R2 / MinIO / S3) (see above)               |
| `STORAGE_SECRET_ACCESS_KEY`| No*    | Secret key (see above)                                                      |
| `STORAGE_BUCKET`         | No*      | Bucket name (`nicoflow-attachments`) (see above)                            |
| `STORAGE_ENDPOINT`       | No       | Object-store endpoint: R2 (`https://<acct>.r2.cloudflarestorage.com`) or MinIO (`http://localhost:9000`). When set, the client uses path-style addressing. Empty ⇒ real AWS S3. |
| `TEST_S3_ENDPOINT`       | No       | MinIO endpoint for the storage integration tests (unset ⇒ those tests skip). Optional `TEST_S3_ACCESS_KEY` / `TEST_S3_SECRET_KEY` / `TEST_S3_BUCKET` / `TEST_S3_REGION` override the defaults. |

> **Storage backend:** local/CI = MinIO, staging/prod = **Cloudflare R2** (S3-compatible, zero egress). The client is vendor-neutral S3 (`aws-sdk-go-v2`); the backend is chosen purely by these env vars. See E-024 PRD for the R2 POST-policy compat caveat.

Docker-compose-only vars (`POSTGRES_*`, `MINIO_*`) are **not** read by the API binary.

---

## Makefile Targets (actual)

| Target                  | Action                                              |
| ----------------------- | --------------------------------------------------- |
| `make dev`              | Start dev server with **air** (hot reload) → :8080  |
| `make build`            | Compile binary to `bin/api`                         |
| `make test`             | Run all Go tests with `-race` + coverage            |
| `make test-integration` | Run tests with `-tags=integration`                  |
| `make lint`             | Run golangci-lint                                   |
| `make docker-up` / `docker-down` | Start/stop Postgres 16 + MinIO            |
| `make docker-migrate-up`| Apply migrations against the docker Postgres        |
| `make migrate-up` / `migrate-down` / `migrate-down-all` | Apply / roll back (1 / all) — needs `DATABASE_URL` |
| `make migrate-version`  | Print current migration version                     |
| `make migrate-create name=<x>` | Scaffold a new `.up/.down.sql` pair          |
| `make migrate-force version=<n>` | Force the migration version (recovery)     |

---

## Local Dev (recommended path)

```bash
cp .env.example .env       # set JWT_SECRET (≥32 chars) + SMTP_DSN
make docker-up             # Postgres 16 (:5432) + MinIO (:9000, console :9001)
make docker-migrate-up     # apply migrations
make dev                   # air hot-reload → http://localhost:8080
```

## Health Check

`GET /v1/health` → `200 OK`. No auth required.
