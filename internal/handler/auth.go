package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register godoc
// @Summary      Register a new user
// @Description  Creates a new user account with email and password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.RegisterRequest  true  "Registration credentials"
// @Success      201   {object}  swaggertypes.UserResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      500   {object}  swaggertypes.ErrorResponse
// @Router       /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	user, err := h.svc.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, user)
}

// Login godoc
// @Summary      Log in
// @Description  Authenticates a user and returns access + refresh tokens
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.LoginRequest  true  "Login credentials"
// @Success      200   {object}  swaggertypes.TokensResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      401   {object}  swaggertypes.ErrorResponse
// @Router       /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	access, refresh, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, response.ErrUnauthorized, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"accessToken": access, "refreshToken": refresh})
}

// Refresh godoc
// @Summary      Refresh access token
// @Description  Exchanges a valid refresh token for a new access token (single-use rotation)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.RefreshRequest  true  "Refresh token"
// @Success      200   {object}  swaggertypes.AccessTokenResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      401   {object}  swaggertypes.ErrorResponse
// @Router       /auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	access, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, response.ErrInvalidToken, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"accessToken": access})
}

// Logout godoc
// @Summary      Log out
// @Description  Logs out the current user (client should discard tokens)
// @Tags         auth
// @Produce      json
// @Success      200  {object}  swaggertypes.MessageResponse
// @Router       /auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	response.RespondOK(c, gin.H{"message": "logged out"})
}
