// Package models holds the domain entities shared by the repository and
// service layers.
//
// Time handling in this package is deliberate and worth reading before
// changing anything:
//
//   - Appointment wall-clock times (StartTime, EndTime) are naive times of
//     day. They carry no date and no zone.
//   - AppointmentDate is a calendar date at midnight UTC. Postgres DATE
//     columns have no zone either; midnight UTC is a stable anchor for them.
//
// The service layer converts these to absolute instants in the clinic
// timezone before comparing them against the clock.
package models

import "time"

// Appointment status values.
const (
	StatusBooked    = "booked"
	StatusCancelled = "cancelled"
)

// SlotDuration is the fixed length of every appointment. It is a constant
// rather than a column because the 30-minute grid in the service layer only
// guarantees no double-booking if every appointment is exactly this long.
const SlotDuration = 30 * time.Minute

// Doctor is a clinician with a single daily working window.
type Doctor struct {
	ID int64
	// Name is the doctor's display name.
	Name string
	// StartWorkingHour is the wall-clock time the shift begins.
	StartWorkingHour time.Time
	// EndWorkingHour is the wall-clock time the shift ends.
	EndWorkingHour time.Time
}

// Patient is someone who can book appointments.
type Patient struct {
	ID   int64
	Name string
}

// Appointment is a single 30-minute booking with a doctor.
type Appointment struct {
	ID        int64
	DoctorID  int64
	PatientID int64
	// AppointmentDate is the calendar date of the appointment.
	AppointmentDate time.Time
	// StartTime is the wall-clock start of the appointment.
	StartTime time.Time
	// EndTime is the wall-clock end of the appointment.
	EndTime time.Time
	// Status is either StatusBooked or StatusCancelled.
	Status string
	// CancellationReason is free text supplied when the appointment was cancelled.
	CancellationReason *string
	// CancelledAt is the absolute instant the appointment was cancelled.
	CancelledAt *time.Time
	// CreatedAt is the absolute instant the row was inserted.
	CreatedAt time.Time
	// RescheduledFromID points at the appointment this one replaced, if any.
	RescheduledFromID *int64
}

// TimeOfDay returns a time.Time carrying only a wall-clock time of day.
// The date component is a fixed placeholder so that two times of day can be
// compared with time.Sub without any date arithmetic getting in the way.
func TimeOfDay(hour, minute int) time.Time {
	return time.Date(2000, time.January, 1, hour, minute, 0, 0, time.UTC)
}

// ClockOfDay extracts the wall-clock time of day from a scanned TIME column.
func ClockOfDay(t time.Time) time.Time {
	return TimeOfDay(t.Hour(), t.Minute())
}

// CalendarDate normalises a scanned DATE column to midnight UTC so that
// formatting it always yields the calendar date it represents.
func CalendarDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
