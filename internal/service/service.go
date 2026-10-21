// Package service holds the clinic's business rules.
//
// Nothing in this package touches HTTP or SQL. It validates requests,
// applies the booking rules and coordinates the persistence layer through
// the Repository interface, which keeps the rules unit-testable without a
// database.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
)

// Repository is the persistence contract the service depends on. It is
// declared here, by the consumer, so that the Postgres implementation in
// internal/repository satisfies it structurally.
type Repository interface {
	DoctorByID(ctx context.Context, id int64) (*models.Doctor, error)
	ListDoctors(ctx context.Context) ([]models.Doctor, error)
	PatientByID(ctx context.Context, id int64) (*models.Patient, error)
	ListPatients(ctx context.Context) ([]models.Patient, error)
	CreateAppointment(ctx context.Context, a *models.Appointment) (*models.Appointment, error)
	AppointmentByID(ctx context.Context, id int64) (*models.Appointment, error)
	BookedAppointmentsForDoctor(ctx context.Context, doctorID int64, date time.Time) ([]models.Appointment, error)
	CancelAppointment(ctx context.Context, id int64, reason *string, now time.Time) (*models.Appointment, error)
	RescheduleAppointment(ctx context.Context, id int64, newDate, newStart time.Time, now time.Time) (*models.Appointment, error)
	AppointmentsForPatient(ctx context.Context, patientID int64, includeCancelled bool, now time.Time) ([]models.Appointment, error)
}

// API is the contract the HTTP handlers depend on. Declaring it here lets
// the handler tests substitute a stub for the whole service.
type API interface {
	BookAppointment(ctx context.Context, req BookRequest) (*models.Appointment, error)
	CancelAppointment(ctx context.Context, id int64, reason *string) (*models.Appointment, error)
	RescheduleAppointment(ctx context.Context, id int64, req RescheduleRequest) (*models.Appointment, error)
	Availability(ctx context.Context, doctorID int64, date string) (*Availability, error)
	ListDoctors(ctx context.Context) ([]models.Doctor, error)
	DoctorByID(ctx context.Context, id int64) (*models.Doctor, error)
	PatientByID(ctx context.Context, id int64) (*models.Patient, error)
	PatientAppointments(ctx context.Context, patientID int64, includeCancelled bool) ([]models.Appointment, error)
}

// Service applies the clinic's booking rules.
type Service struct {
	repo  Repository
	clock Clock
	// loc is the clinic's timezone. It is required to compare a wall-clock
	// appointment against the current instant.
	loc *time.Location
}

// New builds a Service.
func New(repo Repository, clock Clock, loc *time.Location) *Service {
	return &Service{repo: repo, clock: clock, loc: loc}
}

// BookRequest is a request to book a slot.
type BookRequest struct {
	DoctorID  int64
	PatientID int64
	// Date is the calendar date in YYYY-MM-DD form.
	Date string
	// StartTime is the wall-clock start in HH:MM 24-hour form.
	StartTime string
}

// RescheduleRequest is a request to move an appointment to a new slot.
type RescheduleRequest struct {
	// NewDate is the calendar date in YYYY-MM-DD form.
	NewDate string
	// NewStartTime is the wall-clock start in HH:MM 24-hour form.
	NewStartTime string
}

// SlotView is a bookable window rendered for the API.
type SlotView struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Availability is the set of bookable slots for a doctor on a date.
type Availability struct {
	DoctorID int64      `json:"doctor_id"`
	Date     string     `json:"date"`
	Slots    []SlotView `json:"slots"`
}

// BookAppointment validates a booking request and books the slot.
//
// The slot is not checked for availability with a prior read. The insert is
// attempted directly and the database's partial unique index rejects the
// loser of a race, which is reported as a conflict.
func (s *Service) BookAppointment(ctx context.Context, req BookRequest) (*models.Appointment, error) {
	if req.DoctorID <= 0 {
		return nil, httperr.InvalidRequest("doctor_id must be a positive integer")
	}
	if req.PatientID <= 0 {
		return nil, httperr.InvalidRequest("patient_id must be a positive integer")
	}

	date, err := ParseDate(req.Date)
	if err != nil {
		return nil, err
	}
	start, err := ParseTimeOfDay(req.StartTime)
	if err != nil {
		return nil, err
	}

	doctor, err := s.doctor(ctx, req.DoctorID)
	if err != nil {
		return nil, err
	}
	patient, err := s.patient(ctx, req.PatientID)
	if err != nil {
		return nil, err
	}

	if err := ValidateSlot(doctor, date, start, s.clock.Now(), s.loc); err != nil {
		return nil, err
	}

	created, err := s.repo.CreateAppointment(ctx, &models.Appointment{
		DoctorID:        doctor.ID,
		PatientID:       patient.ID,
		AppointmentDate: date,
		StartTime:       start,
		EndTime:         start.Add(SlotDuration),
		Status:          models.StatusBooked,
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, httperr.SlotTaken(fmt.Sprintf(
				"slot %s on %s is already booked for doctor %d",
				FormatTimeOfDay(start), date.Format("2006-01-02"), doctor.ID,
			)).WithDetails(map[string]any{
				"appointment_date": date.Format("2006-01-02"),
				"start_time":       FormatTimeOfDay(start),
				"doctor_id":        doctor.ID,
			})
		}
		return nil, err
	}

	return created, nil
}

