# nicoflow-api — Agent Guide

Go REST API and WebSocket service. The umbrella product/API contract is in `../AGENTS.md`; the canonical API shapes and architecture are in `SPEC.md` §§3–4 and §8. Read those before changing endpoints or persistence.

## Stack and layout

- Go 1.26; chi; PostgreSQL 16 via pgx/pgxpool; golang-migrate; zerolog; Render deployment.
- `cmd/api` wires dependencies. `internal/domain/<domain>` holds handler/service/repository/types. `internal/handler` owns routing/middleware. `internal/apperror` owns typed errors. `pkg/respond` owns the JSON envelope.
- Domains include auth, area, project, task, bucket, AI, billing, notes, notifications, attachments, focus, and calendar integrations. Check the current tree before assuming a domain's completeness.

## Architecture rules

- Flow is **Handler → Service → Repository**. Handlers parse/validate and serialize; services own business rules, quotas, and orchestration; repositories own persistence.
- Define interfaces in the consuming package. Pass `context.Context` first to every I/O function.
- Use parameterized pgx SQL only. Every user-scoped query filters by `user_id`; never concatenate user input into SQL.
- Return typed `apperror` values and the standard envelope, never raw errors to clients or status-only error signals.
- IDs are application-generated strings. Timestamps use `TIMESTAMPTZ`. Soft delete applies only where the schema says so.
- Migrations are append-only. Add a numbered `.up.sql` / `.down.sql` pair; never edit a migration already applied.
- Auth, refresh-token rotation, row isolation, rate limits, and quota enforcement are security-sensitive. Follow the detailed checklist in `SPEC.md` and existing domain patterns.
- No `any`; handle errors explicitly; avoid package-level mutable state.

## TDD and tests

Follow the workspace TDD loop: add a failing test first, observe it fail, implement, then refactor. Use table-driven service/repository tests. Integration tests use the real test database and the `integration` build tag; do not substitute DB mocks for integration behavior. Assert typed error codes as well as relevant HTTP behavior.

```sh
make test
make lint
make build
make test-integration # when DB integration behavior changes and test DB is available
```

## Branching and workflow

Branches use `<type>/NIC-<ticket>-<short-desc>`, normally from `staging`; PRs target `staging`. `hotfix/*` starts at and targets `main`. Run `git` commands from this repo. Use Context7 for current third-party library/API documentation.

Check `.env.example` for configuration. Local API port is `8080`; common setup is `make docker-up`, `make docker-migrate-up`, then `make dev`.
