## 2026-09-02T03:00:00Z — Enrich habit.CellView (5 fields)

tried: validate:"required" on Date/Scheduled/Value/Satisfied, format:"date" on Date, x-nullable on *PeriodProgress
result: gate green (build, vet, habit tests, required[]=4, progress allOf+x-nullable)
learned: pointer-to-struct nullable emits as allOf $ref + x-nullable, not a bare $ref

## 2026-09-02T02:00:00Z — Enrich googlecal.CalendarView (5 fields)

tried: validate:"required" on all 5 value fields in events.go; make swagger
result: gate green (build, vet, googlecal tests, verify required[] = 5 fields)
learned: CalendarView has no pointers/enums — pure required pass

## 2026-09-02T01:00:00Z — Enrich auth.CalendarPrefsView (4 fields)

tried: validate:"required" on all 4 fields in types.go CalendarPrefsView; make swagger
result: gate green (build, vet, auth tests, verify required[] = 4 fields)
learned: workdays is normalized to []int{} in userToView, never nil — safe as required

## 2026-09-02T00:00:00Z — Enrich habit.SubjectView (2 fields)

tried: added validate:"required" to Slug and LabelKey in subjects.go; regenerated swagger
result: gate green (make build, go vet, habit tests, verify asserts required[])
learned: `make swagger` writes docs/swagger.json + docs/docs.go + docs/swagger.yaml — commit all three