// CancelAppointment cancels an appointment and frees its slot.
//
// Cancelling frees the slot because the partial unique index only covers
// appointments whose status is not 'cancelled'.
//
// The state of the appointment is checked here so that the caller gets a
// precise error. The repository re-checks it under a row lock before
// writing, so the rule still holds if a request stalls between the two.
func (s *Service) CancelAppointment(ctx context.Context, id int64, reason *string) (*models.Appointment, error) {
	if id <= 0 {
		return nil, httperr.InvalidRequest("appointment id must be a positive integer")
	}

	now := s.clock.Now()

	current, err := s.repo.AppointmentByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httperr.AppointmentNotFound(id)
		}
		return nil, err
	}
	if current.Status == models.StatusCancelled {
		return nil, httperr.AppointmentAlreadyCancelled(id)
	}
	if s.startsAt(current).Before(now) {
		return nil, httperr.AppointmentInPast(fmt.Sprintf(
			"appointment %d has already started and cannot be cancelled", id))
	}

	updated, err := s.repo.CancelAppointment(ctx, id, reason, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, httperr.AppointmentNotFound(id)
		case errors.Is(err, repository.ErrAlreadyCancelled):
			return nil, httperr.AppointmentAlreadyCancelled(id)
		case errors.Is(err, repository.ErrAppointmentInPast):
			return nil, httperr.AppointmentInPast(fmt.Sprintf(
				"appointment %d has already started and cannot be cancelled", id))
		}
		return nil, err
	}
	return updated, nil
}

// RescheduleAppointment moves an appointment to a new slot.
//
// The original appointment is cancelled and a replacement is inserted in a
// single transaction. If anything fails, the transaction is rolled back and
// the original appointment is still booked.
func (s *Service) RescheduleAppointment(ctx context.Context, id int64, req RescheduleRequest) (*models.Appointment, error) {
	if id <= 0 {
		return nil, httperr.InvalidRequest("appointment id must be a positive integer")
	}

	newDate, err := ParseDate(req.NewDate)
	if err != nil {
		return nil, err
	}
	newStart, err := ParseTimeOfDay(req.NewStartTime)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()

	// The state of the appointment is checked before the new slot so that a
	// cancelled appointment is reported as a conflict rather than being
	// masked by a validation error about the new slot. The repository
	// re-checks the same state under a row lock before writing, so this
	// read is only about producing the best error message.
	current, err := s.repo.AppointmentByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httperr.AppointmentNotFound(id)
		}
		return nil, err
	}
	if current.Status == models.StatusCancelled {
		return nil, httperr.AppointmentAlreadyCancelled(id)
	}
	if s.startsAt(current).Before(now) {
		return nil, httperr.AppointmentInPast(fmt.Sprintf(
			"appointment %d has already started and cannot be rescheduled", id))
	}

	doctor, err := s.doctor(ctx, current.DoctorID)
	if err != nil {
		return nil, err
	}
	if err := ValidateSlot(doctor, newDate, newStart, now, s.loc); err != nil {
		return nil, err
	}

	replacement, err := s.repo.RescheduleAppointment(ctx, id, newDate, newStart, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, httperr.AppointmentNotFound(id)
		case errors.Is(err, repository.ErrAlreadyCancelled):
			return nil, httperr.AppointmentAlreadyCancelled(id)
		case errors.Is(err, repository.ErrAppointmentInPast):
			return nil, httperr.AppointmentInPast(fmt.Sprintf(
				"appointment %d has already started and cannot be rescheduled", id))
		case errors.Is(err, repository.ErrConflict):
			return nil, httperr.SlotTaken(fmt.Sprintf(
				"slot %s on %s is already booked for doctor %d",
				FormatTimeOfDay(newStart), newDate.Format("2006-01-02"), current.DoctorID,
			)).WithDetails(map[string]any{
				"appointment_date": newDate.Format("2006-01-02"),
				"start_time":       FormatTimeOfDay(newStart),
				"doctor_id":        current.DoctorID,
			})
		}
		return nil, err
	}
	return replacement, nil
}

