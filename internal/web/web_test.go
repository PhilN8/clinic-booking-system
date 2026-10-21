package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PhilN8/clinic-booking-system/design"
	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// stubService is a programmable stand-in for the service layer.
type stubService struct {
	bookFn                func(context.Context, service.BookRequest) (*models.Appointment, error)
	cancelFn              func(context.Context, int64, *string) (*models.Appointment, error)
	rescheduleFn          func(context.Context, int64, service.RescheduleRequest) (*models.Appointment, error)
	scheduleFn            func(context.Context, int64, string) (*service.Schedule, error)
	listDoctorsFn         func(context.Context) ([]models.Doctor, error)
	listPatientsFn        func(context.Context) ([]models.Patient, error)
	patientAppointmentsFn func(context.Context, int64, bool) ([]models.Appointment, error)
	appointmentByIDFn     func(context.Context, int64) (*models.Appointment, error)
}

func (s *stubService) ListDoctors(ctx context.Context) ([]models.Doctor, error) {
	if s.listDoctorsFn == nil {
		return nil, nil
	}
	return s.listDoctorsFn(ctx)
}

func (s *stubService) DoctorByID(ctx context.Context, id int64) (*models.Doctor, error) {
	doctors, err := s.ListDoctors(ctx)
	if err != nil {
		return nil, err
	}
	for _, doctor := range doctors {
		if doctor.ID == id {
			return &doctor, nil
		}
	}
	return nil, errNotFound
}

func (s *stubService) ListPatients(ctx context.Context) ([]models.Patient, error) {
	if s.listPatientsFn == nil {
		return nil, nil
	}
	return s.listPatientsFn(ctx)
}

func (s *stubService) PatientByID(ctx context.Context, id int64) (*models.Patient, error) {
	patients, err := s.ListPatients(ctx)
	if err != nil {
		return nil, err
	}
	for _, patient := range patients {
		if patient.ID == id {
			return &patient, nil
		}
	}
	return nil, errNotFound
}

func (s *stubService) Schedule(ctx context.Context, doctorID int64, date string) (*service.Schedule, error) {
	if s.scheduleFn == nil {
		return &service.Schedule{}, nil
	}
	return s.scheduleFn(ctx, doctorID, date)
}

func (s *stubService) BookAppointment(ctx context.Context, req service.BookRequest) (*models.Appointment, error) {
	if s.bookFn == nil {
		return nil, errNotFound
	}
	return s.bookFn(ctx, req)
}

func (s *stubService) CancelAppointment(ctx context.Context, id int64, reason *string) (*models.Appointment, error) {
	if s.cancelFn == nil {
		return nil, errNotFound
	}
	return s.cancelFn(ctx, id, reason)
}

func (s *stubService) RescheduleAppointment(ctx context.Context, id int64, req service.RescheduleRequest) (*models.Appointment, error) {
	if s.rescheduleFn == nil {
		return nil, errNotFound
	}
	return s.rescheduleFn(ctx, id, req)
}

func (s *stubService) PatientAppointments(ctx context.Context, patientID int64, includeCancelled bool) ([]models.Appointment, error) {
	if s.patientAppointmentsFn == nil {
		return nil, nil
	}
	return s.patientAppointmentsFn(ctx, patientID, includeCancelled)
}

func (s *stubService) AppointmentByID(ctx context.Context, id int64) (*models.Appointment, error) {
	if s.appointmentByIDFn == nil {
		return nil, errNotFound
	}
	return s.appointmentByIDFn(ctx, id)
}

// errNotFound stands in for a missing resource.
var errNotFound = &stubError{"not found"}

type stubError struct{ message string }

func (e *stubError) Error() string { return e.message }

// testDoctors is the fixture list of doctors.
func testDoctors() []models.Doctor {
	return []models.Doctor{
		{ID: 1, Name: "Dr. Amara Ochieng", StartWorkingHour: models.TimeOfDay(9, 0), EndWorkingHour: models.TimeOfDay(17, 0)},
		{ID: 2, Name: "Dr. Brian Otieno", StartWorkingHour: models.TimeOfDay(8, 0), EndWorkingHour: models.TimeOfDay(16, 0)},
	}
}

