package web

import (
	"html/template"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// base carries the fields every page needs.
type base struct {
	// Title is the page title, without the site name.
	Title string
	// Description is the meta description.
	Description string
	// SiteName is shown in the header and the footer.
	SiteName string
	// NavDoctors marks the doctors link as the current page.
	NavDoctors bool
	// NavAppointments marks the appointments link as the current page.
	NavAppointments bool
	// PatientID is the patient the site is acting as.
	PatientID int64
	// Patients are the options in the patient picker.
	Patients []patientOption
	// CSSVariables is the palette as CSS custom properties.
	CSSVariables template.CSS
	// Error is the alert shown at the top of the page, if any.
	Error *alertView
	// Notice is a success or information alert.
	Notice *alertView
}

// alertView is a message banner.
type alertView struct {
	// Tone is "danger", "warning", "success" or "info".
	Tone string
	// Title is optional and bold.
	Title string
	// Message is the body of the alert.
	Message string
}

// patientOption is one entry in the patient picker.
type patientOption struct {
	ID       int64
	Name     string
	Selected bool
}

// doctorCard is a doctor in the list.
type doctorCard struct {
	ID       int64
	Name     string
	Initials string
	// Hours is the working window, for example "9:00 AM – 5:00 PM".
	Hours string
	// Detail is the sentence under the name.
	Detail string
}

// doctorView is the doctor being booked with.
type doctorView struct {
	ID       int64
	Name     string
	Initials string
	Hours    string
}

// dateChip is one day in the date strip.
type dateChip struct {
	// Value is the date in YYYY-MM-DD form.
	Value string
	// Weekday is the day of the week, for example "Thu".
	Weekday string
	// DayMonth is the day and month, for example "22 Oct".
	DayMonth string
	// Selected marks the day currently shown.
	Selected bool
	// Today marks the current day.
	Today bool
}

// slotView is one slot in the grid.
type slotView struct {
	// Value is the start time in HH:MM form.
	Value string
	// Label is the start time for display, for example "9:30 AM".
	Label string
	// EndLabel is the end time for display.
	EndLabel string
	// Status is "available", "taken", "tooSoon" or "past".
	Status string
	// Bookable reports whether the slot can be booked.
	Bookable bool
	// Note is the short reason a slot is unavailable.
	Note string
	// AccessibleName is what a screen reader announces for the slot.
	AccessibleName string
}

// appointmentCard is one appointment in the patient's list.
type appointmentCard struct {
	ID                 int64
	DoctorName         string
	DoctorInitials     string
	DateLabel          string
	TimeLabel          string
	Status             string
	StatusLabel        string
	StatusTone         string
	StatusIcon         string
	IsBooked           bool
	CancellationReason string
}

// landingPage is the front page.
type landingPage struct {
	base
}

// doctorsPage is the doctor list.
type doctorsPage struct {
	base
	Doctors []doctorCard
}

// doctorPage is the availability and booking page for one doctor.
type doctorPage struct {
	base
	Doctor doctorView
	Dates  []dateChip
	Slots  []slotView
	// Date is the selected date in YYYY-MM-DD form.
	Date string
	// DateLabel is the selected date, for example "Thursday 22 October 2026".
	DateLabel string
	// AvailableCount is how many slots can be booked.
	AvailableCount int
	// HasSlots reports whether the day has any slots at all.
	HasSlots bool
	// SelectedSlot is the slot the patient had chosen, kept across an error
	// so a conflict does not throw away their place.
	SelectedSlot string
	// LeadTimeNote explains the one hour rule.
	LeadTimeNote string
	// Rescheduling is the appointment being moved, when this page is the
	// reschedule form.
	Rescheduling *models.Appointment
	// ReschedulingDate is the current date of the appointment being moved.
	ReschedulingDate string
	// ReschedulingTime is the current time of the appointment being moved.
	ReschedulingTime string
	// DateFormAction is where the date strip submits to.
	DateFormAction string
	// Action is the form action, which differs between booking and
	// rescheduling.
	Action string
}

// appointmentsPage is the patient's appointment list.
type appointmentsPage struct {
	base
	Patient      models.Patient
	Appointments []appointmentCard
}

// cancelPage is the cancellation confirmation.
type cancelPage struct {
	base
	Appointment models.Appointment
	Doctor      doctorView
	DateLabel   string
	TimeLabel   string
}

// confirmationPage is the confirmation shown after booking.
type confirmationPage struct {
	base
	Appointment models.Appointment
	Doctor      doctorView
	DateLabel   string
	TimeLabel   string
}
