package service

import (
	"errors"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// testLocation is the clinic timezone used throughout the unit tests.
func testLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

// errorCode returns the API error code carried by err, or "" when err is not
// an *httperr.Error.
func errorCode(err error) string {
	var typed *httperr.Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantCode string
	}{
		{name: "valid date", input: "2026-10-22"},
		{name: "valid leap day", input: "2024-02-29"},
		{name: "day out of range", input: "2026-02-30", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "month out of range", input: "2026-13-01", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "wrong separator", input: "2026/10/22", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "missing zero padding", input: "2026-1-2", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "datetime is not a date", input: "2026-10-22T09:00:00Z", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "empty string", input: "", wantErr: true, wantCode: httperr.CodeInvalidDate},
		{name: "non-leap year february 29", input: "2026-02-29", wantErr: true, wantCode: httperr.CodeInvalidDate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %v, want error", tt.input, got)
				}
				if code := errorCode(err); code != tt.wantCode {
					t.Fatalf("ParseDate(%q) error code = %q, want %q", tt.input, code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) returned unexpected error: %v", tt.input, err)
			}
			if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 {
				t.Errorf("ParseDate(%q) = %v, want midnight anchor", tt.input, got)
			}
		})
	}
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantCode string
		want     string
	}{
		{name: "morning", input: "09:00", want: "09:00"},
		{name: "afternoon", input: "13:30", want: "13:30"},
		{name: "midnight", input: "00:00", want: "00:00"},
		{name: "last minute of day", input: "23:59", want: "23:59"},
		{name: "missing zero padding", input: "9:00", wantErr: true, wantCode: httperr.CodeInvalidTime},
		{name: "minute out of range", input: "23:60", wantErr: true, wantCode: httperr.CodeInvalidTime},
		{name: "hour out of range", input: "24:00", wantErr: true, wantCode: httperr.CodeInvalidTime},
		{name: "12 hour clock is not accepted", input: "09:00 PM", wantErr: true, wantCode: httperr.CodeInvalidTime},
		{name: "seconds are not accepted", input: "09:00:00", wantErr: true, wantCode: httperr.CodeInvalidTime},
		{name: "empty string", input: "", wantErr: true, wantCode: httperr.CodeInvalidTime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimeOfDay(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTimeOfDay(%q) = %v, want error", tt.input, got)
				}
				if code := errorCode(err); code != tt.wantCode {
					t.Fatalf("ParseTimeOfDay(%q) error code = %q, want %q", tt.input, code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeOfDay(%q) returned unexpected error: %v", tt.input, err)
			}
			if formatted := FormatTimeOfDay(got); formatted != tt.want {
				t.Errorf("ParseTimeOfDay(%q) = %q, want %q", tt.input, formatted, tt.want)
			}
		})
	}
}

func TestGenerateSlots(t *testing.T) {
	tests := []struct {
		name       string
		shiftStart string
		shiftEnd   string
		wantCount  int
		wantFirst  string
		wantLast   string
	}{
		{name: "full day yields sixteen slots", shiftStart: "09:00", shiftEnd: "17:00", wantCount: 16, wantFirst: "09:00", wantLast: "16:30"},
		{name: "half day yields eight slots", shiftStart: "09:00", shiftEnd: "13:00", wantCount: 8, wantFirst: "09:00", wantLast: "12:30"},
		{name: "minimum shift yields one slot", shiftStart: "09:00", shiftEnd: "09:30", wantCount: 1, wantFirst: "09:00", wantLast: "09:00"},
		{name: "shift shorter than a slot yields none", shiftStart: "09:00", shiftEnd: "09:15", wantCount: 0},
		{name: "shift starting on the half hour", shiftStart: "13:30", shiftEnd: "17:30", wantCount: 8, wantFirst: "13:30", wantLast: "17:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, err := ParseTimeOfDay(tt.shiftStart)
			if err != nil {
				t.Fatalf("parse shift start: %v", err)
			}
			end, err := ParseTimeOfDay(tt.shiftEnd)
			if err != nil {
				t.Fatalf("parse shift end: %v", err)
			}

			slots := GenerateSlots(start, end)
			if len(slots) != tt.wantCount {
				t.Fatalf("GenerateSlots(%s, %s) returned %d slots, want %d",
					tt.shiftStart, tt.shiftEnd, len(slots), tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			if got := FormatTimeOfDay(slots[0].Start); got != tt.wantFirst {
				t.Errorf("first slot start = %q, want %q", got, tt.wantFirst)
			}
			if got := FormatTimeOfDay(slots[len(slots)-1].Start); got != tt.wantLast {
				t.Errorf("last slot start = %q, want %q", got, tt.wantLast)
			}
			// Every slot must be exactly SlotDuration long and contiguous.
			for i, slot := range slots {
				if slot.End.Sub(slot.Start) != SlotDuration {
					t.Errorf("slot %d has length %s, want %s", i, slot.End.Sub(slot.Start), SlotDuration)
				}
				if i > 0 && !slot.Start.Equal(slots[i-1].End) {
					t.Errorf("slot %d starts at %s but previous slot ends at %s", i, slot.Start, slots[i-1].End)
				}
			}
		})
	}
}

func TestIsOnGrid(t *testing.T) {
	shiftStart, err := ParseTimeOfDay("09:00")
	if err != nil {
		t.Fatalf("parse shift start: %v", err)
	}

	tests := []struct {
		name  string
		start string
		want  bool
	}{
		{name: "start of shift", start: "09:00", want: true},
		{name: "second slot", start: "09:30", want: true},
		{name: "later slot", start: "16:30", want: true},
		{name: "quarter past", start: "09:15", want: false},
		{name: "five past", start: "09:05", want: false},
		{name: "before the shift", start: "08:30", want: false},
		// The grid is anchored at the start of the shift and is independent
		// of where the shift ends. Rejecting 17:00 is the working-hours
		// check's job, not the grid's.
		{name: "past the end of the shift is still on the grid", start: "17:00", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, err := ParseTimeOfDay(tt.start)
			if err != nil {
				t.Fatalf("parse start: %v", err)
			}
			if got := IsOnGrid(shiftStart, start); got != tt.want {
				t.Errorf("IsOnGrid(09:00, %s) = %v, want %v", tt.start, got, tt.want)
			}
		})
	}
}

