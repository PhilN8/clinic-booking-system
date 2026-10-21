// Package handlers is the HTTP transport layer.
//
// Handlers decode the request, call the service and encode the response.
// They contain no business rules and no SQL.
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

// API bundles the HTTP endpoints behind one router.
type API struct {
	svc    service.API
	health HealthChecker
	logger *slog.Logger
}

// NewRouter builds the HTTP router.
//
// Go 1.22's ServeMux understands method-and-wildcard patterns such as
// "GET /doctors/{id}/availability", so no third-party router is needed.
func NewRouter(svc service.API, health HealthChecker, logger *slog.Logger) http.Handler {
	api := &API{svc: svc, health: health, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.healthz)
	mux.HandleFunc("POST /appointments", api.bookAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/cancel", api.cancelAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/reschedule", api.rescheduleAppointment)
	mux.HandleFunc("GET /doctors", api.listDoctors)
	mux.HandleFunc("GET /doctors/{id}", api.getDoctor)
	mux.HandleFunc("GET /doctors/{id}/availability", api.availability)
	mux.HandleFunc("GET /patients/{id}", api.getPatient)
	mux.HandleFunc("GET /patients/{id}/appointments", api.patientAppointments)

	return api.withMiddleware(mux)
}

// withMiddleware wraps the router in request logging and panic recovery.
func (a *API) withMiddleware(next http.Handler) http.Handler {
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
