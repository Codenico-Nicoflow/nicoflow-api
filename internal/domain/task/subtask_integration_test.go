//go:build integration

package task_test

import (
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

func createSubtask(t *testing.T, env taskEnv, taskID string, body any) task.SubtaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodPost, "/v1/tasks/"+taskID+"/subtasks", body, env.token)
	assertStatus(t, resp, http.StatusCreated)
	var out struct {
		Data task.SubtaskView `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data
}

func listSubtasks(t *testing.T, env taskEnv, taskID string) []task.SubtaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodGet, "/v1/tasks/"+taskID+"/subtasks", nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data struct {
			Items []task.SubtaskView `json:"items"`
		} `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data.Items
}

func TestIntegration_Subtask_Lifecycle(t *testing.T) {
	env := newTaskServer(t, "pro")
	parent := createTask(t, env, map[string]any{"title": "Parent"})

	a := createSubtask(t, env, parent.ID, map[string]any{"title": "Step A"})
	b := createSubtask(t, env, parent.ID, map[string]any{"title": "Step B"})
	if a.Position != 0 || b.Position != 1 {
		t.Fatalf("positions wrong: a=%d b=%d", a.Position, b.Position)
	}
	if a.Done {
		t.Errorf("new subtask should not be done")
	}

	// Toggle done.
	resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+parent.ID+"/subtasks/"+a.ID,
		map[string]any{"done": true}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var patched struct {
		Data task.SubtaskView `json:"data"`
	}
	decode(t, resp, &patched)
	if !patched.Data.Done {
		t.Errorf("subtask should be done after patch")
	}

	// Reorder: move b ahead of a by giving it a lower position.
	// (Subtask position is a direct set — the caller assigns distinct values.)
	resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+parent.ID+"/subtasks/"+b.ID,
		map[string]any{"position": 0}, env.token)
	assertStatus(t, resp, http.StatusOK)
	resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+parent.ID+"/subtasks/"+a.ID,
		map[string]any{"position": 1}, env.token)
	assertStatus(t, resp, http.StatusOK)

	got := listSubtasks(t, env, parent.ID)
	if len(got) != 2 || got[0].ID != b.ID {
		t.Errorf("reorder wrong: %+v", got)
	}

	// Delete a.
	resp = do(t, env.srv, http.MethodDelete, "/v1/tasks/"+parent.ID+"/subtasks/"+a.ID, nil, env.token)
	assertStatus(t, resp, http.StatusNoContent)
	if got := listSubtasks(t, env, parent.ID); len(got) != 1 {
		t.Errorf("after delete len = %d, want 1", len(got))
	}
}

func TestIntegration_Subtask_CascadeOnTaskDelete(t *testing.T) {
	env := newTaskServer(t, "pro")
	parent := createTask(t, env, map[string]any{"title": "Parent"})
	createSubtask(t, env, parent.ID, map[string]any{"title": "Sub"})

	// Delete the parent task.
	resp := do(t, env.srv, http.MethodDelete, "/v1/tasks/"+parent.ID, nil, env.token)
	assertStatus(t, resp, http.StatusNoContent)

	// Listing subtasks under the gone task → 404 (task not found).
	resp = do(t, env.srv, http.MethodGet, "/v1/tasks/"+parent.ID+"/subtasks", nil, env.token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrCode(t, resp, "RESOURCE_NOT_FOUND")
}

func TestIntegration_Subtask_CrossUser_Returns404(t *testing.T) {
	env := newTaskServer(t, "pro")
	parent := createTask(t, env, map[string]any{"title": "Parent"})

	pool := testutil.NewTestDB(t)
	_, otherToken := insertUser(t, pool, "intruder-sub"+testEmailDomain, "pro")

	resp := do(t, env.srv, http.MethodPost, "/v1/tasks/"+parent.ID+"/subtasks",
		map[string]any{"title": "hax"}, otherToken)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrCode(t, resp, "RESOURCE_NOT_FOUND")
}
