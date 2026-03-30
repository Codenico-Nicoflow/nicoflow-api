package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"nicoflow-api/internal/dto"
	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
	"nicoflow-api/internal/validations"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register godoc
//
//	@Summary		Register a new user
//	@Description	Creates a new user account with email and password
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.RegisterRequest	true	"Registration credentials"
//	@Success		201		{object}	swaggertypes.UserResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		409		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Router			/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			code, msg := validations.FormatValidationError(ve)
			response.RespondError(c, http.StatusBadRequest, code, msg)
			return
		}
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, "invalid request body")
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !validations.VerifyEmailDomain(req.Email) {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidEmail, "email domain is invalid")
		return
	}

	user, err := h.svc.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			response.RespondError(c, http.StatusConflict, response.ErrEmailAlreadyExists, "email already in use")
			return
		}
		response.RespondError(c, http.StatusInternalServerError, response.ErrInternalServerError, "registration failed")
		return
	}
	response.RespondCreated(c, user)
}

// Login godoc
//
//	@Summary		Log in
//	@Description	Authenticates a user and returns access + refresh tokens
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.LoginRequest	true	"Login credentials"
//	@Success		200		{object}	swaggertypes.TokensResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Router			/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	access, refresh, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, response.ErrUnauthorized, "invalid credentials")
		return
	}
	response.RespondOK(c, gin.H{"access_token": access, "refresh_token": refresh})
}

// Refresh godoc
//
//	@Summary		Refresh tokens
//	@Description	Exchanges a valid refresh token for a new access + refresh token pair (single-use rotation)
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.RefreshRequest	true	"Refresh token"
//	@Success		200		{object}	swaggertypes.TokensResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Router			/auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	access, refresh, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, response.ErrInvalidToken, "invalid or expired refresh token")
		return
	}
	response.RespondOK(c, gin.H{"access_token": access, "refresh_token": refresh})
}

// Logout godoc
//
//	@Summary		Log out
//	@Description	Invalidates the provided refresh token
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		swaggertypes.RefreshRequest	true	"Refresh token to invalidate"
//	@Success		200		{object}	swaggertypes.MessageResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Router			/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	if err := h.svc.Logout(c.Request.Context(), req.RefreshToken); err != nil && !errors.Is(err, service.ErrTokenNotFound) {
		response.RespondError(c, http.StatusInternalServerError, response.ErrInternalServerError, "logout failed")
		return
	}
	response.RespondOK(c, gin.H{"message": "logged out successfully"})
}

// LogoutAll godoc
//
//	@Summary		Log out all devices
//	@Description	Invalidates all refresh tokens for the current user
//	@Tags			auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	swaggertypes.MessageResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Router			/auth/logout-all [post]
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.LogoutAll(c.Request.Context(), userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrInternalServerError, "logout failed")
		return
	}
	response.RespondOK(c, gin.H{"message": "logged out from all devices successfully"})
}
