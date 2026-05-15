package auth

import "time"

// User is the domain model for an authenticated user.
type User struct {
	ID           string
	Email        string
	Username     string
	PasswordHash string
	FirstName    string
	LastName     string
	Theme        string
	ImageURL     string
	Status       string
	Plan         string
	Timezone     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

// RegisterRequest is the body for POST /v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
	Platform string `json:"platform"`
}

// LoginRequest is the body for POST /v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
	Platform string `json:"platform"`
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

// UpdateMeRequest is the body for PATCH /v1/users/me.
// All fields are optional (pointer = absent means "do not update").
type UpdateMeRequest struct {
	FirstName *string `json:"firstName"`
	LastName  *string `json:"lastName"`
	Email     *string `json:"email"`
	Timezone  *string `json:"timezone"`
	Theme     *string `json:"theme"`
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
}

// UserView is the public user shape returned in all auth responses.
type UserView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Theme     string `json:"theme"`
	ImageURL  string `json:"imageUrl"`
	Status    string `json:"status"`
}

func userToView(u User) UserView {
	return UserView{
		ID:        u.ID,
		Email:     u.Email,
		Username:  u.Username,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Theme:     u.Theme,
		ImageURL:  u.ImageURL,
		Status:    u.Status,
	}
}
