package search

// Result group names accepted in the `types` query param and returned in the response.
const (
	TypeTask    = "task"
	TypeProject = "project"
	TypeArea    = "area"
	TypeNote    = "note"
)

// Query holds the validated search parameters passed from handler to service.
type Query struct {
	Term  string   // validated 2–100 chars
	Types []string // requested subset of {task, project, area, note}; empty means "all"
	Limit int      // per-type cap, 1–50
}

// TaskResult is a single task search hit.
type TaskResult struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Excerpt     string `json:"excerpt"`
	ProjectName string `json:"projectName"`
	ProjectID   string `json:"projectId"`
}

// ProjectResult is a single project search hit.
type ProjectResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	AreaName string `json:"areaName"`
}

// AreaResult is a single area search hit.
type AreaResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NoteResult is a single note search hit. ProjectID/ProjectName are empty for an
// ORPHANED note — deleting a project sets notes.project_id to NULL rather than
// destroying the rows (migration 044), and search is user-scoped, not
// project-scoped, so those notes stay reachable. Search is the only surface that
// can still find them.
type NoteResult struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Excerpt     string `json:"excerpt"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
}

// Response is the grouped search payload wrapped by the standard {data,error} envelope.
type Response struct {
	Tasks    []TaskResult    `json:"tasks"`
	Projects []ProjectResult `json:"projects"`
	Areas    []AreaResult    `json:"areas"`
	Notes    []NoteResult    `json:"notes"`
}
