---
slug: contract-enrichment
consumers: [api, shared, frontend, mobile]
status: planned
---

# Contract Enrichment — Level 2 for all View types

Make the generated TypeScript strong enough to replace the hand-written types:
literal-union enums, honest nullability, required fields, and date formats on
all 17 response views.

## Why

The pipeline (Go → swaggo → Kubb → TypeScript) is wired and proven, but the Go
structs carry almost no schema information, so what comes out the far end is
weaker than the types it is meant to replace:

```ts
status?: string;    // could be anything
notes?: string;     // absent, or null, or a value? unknowable
id?: string;        // is it ever missing? no
```

Generating from that would be a downgrade — `tsc` would stop catching things it
catches today. The enrichment is what makes generated types worth having.

`task.TaskView` is already done and is the reference implementation: it went
from 22 loosely-typed fields to 13 required, 4 enums, 9 nullable, 5 formatted.
16 views and 134 fields remain.

There is a second prize. Enum values currently live in up to five places that
agree only by luck — the DB `CHECK` constraint, unexported service consts,
inline string comparisons in handlers, hardcoded JSON strings in
`internal/domain/ai/tools.go`, and the TypeScript constants. A named Go type
collapses those into one definition the compiler enforces.

## Scope

### In

- Named Go enum types + consts for every enumerated wire field
- `extensions:"x-nullable"` on every pointer field in a View
- `validate:"required"` on every always-populated non-pointer field
- `format:"date"` / `format:"date-time"` on date and timestamp fields
- Repointing existing untyped consts and inline comparisons at the new types

Each type is aligned **everywhere it is used, in the same change**: the Go
struct, the generated TypeScript, and every call site in `nicoflow-shared`,
`nicoflow-frontend` and `nicoflow-mobile`. A type is not done until all four
repos compile against the real name.

### Out

- Request bodies and internal DTOs (not the consumer contract)
- Generating Zod schemas (input validation is a different contract)

## Non-Goals

- Hand-authoring OpenAPI. The Go structs stay the source of truth.
- Changing any wire value. This is a typing exercise: if the JSON a client
  receives changes, something is wrong.
- Retyping domain models. Only the `*View` structs are enriched; the domain
  layer keeps plain strings so SQL scanning is untouched.
- **Alias shims.** No `export type ITask = TaskView`. An alias leaves two names
  for one thing and defers the rename indefinitely — the call sites keep saying
  `ITask` and nobody ever goes back. Delete the hand-written interface and fix
  the imports in the same change, so there is exactly one name for each type.

## Contract

**Enums — declare a named type, let swaggo do the rest.** No `enums:"..."` tag
is needed; swaggo reads the ordered consts.

```go
type TaskStatus string

const (
    TaskStatusActive    TaskStatus = "active"
    TaskStatusDone      TaskStatus = "done"
    TaskStatusCancelled TaskStatus = "cancelled"
)
```

**Nullability is already in the Go type system.** A pointer field is nullable, a
value field is not. Mark it so Kubb emits `| null`:

```go
Notes        *string `json:"notes" extensions:"x-nullable"`
CreatedAt    string  `json:"createdAt" format:"date-time" validate:"required"`
ScheduledFor *string `json:"scheduledFor" format:"date" extensions:"x-nullable"`
```

**Convert at the view boundary**, not in the domain model:

```go
Status: TaskStatus(t.Status),
```

For a nullable enum, use a small helper that preserves nil — see
`occurrenceStatusPtr` in `internal/domain/task/types.go`.

**Known enum values.** Where the DB has a `CHECK`, it wins:

| Field                    | Values                                     | Source                      |
| ------------------------ | ------------------------------------------ | --------------------------- |
| `tasks.status`           | `active` `done` `cancelled`                | CHECK (migration 025)       |
| `projects.status`        | `active` `completed` `archived`            | `project/handler.go:242`    |
| `recurrence.freq`        | `daily` `weekly` `monthly` `yearly`        | CHECK                       |
| `habits.polarity`        | `build` `quit`                             | CHECK + `habit/types.go:28` |
| `habits.schedule_kind`   | `daily` `weekdays` `weekly_quota`          | CHECK                       |
| google connection status | `pending` `confirmed` `rejected` `expired` | CHECK                       |
| notification `type`      | 12 values                                  | `notification/types.go:11`  |

