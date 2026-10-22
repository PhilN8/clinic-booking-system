package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
)

// The API speaks 24-hour times and ISO dates. The site speaks the way a
// patient would say them out loud. The conversion happens here, at the edge,
// and only for display.

// FormatTime renders a wall-clock time as a 12-hour time, for example
// "9:30 AM". A leading zero is dropped, because "09:30" reads like a
// timetable and "9:30" reads like a person talking.
func FormatTime(t time.Time) string {
	return t.Format("3:04 PM")
}

// FormatTimeRange renders a span, for example "9:30 AM – 10:00 AM".
func FormatTimeRange(start, end time.Time) string {
	return FormatTime(start) + " – " + FormatTime(end)
}

// FormatDateLong renders a calendar date in full, for example
// "Thursday 22 October 2026".
func FormatDateLong(t time.Time) string {
	return t.Format("Monday 2 January 2006")
}

// FormatDateShort renders a calendar date, for example "22 Oct 2026".
func FormatDateShort(t time.Time) string {
	return t.Format("2 Jan 2006")
}

// FormatWeekday renders the day of the week, for example "Thu".
func FormatWeekday(t time.Time) string {
	return t.Format("Mon")
}

// FormatDayMonth renders the day and month, for example "22 Oct".
func FormatDayMonth(t time.Time) string {
	return t.Format("2 Jan")
}

// Initials derives a doctor's initials for the avatar placeholder.
//
// The API has no photo, so the card shows initials rather than a generic
// silhouette, which would look like a broken image.
func Initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}

	// Skip a leading title, so "Dr. Amara Ochieng" reads as "AO" and not
	// "DO".
	if len(parts) > 1 && (strings.HasSuffix(parts[0], ".") || strings.EqualFold(parts[0], "dr")) {
		parts = parts[1:]
	}

	initials := make([]string, 0, 2)
	for _, part := range parts {
		initials = append(initials, strings.ToUpper(part[:1]))
		if len(initials) == 2 {
			break
		}
	}
	return strings.Join(initials, "")
}

// clinicDate returns the calendar date in the clinic's timezone.
//
// The result is anchored at midnight UTC rather than in the clinic's zone on
// purpose: a calendar date is not an instant, and anchoring it in UTC keeps
// the weekday and the day of the month stable.
func clinicDate(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// dateValue renders a calendar date in the format the API expects.
func dateValue(t time.Time) string {
	return t.Format("2006-01-02")
}

// formatError turns an API error into something a patient can act on.
//
// Every code gets its own message. Falling back to the API's own text would
// put phrases like "appointment_date must be a calendar date" in front of
// someone booking a doctor's appointment, so the API's wording is reserved
// for API clients.
func formatError(err error) (message, code string) {
	var typed *httperr.Error
	if !errors.As(err, &typed) {
		return "Something went wrong on our side. Please try again, or call the clinic if it keeps happening.", "unknown"
	}

	switch typed.Code {
	case httperr.CodeSlotTaken:
		return "Someone else has just booked that time. The other times below are still available.", typed.Code
	case httperr.CodeTooSoon:
		return "Appointments need to be booked at least an hour ahead. Please choose a later time.", typed.Code
	case httperr.CodeSlotNotAligned:
		return "Appointments start on the hour or the half hour. Please choose one of the times listed below.", typed.Code
	case httperr.CodeOutsideWorkingHours:
		return "That time is outside the doctor's working hours. The times listed below are the ones you can book.", typed.Code
	case httperr.CodeAppointmentCancelled:
		return "That appointment has already been cancelled.", typed.Code
	case httperr.CodeAppointmentInPast:
		return "That appointment has already started, so it cannot be changed.", typed.Code
	case httperr.CodeDoctorNotFound:
		return "We could not find that doctor. Pick one from the list and try again.", typed.Code
	case httperr.CodePatientNotFound:
		return "We could not find that patient record. Choose a different name from the list.", typed.Code
	case httperr.CodeAppointmentNotFound:
		return "We could not find that appointment. It may have been cancelled or moved.", typed.Code
	case httperr.CodeInvalidDate:
		return "We could not read that date. Please use the format YYYY-MM-DD.", typed.Code
	case httperr.CodeInvalidTime:
		return "We could not read that time. Please choose one of the times listed below.", typed.Code
	case httperr.CodeInvalidJSON:
		return "We could not read what you sent. Please refresh the page and try again.", typed.Code
	case httperr.CodeInvalidRequest:
		return "Some of the details are missing. Please check the form and try again.", typed.Code
	default:
		return "Something went wrong on our side. Please try again, or call the clinic if it keeps happening.", typed.Code
	}
}

// statusForCode maps an API error to the status the page should render with.
func statusForCode(code string) int {
	switch code {
	case httperr.CodeDoctorNotFound, httperr.CodePatientNotFound, httperr.CodeAppointmentNotFound:
		return http.StatusNotFound
	case httperr.CodeSlotTaken, httperr.CodeAppointmentCancelled:
		return http.StatusConflict
	case httperr.CodeInvalidRequest, httperr.CodeInvalidDate, httperr.CodeInvalidTime:
		return http.StatusBadRequest
	default:
		return http.StatusUnprocessableEntity
	}
}

// dateRange returns consecutive calendar dates starting at start.
func dateRange(start time.Time, days int) []time.Time {
	dates := make([]time.Time, 0, days)
	for i := 0; i < days; i++ {
		dates = append(dates, start.AddDate(0, 0, i))
	}
	return dates
}