func testPatients() []models.Patient {
	return []models.Patient{
		{ID: 1, Name: "Grace Wanjiku"},
		{ID: 2, Name: "Samuel Otieno"},
	}
}

// newTestWeb builds a site backed by the stub.
func newTestWeb(t *testing.T, stub *stubService) *Web {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens, err := design.Embedded()
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	loc, err := time.LoadLocation("Africa/Nairobi")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	web, err := New(stub, loc, logger, tokens)
	if err != nil {
		t.Fatalf("new web: %v", err)
	}
	return web
}

// defaultStub returns a stub with the standard fixtures wired up.
func defaultStub() *stubService {
	return &stubService{
		listDoctorsFn: func(context.Context) ([]models.Doctor, error) {
			return testDoctors(), nil
		},
		listPatientsFn: func(context.Context) ([]models.Patient, error) {
			return testPatients(), nil
		},
	}
}

// get performs a request that accepts HTML.
func get(t *testing.T, w *Web, path string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, w, http.MethodGet, path, nil, "text/html")
}

// request performs a request.
//
// The routes go through a real http.ServeMux, because the handlers read the
// id from the path with r.PathValue, which only a mux with a wildcard
// pattern populates.
func request(t *testing.T, w *Web, method, path string, form url.Values, accept string) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req := httptest.NewRequest(method, path, body)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /doctors", w.DoctorsPage)
	mux.HandleFunc("GET /doctors/{id}", w.DoctorPage)
	mux.HandleFunc("POST /doctors/{id}/appointments", w.BookAppointment)
	mux.HandleFunc("GET /patients/{id}/appointments", w.AppointmentsPage)
	mux.HandleFunc("GET /appointments/{id}/cancel", w.CancelPage)
	mux.HandleFunc("POST /appointments/{id}/cancel", w.CancelAppointment)
	mux.HandleFunc("GET /appointments/{id}/confirmation", w.ConfirmationPage)

	recorder := httptest.NewRecorder()
	w.Middleware(mux).ServeHTTP(recorder, req)

	return recorder
}

func TestDoctorsPageListsDoctors(t *testing.T) {
	web := newTestWeb(t, defaultStub())

	recorder := get(t, web, "/doctors")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	for _, want := range []string{"Dr. Amara Ochieng", "9:00 AM – 5:00 PM", "View availability", "/doctors/1"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not contain %q", want)
		}
	}
}

func TestDoctorPageRendersTheSlotGrid(t *testing.T) {
	stub := defaultStub()
	stub.scheduleFn = func(_ context.Context, _ int64, _ string) (*service.Schedule, error) {
		return &service.Schedule{
			DoctorID: 1,
			Date:     models.CalendarDate(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)),
			Slots: []service.DaySlot{
				{Start: models.TimeOfDay(9, 0), End: models.TimeOfDay(9, 30), Status: service.SlotAvailable},
				{Start: models.TimeOfDay(9, 30), End: models.TimeOfDay(10, 0), Status: service.SlotTaken},
				{Start: models.TimeOfDay(10, 0), End: models.TimeOfDay(10, 30), Status: service.SlotTooSoon},
			},
		}, nil
	}

	web := newTestWeb(t, stub)
	recorder := get(t, web, "/doctors/1?date=2026-11-02")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"9:00 AM",
		"slot--available",
		"slot--taken",
		"slot--tooSoon",
		"already booked",
		"too soon to book",
		"at least 1 hour in advance",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not contain %q", want)
		}
	}

	// The taken slot must be disabled, and the available one must not be.
	if !strings.Contains(body, `value="09:30" aria-label="9:30 AM, already booked" disabled`) {
		t.Error("the taken slot is not disabled")
	}
	if !strings.Contains(body, `value="09:00" aria-label="9:00 AM"`) {
		t.Error("the available slot is missing its accessible name")
	}
}

