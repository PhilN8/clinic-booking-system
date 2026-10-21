package handlers

import (
	"net/http"
)

// healthz handles GET /healthz.
//
// It pings the database so that the health check fails when the service
// cannot actually serve traffic. The deploy platform uses this path to
// decide whether the container is ready.
func (a *API) Healthz(w http.ResponseWriter, r *http.Request) {
	if a.health != nil {
		if err := a.health.PingContext(r.Context()); err != nil {
			a.logger.Error("health check failed", "error", err.Error())
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
