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

	// NIC-1997: 11 additional write tools, all propose→confirm.
	ToolSetupRecurringTask      = "setup_recurring_task"
	ToolAdjustRecurringTask     = "adjust_recurring_task"
	ToolPauseRecurringTask      = "pause_recurring_task"
	ToolEndRecurringSeries      = "end_recurring_series"
	ToolCreateNote              = "create_note"
	ToolCreateArea              = "create_area"
	ToolCreateProject           = "create_project"
	ToolUpdateProject           = "update_project"
	ToolAddSubtask              = "add_subtask"
	ToolCompleteSubtask         = "complete_subtask"
	ToolProcessBucketItem       = "process_bucket_item"
	ToolSkipRecurringOccurrence = "skip_recurring_occurrence"
)

// writeTools is the set of tools that require explicit user confirmation
// (proposal → confirm/reject) and never auto-execute.
var writeTools = map[string]bool{
	ToolCompleteTask:            true,
	ToolRescheduleTask:          true,
	ToolCreateTask:              true,
	ToolSetupRecurringTask:      true,
	ToolAdjustRecurringTask:     true,
	ToolPauseRecurringTask:      true,
	ToolEndRecurringSeries:      true,
	ToolCreateNote:              true,
	ToolCreateArea:              true,
	ToolCreateProject:           true,
	ToolUpdateProject:           true,
	ToolAddSubtask:              true,
	ToolCompleteSubtask:         true,
	ToolProcessBucketItem:       true,
	ToolSkipRecurringOccurrence: true,
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
		{
			Name:        ToolSetupRecurringTask,
			Description: "Propose creating a recurring task, either from scratch in a project or by converting an existing task into instance #1 of a new series. This does NOT execute — the user must confirm. Set taskId to convert an existing task (its own title/notes/priority/energy/estimatedMinutes are kept, do not resend them); omit taskId and set projectId+title to create a brand-new recurring task.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["freq", "interval", "startDate"],
				"properties": {
					"taskId":           {"type": "string", "description": "Existing task id to convert. Mutually exclusive with projectId/title."},
					"projectId":        {"type": "string", "description": "Project for a brand-new recurring task. Required if taskId is absent."},
					"title":            {"type": "string", "description": "Task title. Required if taskId is absent."},
					"notes":            {"type": "string"},
					"priority":         {"type": "string", "enum": ["low", "medium", "high"]},
					"energy":           {"type": "string", "enum": ["low", "medium", "deep"]},
					"estimatedMinutes": {"type": "integer", "minimum": 1, "maximum": 1440},
					"freq":             {"type": "string", "enum": ["daily", "weekly", "monthly", "yearly"]},
					"interval":         {"type": "integer", "minimum": 1, "description": "Repeat every N units of freq."},
					"byWeekday":        {"type": "array", "items": {"type": "integer", "minimum": 0, "maximum": 6}, "description": "0=Sunday..6=Saturday, for weekly freq."},
					"byMonthday":       {"type": "integer", "minimum": 1, "maximum": 31, "description": "For monthly/yearly freq."},
					"startDate":        {"type": "string", "description": "YYYY-MM-DD, first occurrence."},
					"endDate":          {"type": "string", "description": "YYYY-MM-DD, optional last occurrence."},
					"scheduledTime":    {"type": "string", "description": "Optional HH:MM stamped on every occurrence (Pro only)."},
					"reason":           {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolAdjustRecurringTask,
			Description: "Propose changing an existing recurring series' schedule or template. This does NOT execute — the user must confirm. Only send the fields that are changing; omitted fields keep their current value.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["ruleId"],
				"properties": {
					"ruleId":           {"type": "string", "description": "The recurrence rule id."},
					"title":            {"type": "string"},
					"notes":            {"type": "string", "description": "Pass an empty string to clear notes."},
					"priority":         {"type": "string", "enum": ["low", "medium", "high"]},
					"energy":           {"type": "string", "enum": ["low", "medium", "deep"]},
					"estimatedMinutes": {"type": "integer", "minimum": 1, "maximum": 1440},
					"freq":             {"type": "string", "enum": ["daily", "weekly", "monthly", "yearly"]},
					"interval":         {"type": "integer", "minimum": 1},
					"byWeekday":        {"type": "array", "items": {"type": "integer", "minimum": 0, "maximum": 6}},
					"byMonthday":       {"type": "integer", "minimum": 1, "maximum": 31},
					"startDate":        {"type": "string", "description": "YYYY-MM-DD."},
					"endDate":          {"type": "string", "description": "YYYY-MM-DD, or null to clear (un-exhaust the series)."},
					"scheduledTime":    {"type": "string", "description": "HH:MM, or null to clear."},
					"reason":           {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolPauseRecurringTask,
			Description: "Propose pausing or resuming a recurring series (stops/resumes future materialization without ending it). This does NOT execute — the user must confirm.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["ruleId", "paused"],
				"properties": {
					"ruleId": {"type": "string", "description": "The recurrence rule id."},
					"paused": {"type": "boolean", "description": "true to pause, false to resume."},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolEndRecurringSeries,
			Description: "Propose ending a recurring series permanently. This does NOT execute — the user must confirm. Every task the series ever produced stays in place as a historical record; only the rule itself is removed, so nothing new materializes.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["ruleId"],
				"properties": {
					"ruleId": {"type": "string", "description": "The recurrence rule id."},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolCreateNote,
			Description: "Propose creating a new project note from structured content blocks. This does NOT execute — the user must confirm. Prefer list_projects first to ground the projectId. Supported block kinds: paragraph, heading (with level 1-6), bulletList/orderedList (items: string[]), taskList (tasks: [{text, checked}]), blockquote, codeBlock (code, optional language), callout (text, variant: info|warn|success|danger), table (optional header: string[], rows: string[][]). No images.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["projectId", "title", "blocks"],
				"properties": {
					"projectId": {"type": "string"},
					"title":     {"type": "string"},
					"blocks": {
						"type": "array",
						"items": {
							"type": "object",
							"required": ["kind"],
							"properties": {
								"kind":     {"type": "string", "enum": ["paragraph", "heading", "bulletList", "orderedList", "taskList", "blockquote", "codeBlock", "callout", "table"]},
								"text":     {"type": "string"},
								"level":    {"type": "integer", "minimum": 1, "maximum": 6},
								"items":    {"type": "array", "items": {"type": "string"}},
								"tasks":    {"type": "array", "items": {"type": "object", "properties": {"text": {"type": "string"}, "checked": {"type": "boolean"}}}},
								"code":     {"type": "string"},
								"language": {"type": "string"},
								"variant":  {"type": "string", "enum": ["info", "warn", "success", "danger"]},
								"header":   {"type": "array", "items": {"type": "string"}},
								"rows":     {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}
							}
						}
					},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolCreateArea,
			Description: "Propose creating a new top-level area of responsibility. This does NOT execute — the user must confirm.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["name"],
				"properties": {
					"name":   {"type": "string"},
					"color":  {"type": "string", "description": "Hex color, e.g. #3B82F6."},
					"icon":   {"type": "string"},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolCreateProject,
			Description: "Propose creating a new project inside an area. This does NOT execute — the user must confirm. Prefer listing areas or grounding areaId from context before proposing.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["areaId", "name"],
				"properties": {
					"areaId":      {"type": "string"},
					"name":        {"type": "string"},
					"description": {"type": "string"},
					"folderIcon":  {"type": "string"},
					"reason":      {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolUpdateProject,
			Description: "Propose editing an existing project (name, description, area, or folder icon). This does NOT execute — the user must confirm. Only send the fields that are changing.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["projectId"],
				"properties": {
					"projectId":   {"type": "string"},
					"name":        {"type": "string"},
					"description": {"type": "string", "description": "Pass an empty string to clear."},
					"areaId":      {"type": "string", "description": "Move the project to a different area."},
					"folderIcon":  {"type": "string"},
					"reason":      {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolAddSubtask,
			Description: "Propose adding a subtask to a task. This does NOT execute — the user must confirm.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId", "title"],
				"properties": {
					"taskId": {"type": "string"},
					"title":  {"type": "string"},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolCompleteSubtask,
			Description: "Propose marking a subtask done. This does NOT execute — the user must confirm.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId", "subtaskId"],
				"properties": {
					"taskId":    {"type": "string", "description": "The parent task id."},
					"subtaskId": {"type": "string"},
					"reason":    {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolProcessBucketItem,
			Description: "Propose processing one inbox (bucket) item into a task, a note, or trash. This does NOT execute — the user must confirm. projectId is required when processingResult is task or note. For task, fill taskDetails (same shape as create_task minus projectId). For note, fill noteDetails with title+blocks (same block vocabulary as create_note).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["bucketId", "processingResult"],
				"properties": {
					"bucketId":         {"type": "string"},
					"processingResult": {"type": "string", "enum": ["task", "note", "trash"]},
					"projectId":        {"type": "string", "description": "Required when processingResult is task or note."},
					"taskDetails": {
						"type": "object",
						"properties": {
							"title":        {"type": "string"},
							"notes":        {"type": "string"},
							"priority":     {"type": "string", "enum": ["low", "medium", "high"]},
							"energy":       {"type": "string", "enum": ["low", "medium", "deep"]},
							"scheduledFor": {"type": "string", "description": "YYYY-MM-DD."}
						}
					},
					"noteDetails": {
						"type": "object",
						"properties": {
							"title": {"type": "string"},
							"blocks": {
								"type": "array",
								"items": {
									"type": "object",
									"required": ["kind"],
									"properties": {
										"kind":     {"type": "string", "enum": ["paragraph", "heading", "bulletList", "orderedList", "taskList", "blockquote", "codeBlock", "callout", "table"]},
										"text":     {"type": "string"},
										"level":    {"type": "integer", "minimum": 1, "maximum": 6},
										"items":    {"type": "array", "items": {"type": "string"}},
										"tasks":    {"type": "array", "items": {"type": "object", "properties": {"text": {"type": "string"}, "checked": {"type": "boolean"}}}},
										"code":     {"type": "string"},
										"language": {"type": "string"},
										"variant":  {"type": "string", "enum": ["info", "warn", "success", "danger"]},
										"header":   {"type": "array", "items": {"type": "string"}},
										"rows":     {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}
									}
								}
							}
						}
					},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
		{
			Name:        ToolSkipRecurringOccurrence,
			Description: "Propose skipping the current live occurrence of a recurring task without breaking the user's streak. The next occurrence will be materialized immediately. This does NOT execute — the user must confirm.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"required": ["taskId"],
				"properties": {
					"taskId": {"type": "string", "description": "The id of the live recurring task occurrence to skip."},
					"reason": {"type": "string", "description": "Short natural-language rationale shown on the confirm card."}
				}
			}`),
		},
	}
}

// AvailableTools filters a full catalog down to the tools whose backing seam
// is actually wired, keyed by tool name. Names absent from the map (the six
// original tools, always available whenever an executor exists at all) are
// never filtered out.
func AvailableTools(all []ToolDefinition, enabled map[string]bool) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(all))
	for _, t := range all {
		if ok, gated := enabled[t.Name]; gated && !ok {
			continue
		}
		out = append(out, t)
	}
	return out
}
