package service

import (
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// SlotDuration is the fixed length of every appointment.
const SlotDuration = models.SlotDuration

// Slot is a bookable window inside a doctor's working day. Both bounds are
// naive wall-clock times of day.
type Slot struct {
	Start time.Time
	End   time.Time
}

// GenerateSlots returns every 30-minute slot that fits inside the shift.
//
// A slot is included when it ends at or before the shift ends, so a
// 09:00-17:00 shift yields 16 slots (09:00 through 16:30) and a 09:00-09:30
// shift yields exactly one.
func GenerateSlots(shiftStart, shiftEnd time.Time) []Slot {
	slots := make([]Slot, 0, 16)
	for start := shiftStart; !start.Add(SlotDuration).After(shiftEnd); start = start.Add(SlotDuration) {
		slots = append(slots, Slot{Start: start, End: start.Add(SlotDuration)})
	}
	return slots
}

// IsOnGrid reports whether start lands exactly on the shift's 30-minute grid.
//
// The grid is anchored at the start of the shift. Because the schema
// constrains working hours to half-hour boundaries, the grid is also the
// wall-clock half-hour grid.
//
// Grid alignment is what makes the database's partial unique index
// sufficient to prevent double-booking: with a fixed duration and aligned
// starts, two appointments can only overlap if they start at the same
// instant.
func IsOnGrid(shiftStart, start time.Time) bool {
	delta := start.Sub(shiftStart)
	if delta < 0 || delta%SlotDuration != 0 {
		return false
	}
	return true
}

// FormatTimeOfDay renders a wall-clock time as HH:MM.
func FormatTimeOfDay(t time.Time) string {
	return t.Format("15:04")
}
