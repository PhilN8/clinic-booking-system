// Package config loads runtime configuration from the environment.
//
// Every setting has a sensible default except DATABASE_URL: without it the
// service has nothing to talk to and must refuse to start.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds everything the API server needs at runtime.
type Config struct {
	// Addr is the TCP address the HTTP server listens on, e.g. ":8080".
	Addr string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// ClinicLocation is the IANA name of the timezone the clinic operates in.
	// Appointment times are wall-clock times in this zone.
	ClinicLocation string
	// Location is the resolved ClinicLocation.
	Location *time.Location
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
}

// DefaultClinicLocation is the timezone the clinic operates in when
// CLINIC_TZ is not set. Nairobi has a fixed +03:00 offset and no DST, which
// is what makes the wall-clock time model in the service layer exact.
const DefaultClinicLocation = "Africa/Nairobi"

// Load reads configuration from the environment and resolves the clinic
// timezone. It returns an error when the environment is unusable.
func Load() (Config, error) {
	cfg := Config{
		Addr:           ":" + envOrDefault("PORT", "8080"),
		DatabaseURL:    strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ClinicLocation: envOrDefault("CLINIC_TZ", DefaultClinicLocation),
		LogLevel:       strings.ToLower(envOrDefault("LOG_LEVEL", "info")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	loc, err := time.LoadLocation(cfg.ClinicLocation)
	if err != nil {
		return Config{}, fmt.Errorf("config: CLINIC_TZ %q is not a known IANA timezone: %w", cfg.ClinicLocation, err)
	}
	cfg.Location = loc

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
