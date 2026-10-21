package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/PhilN8/clinic-booking-system/internal/database"
	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// integrationDB returns the connection string for the integration test
// database, skipping the test when it is not configured.
func integrationDB(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test")
	}
	return dsn
}

// testLocation is the clinic timezone used by the integration tests.
func testLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

// harness is a migrated, truncated database plus the service under test.
type harness struct {
	db   *sql.DB
	repo *repository.Repository
	svc  *service.Service
	now  time.Time
}

// newHarness prepares a clean database for one test.
//
// The "now" is pinned to 08:00 clinic time on 2026-10-21 so that bookings
// on 2026-10-22 are comfortably inside the one-hour lead time.
func newHarness(t *testing.T) *harness {
	t.Helper()

	dsn := integrationDB(t)
	ctx := context.Background()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.ExecContext(ctx, `TRUNCATE appointments, patients, doctors RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// One doctor with a full day of slots and one patient.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO doctors (id, name, start_working_hour, end_working_hour)
		VALUES (1, 'Dr. Amara Ochieng', '09:00', '17:00')`); err != nil {
		t.Fatalf("insert doctor: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO patients (id, name) VALUES (1, 'Grace Wanjiku')`); err != nil {
		t.Fatalf("insert patient: %v", err)
	}

	loc := testLocation(t)
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, loc)
	repo := repository.New(db, loc)

	return &harness{
		db:   db,
		repo: repo,
		svc:  service.New(repo, service.FixedClock{T: now}, loc),
		now:  now,
	}
}

// bookingDate is the day the tests book on.
const bookingDate = "2026-10-22"

// book books a slot and fails the test if it does not succeed.
func (h *harness) book(t *testing.T, start string) *models.Appointment {
	t.Helper()
	created, err := h.svc.BookAppointment(context.Background(), service.BookRequest{
		DoctorID:  1,
		PatientID: 1,
		Date:      bookingDate,
		StartTime: start,
	})
	if err != nil {
		t.Fatalf("book %s: %v", start, err)
	}
	return created
}

// errorCode returns the API error code carried by err.
func errorCode(err error) string {
	var typed *httperr.Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func TestBookingTheSameSlotTwiceConflicts(t *testing.T) {
	h := newHarness(t)
	h.book(t, "09:30")

	_, err := h.svc.BookAppointment(context.Background(), service.BookRequest{
		DoctorID:  1,
		PatientID: 1,
		Date:      bookingDate,
		StartTime: "09:30",
	})
	if err == nil {
		t.Fatal("second booking of the same slot = nil error, want conflict")
	}
	if code := errorCode(err); code != httperr.CodeSlotTaken {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeSlotTaken)
	}

	var count int
	if err := h.db.QueryRow(`SELECT count(*) FROM appointments WHERE status = 'booked'`).Scan(&count); err != nil {
		t.Fatalf("count appointments: %v", err)
	}
	if count != 1 {
		t.Fatalf("booked rows = %d, want 1", count)
	}
}

// TestConcurrentBookingOfTheSameSlot is the headline test for the partial
// unique index: two clients race for one slot and exactly one of them wins.
func TestConcurrentBookingOfTheSameSlot(t *testing.T) {
	h := newHarness(t)

	const racers = 8
	results := make([]error, racers)
	var wg sync.WaitGroup

	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := h.svc.BookAppointment(context.Background(), service.BookRequest{
				DoctorID:  1,
				PatientID: 1,
				Date:      bookingDate,
				StartTime: "10:00",
			})
			results[index] = err
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded, conflicted := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errorCode(err) == httperr.CodeSlotTaken:
			conflicted++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if succeeded != 1 {
		t.Fatalf("successful bookings = %d, want 1", succeeded)
	}
	if conflicted != racers-1 {
		t.Fatalf("conflicted bookings = %d, want %d", conflicted, racers-1)
	}

	var count int
	if err := h.db.QueryRow(`SELECT count(*) FROM appointments WHERE status = 'booked'`).Scan(&count); err != nil {
		t.Fatalf("count appointments: %v", err)
	}
	if count != 1 {
		t.Fatalf("booked rows = %d, want 1", count)
	}
}

func TestCancelFreesTheSlot(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "11:00")

	if _, err := h.svc.CancelAppointment(context.Background(), created.ID, nil); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// The slot must be bookable again.
	h.book(t, "11:00")
}

func TestCancellingTwiceConflicts(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "11:00")

	if _, err := h.svc.CancelAppointment(context.Background(), created.ID, nil); err != nil {
		t.Fatalf("first cancel: %v", err)
	}

	_, err := h.svc.CancelAppointment(context.Background(), created.ID, nil)
	if err == nil {
		t.Fatal("second cancel = nil error, want conflict")
	}
	if code := errorCode(err); code != httperr.CodeAppointmentCancelled {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeAppointmentCancelled)
	}
}