// Availability returns the bookable slots for a doctor on a date.
//
// Slots that are already booked and slots that are too close to now are
// both removed, using the same predicates the booking path uses. The
// availability list therefore never advertises a slot that POST would refuse.
func (s *Service) Availability(ctx context.Context, doctorID int64, date string) (*Availability, error) {
	if doctorID <= 0 {
		return nil, httperr.InvalidRequest("doctor id must be a positive integer")
	}

	day, err := ParseDate(date)
	if err != nil {
		return nil, err
	}

	doctor, err := s.doctor(ctx, doctorID)
	if err != nil {
		return nil, err
	}

	booked, err := s.repo.BookedAppointmentsForDoctor(ctx, doctorID, day)
	if err != nil {
		return nil, err
	}

	taken := make(map[string]bool, len(booked))
	for _, appointment := range booked {
		taken[FormatTimeOfDay(appointment.StartTime)] = true
	}

	now := s.clock.Now()
	slots := make([]SlotView, 0, 16)
	for _, slot := range GenerateSlots(doctor.StartWorkingHour, doctor.EndWorkingHour) {
		if taken[FormatTimeOfDay(slot.Start)] {
			continue
		}
		startsAt := time.Date(day.Year(), day.Month(), day.Day(),
			slot.Start.Hour(), slot.Start.Minute(), 0, 0, s.loc)
		if startsAt.Before(now.Add(MinimumLeadTime)) {
			continue
		}
		slots = append(slots, SlotView{
			Start: FormatTimeOfDay(slot.Start),
			End:   FormatTimeOfDay(slot.End),
		})
	}

	return &Availability{DoctorID: doctorID, Date: day.Format("2006-01-02"), Slots: slots}, nil
}

// ListDoctors returns every doctor.
func (s *Service) ListDoctors(ctx context.Context) ([]models.Doctor, error) {
	return s.repo.ListDoctors(ctx)
}

// DoctorByID returns a single doctor.
func (s *Service) DoctorByID(ctx context.Context, id int64) (*models.Doctor, error) {
	if id <= 0 {
		return nil, httperr.InvalidRequest("doctor id must be a positive integer")
	}
	return s.doctor(ctx, id)
}

// PatientByID returns a single patient.
func (s *Service) PatientByID(ctx context.Context, id int64) (*models.Patient, error) {
	if id <= 0 {
		return nil, httperr.InvalidRequest("patient id must be a positive integer")
	}
	return s.patient(ctx, id)
}

// PatientAppointments returns a patient's upcoming appointments.
func (s *Service) PatientAppointments(ctx context.Context, patientID int64, includeCancelled bool) ([]models.Appointment, error) {
	if patientID <= 0 {
		return nil, httperr.InvalidRequest("patient id must be a positive integer")
	}
	if _, err := s.patient(ctx, patientID); err != nil {
		return nil, err
	}
	return s.repo.AppointmentsForPatient(ctx, patientID, includeCancelled, s.clock.Now())
}

// doctor loads a doctor and converts a missing row into an API error.
func (s *Service) doctor(ctx context.Context, id int64) (*models.Doctor, error) {
	doctor, err := s.repo.DoctorByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httperr.DoctorNotFound(id)
		}
		return nil, err
	}
	return doctor, nil
}

// patient loads a patient and converts a missing row into an API error.
func (s *Service) patient(ctx context.Context, id int64) (*models.Patient, error) {
	patient, err := s.repo.PatientByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httperr.PatientNotFound(id)
		}
		return nil, err
	}
	return patient, nil
}

// startsAt converts an appointment's naive wall-clock date and time into the
// absolute instant it occurs at, in the clinic's timezone.
func (s *Service) startsAt(a *models.Appointment) time.Time {
	return time.Date(
		a.AppointmentDate.Year(), a.AppointmentDate.Month(), a.AppointmentDate.Day(),
		a.StartTime.Hour(), a.StartTime.Minute(), 0, 0,
		s.loc,
	)
}

// compile-time check that *Service satisfies the API contract.
var _ API = (*Service)(nil)
