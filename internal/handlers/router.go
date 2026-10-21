// Package handlers is the HTTP transport layer for the JSON API.
//
// Handlers decode the request, call the service and encode the response.
// They contain no business rules and no SQL.
//
// The route table itself lives in cmd/api, because the API and the patient
// facing site share some paths and the router has to negotiate between them.
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// HealthChecker reports whether the dependencies of the service are healthy.
// *sql.DB satisfies it.
type HealthChecker interface {
	PingContext(ctx context.Context) error
}

// API bundles the JSON API endpoints.
type API struct {
	svc    service.API
	health HealthChecker
	logger *slog.Logger
}

// New builds the API handlers.
func New(svc service.API, health HealthChecker, logger *slog.Logger) *API {
	return &API{svc: svc, health: health, logger: logger}
}

// Middleware wraps the router in request logging and panic recovery.
func (a *API) Middleware(next http.Handler) http.Handler {
	return a.recoverer(a.logRequests(next))
}

// logRequests records one structured line per request.
func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		a.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(start).String(),
		)
	})
}

// recoverer turns a panic into a 500 instead of tearing down the connection.
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("panic serving request",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				httperr.Write(w, httperr.Internal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status code written to the response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
