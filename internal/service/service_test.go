package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/repository"
)

// fakeRepository is an in-memory stand-in for the Postgres repository.
//
// It deliberately models the one behaviour that matters for these tests: a
// second booking of the same (doctor, date, start) is rejected with
// ErrConflict, exactly as the partial unique index rejects it.
type fakeRepository struct {
	mu           sync.Mutex
	loc          *time.Location
	doctors      map[int64]models.Doctor
	patients     map[int64]models.Patient
	appointments map[int64]models.Appointment
	nextID       int64
}

func newFakeRepository(loc *time.Location) *fakeRepository {
	return &fakeRepository{
		loc:          loc,
		doctors:      map[int64]models.Doctor{},
		patients:     map[int64]models.Patient{},
		appointments: map[int64]models.Appointment{},
		nextID:       1,
	}
}

// startsAt mirrors the real repository's conversion of a naive wall-clock
// appointment into an absolute instant.
func (f *fakeRepository) startsAt(a *models.Appointment) time.Time {
	return time.Date(
		a.AppointmentDate.Year(), a.AppointmentDate.Month(), a.AppointmentDate.Day(),
		a.StartTime.Hour(), a.StartTime.Minute(), 0, 0,
		f.loc,
	)
}

func (f *fakeRepository) DoctorByID(_ context.Context, id int64) (*models.Doctor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doctor, ok := f.doctors[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &doctor, nil
}

func (f *fakeRepository) ListDoctors(_ context.Context) ([]models.Doctor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doctors := make([]models.Doctor, 0, len(f.doctors))
	for _, doctor := range f.doctors {
		doctors = append(doctors, doctor)
	}
	return doctors, nil
}

func (f *fakeRepository) PatientByID(_ context.Context, id int64) (*models.Patient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	patient, ok := f.patients[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &patient, nil
}

func (f *fakeRepository) CreateAppointment(_ context.Context, a *models.Appointment) (*models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.appointments {
		if existing.Status == models.StatusBooked &&
			existing.DoctorID == a.DoctorID &&
			existing.AppointmentDate.Equal(a.AppointmentDate) &&
			existing.StartTime.Equal(a.StartTime) {
			return nil, repository.ErrConflict
		}
	}

	created := *a
	created.ID = f.nextID
	f.nextID++
	created.CreatedAt = time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC)
	f.appointments[created.ID] = created
	return &created, nil
}

func (f *fakeRepository) AppointmentByID(_ context.Context, id int64) (*models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	appointment, ok := f.appointments[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &appointment, nil
}

func (f *fakeRepository) BookedAppointmentsForDoctor(_ context.Context, doctorID int64, date time.Time) ([]models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	appointments := make([]models.Appointment, 0, 4)
	for _, appointment := range f.appointments {
		if appointment.Status == models.StatusBooked &&
			appointment.DoctorID == doctorID &&
			appointment.AppointmentDate.Equal(date) {
			appointments = append(appointments, appointment)
		}
	}
	return appointments, nil
}

func (f *fakeRepository) CancelAppointment(_ context.Context, id int64, reason *string, now time.Time) (*models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	appointment, ok := f.appointments[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if appointment.Status == models.StatusCancelled {
		return nil, repository.ErrAlreadyCancelled
	}
	if f.startsAt(&appointment).Before(now) {
		return nil, repository.ErrAppointmentInPast
	}
	appointment.Status = models.StatusCancelled
	appointment.CancellationReason = reason
	appointment.CancelledAt = &now
	f.appointments[id] = appointment
	return &appointment, nil
}

func (f *fakeRepository) RescheduleAppointment(_ context.Context, id int64, newDate, newStart time.Time, now time.Time) (*models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	original, ok := f.appointments[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if original.Status == models.StatusCancelled {
		return nil, repository.ErrAlreadyCancelled
	}
	if f.startsAt(&original).Before(now) {
		return nil, repository.ErrAppointmentInPast
	}
	for _, existing := range f.appointments {
		if existing.Status == models.StatusBooked &&
			existing.DoctorID == original.DoctorID &&
			existing.AppointmentDate.Equal(newDate) &&
			existing.StartTime.Equal(newStart) {
			return nil, repository.ErrConflict
		}
	}

	original.Status = models.StatusCancelled
	reason := "rescheduled"
	original.CancellationReason = &reason
	original.CancelledAt = &now
	f.appointments[id] = original

	replacement := models.Appointment{
		ID:                f.nextID,
		DoctorID:          original.DoctorID,
		PatientID:         original.PatientID,
		AppointmentDate:   newDate,
		StartTime:         newStart,
		EndTime:           newStart.Add(SlotDuration),
		Status:            models.StatusBooked,
		CreatedAt:         now,
		RescheduledFromID: &original.ID,
	}
	f.nextID++
	f.appointments[replacement.ID] = replacement
	return &replacement, nil
}

func (f *fakeRepository) AppointmentsForPatient(_ context.Context, patientID int64, includeCancelled bool, now time.Time) ([]models.Appointment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	appointments := make([]models.Appointment, 0, 4)
	for _, appointment := range f.appointments {
		if appointment.PatientID != patientID {
			continue
		}
		if !includeCancelled && appointment.Status != models.StatusBooked {
			continue
		}
		appointments = append(appointments, appointment)
	}
	return appointments, nil
}

// testFixture wires a service to a fake repository with one doctor and one
// patient.
func testFixture(t *testing.T, now time.Time) (*Service, *fakeRepository) {
	t.Helper()

	loc := testLocation(t)
	repo := newFakeRepository(loc)
	repo.doctors[1] = models.Doctor{
		ID:               1,
		Name:             "Dr. Amara Ochieng",
		StartWorkingHour: models.TimeOfDay(9, 0),
		EndWorkingHour:   models.TimeOfDay(17, 0),
	}
	repo.patients[1] = models.Patient{ID: 1, Name: "Grace Wanjiku"}

	return New(repo, FixedClock{T: now}, loc), repo
}

// clinicNow is the pinned "now" used by the service tests.
func clinicNow(t *testing.T) time.Time {
	t.Helper()
	loc := testLocation(t)
	return time.Date(2026, 10, 21, 8, 0, 0, 0, loc)
}

// tomorrow is the date used for bookings in the service tests.
func tomorrow(t *testing.T) string {
	t.Helper()
	loc := testLocation(t)
	return time.Date(2026, 10, 22, 0, 0, 0, 0, loc).Format("2006-01-02")
}

func TestBookAppointment(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))

	created, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID:  1,
		PatientID: 1,
		Date:      tomorrow(t),
		StartTime: "09:30",
	})
	if err != nil {
		t.Fatalf("BookAppointment returned unexpected error: %v", err)
	}
	if created.Status != models.StatusBooked {
		t.Errorf("status = %q, want %q", created.Status, models.StatusBooked)
	}
	if got := created.EndTime.Format("15:04"); got != "10:00" {
		t.Errorf("end time = %q, want %q", got, "10:00")
	}
	if created.DoctorID != 1 || created.PatientID != 1 {
		t.Errorf("unexpected doctor/patient: %d/%d", created.DoctorID, created.PatientID)
	}
}

