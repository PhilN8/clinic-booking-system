package service

import (
	"fmt"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// MinimumLeadTime is how far in advance an appointment must be booked. A
// booking that starts less than this far from now is refused.
//
// The boundary is inclusive of exactly one hour: a slot starting precisely
// 60 minutes from now is allowed, anything closer is not.
const MinimumLeadTime = time.Hour

// ParseDate parses a YYYY-MM-DD calendar date into a midnight-UTC anchor.
func ParseDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, httperr.InvalidDate(
			fmt.Sprintf("appointment_date %q must be a calendar date in YYYY-MM-DD format", value)).
			WithCause(err)
	}
	return models.CalendarDate(parsed), nil
}

// ParseTimeOfDay parses a HH:MM 24-hour wall-clock time.
//
// The layout is deliberately strict. Go's time.Parse accepts a single-digit
// hour for the "15" layout, so the parsed value is formatted back and
// compared with the input: "9:00" is rejected while "09:00" is accepted.
// Impossible values such as "23:60" or "24:00" fail to parse at all.
func ParseTimeOfDay(value string) (time.Time, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, httperr.InvalidTime(
			fmt.Sprintf("start_time %q must be a time of day in HH:MM 24-hour format", value)).
			WithCause(err)
	}
	if formatted := parsed.Format("15:04"); formatted != value {
		return time.Time{}, httperr.InvalidTime(
			fmt.Sprintf("start_time %q must be a time of day in HH:MM 24-hour format", value))
	}
	return models.TimeOfDay(parsed.Hour(), parsed.Minute()), nil
}

// ValidateSlot checks a requested start time against the doctor's working
// window, the 30-minute grid and the minimum lead time.
//
// The checks run in the order that produces the most useful message:
//
//  1. working hours first, so "08:30 when the doctor starts at 09:00" is
//     reported as being outside working hours rather than as a misaligned
//     start;
//  2. grid alignment second, so "09:15" is reported as not landing on the
//     grid;
//  3. the lead time last, which only makes sense once the slot is known to
//     be bookable at all.
//
// loc is the clinic's timezone and is used to turn the naive wall-clock start
// into an absolute instant for comparison against now.
func ValidateSlot(doctor *models.Doctor, date, start time.Time, now time.Time, loc *time.Location) error {
	end := start.Add(SlotDuration)

	if start.Before(doctor.StartWorkingHour) || end.After(doctor.EndWorkingHour) {
		return httperr.OutsideWorkingHours(fmt.Sprintf(
			"start time %s is outside the doctor's working hours (%s-%s)",
			FormatTimeOfDay(start),
			FormatTimeOfDay(doctor.StartWorkingHour),
			FormatTimeOfDay(doctor.EndWorkingHour),
		)).WithDetails(map[string]any{
			"start_time":  FormatTimeOfDay(start),
			"shift_start": FormatTimeOfDay(doctor.StartWorkingHour),
			"shift_end":   FormatTimeOfDay(doctor.EndWorkingHour),
		})
	}

	if !IsOnGrid(doctor.StartWorkingHour, start) {
		return httperr.SlotNotAligned(fmt.Sprintf(
			"start time %s is not on the doctor's %d-minute slot grid",
			FormatTimeOfDay(start), int64(SlotDuration.Minutes()),
		)).WithDetails(map[string]any{
			"start_time":  FormatTimeOfDay(start),
			"shift_start": FormatTimeOfDay(doctor.StartWorkingHour),
		})
	}

	startsAt := time.Date(date.Year(), date.Month(), date.Day(),
		start.Hour(), start.Minute(), 0, 0, loc)
	if startsAt.Before(now.Add(MinimumLeadTime)) {
		return httperr.TooSoon(fmt.Sprintf(
			"appointments must be booked at least %s in advance; %s on %s is only %s away",
			MinimumLeadTime, FormatTimeOfDay(start), date.Format("2006-01-02"),
			startsAt.Sub(now).Round(time.Minute),
		)).WithDetails(map[string]any{
			"appointment_date":  date.Format("2006-01-02"),
			"start_time":        FormatTimeOfDay(start),
			"minimum_lead_time": MinimumLeadTime.String(),
		})
	}

	return nil
}
