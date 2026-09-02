package auth

import "time"

// User is the domain model for an authenticated user.
type User struct {
	ID               string
	Email            string
	Username         string
	PasswordHash     string
	FirstName        string
	LastName         string
	Theme            string
	Language         string
	ImageURL         string
	Status           string
	Plan             string
	Timezone         string
	Calendar         CalendarPrefs
	EmailVerified    bool
	FailedLoginCount int
	LockedUntil      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CalendarPrefs is how a user wants the calendar grid drawn (NIC-1890).
//
// Grouped rather than four loose fields on User because they are only ever read
// and written together, and the client renders them as one settings panel.
type CalendarPrefs struct {
	// WeekStart is 0=Sunday … 6=Saturday, matching JS getDay() and Go
	// time.Weekday so no consumer needs a translation table.
	WeekStart int
	// Workdays are the weekdays the grid draws, same 0–6 encoding. A set rather
	// than a "workdays only" flag: the work week is Mon–Fri in most of Europe
	// but Sun–Thu in Israel, so a boolean cannot express both.
	Workdays []int
	// DayStartHour is the first hour drawn, 0–23.
	DayStartHour int
	// DayEndHour is the last hour drawn, EXCLUSIVE, 1–24. 24 means "through
	// midnight" — an inclusive 23 could not express 08:00–00:00.
	DayEndHour int
}

// RefreshToken is a stored refresh token row.
type RefreshToken struct {
	ID               string
	UserID           string
	TokenHash        string
	TokenFingerprint string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

// PasswordResetToken is a stored password-reset token row.
type PasswordResetToken struct {
	ID               string
	UserID           string
	TokenHash        string
	TokenFingerprint string
	ExpiresAt        time.Time
	UsedAt           *time.Time
	CreatedAt        time.Time
}

// EmailVerificationToken is a stored email-verification token row.
type EmailVerificationToken struct {
	ID               string
	UserID           string
	TokenHash        string
	TokenFingerprint string
	ExpiresAt        time.Time
	UsedAt           *time.Time
	CreatedAt        time.Time
}

// RegisterRequest is the body for POST /v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
	Platform string `json:"platform"`
}

// LoginRequest is the body for POST /v1/auth/login.
// Identifier accepts either an email address or a username. Email is kept as a
// fallback for older clients that still send the `email` field.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Remember   bool   `json:"remember"`
	Platform   string `json:"platform"`
	Timezone   string `json:"timezone"`
}

// LoginIdentifier returns the identifier to authenticate with, preferring the
// new `identifier` field and falling back to the legacy `email` field.
func (r LoginRequest) LoginIdentifier() string {
	if r.Identifier != "" {
		return r.Identifier
	}
	return r.Email
}

// ForgotPasswordRequest is the body for POST /v1/auth/forgot-password.
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest is the body for POST /v1/auth/reset-password.
type ResetPasswordRequest struct {
	Token           string `json:"token"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

// ChangePasswordRequest is the body for POST /v1/auth/change-password.
// The user is already authenticated (JWT); currentPassword is re-verified
// via bcrypt before the change is applied.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

// VerifyEmailRequest is the body for POST /v1/auth/verify-email.
type VerifyEmailRequest struct {
	Token string `json:"token"`
}

// ResendVerificationRequest is the body for POST /v1/auth/resend-verification.
type ResendVerificationRequest struct {
	Email string `json:"email"`
}

// UpdateMeRequest is the body for PATCH /v1/users/me.
// All fields are optional (pointer = absent means "do not update").
// Email is deliberately absent — it is immutable via this path (account-takeover
// vector: change-then-forgot-password). Username is likewise a login credential
// and not updatable here.
type UpdateMeRequest struct {
	FirstName *string `json:"firstName"`
	LastName  *string `json:"lastName"`
	Timezone  *string `json:"timezone"`
	Theme     *string `json:"theme"`
	Language  *string `json:"language"`
	// Calendar preferences are individually optional too, so a client changing
	// only the day window never has to echo back a week_start it did not read.
	WeekStart    *int  `json:"weekStart"`
	Workdays     []int `json:"workdays"`
	DayStartHour *int  `json:"dayStartHour"`
	DayEndHour   *int  `json:"dayEndHour"`
}

// RegisterPushTokenRequest is the body for POST /v1/users/push-token.
type RegisterPushTokenRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

// AuthResponse is returned by login/register/refresh.
type AuthResponse struct {
	Token        string   `json:"token"`
	RefreshToken string   `json:"refreshToken"`
	User         UserView `json:"user"`
	CookieMaxAge int      `json:"-"` // seconds; 0 = session cookie (no Max-Age)
}

// UserView is the public user shape returned in all auth responses.
type UserView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Theme     string `json:"theme"`
	Language  string `json:"language"`
	Timezone  string `json:"timezone"`
	ImageURL  string `json:"imageUrl"`
	Status    string `json:"status"`
	// Calendar always travels with the user: the grid needs it on first paint,
	// and a second round trip would render one frame of the wrong week.
	Calendar CalendarPrefsView `json:"calendar"`
}

// CalendarPrefsView is the wire shape of the calendar display preferences.
type CalendarPrefsView struct {
	WeekStart    int   `json:"weekStart" validate:"required"`
	Workdays     []int `json:"workdays" validate:"required"`
	DayStartHour int   `json:"dayStartHour" validate:"required"`
	DayEndHour   int   `json:"dayEndHour" validate:"required"`
}

func userToView(u User) UserView {
	// Never nil on the wire: `workdays: null` would make every client write its
	// own fallback, and they would not agree.
	workdays := u.Calendar.Workdays
	if workdays == nil {
		workdays = []int{}
	}

	return UserView{
		ID:        u.ID,
		Email:     u.Email,
		Username:  u.Username,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Theme:     u.Theme,
		Language:  u.Language,
		Timezone:  u.Timezone,
		ImageURL:  u.ImageURL,
		Status:    u.Status,
		Calendar: CalendarPrefsView{
			WeekStart:    u.Calendar.WeekStart,
			Workdays:     workdays,
			DayStartHour: u.Calendar.DayStartHour,
			DayEndHour:   u.Calendar.DayEndHour,
		},
	}
}
