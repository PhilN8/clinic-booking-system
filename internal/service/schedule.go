package service

import (
	"context"
	"errors"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
)

// SlotStatus explains whether a slot can be booked, and why not.
type SlotStatus string

const (
	// SlotAvailable means the slot is free and far enough in the future.
	SlotAvailable SlotStatus = "available"
	// SlotTaken means another patient has already booked it.
	SlotTaken SlotStatus = "taken"
	// SlotTooSoon means the slot is inside the minimum lead time.
	SlotTooSoon SlotStatus = "tooSoon"
	// SlotPast means the slot has already started.
	SlotPast SlotStatus = "past"
)

// DaySlot is one slot in a doctor's working day.
type DaySlot struct {
	// Start is the wall-clock start of the slot.
	Start time.Time
	// End is the wall-clock end of the slot.
	End time.Time
	// Status says whether the slot can be booked.
	Status SlotStatus
}

// Bookable reports whether the slot can be booked.
func (s DaySlot) Bookable() bool { return s.Status == SlotAvailable }

// Schedule is a doctor's working day, with every slot and its status.
//
// The public availability endpoint returns only the slots that are free. The
// site needs the whole grid, because a patient who cannot see why a slot is
// missing cannot tell the difference between "somebody took it" and "the
// clinic is closed".
type Schedule struct {
	DoctorID int64
	// Date is the calendar date the schedule covers.
	Date time.Time
	// Slots is every slot in the doctor's working day, in order.
	Slots []DaySlot
}

// AvailableCount returns how many slots can be booked.
func (s *Schedule) AvailableCount() int {
	count := 0
	for _, slot := range s.Slots {
		if slot.Bookable() {
			count++
		}
	}
	return count
}

// Schedule returns every slot in a doctor's working day with its status.
//
// The rules are the same ones the booking path applies, so the grid can
// never offer a slot that POST /appointments would refuse.
func (s *Service) Schedule(ctx context.Context, doctorID int64, date string) (*Schedule, error) {
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
	slots := make([]DaySlot, 0, 16)
	for _, slot := range GenerateSlots(doctor.StartWorkingHour, doctor.EndWorkingHour) {
		status := SlotAvailable

		switch {
		case taken[FormatTimeOfDay(slot.Start)]:
			status = SlotTaken
		default:
			startsAt := time.Date(day.Year(), day.Month(), day.Day(),
				slot.Start.Hour(), slot.Start.Minute(), 0, 0, s.loc)
			switch {
			case startsAt.Before(now):
				status = SlotPast
			case startsAt.Before(now.Add(MinimumLeadTime)):
				status = SlotTooSoon
			}
		}

		slots = append(slots, DaySlot{Start: slot.Start, End: slot.End, Status: status})
	}

	return &Schedule{DoctorID: doctorID, Date: day, Slots: slots}, nil
}

// ListPatients returns every patient.
//
// The site uses it for the "acting as" picker, because the API deliberately
// has no authentication.
func (s *Service) ListPatients(ctx context.Context) ([]models.Patient, error) {
	return s.repo.ListPatients(ctx)
}

// AppointmentByID returns a single appointment.
func (s *Service) AppointmentByID(ctx context.Context, id int64) (*models.Appointment, error) {
	appointment, err := s.repo.AppointmentByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httperr.AppointmentNotFound(id)
		}
		return nil, err
	}
	return appointment, nil
}
