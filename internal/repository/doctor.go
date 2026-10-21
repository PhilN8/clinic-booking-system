package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

const doctorColumns = `id, name, start_working_hour, end_working_hour`

// DoctorByID returns the doctor with the given id, or ErrNotFound.
func (r *Repository) DoctorByID(ctx context.Context, id int64) (*models.Doctor, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	row := r.db.QueryRowContext(ctx,
		`SELECT `+doctorColumns+` FROM doctors WHERE id = $1`, id)

	doctor, err := scanDoctor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapErr("repository: doctor by id", err)
	}
	return doctor, nil
}

// ListDoctors returns every doctor, ordered by id.
func (r *Repository) ListDoctors(ctx context.Context) ([]models.Doctor, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	rows, err := r.db.QueryContext(ctx, `SELECT `+doctorColumns+` FROM doctors ORDER BY id`)
	if err != nil {
		return nil, wrapErr("repository: list doctors", err)
	}
	defer rows.Close()

	doctors := make([]models.Doctor, 0, 8)
	for rows.Next() {
		doctor, err := scanDoctor(rows)
		if err != nil {
			return nil, wrapErr("repository: list doctors", err)
		}
		doctors = append(doctors, *doctor)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("repository: list doctors", err)
	}
	return doctors, nil
}

func scanDoctor(row rowScanner) (*models.Doctor, error) {
	var (
		doctor     models.Doctor
		startValue any
		endValue   any
	)
	if err := row.Scan(&doctor.ID, &doctor.Name, &startValue, &endValue); err != nil {
		return nil, err
	}

	start, err := scanTimeOfDay(startValue)
	if err != nil {
		return nil, err
	}
	end, err := scanTimeOfDay(endValue)
	if err != nil {
		return nil, err
	}

	doctor.StartWorkingHour = start
	doctor.EndWorkingHour = end
	return &doctor, nil
}
