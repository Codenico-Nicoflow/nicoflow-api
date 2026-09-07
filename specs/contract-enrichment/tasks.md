# Tasks — contract-enrichment (nicoflow-api)

Nothing is enriched yet — there is no worked example in the tree to copy. The
rules are in `GATES.md` under "Enrichment rules": named enum types, `x-nullable`
on pointers, `validate:"required"` on value fields, `format` on dates, and
conversion at the view boundary rather than in the domain model.

`task.TaskView` is the largest and comes last, so the pattern is settled on
smaller views first.

One view per iteration:

1. Enrich the Go struct — `make build`, `go vet`, domain tests
2. `make swagger`, then the task's `[verify:]`
3. Tick the box

TypeScript stays hand-written — there is no generation step. The annotations
here make `docs/swagger.json` an accurate description of the wire, which is what
the TypeScript side is then aligned against by its own task list. Keeping one
hand-maintained definition per type was the deliberate choice; the cost is that
the alignment is checked by a human rather than by `tsc`.

So a mismatch found while enriching is worth writing down even when it is not
this repo's to fix — see `CORRECTIONS.md` below.

**Corrections are in scope.** Where enrichment reveals the contract was wrong —
a field marked optional the handler always requires, a nullable that is never
null — fix it and change the wire. Do not preserve a bug to keep the JSON
identical.

Record every correction in `CORRECTIONS.md` next to this file: the field, the
old behaviour, the new one, and why. That file becomes the breaking-change list
in the PR.

Two rules on direction. Narrowing a **response** (optional → required) is safe
for readers. Making a **request** field required is not: the API starts
rejecting bodies it used to accept, and a shipped client may be sending exactly
those. Before requiring a request field, confirm no consumer omits it — if one
does and cannot supply it, that is a blocker, not a rename.

The `[verify:]` commands read the regenerated definition and assert the
enrichment actually landed, because a build passing only proves the Go compiles,
not that swaggo emitted anything.

## Planned

