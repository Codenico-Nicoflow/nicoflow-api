//go:build integration

package task_test

import (
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

func focusList(t *testing.T, env taskEnv, query string) []task.TaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodGet, "/v1/focus"+query, nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data struct {
			Items []task.TaskView `json:"items"`
		} `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data.Items
}

func titles(items []task.TaskView) map[string]bool {
	m := map[string]bool{}
	for _, it := range items {
		m[it.Title] = true
	}
	return m
}

func TestIntegration_Focus_TimeBudgetExcludesOverBudget(t *testing.T) {
	env := newTaskServer(t, "pro")
	createTask(t, env, map[string]any{"title": "short", "status": "active", "estimatedMinutes": 20})
	createTask(t, env, map[string]any{"title": "long", "status": "active", "estimatedMinutes": 90})

	got := titles(focusList(t, env, "?available=30"))
	if !got["short"] || got["long"] {
		t.Errorf("available=30 should keep short, drop long; got %v", got)
	}
}

func TestIntegration_Focus_EnergyRanksFirst(t *testing.T) {
	env := newTaskServer(t, "pro")
	createTask(t, env, map[string]any{"title": "deepwork", "status": "active", "energy": "deep", "estimatedMinutes": 30})
	createTask(t, env, map[string]any{"title": "lowwork", "status": "active", "energy": "low", "estimatedMinutes": 30})

	got := focusList(t, env, "?available=60&energy=low")
	if len(got) < 1 || got[0].Title != "lowwork" {
		t.Errorf("low-energy task should rank first; got %+v", got)
	}
}

func TestIntegration_Focus_ExcludesDoneCancelled(t *testing.T) {
	env := newTaskServer(t, "pro")
	createTask(t, env, map[string]any{"title": "active1", "status": "active"})
	createTask(t, env, map[string]any{"title": "cancelled1", "status": "cancelled"})
	createTask(t, env, map[string]any{"title": "done1", "status": "done"})

	got := titles(focusList(t, env, ""))
	if !got["active1"] {
		t.Errorf("active task should appear; got %v", got)
	}
	if got["cancelled1"] || got["done1"] {
		t.Errorf("cancelled/done must be excluded; got %v", got)
	}
}

func TestIntegration_Focus_LimitClamped(t *testing.T) {
	env := newTaskServer(t, "pro")
	for i := 0; i < 7; i++ {
		createTask(t, env, map[string]any{"title": "t", "status": "active"})
	}
	// default limit is 5
	if got := focusList(t, env, ""); len(got) != 5 {
		t.Errorf("default focus limit = %d, want 5", len(got))
	}
	// explicit limit honoured
	if got := focusList(t, env, "?limit=3"); len(got) != 3 {
		t.Errorf("limit=3 returned %d", len(got))
	}
}

func TestIntegration_Focus_BadParam(t *testing.T) {
	env := newTaskServer(t, "pro")
	resp := do(t, env.srv, http.MethodGet, "/v1/focus?available=lots", nil, env.token)
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrCode(t, resp, "INVALID_INPUT")
}