If Go and the DB disagree, that is a real bug. Record it in `blockers.md` — do
not pick a side.

## Design Risks

| Risk                                   | Mitigation                                                                                              |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| A "typing-only" change alters the wire | Enums are string-kinded; `json.Marshal` output is identical. Tests assert on JSON bodies                |
| Named type breaks a caller             | The compiler finds every one — that is the point. `TaskView` surfaced exactly one, in `cmd/api/main.go` |
| Wrong enum values invented             | Values come from the table above, not from inference. Disagreement is a blocker                         |
| Enrichment misses a field              | The verify step diffs the definition's `required` array against the struct's non-pointer count          |
| Frontend breaks on the new types       | Expected, and the point — `tsc` names every call site. Fix them in the same change rather than aliasing |

## Test Strategy

| Layer                               | Covers                                               |
| ----------------------------------- | ---------------------------------------------------- |
| `make build`                        | the named types compile and every caller was updated |
| `go test ./internal/domain/<d>/...` | no behavioural change in the domain                  |
| `make swagger` + inspect definition | enum/required/nullable/format actually emitted       |
| `pnpm codegen` + `tsc`              | generated TypeScript is valid and self-consistent    |
| `pnpm codegen:check`                | generated output committed and in sync               |
| existing handler tests              | JSON bodies unchanged — the wire did not move        |

## Rollout

One type at a time, all the way through every repo that uses it:

1. Enrich the Go struct, `make swagger`, `make build`, domain tests
2. `pnpm codegen` in `nicoflow-shared`
3. Delete the hand-written interface for that type
4. Fix every call site `tsc` names — in shared, frontend and mobile
5. All four repos green

A type is done only after step 5. Half-migrated is worse than not started: two
names for one shape, and no compiler pressure to finish.

Order is smallest-blast-radius first, so a mistake in the pattern is caught on a
2-field view rather than a 22-field one:

`habit.SubjectView` → `auth.CalendarPrefsView` → `googlecal.CalendarView` →
`habit.CellView` → `note.NoteView` → `note.NoteDetailView` → `area.AreaView` →
`task.SubtaskView` → `area.AreaWithProjectsView` → `notification.PreferencesView` →
`bucket.BucketView` → `notification.NotificationView` → `auth.UserView` →
`project.ProjectView` → `googlecal.GoogleEventView` → `habit.HabitView`

**Rollback:** each domain is one commit touching one `types.go` plus regenerated
artifacts. `git revert` restores the previous contract exactly.

## Acceptance Criteria

- [ ] **AC1** Every `*View` definition in `docs/swagger.json` has a non-empty
      `required` array listing exactly its non-pointer fields.
- [ ] **AC2** Every pointer field in a `*View` carries `x-nullable: true` in
      `docs/swagger.json`, and its generated TypeScript is `| null`.
- [ ] **AC3** Every enumerated wire field is a named Go type whose values match
      the table in this spec, and its generated TypeScript is a literal union
      rather than `string`.
- [ ] **AC4** Every date or timestamp field carries `format: date` or
      `format: date-time`.
- [ ] **AC5** `make build` and `go test ./...` pass, and no handler test's
      asserted JSON body changed — the wire is identical.
- [ ] **AC6** `pnpm type-check` passes in `nicoflow-shared` and
      `pnpm codegen:check` exits 0.
- [ ] **AC7** No enum value is defined in more than one Go location: the former
      unexported consts, inline comparisons, and the hardcoded schema strings in
      `internal/domain/ai/tools.go` all reference the named types.
- [ ] **AC8** Every hand-written interface that duplicates a generated type is
      deleted, and no alias re-export of one exists (`export type ITask =
      TaskView` and the like). Exactly one name per shape, across all four repos.
- [ ] **AC9** `pnpm type-check` passes in `nicoflow-frontend` and
      `nicoflow-mobile` against the generated types, with no `as` cast or local
      re-declaration introduced to get there.

## Open Questions

_(none)_
