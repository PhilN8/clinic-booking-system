package service

import (
	"context"
	"testing"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// TestScheduleSlotStatuses checks the four states a slot can be in, which is
// what the site's grid renders.
//
// The clock is pinned so the boundaries can be asserted exactly.
func TestScheduleSlotStatuses(t *testing.T) {
	loc := testLocation(t)
	date := models.CalendarDate(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))

	// 09:00 clinic time. The doctor works 09:00 to 17:00, so 09:00 is
	// exactly at the lead time boundary and 09:30 is inside it.
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, loc)

	svc, repo := testFixture(t, now)

	// Take the 11:00 slot so that one shows as booked.
	if _, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID:  1,
		PatientID: 1,
		Date:      "2026-10-21",
		StartTime: "11:00",
	}); err != nil {
		t.Fatalf("book: %v", err)
	}

	schedule, err := svc.Schedule(context.Background(), 1, "2026-10-21")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}

	if len(schedule.Slots) != 16 {
		t.Fatalf("got %d slots, want 16", len(schedule.Slots))
	}

	tests := []struct {
		start string
		want  SlotStatus
	}{
		{start: "09:00", want: SlotTooSoon},
		{start: "09:30", want: SlotTooSoon},
		{start: "10:00", want: SlotAvailable},
		{start: "10:30", want: SlotAvailable},
		{start: "11:00", want: SlotTaken},
		{start: "16:30", want: SlotAvailable},
	}

	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			slot := findSlot(t, schedule, tt.start)
			if slot.Status != tt.want {
				t.Errorf("slot %s status = %q, want %q", tt.start, slot.Status, tt.want)
			}
			if slot.Bookable() != (tt.want == SlotAvailable) {
				t.Errorf("slot %s bookable = %v, want %v", tt.start, slot.Bookable(), tt.want == SlotAvailable)
			}
		})
	}

	// The count must agree with the individual slots.
	available := 0
	for _, slot := range schedule.Slots {
		if slot.Bookable() {
			available++
		}
	}
	if schedule.AvailableCount() != available {
		t.Errorf("AvailableCount() = %d, want %d", schedule.AvailableCount(), available)
	}
	if available != 13 {
		t.Errorf("available slots = %d, want 13", available)
	}

	// The booking must be in the repository, so the schedule is not
	// inventing anything.
	booked, err := repo.BookedAppointmentsForDoctor(context.Background(), 1, date)
	if err != nil {
		t.Fatalf("booked: %v", err)
	}
	if len(booked) != 1 {
		t.Fatalf("booked appointments = %d, want 1", len(booked))
	}
}

// TestScheduleMarksPastSlots covers a day that has already finished.
func TestScheduleMarksPastSlots(t *testing.T) {
	loc := testLocation(t)
	now := time.Date(2026, 10, 21, 18, 0, 0, 0, loc)

	svc, _ := testFixture(t, now)

	schedule, err := svc.Schedule(context.Background(), 1, "2026-10-21")
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(schedule.Slots) != 16 {
		t.Fatalf("got %d slots, want 16", len(schedule.Slots))
	}
	for _, slot := range schedule.Slots {
		if slot.Status != SlotPast {
			t.Errorf("slot %s status = %q, want %q", slot.Start.Format("15:04"), slot.Status, SlotPast)
		}
	}
	if schedule.AvailableCount() != 0 {
		t.Errorf("available = %d, want 0", schedule.AvailableCount())
	}
}

// TestScheduleRejectsAnUnknownDoctor checks the 404 path.
func TestScheduleRejectsAnUnknownDoctor(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))

	_, err := svc.Schedule(context.Background(), 999, "2026-10-22")
	if err == nil {
		t.Fatal("Schedule = nil error, want not found")
	}
	if code := errorCode(err); code != "DOCTOR_NOT_FOUND" {
		t.Fatalf("error code = %q, want %q", code, "DOCTOR_NOT_FOUND")
	}
}

// findSlot returns the slot with the given start time.
func findSlot(t *testing.T, schedule *Schedule, start string) DaySlot {
	t.Helper()

	for _, slot := range schedule.Slots {
		if slot.Start.Format("15:04") == start {
			return slot
		}
	}
	t.Fatalf("no slot starting at %s", start)
	return DaySlot{}
}
