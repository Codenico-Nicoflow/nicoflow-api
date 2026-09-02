## 2026-09-02T00:00:00Z — Enrich habit.SubjectView (2 fields)

tried: added validate:"required" to Slug and LabelKey in subjects.go; regenerated swagger
result: gate green (make build, go vet, habit tests, verify asserts required[])
learned: `make swagger` writes docs/swagger.json + docs/docs.go + docs/swagger.yaml — commit all three
