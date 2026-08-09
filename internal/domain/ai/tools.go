package ai

import "encoding/json"

// Tool names Claude sees over the wire. Constants so the switch in the
// stream loop and the write-set below cannot drift.
const (
	ToolListTasks      = "list_tasks"
	ToolGetTask        = "get_task"
	ToolListProjects   = "list_projects"
	ToolCompleteTask   = "complete_task"
	ToolRescheduleTask = "reschedule_task"
	ToolCreateTask     = "create_task"
)

// writeTools is the set of tools that require explicit user confirmation
// (proposal → confirm/reject) and never auto-execute.
var writeTools = map[string]bool{
	ToolCompleteTask:   true,
	ToolRescheduleTask: true,
	ToolCreateTask:     true,
}

// IsWriteTool reports whether the named tool is a write tool.
func IsWriteTool(name string) bool { return writeTools[name] }

// ToolDefinition is the provider-agnostic tool shape the anthropic adapter
// translates into sdk.ToolUnionParam. Kept here (the consumer package) so
// the ai domain stays provider-agnostic — the same pattern as ChatRequest.
//
// InputSchema is a raw JSON schema object; using json.RawMessage rather than
// a typed struct keeps this file free of provider assumptions and avoids
// the `any` type the project bans (the value is the schema bytes we pass
// through verbatim).
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// DefaultTools returns the tool catalog Claude sees on every turn. Static —
// the set never varies per user/plan (plan is enforced by the executor when
// a write tool runs, not by hiding a tool from the model).
func DefaultTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolListTasks,
			Description: "List the user's tasks with optional filters. Returns a slim payload (id, title, status, projectId, scheduledFor) by default — set verbose=true only when the full task details are needed.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"status":        {"type": "string", "enum": ["active", "done", "cancelled"], "description": "Filter by status."},
					"priority":      {"type": "string", "enum": ["low", "medium", "high"], "description": "Filter by priority."},
					"energy":        {"type": "string", "enum": ["low", "medium", "deep"], "description": "Filter by required energy level."},
					"projectId":     {"type": "string", "description": "Restrict to tasks in this project."},
					"scheduledFrom": {"type": "string", "description": "Inclusive start date, YYYY-MM-DD."},
					"scheduledTo":   {"type": "string", "description": "Inclusive end date, YYYY-MM-DD."},
					"search":        {"type": "string", "description": "Case-insensitive substring match over title and notes."},
					"limit":         {"type": "integer", "minimum": 1, "maximum": 50, "description": "Cap on returned items; default 20."},
					"verbose":       {"type": "boolean", "description": "When true, return the full task shape instead of the slim projection."}
				}
			}`),
		},
		{
			Name:        ToolGetTask,
			Description: "Fetch one task's full detail by id. Use when the user is asking about a specific task and list_tasks alone isn't enough.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId"],
				"properties": {
					"taskId": {"type": "string", "description": "The task id."}
				}
			}`),
		},
		{
			Name:        ToolListProjects,
			Description: "List all of the user's projects (id, name, areaId, status). Use to ground project references in real data before proposing a create_task.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name:        ToolCompleteTask,
			Description: "Propose marking a task complete. This does NOT execute — the user must confirm the proposal in the UI. Never claim the task is done until the user confirms.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId"],
				"properties": {
					"taskId": {"type": "string", "description": "The task id to complete."},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolRescheduleTask,
			Description: "Propose rescheduling a task. This does NOT execute — the user must confirm the proposal in the UI. Never claim the task is rescheduled until the user confirms.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId"],
				"properties": {
					"taskId":        {"type": "string", "description": "The task id."},
					"scheduledFor":  {"type": "string", "description": "New soft date YYYY-MM-DD, or null to unschedule."},
					"scheduledTime": {"type": "string", "description": "Optional time HH:MM (Pro only). Requires scheduledFor."},
					"rollsOver":     {"type": "boolean", "description": "Whether the task rolls forward if the day passes."},
					"reason":        {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolCreateTask,
			Description: "Propose creating a new task in a project. This does NOT execute — the user must confirm the proposal in the UI. Never claim the task is created until the user confirms. Prefer list_projects first to ground the projectId.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["projectId", "title"],
				"properties": {
					"projectId":        {"type": "string", "description": "The project the task should live in."},
					"title":            {"type": "string", "description": "Task title (1..255 chars)."},
					"notes":            {"type": "string", "description": "Optional notes (<= 2000 chars)."},
					"status":           {"type": "string", "enum": ["active", "done", "cancelled"], "description": "Initial status (defaults to active)."},
					"priority":         {"type": "string", "enum": ["low", "medium", "high"]},
					"energy":           {"type": "string", "enum": ["low", "medium", "deep"]},
					"rollsOver":        {"type": "boolean"},
					"scheduledFor":     {"type": "string", "description": "Soft date YYYY-MM-DD."},
					"scheduledTime":    {"type": "string", "description": "Optional time HH:MM (Pro only). Requires scheduledFor."},
					"estimatedMinutes": {"type": "integer", "minimum": 1, "maximum": 1440},
					"url":              {"type": "string"},
					"reason":           {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
	}
}
