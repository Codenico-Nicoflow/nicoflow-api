# Gates — nicoflow-api

Measured 2026-09-01. The full suite is **184s**, so it cannot run every
iteration: 40 iterations would be two hours of gate time alone. Tier 1 is the
inner loop; Tier 2 every 5th and before every push.

## Tier 1 — every iteration (~3s)

```bash
make build               # go build ./cmd/api                  ~2.8s
go vet ./...
```

## Tier 1 — targeted tests

Only the domain you touched:

```bash
go test ./internal/domain/<domain>/...
```

Run the package you changed, not `./...`. `hashutil` alone is 18s (bcrypt
cost-12, deliberately slow) and has nothing to do with most changes.

## Contract staleness gate — run whenever a View struct changes

The committed OpenAPI spec must match the structs. Regenerate and diff:

```bash
make swagger
git diff --exit-code docs/swagger.json
```

Non-empty diff = **FAIL**. Commit the regenerated spec together with the struct
change — never separately, or `nicoflow-shared` generates from a stale contract.

## Tier 2 — every 5th iteration, and before every push (~190s)

```bash
make build
make lint
make test                # full suite, ~184s
```

## Tier 3 — exit gate (human-run)

```bash
make test-integration    # requires docker: make docker-up
```

## Enrichment rules (contract-enrichment feature)

Wire shapes are the source of truth for every consumer. When enriching a View:

- **Enums** — declare a named type plus consts. swaggo emits the enum
  automatically; no `enums:"..."` tag needed.

  ```go
  type TaskStatus string

  const (
      TaskStatusActive    TaskStatus = "active"
      TaskStatusDone      TaskStatus = "done"
      TaskStatusCancelled TaskStatus = "cancelled"
  )
  ```

  Point the existing unexported consts and inline string comparisons at the new
  type so the compiler enforces one source. `internal/domain/ai/tools.go`
  hardcodes several of these inside raw JSON strings — those must reference the
  consts too.

- **Nullable** — a pointer field is nullable. Mark it so Kubb emits `| null`:

  ```go
  Notes *string `json:"notes" extensions:"x-nullable"`
  ```

- **Required** — non-pointer fields that are always populated:

  ```go
  ID string `json:"id" validate:"required"`
  ```

- **Format** — dates and timestamps:

  ```go
  ScheduledFor *string `json:"scheduledFor" format:"date" extensions:"x-nullable"`
  CreatedAt    string  `json:"createdAt" format:"date-time" validate:"required"`
  ```

Verify after each View:

```bash
make swagger
python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['<pkg>.<View>'];print(json.dumps(d,indent=1))"
```

Enum values must match the DB `CHECK` constraint in `migrations/`. If Go and the
DB disagree, that is a real bug — record it in `blockers.md`, do not pick a side.

## Never

- Edit a deployed migration. Migrations are append-only.
- String-concatenated SQL. Parameterised pgx only.
- Skip `WHERE user_id = ...` on a user-scoped query.
- Return a raw error. Typed `apperror` codes only.
- Commit a struct change without the regenerated `docs/swagger.json`.
