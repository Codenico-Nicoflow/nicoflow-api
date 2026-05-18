package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Health returns 200 when the server is up and the DB pool is reachable,
// or 503 when the DB ping fails — used by Render health checks.
func Health(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			respond.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
