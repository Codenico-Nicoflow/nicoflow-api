package handler

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

func Health(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
