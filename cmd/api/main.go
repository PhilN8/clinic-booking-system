// Command api runs the clinic booking HTTP service.
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

	"github.com/PhilN8/clinic-booking-system/internal/config"
	"github.com/PhilN8/clinic-booking-system/internal/database"
	"github.com/PhilN8/clinic-booking-system/internal/handlers"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
	"github.com/PhilN8/clinic-booking-system/internal/service"
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
	router := handlers.NewRouter(svc, db, logger)

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
