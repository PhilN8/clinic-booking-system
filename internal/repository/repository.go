// Package repository is the Postgres implementation of the persistence layer.
//
// It owns every SQL statement and every transaction. It contains no business
// rules: validation lives in the service layer, and transport concerns live
// in the handler layer.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// Repository is the Postgres-backed implementation of the persistence layer.
type Repository struct {
	db *sql.DB
	// loc is the clinic's timezone. It is needed to turn a naive wall-clock
	// appointment into an absolute instant when comparing it against "now".
	loc *time.Location
}

// New builds a Repository bound to db. The clinic location is required to
// evaluate time-based rules against the database rows.
func New(db *sql.DB, loc *time.Location) *Repository {
	return &Repository{db: db, loc: loc}
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTimeOfDay converts a scanned TIME column into a wall-clock time.
//
// The driver may hand the value back either as text ("09:00:00") or as a
// time.Time, depending on the wire format it negotiated, so both are
// accepted. Keeping the conversion explicit means the repository does not
// depend on a driver-specific type mapping.
func scanTimeOfDay(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return models.ClockOfDay(v), nil
	case string:
		return parseClockTime(v)
	case []byte:
		return parseClockTime(string(v))
	default:
		return time.Time{}, fmt.Errorf("repository: unexpected type %T for a time column", value)
	}
}

// parseClockTime parses the text form of a Postgres TIME column.
func parseClockTime(value string) (time.Time, error) {
	for _, layout := range []string{"15:04:05", "15:04:05.999999", "15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return models.TimeOfDay(parsed.Hour(), parsed.Minute()), nil
		}
	}
	return time.Time{}, fmt.Errorf("repository: cannot parse %q as a time of day", value)
}

// scanDate converts a scanned DATE column into a calendar date.
func scanDate(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return models.CalendarDate(v), nil
	case string:
		return parseCalendarDate(v)
	case []byte:
		return parseCalendarDate(string(v))
	default:
		return time.Time{}, fmt.Errorf("repository: unexpected type %T for a date column", value)
	}
}

// parseCalendarDate parses the text form of a Postgres DATE column.
func parseCalendarDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("repository: cannot parse %q as a date: %w", value, err)
	}
	return models.CalendarDate(parsed), nil
}

// scanTimestamp converts a scanned TIMESTAMPTZ column into an absolute
// instant. A nil value means SQL NULL.
func scanTimestamp(value any) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case time.Time:
		return &v, nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return nil, fmt.Errorf("repository: cannot parse %q as a timestamp: %w", v, err)
		}
		return &parsed, nil
	case []byte:
		parsed, err := time.Parse(time.RFC3339Nano, string(v))
		if err != nil {
			return nil, fmt.Errorf("repository: cannot parse %q as a timestamp: %w", string(v), err)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("repository: unexpected type %T for a timestamp column", value)
	}
}

// startsAt converts an appointment's naive wall-clock date and time into the
// absolute instant it occurs at, in the clinic's timezone.
//
// The clinic zone has a fixed UTC offset and no daylight saving, so this
// conversion is exact. A zone with DST would need revisiting.
func (r *Repository) startsAt(a *models.Appointment) time.Time {
	return time.Date(
		a.AppointmentDate.Year(), a.AppointmentDate.Month(), a.AppointmentDate.Day(),
		a.StartTime.Hour(), a.StartTime.Minute(), 0, 0,
		r.loc,
	)
}

// ctxWithTimeout bounds a single database operation.
func ctxWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}
