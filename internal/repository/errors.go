package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors returned by the repository layer. The service layer
// translates these into API errors; no HTTP semantics leak down here.
var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("repository: not found")
	// ErrConflict means the write violated a uniqueness constraint.
	ErrConflict = errors.New("repository: conflict")
	// ErrAlreadyCancelled means the appointment has already been cancelled.
	ErrAlreadyCancelled = errors.New("repository: appointment already cancelled")
	// ErrAppointmentInPast means the appointment has already started.
	ErrAppointmentInPast = errors.New("repository: appointment already started")
)

// IsUniqueViolation reports whether err is a Postgres unique constraint
// violation (SQLSTATE 23505).
//
// This is the mechanism that makes double-booking impossible: the service
// layer attempts the insert optimistically and lets the partial unique index
// reject the loser of a race. A read-then-write check would have a window
// between the check and the write where two clients could both pass.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// wrapErr converts a driver error into one of the sentinel errors where
// possible, otherwise it returns a wrapped error for logging.
func wrapErr(op string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return err
	case IsUniqueViolation(err):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