func TestValidateSlot(t *testing.T) {
	loc := testLocation(t)
	doctor := &models.Doctor{
		ID:               1,
		Name:             "Dr. Amara Ochieng",
		StartWorkingHour: models.TimeOfDay(9, 0),
		EndWorkingHour:   models.TimeOfDay(17, 0),
	}
	date := models.CalendarDate(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))

	// 08:00 clinic time. The default "now" is two hours before the first
	// slot of the day, so every slot is comfortably inside the lead time.
	baseNow := time.Date(2026, 10, 21, 8, 0, 0, 0, loc)

	tests := []struct {
		name     string
		start    string
		now      time.Time
		wantErr  bool
		wantCode string
	}{
		{
			name:  "first slot of the shift is allowed",
			start: "09:00",
		},
		{
			name:  "last slot of the shift is allowed",
			start: "16:30",
		},
		{
			name:     "a slot that would end after the shift is rejected",
			start:    "16:45",
			wantErr:  true,
			wantCode: httperr.CodeOutsideWorkingHours,
		},
		{
			name:     "a start at the shift end is rejected",
			start:    "17:00",
			wantErr:  true,
			wantCode: httperr.CodeOutsideWorkingHours,
		},
		{
			name:     "a start before the shift is rejected",
			start:    "08:30",
			wantErr:  true,
			wantCode: httperr.CodeOutsideWorkingHours,
		},
		{
			name:     "a start before the shift is reported as out of hours, not as misaligned",
			start:    "08:45",
			wantErr:  true,
			wantCode: httperr.CodeOutsideWorkingHours,
		},
		{
			name:     "an off-grid start inside the shift is rejected",
			start:    "09:15",
			wantErr:  true,
			wantCode: httperr.CodeSlotNotAligned,
		},
		{
			name:  "exactly one hour away is allowed",
			start: "09:00",
			now:   time.Date(2026, 10, 21, 8, 0, 0, 0, loc),
		},
		{
			name:     "one second under an hour away is rejected",
			start:    "09:00",
			now:      time.Date(2026, 10, 21, 8, 0, 1, 0, loc),
			wantErr:  true,
			wantCode: httperr.CodeTooSoon,
		},
		{
			name:     "thirty minutes away is rejected",
			start:    "09:00",
			now:      time.Date(2026, 10, 21, 8, 30, 0, 0, loc),
			wantErr:  true,
			wantCode: httperr.CodeTooSoon,
		},
		{
			name:     "a slot that has already started is rejected",
			start:    "09:00",
			now:      time.Date(2026, 10, 21, 9, 30, 0, 0, loc),
			wantErr:  true,
			wantCode: httperr.CodeTooSoon,
		},
		{
			name:     "a slot earlier today is rejected",
			start:    "09:00",
			now:      time.Date(2026, 10, 21, 14, 0, 0, 0, loc),
			wantErr:  true,
			wantCode: httperr.CodeTooSoon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, err := ParseTimeOfDay(tt.start)
			if err != nil {
				t.Fatalf("parse start: %v", err)
			}
			now := tt.now
			if now.IsZero() {
				now = baseNow
			}

			err = ValidateSlot(doctor, date, start, now, loc)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateSlot(%s) = nil, want error", tt.start)
				}
				if code := errorCode(err); code != tt.wantCode {
					t.Fatalf("ValidateSlot(%s) error code = %q, want %q (err: %v)", tt.start, code, tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateSlot(%s) returned unexpected error: %v", tt.start, err)
			}
		})
	}
}

// TestValidateSlotUsesClinicTimezone guards the assumption that the whole
// wall-clock model rests on: the comparison happens in the clinic's zone, not
// in the host's zone.
func TestValidateSlotUsesClinicTimezone(t *testing.T) {
	loc := testLocation(t)
	doctor := &models.Doctor{
		ID:               1,
		StartWorkingHour: models.TimeOfDay(9, 0),
		EndWorkingHour:   models.TimeOfDay(17, 0),
	}
	date := models.CalendarDate(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))

	// 09:30 in Nairobi is 06:30 UTC. The slot must be judged against
	// clinic time: if the comparison were done in UTC it would look three
	// hours away instead of two.
	start, err := ParseTimeOfDay("09:30")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}

	now := time.Date(2026, 10, 21, 8, 30, 0, 0, loc) // 09:30 Nairobi is one hour away
	if err := ValidateSlot(doctor, date, start, now, loc); err != nil {
		t.Fatalf("ValidateSlot returned unexpected error: %v", err)
	}

	// 08:31 clinic time is 59 minutes before the slot: too soon.
	now = time.Date(2026, 10, 21, 8, 31, 0, 0, loc)
	err = ValidateSlot(doctor, date, start, now, loc)
	if err == nil {
		t.Fatal("ValidateSlot = nil, want TOO_SOON")
	}
	if code := errorCode(err); code != httperr.CodeTooSoon {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeTooSoon)
	}
}