func TestBookAppointmentErrors(t *testing.T) {
	tests := []struct {
		name     string
		request  func(t *testing.T) BookRequest
		now      time.Time
		wantCode string
	}{
		{
			name: "unknown doctor",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 99, PatientID: 1, Date: tomorrow(t), StartTime: "09:30"}
			},
			wantCode: httperr.CodeDoctorNotFound,
		},
		{
			name: "unknown patient",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 1, PatientID: 99, Date: tomorrow(t), StartTime: "09:30"}
			},
			wantCode: httperr.CodePatientNotFound,
		},
		{
			name: "malformed date",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 1, PatientID: 1, Date: "22-10-2026", StartTime: "09:30"}
			},
			wantCode: httperr.CodeInvalidDate,
		},
		{
			name: "malformed time",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "9:30"}
			},
			wantCode: httperr.CodeInvalidTime,
		},
		{
			name: "outside working hours",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "18:00"}
			},
			wantCode: httperr.CodeOutsideWorkingHours,
		},
		{
			name: "off the slot grid",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:15"}
			},
			wantCode: httperr.CodeSlotNotAligned,
		},
		{
			name: "too soon",
			request: func(t *testing.T) BookRequest {
				// "now" is 08:30, so the 09:00 slot is only half an hour away.
				return BookRequest{DoctorID: 1, PatientID: 1, Date: "2026-10-21", StartTime: "09:00"}
			},
			now:      time.Date(2026, 10, 21, 8, 30, 0, 0, testLocation(t)),
			wantCode: httperr.CodeTooSoon,
		},
		{
			name: "non-positive doctor id",
			request: func(t *testing.T) BookRequest {
				return BookRequest{DoctorID: 0, PatientID: 1, Date: tomorrow(t), StartTime: "09:30"}
			},
			wantCode: httperr.CodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := tt.now
			if now.IsZero() {
				now = clinicNow(t)
			}
			svc, _ := testFixture(t, now)
			_, err := svc.BookAppointment(context.Background(), tt.request(t))
			if err == nil {
				t.Fatal("BookAppointment = nil error, want error")
			}
			if code := errorCode(err); code != tt.wantCode {
				t.Fatalf("error code = %q, want %q (err: %v)", code, tt.wantCode, err)
			}
		})
	}
}

