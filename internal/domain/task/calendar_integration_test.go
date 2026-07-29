//go:build integration

package task_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

func rangedTasks(t *testing.T, env taskEnv, from, to string) []task.TaskView {
	t.Helper()
	q := url.Values{"scheduledFrom": {from}, "scheduledTo": {to}}
	resp := do(t, env.srv, http.MethodGet, "/v1/tasks?"+q.Encode(), nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.ListTasksResponse `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data.Items
}

// orderedTitles keeps the response sequence — the calendar's ordering is part
// of the contract, so unlike the set-shaped `titles` helper this preserves it.
func orderedTitles(items []task.TaskView) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

// The calendar's defining difference from Time Spread: an overdue rollsOver
// task stays on its real date. Rolling it forward would make a month grid lie
// about history.
func TestIntegration_Calendar_NoRollForward(t *testing.T) {
	env := newTaskServer(t, "pro")

	overdue := createTask(t, env, map[string]any{"title": "overdue-rolls", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+overdue.ID+"/schedule",
		map[string]any{"scheduledFor": isoDay(-30), "rollsOver": true})

	// Its real date is inside the past window...
	past := rangedTasks(t, env, isoDay(-31), isoDay(-29))
	if !bucketHas(past, "overdue-rolls") {
		t.Errorf("task missing from its real date; got %v", orderedTitles(past))
	}

	// ...and it must NOT have been rolled forward onto today.
	current := rangedTasks(t, env, isoDay(0), isoDay(1))
	if bucketHas(current, "overdue-rolls") {
		t.Error("overdue rollsOver task was rolled forward into the current window")
	}

	// Time Spread still rolls it forward — the two endpoints stay independent.
	if !bucketHas(timeSpread(t, env).Today, "overdue-rolls") {
		t.Error("Time Spread roll-forward regressed")
	}
}

// The grid renders in a fixed order: all-day chips before timed blocks, then by
// clock time. Ordering is the repository's job, not the client's.
func TestIntegration_Calendar_OrderingAndRange(t *testing.T) {
	env := newTaskServer(t, "pro")
	day := isoDay(3)

	timed := createTask(t, env, map[string]any{"title": "timed-0900", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+timed.ID+"/schedule",
		map[string]any{"scheduledFor": day, "scheduledTime": "09:00"})

	later := createTask(t, env, map[string]any{"title": "timed-1430", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+later.ID+"/schedule",
		map[string]any{"scheduledFor": day, "scheduledTime": "14:30"})

	allDay := createTask(t, env, map[string]any{"title": "all-day", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+allDay.ID+"/schedule", map[string]any{"scheduledFor": day})

	// Outside the queried window.
	outside := createTask(t, env, map[string]any{"title": "outside", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+outside.ID+"/schedule", map[string]any{"scheduledFor": isoDay(40)})

	// Never scheduled at all.
	createTask(t, env, map[string]any{"title": "unscheduled", "status": "active"})

	items := rangedTasks(t, env, isoDay(2), isoDay(4))

	got := orderedTitles(items)
	want := []string{"all-day", "timed-0900", "timed-1430"}
	if len(got) != len(want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("titles = %v, want %v", got, want)
		}
	}
	if items[0].ScheduledTime != nil {
		t.Errorf("all-day task has a time: %v", *items[0].ScheduledTime)
	}
	if items[1].ScheduledTime == nil || *items[1].ScheduledTime != "09:00" {
		t.Errorf("scheduledTime = %v, want 09:00 (HH:MM, no seconds)", items[1].ScheduledTime)
	}
}

// A completed task must still appear on its real date — the calendar is a
// record of what happened, not only of what is outstanding.
func TestIntegration_Calendar_IncludesCompletedTasks(t *testing.T) {
	env := newTaskServer(t, "pro")
	day := isoDay(1)

	done := createTask(t, env, map[string]any{"title": "finished", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+done.ID+"/schedule", map[string]any{"scheduledFor": day})
	patchTask(t, env, "/v1/tasks/"+done.ID+"/status", map[string]any{"status": "done"})

	if !bucketHas(rangedTasks(t, env, day, day), "finished") {
		t.Error("completed task missing from the calendar range")
	}
}

func TestIntegration_Calendar_RowIsolation(t *testing.T) {
	day := isoDay(2)

	env := newTaskServer(t, "pro")
	mine := createTask(t, env, map[string]any{"title": "mine", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+mine.ID+"/schedule", map[string]any{"scheduledFor": day})

	// Second user on the SAME server — a second newTaskServer would re-run the
	// shared cleanup and delete the first user's rows.
	pool := testutil.NewTestDB(t)
	_, otherToken := insertUser(t, pool, "other-"+sanitizeEmail(t.Name())+testEmailDomain, "pro")
	otherArea := createArea(t, env.srv, otherToken)
	otherProject := createProject(t, env.srv, otherToken, otherArea)

	resp := do(t, env.srv, http.MethodPost, "/v1/projects/"+otherProject+"/tasks",
		map[string]any{"title": "theirs", "status": "active"}, otherToken)
	assertStatus(t, resp, http.StatusCreated)
	var created struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &created)
	resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.Data.ID+"/schedule",
		map[string]any{"scheduledFor": day}, otherToken)
	assertStatus(t, resp, http.StatusOK)

	items := rangedTasks(t, env, day, day)
	if bucketHas(items, "theirs") {
		t.Error("another user's task leaked into the range query")
	}
	if !bucketHas(items, "mine") {
		t.Errorf("own task missing; got %v", orderedTitles(items))
	}
}

func TestIntegration_Calendar_RangeValidation(t *testing.T) {
	env := newTaskServer(t, "pro")

	tests := []struct {
		name  string
		query string
	}{
		{name: "missing both bounds", query: ""},
		{name: "missing scheduledTo", query: "scheduledFrom=2026-08-01"},
		{name: "missing scheduledFrom", query: "scheduledTo=2026-08-31"},
		{name: "non-ISO date", query: "scheduledFrom=August&scheduledTo=2026-08-31"},
		{name: "span over 62 days", query: "scheduledFrom=2026-01-01&scheduledTo=2026-12-31"},
		{name: "reversed bounds", query: "scheduledFrom=2026-08-31&scheduledTo=2026-08-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, env.srv, http.MethodGet, "/v1/tasks?"+tt.query, nil, env.token)
			assertStatus(t, resp, http.StatusUnprocessableEntity)
			assertErrCode(t, resp, apperror.ErrInvalidInput)
		})
	}
}

// Every pre-existing row must read back as all-day with no data migration.
func TestIntegration_Calendar_BackwardCompatibleNullTime(t *testing.T) {
	env := newTaskServer(t, "pro")

	created := createTask(t, env, map[string]any{"title": "legacy", "status": "active"})
	if created.ScheduledTime != nil {
		t.Errorf("new task scheduledTime = %v, want null", *created.ScheduledTime)
	}

	resp := do(t, env.srv, http.MethodGet, "/v1/tasks/"+created.ID, nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &out)
	if out.Data.ScheduledTime != nil {
		t.Errorf("fetched scheduledTime = %v, want null", *out.Data.ScheduledTime)
	}
}

// The plan gate is enforced server-side on every write path, and a downgraded
// user keeps both their stored time and the ability to clear it.
func TestIntegration_TimedScheduling_PlanGate(t *testing.T) {
	t.Run("free plan is blocked on all three write paths", func(t *testing.T) {
		env := newTaskServer(t, "free")
		existing := createTask(t, env, map[string]any{"title": "x", "status": "active"})

		resp := do(t, env.srv, http.MethodPost, "/v1/projects/"+env.projectID+"/tasks",
			map[string]any{"title": "timed", "scheduledFor": isoDay(1), "scheduledTime": "09:00"}, env.token)
		assertStatus(t, resp, http.StatusForbidden)
		assertErrCode(t, resp, apperror.ErrPlanLimitExceeded)

		resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+existing.ID,
			map[string]any{"scheduledTime": "09:00"}, env.token)
		assertStatus(t, resp, http.StatusForbidden)
		assertErrCode(t, resp, apperror.ErrPlanLimitExceeded)

		resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+existing.ID+"/schedule",
			map[string]any{"scheduledFor": isoDay(1), "scheduledTime": "09:00"}, env.token)
		assertStatus(t, resp, http.StatusForbidden)
		assertErrCode(t, resp, apperror.ErrPlanLimitExceeded)
	})

	t.Run("pro persists a time and free can still clear it", func(t *testing.T) {
		env := newTaskServer(t, "pro")
		created := createTask(t, env, map[string]any{"title": "timed", "status": "active"})

		updated := patchTask(t, env, "/v1/tasks/"+created.ID+"/schedule",
			map[string]any{"scheduledFor": isoDay(1), "scheduledTime": "09:15"})
		if updated.ScheduledTime == nil || *updated.ScheduledTime != "09:15" {
			t.Fatalf("scheduledTime = %v, want 09:15", updated.ScheduledTime)
		}

		// Clearing carries no plan gate, so the same call succeeds on free.
		cleared := patchTask(t, env, "/v1/tasks/"+created.ID,
			map[string]any{"scheduledTime": nil})
		if cleared.ScheduledTime != nil {
			t.Errorf("scheduledTime = %v, want null after clear", *cleared.ScheduledTime)
		}
	})

	t.Run("non-snapped time is rejected", func(t *testing.T) {
		env := newTaskServer(t, "pro")
		created := createTask(t, env, map[string]any{"title": "x", "status": "active"})

		resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID,
			map[string]any{"scheduledTime": "09:07"}, env.token)
		assertStatus(t, resp, http.StatusUnprocessableEntity)
		assertErrCode(t, resp, apperror.ErrInvalidInput)
	})

	t.Run("estimate is clamped so a task cannot cross midnight", func(t *testing.T) {
		env := newTaskServer(t, "pro")
		created := createTask(t, env, map[string]any{
			"title": "late", "status": "active", "estimatedMinutes": 60,
		})

		updated := patchTask(t, env, "/v1/tasks/"+created.ID,
			map[string]any{"scheduledTime": "23:30"})
		if updated.EstimatedMinutes == nil || *updated.EstimatedMinutes != 29 {
			t.Errorf("estimatedMinutes = %v, want 29 (clamped at 23:59)", updated.EstimatedMinutes)
		}
	})
}
