// Command api runs the clinic booking HTTP service.
//
// It serves two things on one port:
//
//   - the JSON API, which is what the assessment specifies
//   - the patient facing site, which renders the same data as HTML
//
// Three paths exist as both. Rather than move either one, they negotiate on
// the Accept header: a browser gets the page, an API client gets JSON. The
// JSON API is also mounted under /api, where it is always JSON.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone database so the binary works regardless of the
	// base image's tzdata. The clinic's timezone is a hard requirement, not
	// a nicety.
	_ "time/tzdata"

	"github.com/PhilN8/clinic-booking-system/design"
	"github.com/PhilN8/clinic-booking-system/internal/config"
	"github.com/PhilN8/clinic-booking-system/internal/database"
	"github.com/PhilN8/clinic-booking-system/internal/handlers"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
	"github.com/PhilN8/clinic-booking-system/internal/service"
	"github.com/PhilN8/clinic-booking-system/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// SIGINT and SIGTERM cancel this context, which starts the graceful
	// shutdown. Render sends SIGTERM when it replaces a container.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	repo := repository.New(db, cfg.Location)
	svc := service.New(repo, service.NewClock(), cfg.Location)

	tokens, err := design.Embedded()
	if err != nil {
		return err
	}

	api := handlers.New(svc, db, logger)
	site, err := web.New(svc, cfg.Location, logger, tokens)
	if err != nil {
		return err
	}

	router := routes(api, site)

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
		// A slow or hostile client must not be able to tie up a connection
		// indefinitely.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			"addr", cfg.Addr,
			"clinic_tz", cfg.ClinicLocation,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

// routes builds the router.
//
// The route table lives here rather than in either package because the API
// and the site share three paths and the router has to negotiate between
// them.
func routes(api *handlers.API, site *web.Web) http.Handler {
	mux := http.NewServeMux()

	// The JSON API, always JSON, at /api. This is the unambiguous way to
	// reach it from a browser or a tool that sends "Accept: text/html".
	apiMux := http.NewServeMux()
	registerAPI(apiMux, api)
	mux.Handle("/api/", http.StripPrefix("/api", apiMux))

	// Paths that exist as both an endpoint and a page.
	mux.HandleFunc("GET /doctors", web.Negotiate(api.ListDoctors, site.DoctorsPage))
	mux.HandleFunc("GET /doctors/{id}", web.Negotiate(api.GetDoctor, site.DoctorPage))
	mux.HandleFunc("GET /patients/{id}/appointments",
		web.Negotiate(api.PatientAppointments, site.AppointmentsPage))

	// JSON only.
	mux.HandleFunc("GET /healthz", api.Healthz)
	mux.HandleFunc("POST /appointments", api.BookAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/cancel", api.CancelAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/reschedule", api.RescheduleAppointment)
	mux.HandleFunc("GET /doctors/{id}/availability", api.Availability)
	mux.HandleFunc("GET /patients/{id}", api.GetPatient)

	// Site only.
	mux.HandleFunc("GET /{$}", site.DoctorsPage)
	mux.HandleFunc("POST /doctors/{id}/appointments", site.BookAppointment)
	mux.HandleFunc("GET /appointments/{id}/cancel", site.CancelPage)
	mux.HandleFunc("POST /appointments/{id}/cancel", site.CancelAppointment)
	mux.HandleFunc("GET /appointments/{id}/reschedule", site.ReschedulePage)
	mux.HandleFunc("POST /appointments/{id}/reschedule", site.RescheduleAppointment)
	mux.HandleFunc("GET /appointments/{id}/confirmation", site.ConfirmationPage)
	mux.Handle("/assets/", site.Assets())

	return api.Middleware(mux)
}

// registerAPI adds the JSON API routes to a mux.
func registerAPI(mux *http.ServeMux, api *handlers.API) {
	mux.HandleFunc("GET /healthz", api.Healthz)
	mux.HandleFunc("POST /appointments", api.BookAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/cancel", api.CancelAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/reschedule", api.RescheduleAppointment)
	mux.HandleFunc("GET /doctors", api.ListDoctors)
	mux.HandleFunc("GET /doctors/{id}", api.GetDoctor)
	mux.HandleFunc("GET /doctors/{id}/availability", api.Availability)
	mux.HandleFunc("GET /patients/{id}", api.GetPatient)
	mux.HandleFunc("GET /patients/{id}/appointments", api.PatientAppointments)
}

// newLogger builds the structured logger used across the service.
func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
