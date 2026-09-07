## 2026-09-07T16:55:00Z — Enrich note.NoteDetailView (7 fields)

tried: validate:"required" on all 7, format:"date-time" on CreatedAt/UpdatedAt, extensions:"x-nullable" on *string ProjectID in internal/domain/note/types.go; regenerated swagger
result: green — build, vet, note tests (0.5s), required[]=all 7, projectId x-nullable
learned: Content is json.RawMessage with swaggertype:"object" — the extra validate tag composes fine and it is always set (EmptyDoc default), so required is correct.

## 2026-09-07T16:30:00Z — Enrich note.NoteView (7 fields)

tried: validate:"required" on all 7, format:"date-time" on CreatedAt/UpdatedAt, extensions:"x-nullable" on *string ProjectID in internal/domain/note/types.go; regenerated swagger
result: green — build, vet, note tests (0.6s), required[]=all 7, projectId emits x-nullable:true
learned: a *string (unlike a struct pointer) stays a plain {type:string, x-nullable:true} — no allOf wrapper, so required+x-nullable coexist cleanly on it.

## 2026-09-07T16:05:00Z — Enrich habit.CellView (5 fields)

tried: validate:"required" on Date/Scheduled/Value/Satisfied, format:"date" on Date, extensions:"x-nullable" on *PeriodProgress in internal/domain/habit/types.go; regenerated swagger
result: green — build, vet, habit tests (0.5s), required[]=date,satisfied,scheduled,value; progress emits allOf+x-nullable
learned: swaggo renders a nullable struct pointer as allOf[$ref]+x-nullable — `extensions:"x-nullable"` is the tag that works (not a `x-nullable` key). Date is a plain string formatted with habit.DateLayout, so `format:"date"` is the right annotation.

## 2026-09-07T15:40:00Z — Enrich googlecal.CalendarView (5 fields)

tried: added `validate:"required"` to ID/Summary/BackgroundColor/Primary/Selected in internal/domain/googlecal/events.go, regenerated swagger
result: green — build, vet, googlecal tests (0.4s), swagger required[]=all 5
learned: the mapper at calendars.go:73 sets every field unconditionally (Selected is a computed bool, never omitted) — no pointers in this view, so no x-nullable.

## 2026-09-07T15:10:00Z — Enrich auth.CalendarPrefsView (4 fields)

tried: added `validate:"required"` to WeekStart/Workdays/DayStartHour/DayEndHour in internal/domain/auth/types.go, regenerated swagger
result: green — build, vet, auth tests (17.9s), swagger required[]=['dayEndHour','dayStartHour','weekStart','workdays']
learned: userToView normalises nil Workdays to []int{} so the slice is never null on the wire — required is correct, no x-nullable.

## 2026-09-07T14:48:00Z — Enrich habit.SubjectView (2 fields)

tried: added `validate:"required"` to Slug and LabelKey in internal/domain/habit/subjects.go, regenerated swagger
result: green — build, vet, habit tests, swagger required[]=['labelKey','slug']
learned: SubjectView.Slug is deliberately NOT an enum (open catalog; unknown slug must render a fallback icon on old clients) — do not add a named type for it. Loop files live in ./specs/contract-enrichment/, not the shared spec dir; the shared dir holds the TypeScript-side list.