func TestRescheduleMovesTheAppointment(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "09:30")

	replacement, err := h.svc.RescheduleAppointment(context.Background(), created.ID, service.RescheduleRequest{
		NewDate:      bookingDate,
		NewStartTime: "14:00",
	})
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if replacement.StartTime.Format("15:04") != "14:00" {
		t.Errorf("replacement start = %q, want %q", replacement.StartTime.Format("15:04"), "14:00")
	}
	if replacement.RescheduledFromID == nil || *replacement.RescheduledFromID != created.ID {
		t.Errorf("rescheduled_from_id = %v, want %d", replacement.RescheduledFromID, created.ID)
	}

	original, err := h.repo.AppointmentByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("load original: %v", err)
	}
	if original.Status != models.StatusCancelled {
		t.Errorf("original status = %q, want %q", original.Status, models.StatusCancelled)
	}
	if original.CancellationReason == nil || *original.CancellationReason != "rescheduled" {
		t.Errorf("original cancellation reason = %v, want %q", original.CancellationReason, "rescheduled")
	}

	// The original slot must be available again.
	availability, err := h.svc.Availability(context.Background(), 1, bookingDate)
	if err != nil {
		t.Fatalf("availability: %v", err)
	}
	originalSlotAvailable := false
	for _, slot := range availability.Slots {
		if slot.Start == "09:30" {
			originalSlotAvailable = true
		}
	}
	if !originalSlotAvailable {
		t.Error("the original slot is still marked as taken after the reschedule")
	}
}

// TestRescheduleToAnOccupiedSlotRollsBack proves the transaction is atomic:
// when the replacement cannot be inserted, the original is still booked.
func TestRescheduleToAnOccupiedSlotRollsBack(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "09:30")
	h.book(t, "14:00")

	_, err := h.svc.RescheduleAppointment(context.Background(), created.ID, service.RescheduleRequest{
		NewDate:      bookingDate,
		NewStartTime: "14:00",
	})
	if err == nil {
		t.Fatal("reschedule onto an occupied slot = nil error, want conflict")
	}
	if code := errorCode(err); code != httperr.CodeSlotTaken {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeSlotTaken)
	}

	original, err := h.repo.AppointmentByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("load original: %v", err)
	}
	if original.Status != models.StatusBooked {
		t.Fatalf("original status = %q, want %q: the transaction did not roll back", original.Status, models.StatusBooked)
	}
	if original.CancelledAt != nil {
		t.Error("the original was cancelled despite the rollback")
	}
}

// TestRescheduleToTheSameSlotIsAllowed covers the edge case where the
// replacement is booked for the slot the original occupied.
func TestRescheduleToTheSameSlotIsAllowed(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "09:30")

	replacement, err := h.svc.RescheduleAppointment(context.Background(), created.ID, service.RescheduleRequest{
		NewDate:      bookingDate,
		NewStartTime: "09:30",
	})
	if err != nil {
		t.Fatalf("reschedule to the same slot: %v", err)
	}
	if replacement.ID == created.ID {
		t.Error("the replacement should be a new appointment row")
	}

	var booked int
	if err := h.db.QueryRow(`SELECT count(*) FROM appointments WHERE status = 'booked'`).Scan(&booked); err != nil {
		t.Fatalf("count appointments: %v", err)
	}
	if booked != 1 {
		t.Fatalf("booked rows = %d, want 1", booked)
	}
}

// TestMigrationsAreIdempotent checks that running the migrations twice is
// safe, which is what happens whenever a second instance boots.
func TestMigrationsAreIdempotent(t *testing.T) {
	dsn := integrationDB(t)
	ctx := context.Background()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestSeededDataExists checks that the seed migration produces doctors a
// reviewer can use straight away.
func TestSeededDataExists(t *testing.T) {
	dsn := integrationDB(t)
	ctx := context.Background()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM doctors`).Scan(&count); err != nil {
		t.Fatalf("count doctors: %v", err)
	}
	if count == 0 {
		t.Fatal("no seeded doctors found")
	}
}

// TestWallClockValuesRoundTrip guards the pgx encoding of TIME and DATE
// columns: a booked slot must come back with the same wall-clock time.
func TestWallClockValuesRoundTrip(t *testing.T) {
	h := newHarness(t)
	created := h.book(t, "13:30")

	stored, err := h.repo.AppointmentByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("load appointment: %v", err)
	}
	if got := stored.StartTime.Format("15:04"); got != "13:30" {
		t.Errorf("start time = %q, want %q", got, "13:30")
	}
	if got := stored.EndTime.Format("15:04"); got != "14:00" {
		t.Errorf("end time = %q, want %q", got, "14:00")
	}
	if got := stored.AppointmentDate.Format("2006-01-02"); got != bookingDate {
		t.Errorf("appointment date = %q, want %q", got, bookingDate)
	}
}
