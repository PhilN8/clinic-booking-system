package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

const appointmentColumns = `id, doctor_id, patient_id, appointment_date, start_time, end_time,
	status, cancellation_reason, cancelled_at, created_at, rescheduled_from_id`

// CreateAppointment inserts a new booked appointment.
//
// The insert is intentionally optimistic: it does not check whether the slot
// is already taken. The partial unique index is the arbiter, and a violation
// is translated into ErrConflict so the caller can answer 409. A prior
// read-then-write check would leave a race window open.
func (r *Repository) CreateAppointment(ctx context.Context, a *models.Appointment) (*models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO appointments
			(doctor_id, patient_id, appointment_date, start_time, end_time, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+appointmentColumns,
		a.DoctorID, a.PatientID, a.AppointmentDate, a.StartTime, a.EndTime, a.Status)

	created, err := scanAppointment(row)
	if err != nil {
		return nil, wrapErr("repository: create appointment", err)
	}
	return created, nil
}

// AppointmentByID returns the appointment with the given id, or ErrNotFound.
func (r *Repository) AppointmentByID(ctx context.Context, id int64) (*models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	row := r.db.QueryRowContext(ctx,
		`SELECT `+appointmentColumns+` FROM appointments WHERE id = $1`, id)

	appointment, err := scanAppointment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapErr("repository: appointment by id", err)
	}
	return appointment, nil
}

// BookedAppointmentsForDoctor returns the booked appointments for one doctor
// on one calendar date.
func (r *Repository) BookedAppointmentsForDoctor(ctx context.Context, doctorID int64, date time.Time) ([]models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+appointmentColumns+`
		FROM appointments
		WHERE doctor_id = $1 AND appointment_date = $2 AND status = 'booked'`,
		doctorID, date)
	if err != nil {
		return nil, wrapErr("repository: booked appointments for doctor", err)
	}
	defer rows.Close()

	appointments := make([]models.Appointment, 0, 16)
	for rows.Next() {
		appointment, err := scanAppointment(rows)
		if err != nil {
			return nil, wrapErr("repository: booked appointments for doctor", err)
		}
		appointments = append(appointments, *appointment)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("repository: booked appointments for doctor", err)
	}
	return appointments, nil
}

// CancelAppointment cancels an appointment and returns the updated row.
//
// The UPDATE carries its own `status = 'booked'` guard, so two concurrent
// cancels of the same appointment cannot both succeed: the loser matches no
// row and is told the appointment was already cancelled.
func (r *Repository) CancelAppointment(ctx context.Context, id int64, reason *string, now time.Time) (*models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	current, err := r.AppointmentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == models.StatusCancelled {
		return nil, ErrAlreadyCancelled
	}
	if r.startsAt(current).Before(now) {
		return nil, ErrAppointmentInPast
	}

	row := r.db.QueryRowContext(ctx, `
		UPDATE appointments
		SET status = 'cancelled', cancellation_reason = $2, cancelled_at = $3
		WHERE id = $1 AND status = 'booked'
		RETURNING `+appointmentColumns,
		id, reason, now)

	updated, err := scanAppointment(row)
	if errors.Is(err, sql.ErrNoRows) {
		// Lost the race against a concurrent cancel.
		return nil, ErrAlreadyCancelled
	}
	if err != nil {
		return nil, wrapErr("repository: cancel appointment", err)
	}
	return updated, nil
}

// RescheduleAppointment moves an appointment to a new slot atomically.
//
// The whole operation runs in one transaction:
//
//  1. lock the appointment row with SELECT ... FOR UPDATE,
//  2. re-check its status and that it has not started,
//  3. cancel the original,
//  4. insert the replacement.
//
// If step 4 violates the unique index the transaction is rolled back and the
// original appointment is still booked. The row lock in step 1 is what stops
// two concurrent reschedules of the same appointment from both cancelling it
// and inserting two replacements.
func (r *Repository) RescheduleAppointment(ctx context.Context, id int64, newDate, newStart time.Time, now time.Time) (*models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapErr("repository: begin reschedule", err)
	}
	defer tx.Rollback()

	current, err := scanAppointment(tx.QueryRowContext(ctx,
		`SELECT `+appointmentColumns+` FROM appointments WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapErr("repository: lock appointment", err)
	}

	if current.Status == models.StatusCancelled {
		return nil, ErrAlreadyCancelled
	}
	if r.startsAt(current).Before(now) {
		return nil, ErrAppointmentInPast
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET status = 'cancelled', cancellation_reason = 'rescheduled', cancelled_at = $2
		WHERE id = $1`, id, now); err != nil {
		return nil, wrapErr("repository: cancel original appointment", err)
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO appointments
			(doctor_id, patient_id, appointment_date, start_time, end_time, status, rescheduled_from_id)
		VALUES ($1, $2, $3, $4, $5, 'booked', $6)
		RETURNING `+appointmentColumns,
		current.DoctorID, current.PatientID, newDate, newStart, newStart.Add(models.SlotDuration), id)

	created, err := scanAppointment(row)
	if err != nil {
		return nil, wrapErr("repository: insert replacement appointment", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, wrapErr("repository: commit reschedule", err)
	}
	return created, nil
}

// AppointmentsForPatient returns a patient's appointments ordered by date and
// start time.
//
// When includeCancelled is false the result contains only booked
// appointments that have not started yet. When it is true the result also
// contains cancelled appointments, past and future, so that a reschedule's
// audit trail is visible.
func (r *Repository) AppointmentsForPatient(ctx context.Context, patientID int64, includeCancelled bool, now time.Time) ([]models.Appointment, error) {
	ctx, cancel := ctxWithTimeout(ctx)
	defer cancel()

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+appointmentColumns+`
		FROM appointments
		WHERE patient_id = $1
		ORDER BY appointment_date, start_time`, patientID)
	if err != nil {
		return nil, wrapErr("repository: appointments for patient", err)
	}
	defer rows.Close()

	appointments := make([]models.Appointment, 0, 8)
	for rows.Next() {
		appointment, err := scanAppointment(rows)
		if err != nil {
			return nil, wrapErr("repository: appointments for patient", err)
		}
		if !includeCancelled {
			if appointment.Status != models.StatusBooked {
				continue
			}
			if r.startsAt(appointment).Before(now) {
				continue
			}
		}
		appointments = append(appointments, *appointment)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("repository: appointments for patient", err)
	}
	return appointments, nil
}

func scanAppointment(row rowScanner) (*models.Appointment, error) {
	var (
		appointment    models.Appointment
		dateValue      any
		startValue     any
		endValue       any
		cancelledValue any
		createdValue   any
	)
	if err := row.Scan(
		&appointment.ID,
		&appointment.DoctorID,
		&appointment.PatientID,
		&dateValue,
		&startValue,
		&endValue,
		&appointment.Status,
		&appointment.CancellationReason,
		&cancelledValue,
		&createdValue,
		&appointment.RescheduledFromID,
	); err != nil {
		return nil, err
	}

	date, err := scanDate(dateValue)
	if err != nil {
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
	cancelledAt, err := scanTimestamp(cancelledValue)
	if err != nil {
		return nil, err
	}
	createdAt, err := scanTimestamp(createdValue)
	if err != nil {
		return nil, err
	}

	appointment.AppointmentDate = date
	appointment.StartTime = start
	appointment.EndTime = end
	appointment.CancelledAt = cancelledAt
	if createdAt != nil {
		appointment.CreatedAt = *createdAt
	}
	return &appointment, nil
}
