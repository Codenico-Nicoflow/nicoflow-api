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

// The counts ride on every task read, not an enrichment seam — the client uses
// openSubtaskCount to decide whether completing a task needs a confirmation,
// and it has to work from the list as well as the detail read.
func TestIntegration_Subtask_CountsOnTaskReads(t *testing.T) {
	env := newTaskServer(t, "pro")
	parent := createTask(t, env, map[string]any{"title": "Parent"})
	if parent.SubtaskCount != 0 || parent.OpenSubtaskCount != 0 {
		t.Fatalf("fresh task counts = %d/%d, want 0/0", parent.SubtaskCount, parent.OpenSubtaskCount)
	}

	a := createSubtask(t, env, parent.ID, map[string]any{"title": "Step A"})
	createSubtask(t, env, parent.ID, map[string]any{"title": "Step B"})

	assertCounts := func(where string, v task.TaskView, total, open int) {
		t.Helper()
		if v.SubtaskCount != total || v.OpenSubtaskCount != open {
			t.Errorf("%s counts = %d/%d, want %d/%d", where, v.SubtaskCount, v.OpenSubtaskCount, total, open)
		}
	}

	assertCounts("get", getTaskByID(t, env, parent.ID), 2, 2)
	assertCounts("list", findTask(t, listTasks(t, env, ""), parent.ID), 2, 2)

	// Closing one subtask drops only the open count.
	resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+parent.ID+"/subtasks/"+a.ID,
		map[string]any{"done": true}, env.token)
	assertStatus(t, resp, http.StatusOK)

	assertCounts("get after done", getTaskByID(t, env, parent.ID), 2, 1)
	assertCounts("list after done", findTask(t, listTasks(t, env, ""), parent.ID), 2, 1)

	// Deleting the closed subtask drops the total, leaving the open count alone.
	resp = do(t, env.srv, http.MethodDelete, "/v1/tasks/"+parent.ID+"/subtasks/"+a.ID, nil, env.token)
	assertStatus(t, resp, http.StatusNoContent)
	assertCounts("get after delete", getTaskByID(t, env, parent.ID), 1, 1)
}

func getTaskByID(t *testing.T, env taskEnv, id string) task.TaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodGet, "/v1/tasks/"+id, nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data
}

func findTask(t *testing.T, items []task.TaskView, id string) task.TaskView {
	t.Helper()
	for _, v := range items {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("task %s not in list", id)
	return task.TaskView{}
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