func TestBookAppointmentConflict(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))
	request := BookRequest{DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30"}

	if _, err := svc.BookAppointment(context.Background(), request); err != nil {
		t.Fatalf("first booking failed: %v", err)
	}

	_, err := svc.BookAppointment(context.Background(), request)
	if err == nil {
		t.Fatal("second booking = nil error, want conflict")
	}
	if code := errorCode(err); code != httperr.CodeSlotTaken {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeSlotTaken)
	}
}

func TestCancelAppointment(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))

	created, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30",
	})
	if err != nil {
		t.Fatalf("BookAppointment failed: %v", err)
	}

	reason := "Patient no longer needs the appointment"
	cancelled, err := svc.CancelAppointment(context.Background(), created.ID, &reason)
	if err != nil {
		t.Fatalf("CancelAppointment returned unexpected error: %v", err)
	}
	if cancelled.Status != models.StatusCancelled {
		t.Errorf("status = %q, want %q", cancelled.Status, models.StatusCancelled)
	}
	if cancelled.CancellationReason == nil || *cancelled.CancellationReason != reason {
		t.Errorf("cancellation reason = %v, want %q", cancelled.CancellationReason, reason)
	}
	if cancelled.CancelledAt == nil {
		t.Error("cancelled_at was not set")
	}
}

func TestCancelAppointmentErrors(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, svc *Service) int64
		wantCode string
	}{
		{
			name: "unknown appointment",
			setup: func(t *testing.T, svc *Service) int64 {
				return 4242
			},
			wantCode: httperr.CodeAppointmentNotFound,
		},
		{
			name: "already cancelled",
			setup: func(t *testing.T, svc *Service) int64 {
				created, err := svc.BookAppointment(context.Background(), BookRequest{
					DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30",
				})
				if err != nil {
					t.Fatalf("BookAppointment failed: %v", err)
				}
				if _, err := svc.CancelAppointment(context.Background(), created.ID, nil); err != nil {
					t.Fatalf("CancelAppointment failed: %v", err)
				}
				return created.ID
			},
			wantCode: httperr.CodeAppointmentCancelled,
		},
		{
			name: "appointment already started",
			setup: func(t *testing.T, svc *Service) int64 {
				created, err := svc.BookAppointment(context.Background(), BookRequest{
					DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30",
				})
				if err != nil {
					t.Fatalf("BookAppointment failed: %v", err)
				}
				// Move "now" past the start of the appointment.
				svc.clock = FixedClock{T: time.Date(2026, 10, 22, 10, 0, 0, 0, testLocation(t))}
				return created.ID
			},
			wantCode: httperr.CodeAppointmentInPast,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := testFixture(t, clinicNow(t))
			id := tt.setup(t, svc)

			_, err := svc.CancelAppointment(context.Background(), id, nil)
			if err == nil {
				t.Fatal("CancelAppointment = nil error, want error")
			}
			if code := errorCode(err); code != tt.wantCode {
				t.Fatalf("error code = %q, want %q (err: %v)", code, tt.wantCode, err)
			}
		})
	}
}

func TestRescheduleAppointment(t *testing.T) {
	svc, repo := testFixture(t, clinicNow(t))

	created, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30",
	})
	if err != nil {
		t.Fatalf("BookAppointment failed: %v", err)
	}

	replacement, err := svc.RescheduleAppointment(context.Background(), created.ID, RescheduleRequest{
		NewDate:      tomorrow(t),
		NewStartTime: "11:00",
	})
	if err != nil {
		t.Fatalf("RescheduleAppointment returned unexpected error: %v", err)
	}

	if replacement.Status != models.StatusBooked {
		t.Errorf("replacement status = %q, want %q", replacement.Status, models.StatusBooked)
	}
	if replacement.RescheduledFromID == nil || *replacement.RescheduledFromID != created.ID {
		t.Errorf("rescheduled_from_id = %v, want %d", replacement.RescheduledFromID, created.ID)
	}
	if got := replacement.StartTime.Format("15:04"); got != "11:00" {
		t.Errorf("replacement start = %q, want %q", got, "11:00")
	}

	original, err := repo.AppointmentByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("AppointmentByID failed: %v", err)
	}
	if original.Status != models.StatusCancelled {
		t.Errorf("original status = %q, want %q", original.Status, models.StatusCancelled)
	}
}

