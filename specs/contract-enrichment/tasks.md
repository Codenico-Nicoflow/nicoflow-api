# Tasks — contract-enrichment (nicoflow-api)

Reference implementation: `task.TaskView` in `internal/domain/task/types.go`
(already enriched). Copy its shape — named enum types, `x-nullable` on pointers,
`validate:"required"` on value fields, `format` on dates, conversion at the view
boundary via `TaskStatus(t.Status)` and the `occurrenceStatusPtr` helper.

One view per iteration. After each: `make build`, `make swagger`, then the
task's `[verify:]`. Domain tests must stay green — this changes types, never
wire values.

The `[verify:]` commands read the regenerated definition and assert the
enrichment actually landed, because a build passing only proves the Go compiles,
not that swaggo emitted anything.

## Planned

- [x] Enrich habit.SubjectView (2 fields) [ac:AC1] [files:internal/domain/habit/subjects.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.SubjectView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich auth.CalendarPrefsView (4 fields) [ac:AC1] [files:internal/domain/auth/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['auth.CalendarPrefsView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich googlecal.CalendarView (5 fields) [ac:AC1] [files:internal/domain/googlecal/events.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['googlecal.CalendarView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich habit.CellView (5 fields) [ac:AC1] [files:internal/domain/habit/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.CellView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich note.NoteView (7 fields, createdAt/updatedAt are date-time) [ac:AC1,AC4] [files:internal/domain/note/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['note.NoteView'];assert d.get('required'),'no required[]';assert d['properties']['createdAt'].get('format')=='date-time','createdAt needs format';print('ok')"]

- [x] Enrich note.NoteDetailView (7 fields) [ac:AC1,AC4] [files:internal/domain/note/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['note.NoteDetailView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich area.AreaView (7 fields) [ac:AC1,AC4] [files:internal/domain/area/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['area.AreaView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich task.SubtaskView (7 fields) [ac:AC1,AC4] [files:internal/domain/task/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['task.SubtaskView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [x] Enrich area.AreaWithProjectsView (8 fields) [ac:AC1,AC4] [files:internal/domain/area/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['area.AreaWithProjectsView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich notification.PreferencesView (8 fields) [ac:AC1] [files:internal/domain/notification/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['notification.PreferencesView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich bucket.BucketView (9 fields) [ac:AC1,AC2,AC4] [files:internal/domain/bucket/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['bucket.BucketView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich notification.NotificationView (9 fields) — type is an enum, see the 12 values at internal/domain/notification/types.go:11 [ac:AC1,AC3,AC4] [files:internal/domain/notification/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['notification.NotificationView'];p=d['properties']['type'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'type must be an enum';print('ok')"]

- [ ] Enrich auth.UserView (11 fields) [ac:AC1,AC2,AC4] [files:internal/domain/auth/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['auth.UserView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich project.ProjectView (11 fields) — status enum is active|completed|archived, see project/handler.go:242 [ac:AC1,AC3,AC4] [files:internal/domain/project/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['project.ProjectView'];p=d['properties']['status'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'status must be an enum';print('ok')"]

- [ ] Enrich googlecal.GoogleEventView (12 fields) [ac:AC1,AC2,AC4] [files:internal/domain/googlecal/events.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['googlecal.GoogleEventView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]

- [ ] Enrich habit.HabitView (22 fields) — polarity build|quit and scheduleKind daily|weekdays|weekly_quota already have consts at habit/types.go:28 [ac:AC1,AC2,AC3,AC4] [files:internal/domain/habit/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['habit.HabitView'];p=d['properties']['polarity'];assert p.get('enum') or '\$ref' in p or 'allOf' in p,'polarity must be an enum';print('ok')"]

- [ ] Repoint the hardcoded enum strings in internal/domain/ai/tools.go at the named types so no enum value is defined twice [ac:AC7] [files:internal/domain/ai/tools.go] [verify:go build ./... && ! grep -q '"active", "done", "cancelled"' internal/domain/ai/tools.go]

- [ ] Repoint the unexported status consts and inline string comparisons at the named types [ac:AC7] [files:internal/domain/task/service.go,internal/domain/project/handler.go] [verify:go build ./... && go test ./internal/domain/task/... ./internal/domain/project/...]

- [ ] Regenerate and verify the full contract: every View has required[], no field is a bare enum string [ac:AC1,AC3] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions'];bad=[k for k,v in d.items() if k.endswith('View') and not v.get('required')];assert not bad,f'missing required[]: {bad}';print('all views enriched')"]

## Discovered

_(the loop appends here — never reorder or delete the planned list above)_

- [ ] Enrich task.TaskView (22 fields) — tasks.md names it the enriched reference implementation but internal/domain/task/types.go:46 has no validate/format/x-nullable tags and no named status enum [ac:AC1,AC2,AC3,AC4] [files:internal/domain/task/types.go] [verify:make swagger && python3 -c "import json;d=json.load(open('docs/swagger.json'))['definitions']['task.TaskView'];assert d.get('required'),'no required[]';print('ok',d['required'])"]
