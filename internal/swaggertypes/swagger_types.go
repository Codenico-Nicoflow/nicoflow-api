// Package swaggertypes contains request/response types used exclusively for Swagger documentation.
// These types mirror the anonymous structs used in handlers and give swaggo concrete
// named types to generate accurate OpenAPI specs.
package swaggertypes

import (
	"time"
)

// ── Request types ────────────────────────────────────────────────────────────

type RegisterRequest struct {
	Email    string `json:"email"    example:"user@example.com"`
	Password string `json:"password" example:"password123"`
}

type LoginRequest struct {
	Email    string `json:"email"    example:"user@example.com"`
	Password string `json:"password" example:"password123"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

type CreateAreaRequest struct {
	Name string `json:"name" example:"Work"`
}

type UpdateAreaRequest struct {
	Name string `json:"name" example:"Personal"`
}

type CreateProjectRequest struct {
	Name   string  `json:"name"   example:"Launch blog"`
	AreaID *string `json:"areaId" example:"area_01jw..."`
}

type UpdateProjectRequest struct {
	Name   string  `json:"name"   example:"Launch blog v2"`
	AreaID *string `json:"areaId" example:"area_01jw..."`
}

type CreateTaskRequest struct {
	Title        string  `json:"title"        example:"Write unit tests"`
	ProjectID    *string `json:"projectId"    example:"proj_01jw..."`
	DueDate      *string `json:"dueDate"      example:"2026-04-01"`
	ScheduledFor *string `json:"scheduledFor" example:"today"`
}

type UpdateTaskRequest struct {
	Title        string  `json:"title"        example:"Write unit tests"`
	ProjectID    *string `json:"projectId"    example:"proj_01jw..."`
	DueDate      *string `json:"dueDate"      example:"2026-04-01"`
	ScheduledFor *string `json:"scheduledFor" example:"today"`
	Status       string  `json:"status"       example:"done"`
	Notes        *string `json:"notes"        example:"See PR #42"`
}

type CreateSubtaskRequest struct {
	Title string `json:"title" example:"Write test cases"`
}

type UpdateSubtaskRequest struct {
	Title string `json:"title" example:"Write test cases"`
	Done  bool   `json:"done"  example:"true"`
}

type CaptureInboxRequest struct {
	Title string `json:"title" example:"Read Go spec"`
}

type NLPParseRequest struct {
	Input string `json:"input" example:"Call dentist tomorrow"`
}

type UploadURLRequest struct {
	Filename string `json:"filename" example:"screenshot.png"`
}

type DownloadURLRequest struct {
	Key string `json:"key" example:"uploads/user_01jw.../screenshot.png"`
}

type SendMessageRequest struct {
	Content string `json:"content" example:"Summarise my tasks for today"`
}

// ── Response envelope helpers ─────────────────────────────────────────────────

type ErrorBody struct {
	Code    string `json:"code"    example:"INVALID_INPUT"`
	Message string `json:"message" example:"email is required"`
}

type ErrorResponse struct {
	Data  *struct{}  `json:"data"`
	Error *ErrorBody `json:"error"`
}

// ── Typed success envelopes ───────────────────────────────────────────────────

type UserData struct {
	ID        string    `json:"id"         example:"usr_01jw..."`
	Email     string    `json:"email"      example:"user@example.com"`
	Name      string    `json:"name"       example:"Jane Doe"`
	Timezone  string    `json:"timezone"   example:"UTC"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserResponse struct {
	Data  *UserData  `json:"data"`
	Error *ErrorBody `json:"error"`
}

type TokensData struct {
	AccessToken  string `json:"accessToken"  example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	RefreshToken string `json:"refreshToken" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

type TokensResponse struct {
	Data  *TokensData `json:"data"`
	Error *ErrorBody  `json:"error"`
}

type AccessTokenData struct {
	AccessToken string `json:"accessToken" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

type AccessTokenResponse struct {
	Data  *AccessTokenData `json:"data"`
	Error *ErrorBody       `json:"error"`
}

type MessageData struct {
	Message string `json:"message" example:"logged out"`
}

type MessageResponse struct {
	Data  *MessageData `json:"data"`
	Error *ErrorBody   `json:"error"`
}

type AreaData struct {
	ID           string    `json:"id"           example:"area_01jw..."`
	UserID       string    `json:"userId"       example:"usr_01jw..."`
	Name         string    `json:"name"         example:"Work"`
	Color        string    `json:"color"        example:"#3B82F6"`
	DisplayOrder int       `json:"displayOrder" example:"0"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AreaResponse struct {
	Data  *AreaData  `json:"data"`
	Error *ErrorBody `json:"error"`
}

type AreaListResponse struct {
	Data  []AreaData `json:"data"`
	Error *ErrorBody `json:"error"`
}

type ProjectData struct {
	ID           string    `json:"id"            example:"proj_01jw..."`
	UserID       string    `json:"user_id"       example:"usr_01jw..."`
	AreaID       *string   `json:"area_id"       example:"area_01jw..."`
	Name         string    `json:"name"          example:"Launch blog"`
	Status       string    `json:"status"        example:"active"`
	DisplayOrder int       `json:"display_order" example:"0"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ProjectResponse struct {
	Data  *ProjectData `json:"data"`
	Error *ErrorBody   `json:"error"`
}

type ProjectListResponse struct {
	Data  []ProjectData `json:"data"`
	Error *ErrorBody    `json:"error"`
}

type TaskData struct {
	ID           string    `json:"id"                      example:"task_01jw..."`
	UserID       string    `json:"user_id"                 example:"usr_01jw..."`
	ProjectID    *string   `json:"project_id,omitempty"    example:"proj_01jw..."`
	Title        string    `json:"title"                   example:"Write unit tests"`
	Notes        *string   `json:"notes,omitempty"         example:"See PR #42"`
	DueDate      *string   `json:"due_date,omitempty"      example:"2026-04-01"`
	ScheduledFor *string   `json:"scheduled_for,omitempty" example:"today"`
	Status       string    `json:"status"                  example:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type TaskResponse struct {
	Data  *TaskData  `json:"data"`
	Error *ErrorBody `json:"error"`
}

type TaskListResponse struct {
	Data  []TaskData `json:"data"`
	Error *ErrorBody `json:"error"`
}

type SubtaskData struct {
	ID        string    `json:"id"        example:"sub_01jw..."`
	TaskID    string    `json:"task_id"   example:"task_01jw..."`
	Title     string    `json:"title"     example:"Write test cases"`
	Done      bool      `json:"done"      example:"false"`
	Position  int       `json:"position"  example:"0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SubtaskResponse struct {
	Data  *SubtaskData `json:"data"`
	Error *ErrorBody   `json:"error"`
}

type SubtaskListResponse struct {
	Data  []SubtaskData `json:"data"`
	Error *ErrorBody    `json:"error"`
}

type NLPResultData struct {
	Title        string  `json:"title"                   example:"Call dentist"`
	DueDate      *string `json:"due_date,omitempty"      example:"2026-03-29"`
	ScheduledFor *string `json:"scheduled_for,omitempty" example:"tomorrow"`
}

type NLPResponse struct {
	Data  *NLPResultData `json:"data"`
	Error *ErrorBody     `json:"error"`
}

type UserPlanData struct {
	ID                         string     `json:"id"                                    example:"plan_01jw..."`
	UserID                     string     `json:"user_id"                               example:"usr_01jw..."`
	Plan                       string     `json:"plan"                                  example:"free"`
	Status                     string     `json:"status"                                example:"active"`
	LemonSqueezySubscriptionID *string    `json:"lemon_squeezy_subscription_id,omitempty"`
	LemonSqueezyCustomerID     *string    `json:"lemon_squeezy_customer_id,omitempty"`
	CurrentPeriodStart         *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd           *time.Time `json:"current_period_end,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type UserPlanResponse struct {
	Data  *UserPlanData `json:"data"`
	Error *ErrorBody    `json:"error"`
}

type AISessionData struct {
	ID        string     `json:"id"         example:"sess_01jw..."`
	UserID    string     `json:"user_id"    example:"usr_01jw..."`
	Title     string     `json:"title"      example:"New Conversation"`
	Status    string     `json:"status"     example:"active"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type AISessionResponse struct {
	Data  *AISessionData `json:"data"`
	Error *ErrorBody     `json:"error"`
}

type AISessionListResponse struct {
	Data  []AISessionData `json:"data"`
	Error *ErrorBody      `json:"error"`
}

type AIMessageData struct {
	ID        string    `json:"id"         example:"msg_01jw..."`
	SessionID string    `json:"session_id" example:"sess_01jw..."`
	Role      string    `json:"role"       example:"user"`
	Content   string    `json:"content"    example:"Summarise my tasks for today"`
	CreatedAt time.Time `json:"created_at"`
}

type AIMessageResponse struct {
	Data  *AIMessageData `json:"data"`
	Error *ErrorBody     `json:"error"`
}

type AIMessageListResponse struct {
	Data  []AIMessageData `json:"data"`
	Error *ErrorBody      `json:"error"`
}

type URLData struct {
	URL string `json:"url" example:"https://bucket.s3.amazonaws.com/..."`
}

type URLResponse struct {
	Data  *URLData   `json:"data"`
	Error *ErrorBody `json:"error"`
}

type BillingURLData struct {
	URL string `json:"url" example:"https://app.lemonsqueezy.com/checkout/..."`
}

type BillingURLResponse struct {
	Data  *BillingURLData `json:"data"`
	Error *ErrorBody      `json:"error"`
}

type EmptyData struct{}

type EmptyResponse struct {
	Data  *EmptyData `json:"data"`
	Error *ErrorBody `json:"error"`
}

type HealthData struct {
	Status string `json:"status" example:"ok"`
}

type HealthResponse struct {
	Data  *HealthData `json:"data"`
	Error *ErrorBody  `json:"error"`
}
