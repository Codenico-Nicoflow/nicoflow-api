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

func (h *AuthHandler) Logout(c *gin.Context) {
	response.RespondOK(c, gin.H{"message": "logged out"})
}
