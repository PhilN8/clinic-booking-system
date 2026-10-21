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

// ListPatients returns every patient, ordered by id.
func (r *Repository) ListPatients(ctx context.Context) ([]models.Patient, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	rows, err := r.db.QueryContext(ctx, `SELECT `+patientColumns+` FROM patients ORDER BY id`)
	if err != nil {
		return nil, wrapErr("repository: list patients", err)
	}
	defer rows.Close()

	patients := make([]models.Patient, 0, 8)
	for rows.Next() {
		var patient models.Patient
		if err := rows.Scan(&patient.ID, &patient.Name); err != nil {
			return nil, wrapErr("repository: list patients", err)
		}
		patients = append(patients, patient)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("repository: list patients", err)
	}
	return patients, nil
}
