package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

const patientColumns = `id, name`

// PatientByID returns the patient with the given id, or ErrNotFound.
func (r *Repository) PatientByID(ctx context.Context, id int64) (*models.Patient, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	row := r.db.QueryRowContext(ctx,
		`SELECT `+patientColumns+` FROM patients WHERE id = $1`, id)

	var patient models.Patient
	if err := row.Scan(&patient.ID, &patient.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, wrapErr("repository: patient by id", err)
	}
	return &patient, nil
}