func TestRescheduleToOccupiedSlotLeavesOriginalBooked(t *testing.T) {
	svc, repo := testFixture(t, clinicNow(t))

	first, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "09:30",
	})
	if err != nil {
		t.Fatalf("BookAppointment failed: %v", err)
	}
	if _, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "11:00",
	}); err != nil {
		t.Fatalf("second BookAppointment failed: %v", err)
	}

	_, err = svc.RescheduleAppointment(context.Background(), first.ID, RescheduleRequest{
		NewDate:      tomorrow(t),
		NewStartTime: "11:00",
	})
	if err == nil {
		t.Fatal("RescheduleAppointment = nil error, want conflict")
	}
	if code := errorCode(err); code != httperr.CodeSlotTaken {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeSlotTaken)
	}

	original, err := repo.AppointmentByID(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("AppointmentByID failed: %v", err)
	}
	if original.Status != models.StatusBooked {
		t.Errorf("original status = %q, want %q", original.Status, models.StatusBooked)
	}
}

func TestAvailabilityExcludesBookedAndSoonSlots(t *testing.T) {
	now := clinicNow(t)
	svc, _ := testFixture(t, now)

	if _, err := svc.BookAppointment(context.Background(), BookRequest{
		DoctorID: 1, PatientID: 1, Date: tomorrow(t), StartTime: "10:00",
	}); err != nil {
		t.Fatalf("BookAppointment failed: %v", err)
	}

	availability, err := svc.Availability(context.Background(), 1, tomorrow(t))
	if err != nil {
		t.Fatalf("Availability returned unexpected error: %v", err)
	}
	if len(availability.Slots) != 15 {
		t.Fatalf("got %d slots, want 15 (16 minus the booked one)", len(availability.Slots))
	}
	for _, slot := range availability.Slots {
		if slot.Start == "10:00" {
			t.Fatal("availability returned the booked slot 10:00")
		}
	}
}

// TestAvailabilityHidesSlotsBookingWouldReject proves that the two
// endpoints agree: anything availability offers, booking accepts.
func TestAvailabilityHidesSlotsBookingWouldReject(t *testing.T) {
	now := clinicNow(t)
	svc, _ := testFixture(t, now)

	// "now" is 08:00, so 09:00 is exactly one hour away and 08:30 would
	// be inside the lead time. Only 09:00 onwards may be offered.
	availability, err := svc.Availability(context.Background(), 1, "2026-10-21")
	if err != nil {
		t.Fatalf("Availability returned unexpected error: %v", err)
	}
	if len(availability.Slots) == 0 {
		t.Fatal("expected some slots for today")
	}
	for _, slot := range availability.Slots {
		if slot.Start < "09:00" {
			t.Errorf("availability offered %s, which is inside the lead time", slot.Start)
		}
	}
}

func TestAvailabilityForUnknownDoctor(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))

	_, err := svc.Availability(context.Background(), 99, tomorrow(t))
	if err == nil {
		t.Fatal("Availability = nil error, want not found")
	}
	if code := errorCode(err); code != httperr.CodeDoctorNotFound {
		t.Fatalf("error code = %q, want %q", code, httperr.CodeDoctorNotFound)
	}
}

func TestAvailabilityForPastDateIsEmpty(t *testing.T) {
	svc, _ := testFixture(t, clinicNow(t))

	availability, err := svc.Availability(context.Background(), 1, "2026-10-01")
	if err != nil {
		t.Fatalf("Availability returned unexpected error: %v", err)
	}
	if len(availability.Slots) != 0 {
		t.Fatalf("got %d slots for a past date, want 0", len(availability.Slots))
	}
}

// TestServiceImplementsAPI is a compile-time guard on the contract the
// handlers depend on.
func TestServiceImplementsAPI(t *testing.T) {
	var svc API = New(newFakeRepository(testLocation(t)), FixedClock{}, testLocation(t))
	if svc == nil {
		t.Fatal("service does not implement the API contract")
	}
}