- [ ] Enrich habit.SubjectView (2 fields) [ac:AC1] [files:internal/domain/habit/subjects.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.SubjectView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich auth.CalendarPrefsView (4 fields) [ac:AC1] [files:internal/domain/auth/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['auth.CalendarPrefsView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich googlecal.CalendarView (5 fields) [ac:AC1] [files:internal/domain/googlecal/events.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['googlecal.CalendarView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich habit.CellView (5 fields) [ac:AC1] [files:internal/domain/habit/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.CellView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich note.NoteView (7 fields, createdAt/updatedAt are date-time) [ac:AC1,AC4] [files:internal/domain/note/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['note.NoteView'];assert d.get('required'),'no required[]';assert d['properties']['createdAt'].get('format')=='date-time','createdAt needs format';print('ok')"]

- [ ] Enrich note.NoteDetailView (7 fields) [ac:AC1,AC4] [files:internal/domain/note/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['note.NoteDetailView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich area.AreaView (7 fields) [ac:AC1,AC4] [files:internal/domain/area/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['area.AreaView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich task.SubtaskView (7 fields) [ac:AC1,AC4] [files:internal/domain/task/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['task.SubtaskView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich area.AreaWithProjectsView (8 fields) [ac:AC1,AC4] [files:internal/domain/area/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['area.AreaWithProjectsView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich notification.PreferencesView (8 fields) [ac:AC1] [files:internal/domain/notification/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['notification.PreferencesView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich bucket.BucketView (9 fields) [ac:AC1,AC2,AC4] [files:internal/domain/bucket/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['bucket.BucketView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich notification.NotificationView (9 fields) — type is an enum, see the 12 values at internal/domain/notification/types.go:11 [ac:AC1,AC3,AC4] [files:internal/domain/notification/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['notification.NotificationView'];p=d['properties']['type'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'type must be an enum';print('ok')"]

- [ ] Enrich auth.UserView (11 fields) [ac:AC1,AC2,AC4] [files:internal/domain/auth/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['auth.UserView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich project.ProjectView (11 fields) — status enum is active|completed|archived, see project/handler.go:242 [ac:AC1,AC3,AC4] [files:internal/domain/project/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['project.ProjectView'];p=d['properties']['status'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'status must be an enum';print('ok')"]

- [ ] Enrich googlecal.GoogleEventView (12 fields) [ac:AC1,AC2,AC4] [files:internal/domain/googlecal/events.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['googlecal.GoogleEventView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich habit.HabitView (22 fields) — polarity build|quit and scheduleKind daily|weekdays|weekly_quota already have consts at habit/types.go:28 [ac:AC1,AC2,AC3,AC4] [files:internal/domain/habit/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.HabitView'];p=d['properties']['polarity'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'polarity must be an enum';print('ok')"]

- [ ] Enrich task.TaskView (22 fields) — the largest, and last so the pattern is settled first. status/priority/energy are enums (active|done|cancelled, low|medium|high, low|medium|deep), occurrenceStatus is a nullable enum (missed|cancelled|skipped), 9 pointer fields are nullable, scheduledFor and occurrenceDate are date, completedAt/createdAt/updatedAt are date-time [ac:AC1,AC2,AC3,AC4] [files:internal/domain/task/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['task.TaskView'];assert d.get('required'),'no required[]';p=d['properties']['status'];assert p.get('enum') or 'allOf' in p or '\$ref' in p,'status must be an enum';assert d['properties']['notes'].get('x-nullable'),'notes must be nullable';print('ok',len(d['required']),'required')"]

- [ ] Repoint the hardcoded enum strings in internal/domain/ai/tools.go at the named types so no enum value is defined twice [ac:AC7] [files:internal/domain/ai/tools.go] [verify:go build ./... && ! grep -q '"active", "done", "cancelled"' internal/domain/ai/tools.go]

- [ ] Repoint the unexported status consts and inline string comparisons at the named types [ac:AC7] [files:internal/domain/task/service.go,internal/domain/project/handler.go] [verify:go build ./... && go test ./internal/domain/task/... ./internal/domain/project/...]

- [ ] Regenerate and verify the full contract: every View has required[], no field is a bare enum string [ac:AC1,AC3] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k,v in d.items() if k.endswith('View') and not v.get('required')];assert not bad,f'missing required[]: {bad}';print('all views enriched')"]

- [ ] Fix the four definitions that emit zero properties — task.UpdateTaskRequest and project.UpdateProjectRequest use optional.Field[T] generics swaggo cannot introspect; googlecal.GoogleStatus and googlecal.ResponseStatus emit nothing at all. All four have a silently empty contract [ac:AC11] [files:internal/domain/task/types.go,internal/domain/project/types.go,internal/domain/googlecal] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k,v in d.items() if not v.get('properties')];assert not bad,f'empty definitions: {bad}';print('ok')"]

- [ ] Enrich the auth request types (RegisterRequest, LoginRequest, UpdateMeRequest, ChangePasswordRequest, ResetPasswordRequest and the three single-field ones) [ac:AC10] [files:internal/domain/auth/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k in d if k.startswith('auth.') and k.endswith('Request') and not d[k].get('required')];assert not bad,f'missing required[]: {bad}';print('ok')"]

- [ ] Enrich the task and subtask request types (CreateTaskRequest, UpdateTaskRequest, ScheduleRequest, SetStatusRequest, ReorderOneRequest, CreateSubtaskRequest, UpdateSubtaskRequest) — status/priority/energy reuse the named enums [ac:AC10,AC3] [files:internal/domain/task/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k in d if k.startswith('task.') and k.endswith('Request') and not d[k].get('required')];assert not bad,f'missing required[]: {bad}';print('ok')"]

- [ ] Enrich the remaining request types (area, project, bucket, note, habit, notification, googlecal) [ac:AC10] [files:internal/domain] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k,v in d.items() if k.endswith('Request') and not v.get('required')];assert not bad,f'missing required[]: {bad}';print('all requests enriched')"]

- [ ] Point the 5 hardcoded z.enum lists at the generated unions so a value can only be added in Go [ac:AC12] [files:../nicoflow-shared/src/schemas] [verify:cd ../nicoflow-shared && ! grep -rqE "z\\.enum\\(\\['(active|low|task)" src/schemas/ && pnpm type-check && pnpm test]

- [ ] Final sweep: every definition enriched, no empty ones, no duplicate enum definitions anywhere in the four repos [ac:AC1,AC3,AC10,AC11,AC13] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k,v in d.items() if (k.endswith('View') or k.endswith('Request')) and not v.get('required') and v.get('properties')];assert not bad,f'unenriched: {bad}';empty=[k for k,v in d.items() if not v.get('properties')];assert not empty,f'empty: {empty}';print('contract complete')"]

- [ ] Review CORRECTIONS.md as a whole: confirm every wire change is intentional, that no request field was made required while a consumer still omits it, and that each has an updated handler test [ac:AC5] [files:specs/contract-enrichment/CORRECTIONS.md] [verify:test -f specs/contract-enrichment/CORRECTIONS.md && make test]

## Discovered

_(the loop appends here — never reorder or delete the planned list above)_
