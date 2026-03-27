package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type SubtaskHandler struct {
	svc *service.SubtaskService
}

func NewSubtaskHandler(svc *service.SubtaskService) *SubtaskHandler {
	return &SubtaskHandler{svc: svc}
}

func (h *SubtaskHandler) Create(c *gin.Context) {
	taskID := c.Param("taskId")
	var req struct {
		Title string `json:"title" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	s, err := h.svc.Create(c.Request.Context(), taskID, req.Title)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, s)
}

func (h *SubtaskHandler) List(c *gin.Context) {
	taskID := c.Param("taskId")
	subtasks, err := h.svc.ListByTask(c.Request.Context(), taskID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, subtasks)
}

func (h *SubtaskHandler) Update(c *gin.Context) {
	taskID := c.Param("taskId")
	id := c.Param("subtaskId")
	var req struct {
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	s, err := h.svc.Update(c.Request.Context(), id, taskID, req.Done, req.Title)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, s)
}

func (h *SubtaskHandler) Delete(c *gin.Context) {
	taskID := c.Param("taskId")
	id := c.Param("subtaskId")
	if err := h.svc.Delete(c.Request.Context(), id, taskID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
