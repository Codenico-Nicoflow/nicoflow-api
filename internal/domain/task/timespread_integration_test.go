//go:build integration

package task_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

func isoDay(offset int) string {
	return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
}

func timeSpread(t *testing.T, env taskEnv) task.TimeSpreadResponse {
	t.Helper()
	resp := do(t, env.srv, http.MethodGet, "/v1/time-spread", nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.TimeSpreadResponse `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data
}

func bucketHas(items []task.TaskView, title string) bool {
	for _, it := range items {
		if it.Title == title {
			return true
		}
	}
	return false
}

func TestIntegration_TimeSpread_BucketsAndRollForward(t *testing.T) {
	env := newTaskServer(t, "pro")

	// Schedule a task for today.
	today := createTask(t, env, map[string]any{"title": "today-task", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+today.ID+"/schedule", map[string]any{"scheduledFor": isoDay(0)})

	// A past soft-scheduled task that rolls over → should appear in today.
	rolled := createTask(t, env, map[string]any{"title": "rolled", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+rolled.ID+"/schedule", map[string]any{"scheduledFor": isoDay(-2), "rollsOver": true})

	// A past soft-scheduled task that does NOT roll over → should drop off.
	dropped := createTask(t, env, map[string]any{"title": "dropped", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+dropped.ID+"/schedule", map[string]any{"scheduledFor": isoDay(-2), "rollsOver": false})

	// Scheduled tomorrow.
	tomorrow := createTask(t, env, map[string]any{"title": "tomorrow-task", "status": "active"})
	patchTask(t, env, "/v1/tasks/"+tomorrow.ID+"/schedule", map[string]any{"scheduledFor": isoDay(1)})

	// cancelled must never appear, even when scheduled for today.
	cancelled := createTask(t, env, map[string]any{"title": "cancelled-task", "status": "cancelled"})
	patchTask(t, env, "/v1/tasks/"+cancelled.ID+"/schedule", map[string]any{"scheduledFor": isoDay(0)})

	ts := timeSpread(t, env)

	if !bucketHas(ts.Today, "today-task") {
		t.Error("today-task missing from today")
	}
	if !bucketHas(ts.Today, "rolled") {
		t.Error("rolled task should carry into today")
	}
	if !bucketHas(ts.Tomorrow, "tomorrow-task") {
		t.Error("tomorrow-task missing from tomorrow")
	}

	all := func(title string) bool {
		return bucketHas(ts.Today, title) || bucketHas(ts.Tomorrow, title) || bucketHas(ts.ThisWeek, title)
	}
	if all("dropped") {
		t.Error("non-rollsOver past task should drop off entirely")
	}
	if all("cancelled-task") {
		t.Error("cancelled task must be excluded from all buckets")
	}
}