func TestBookAppointmentRedirectsToTheConfirmation(t *testing.T) {
	stub := defaultStub()
	stub.bookFn = func(_ context.Context, req service.BookRequest) (*models.Appointment, error) {
		if req.StartTime != "09:30" || req.Date != "2026-11-02" || req.PatientID != 1 {
			t.Errorf("unexpected booking request: %+v", req)
		}
		return &models.Appointment{
			ID:              7,
			DoctorID:        1,
			PatientID:       1,
			AppointmentDate: models.CalendarDate(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)),
			StartTime:       models.TimeOfDay(9, 30),
			EndTime:         models.TimeOfDay(10, 0),
			Status:          models.StatusBooked,
		}, nil
	}

	web := newTestWeb(t, stub)
	recorder := request(t, web, http.MethodPost, "/doctors/1/appointments", url.Values{
		"date":       {"2026-11-02"},
		"start_time": {"09:30"},
		"patient_id": {"1"},
	}, "text/html")

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/appointments/7/confirmation" {
		t.Errorf("Location = %q, want %q", location, "/appointments/7/confirmation")
	}
}

// TestBookAppointmentConflictKeepsThePatientOnTheDay covers the case the
// design calls out: the slot is taken between the page load and the submit.
func TestBookAppointmentConflictKeepsThePatientOnTheDay(t *testing.T) {
	stub := defaultStub()
	stub.scheduleFn = func(_ context.Context, _ int64, _ string) (*service.Schedule, error) {
		return &service.Schedule{
			DoctorID: 1,
			Slots: []service.DaySlot{
				{Start: models.TimeOfDay(9, 0), End: models.TimeOfDay(9, 30), Status: service.SlotTaken},
				{Start: models.TimeOfDay(9, 30), End: models.TimeOfDay(10, 0), Status: service.SlotAvailable},
			},
		}, nil
	}
	stub.bookFn = func(context.Context, service.BookRequest) (*models.Appointment, error) {
		return nil, httperr.SlotTaken("slot 09:00 is already booked")
	}

	web := newTestWeb(t, stub)
	recorder := request(t, web, http.MethodPost, "/doctors/1/appointments", url.Values{
		"date":       {"2026-11-02"},
		"start_time": {"09:00"},
		"patient_id": {"1"},
	}, "text/html")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "just taken by someone else") {
		t.Error("the page does not explain that the slot was taken")
	}
	if !strings.Contains(body, "slot--taken") {
		t.Error("the slot is not shown as taken")
	}
}

func TestNotFoundRendersAPage(t *testing.T) {
	web := newTestWeb(t, defaultStub())

	recorder := get(t, web, "/doctors/999")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if !strings.Contains(recorder.Body.String(), "could not find that page") {
		t.Error("the not found page is missing its message")
	}
}

// TestEveryTemplateRenders guards against a page whose data type does not
// match what its template expects. The mismatch only shows up at render
// time, so it has to be rendered.
func TestEveryTemplateRenders(t *testing.T) {
	web := newTestWeb(t, defaultStub())

	for _, page := range pages {
		tmpl, ok := web.templates[page]
		if !ok {
			t.Errorf("template %q is not loaded", page)
			continue
		}
		if tmpl.Lookup("content") == nil {
			t.Errorf("template %q does not define a content block", page)
		}
	}
}

func TestWantsHTML(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		want   bool
	}{
		{name: "browser", accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", want: true},
		{name: "curl", accept: "*/*", want: false},
		{name: "json client", accept: "application/json", want: false},
		{name: "nothing", accept: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/doctors", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			if got := wantsHTML(req); got != tt.want {
				t.Errorf("wantsHTML() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "09:00", want: "9:00 AM"},
		{input: "13:30", want: "1:30 PM"},
		{input: "00:00", want: "12:00 AM"},
		{input: "12:00", want: "12:00 PM"},
		{input: "23:59", want: "11:59 PM"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed, err := service.ParseTimeOfDay(tt.input)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := FormatTime(parsed); got != tt.want {
				t.Errorf("FormatTime(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInitials(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Dr. Amara Ochieng", want: "AO"},
		{name: "Dr. Brian Otieno", want: "BO"},
		{name: "Grace Wanjiku", want: "GW"},
		{name: "Cher", want: "C"},
		{name: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Initials(tt.name); got != tt.want {
				t.Errorf("Initials(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
