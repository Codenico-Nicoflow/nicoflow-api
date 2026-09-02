## 2026-09-02T08:00:00Z — Enrich area.AreaWithProjectsView (8 fields)

tried: validate:"required" on Projects; AreaView embed already enriched
result: gate green (build, vet, area tests, required[]=8 incl. flattened AreaView fields)
learned: swaggo flattens embedded structs into the outer required[] — embed enrichment inherits for free; Projects built with make(...,len) so never nil

## 2026-09-02T07:00:00Z — Enrich task.SubtaskView (7 fields)

tried: validate:"required" on all 7 value fields, format:"date-time" on CreatedAt/UpdatedAt
result: gate green (build, vet, task tests, required[]=7 + createdAt date-time)
learned: SubtaskView lives in task/subtask_types.go, NOT types.go — the [files:] hint was wrong; TaskView in types.go is still un-enriched despite tasks.md calling it the reference

## 2026-09-02T06:00:00Z — Enrich area.AreaView (7 fields)

tried: validate:"required" on all 7 value fields, format:"date-time" on CreatedAt/UpdatedAt
result: gate green — Tier 1 + Tier 2 (lint 0 issues, full suite no FAIL), required[]=7
learned: AreaView has no pointers/enums; AreaWithProjectsView embeds it, so its required[] should inherit — check that next

## 2026-09-02T05:00:00Z — Enrich note.NoteDetailView (7 fields)

tried: mirrored NoteView tags — required on 6 value fields, x-nullable on *ProjectID, date-time on CreatedAt/UpdatedAt
result: gate green — Tier 1 + Tier 2 (lint 0 issues, full suite no FAIL), required[]=6 incl. content
learned: Content json.RawMessage defaults to EmptyDoc on create, never nil — safe as required; swaggertype:"object" tag coexists with validate

## 2026-09-02T04:00:00Z — Enrich note.NoteView (7 fields)

tried: validate:"required" on 6 value fields, x-nullable on *ProjectID, format:"date-time" on CreatedAt/UpdatedAt
result: gate green — Tier 1 + Tier 2 (lint 0 issues, full suite ok), verify required[]=6 + createdAt format
learned: NoteDetailView shares the same field set minus excerpt plus content — next task is a near-copy


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
