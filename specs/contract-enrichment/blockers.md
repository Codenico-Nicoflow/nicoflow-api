## 2026-09-07T17:05:00Z — Enrich note.NoteDetailView (push step)

what: how to reconcile a diverged feature/contract-enrichment branch. Local and origin have independently enriched the SAME views under different commits.
why: origin/feature/contract-enrichment is 9 commits the local branch does not have (d7d6635..f77d143) covering SubjectView, CalendarPrefsView, CalendarView, CellView, NoteView, NoteDetailView, AreaView, SubtaskView, AreaWithProjectsView. Local is 9 ahead with its own commits for the first 6 of those. Same struct tags, different commits — a merge will conflict in every types.go touched, and picking a side silently discards one history. Choosing which lineage survives (and whether the 3 extra remote views are then already done) is a human call, not a mechanical one.
tried: git push (rejected, non-fast-forward), git fetch + git log both directions to confirm the divergence is duplicated work rather than a stale local. Did not merge, rebase, reset or force-push.
