package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// stubService is a programmable stand-in for the service layer.
type stubService struct {
	bookFn                func(context.Context, service.BookRequest) (*models.Appointment, error)
	cancelFn              func(context.Context, int64, *string) (*models.Appointment, error)
	rescheduleFn          func(context.Context, int64, service.RescheduleRequest) (*models.Appointment, error)
	availabilityFn        func(context.Context, int64, string) (*service.Availability, error)
	listDoctorsFn         func(context.Context) ([]models.Doctor, error)
	doctorFn              func(context.Context, int64) (*models.Doctor, error)
	patientFn             func(context.Context, int64) (*models.Patient, error)
	patientAppointmentsFn func(context.Context, int64, bool) ([]models.Appointment, error)
}

func (s *stubService) BookAppointment(ctx context.Context, req service.BookRequest) (*models.Appointment, error) {
	if s.bookFn == nil {
		return nil, httperr.Internal()
	}
	return s.bookFn(ctx, req)
}

func (s *stubService) CancelAppointment(ctx context.Context, id int64, reason *string) (*models.Appointment, error) {
	if s.cancelFn == nil {
		return nil, httperr.Internal()
	}
	return s.cancelFn(ctx, id, reason)
}

func (s *stubService) RescheduleAppointment(ctx context.Context, id int64, req service.RescheduleRequest) (*models.Appointment, error) {
	if s.rescheduleFn == nil {
		return nil, httperr.Internal()
	}
	return s.rescheduleFn(ctx, id, req)
}

func (s *stubService) Availability(ctx context.Context, doctorID int64, date string) (*service.Availability, error) {
	if s.availabilityFn == nil {
		return nil, httperr.Internal()
	}
	return s.availabilityFn(ctx, doctorID, date)
}

func (s *stubService) ListDoctors(ctx context.Context) ([]models.Doctor, error) {
	if s.listDoctorsFn == nil {
		return nil, httperr.Internal()
	}
	return s.listDoctorsFn(ctx)
}

func (s *stubService) DoctorByID(ctx context.Context, id int64) (*models.Doctor, error) {
	if s.doctorFn == nil {
		return nil, httperr.Internal()
	}
	return s.doctorFn(ctx, id)
}

func (s *stubService) PatientByID(ctx context.Context, id int64) (*models.Patient, error) {
	if s.patientFn == nil {
		return nil, httperr.Internal()
	}
	return s.patientFn(ctx, id)
}

func (s *stubService) PatientAppointments(ctx context.Context, patientID int64, includeCancelled bool) ([]models.Appointment, error) {
	if s.patientAppointmentsFn == nil {
		return nil, httperr.Internal()
	}
	return s.patientAppointmentsFn(ctx, patientID, includeCancelled)
}

// stubHealth always reports a healthy database.
type stubHealth struct{ err error }

func (h stubHealth) PingContext(context.Context) error { return h.err }

// newTestRouter builds a router with just the JSON API routes.
//
// The full route table lives in cmd/api, where the API and the site are
// negotiated against each other; this is the API half of it.
func newTestRouter(t *testing.T, stub *stubService) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New(stub, stubHealth{}, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.Healthz)
	mux.HandleFunc("POST /appointments", api.BookAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/cancel", api.CancelAppointment)
	mux.HandleFunc("PATCH /appointments/{id}/reschedule", api.RescheduleAppointment)
	mux.HandleFunc("GET /doctors", api.ListDoctors)
	mux.HandleFunc("GET /doctors/{id}", api.GetDoctor)
	mux.HandleFunc("GET /doctors/{id}/availability", api.Availability)
	mux.HandleFunc("GET /patients/{id}", api.GetPatient)
	mux.HandleFunc("GET /patients/{id}/appointments", api.PatientAppointments)

	return api.Middleware(mux)
}

// sampleAppointment is a booked appointment used in the response assertions.
func sampleAppointment() models.Appointment {
	cancelledAt := time.Date(2026, 10, 21, 6, 30, 0, 0, time.UTC)
	return models.Appointment{
		ID:              42,
		DoctorID:        1,
		PatientID:       1,
		AppointmentDate: models.CalendarDate(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)),
		StartTime:       models.TimeOfDay(9, 30),
		EndTime:         models.TimeOfDay(10, 0),
		Status:          models.StatusBooked,
		CreatedAt:       time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC),
		CancelledAt:     &cancelledAt,
	}
}

// do performs a request against the router and returns the recorder.
func do(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// decodeError reads the standard error envelope from a response.
func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) httperr.Body {
	t.Helper()
	var body httperr.Body
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", recorder.Body.String(), err)
	}
	return body
}

func TestBookAppointmentCreated(t *testing.T) {
	var received service.BookRequest
	stub := &stubService{bookFn: func(_ context.Context, req service.BookRequest) (*models.Appointment, error) {
		received = req
		appointment := sampleAppointment()
		return &appointment, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodPost, "/appointments",
		`{"doctor_id":1,"patient_id":2,"appointment_date":"2026-10-22","start_time":"09:30"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if received.DoctorID != 1 || received.PatientID != 2 || received.Date != "2026-10-22" || received.StartTime != "09:30" {
		t.Fatalf("service received unexpected request: %+v", received)
	}

	var response struct {
		ID              int64   `json:"id"`
		DoctorID        int64   `json:"doctor_id"`
		PatientID       int64   `json:"patient_id"`
		AppointmentDate string  `json:"appointment_date"`
		StartTime       string  `json:"start_time"`
		EndTime         string  `json:"end_time"`
		Status          string  `json:"status"`
		CancelledAt     *string `json:"cancelled_at"`
		CreatedAt       string  `json:"created_at"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.StartTime != "09:30" || response.EndTime != "10:00" {
		t.Errorf("unexpected response body: %+v", response)
	}
	if response.AppointmentDate != "2026-10-22" {
		t.Errorf("appointment_date = %q, want %q", response.AppointmentDate, "2026-10-22")
	}
	if response.CreatedAt == "" {
		t.Error("created_at is missing from the response")
	}
}

func TestBookAppointmentRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{name: "malformed json", body: `{"doctor_id":1,`, wantCode: httperr.CodeInvalidJSON},
		{name: "not an object", body: `[]`, wantCode: httperr.CodeInvalidJSON},
		{name: "unknown field", body: `{"doctor_id":1,"patient_id":1,"appointment_date":"2026-10-22","start_time":"09:30","extra":true}`, wantCode: httperr.CodeInvalidJSON},
		{name: "two objects", body: `{"doctor_id":1}{"doctor_id":2}`, wantCode: httperr.CodeInvalidJSON},
		{name: "wrong field type", body: `{"doctor_id":"one","patient_id":1,"appointment_date":"2026-10-22","start_time":"09:30"}`, wantCode: httperr.CodeInvalidJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubService{}
			recorder := do(t, newTestRouter(t, stub), http.MethodPost, "/appointments", tt.body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if got := decodeError(t, recorder).Error.Code; got != tt.wantCode {
				t.Fatalf("error code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestBookAppointmentMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name         string
		serviceError error
		wantStatus   int
		wantCode     string
	}{
		{
			name:         "slot taken is a conflict",
			serviceError: httperr.SlotTaken("slot 09:30 is already booked"),
			wantStatus:   http.StatusConflict,
			wantCode:     httperr.CodeSlotTaken,
		},
		{
			name:         "unknown doctor is a not found",
			serviceError: httperr.DoctorNotFound(9),
			wantStatus:   http.StatusNotFound,
			wantCode:     httperr.CodeDoctorNotFound,
		},
		{
			name:         "outside working hours is unprocessable",
			serviceError: httperr.OutsideWorkingHours("outside the doctor's working hours"),
			wantStatus:   http.StatusUnprocessableEntity,
			wantCode:     httperr.CodeOutsideWorkingHours,
		},
		{
			name:         "too soon is unprocessable",
			serviceError: httperr.TooSoon("too soon"),
			wantStatus:   http.StatusUnprocessableEntity,
			wantCode:     httperr.CodeTooSoon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubService{bookFn: func(context.Context, service.BookRequest) (*models.Appointment, error) {
				return nil, tt.serviceError
			}}

			recorder := do(t, newTestRouter(t, stub), http.MethodPost, "/appointments",
				`{"doctor_id":1,"patient_id":1,"appointment_date":"2026-10-22","start_time":"09:30"}`)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			body := decodeError(t, recorder)
			if body.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.wantCode)
			}
			if body.Error.Message == "" {
				t.Error("error message is empty")
			}
		})
	}
}

func TestCancelAppointment(t *testing.T) {
	var receivedID int64
	var receivedReason *string
	stub := &stubService{cancelFn: func(_ context.Context, id int64, reason *string) (*models.Appointment, error) {
		receivedID = id
		receivedReason = reason
		appointment := sampleAppointment()
		appointment.Status = models.StatusCancelled
		return &appointment, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodPatch, "/appointments/42/cancel",
		`{"reason":"Patient no longer needs the appointment"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if receivedID != 42 {
		t.Errorf("service received id %d, want 42", receivedID)
	}
	if receivedReason == nil || *receivedReason != "Patient no longer needs the appointment" {
		t.Errorf("service received reason %v", receivedReason)
	}
}

func TestCancelAppointmentAcceptsEmptyBody(t *testing.T) {
	stub := &stubService{cancelFn: func(context.Context, int64, *string) (*models.Appointment, error) {
		appointment := sampleAppointment()
		return &appointment, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodPatch, "/appointments/42/cancel", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestCancelAppointmentRejectsMismatchedBodyID(t *testing.T) {
	stub := &stubService{}

	recorder := do(t, newTestRouter(t, stub), http.MethodPatch, "/appointments/42/cancel",
		`{"appointment_id":7,"reason":"changed my mind"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := decodeError(t, recorder).Error.Code; got != httperr.CodeInvalidRequest {
		t.Fatalf("error code = %q, want %q", got, httperr.CodeInvalidRequest)
	}
}

func TestRescheduleAppointment(t *testing.T) {
	var received service.RescheduleRequest
	stub := &stubService{rescheduleFn: func(_ context.Context, id int64, req service.RescheduleRequest) (*models.Appointment, error) {
		received = req
		appointment := sampleAppointment()
		appointment.StartTime = models.TimeOfDay(11, 0)
		appointment.EndTime = models.TimeOfDay(11, 30)
		return &appointment, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodPatch, "/appointments/42/reschedule",
		`{"new_appointment_date":"2026-10-23","new_start_time":"11:00"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if received.NewDate != "2026-10-23" || received.NewStartTime != "11:00" {
		t.Fatalf("service received unexpected request: %+v", received)
	}
}

func TestPathIDMustBePositiveInteger(t *testing.T) {
	stub := &stubService{}

	for _, path := range []string{"/appointments/abc/cancel", "/appointments/0/cancel", "/appointments/-1/cancel"} {
		recorder := do(t, newTestRouter(t, stub), http.MethodPatch, path, "{}")
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", path, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestAvailability(t *testing.T) {
	stub := &stubService{availabilityFn: func(_ context.Context, doctorID int64, date string) (*service.Availability, error) {
		return &service.Availability{
			DoctorID: doctorID,
			Date:     date,
			Slots:    []service.SlotView{{Start: "09:00", End: "09:30"}},
		}, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/doctors/1/availability?date=2026-10-22", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		DoctorID int64  `json:"doctor_id"`
		Date     string `json:"date"`
		Slots    []struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DoctorID != 1 || response.Date != "2026-10-22" {
		t.Errorf("unexpected response: %+v", response)
	}
	if len(response.Slots) != 1 || response.Slots[0].Start != "09:00" {
		t.Errorf("unexpected slots: %+v", response.Slots)
	}
}

func TestAvailabilityRequiresADate(t *testing.T) {
	stub := &stubService{}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/doctors/1/availability", "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if got := decodeError(t, recorder).Error.Code; got != httperr.CodeInvalidDate {
		t.Fatalf("error code = %q, want %q", got, httperr.CodeInvalidDate)
	}
}

func TestPatientAppointments(t *testing.T) {
	var includeCancelled bool
	stub := &stubService{patientAppointmentsFn: func(_ context.Context, patientID int64, include bool) ([]models.Appointment, error) {
		includeCancelled = include
		return []models.Appointment{sampleAppointment()}, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/patients/1/appointments?include_cancelled=true", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !includeCancelled {
		t.Error("include_cancelled was not forwarded to the service")
	}

	var response struct {
		PatientID    int64 `json:"patient_id"`
		Appointments []struct {
			ID int64 `json:"id"`
		} `json:"appointments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.PatientID != 1 || len(response.Appointments) != 1 {
		t.Errorf("unexpected response: %+v", response)
	}
}

func TestListDoctors(t *testing.T) {
	stub := &stubService{listDoctorsFn: func(context.Context) ([]models.Doctor, error) {
		return []models.Doctor{{
			ID:               1,
			Name:             "Dr. Amara Ochieng",
			StartWorkingHour: models.TimeOfDay(9, 0),
			EndWorkingHour:   models.TimeOfDay(17, 0),
		}}, nil
	}}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/doctors", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response []struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		StartWorkingHour string `json:"start_working_hour"`
		EndWorkingHour   string `json:"end_working_hour"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 1 || response[0].StartWorkingHour != "09:00" || response[0].EndWorkingHour != "17:00" {
		t.Errorf("unexpected response: %+v", response)
	}
}

func TestHealthz(t *testing.T) {
	stub := &stubService{}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/healthz", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Errorf("unexpected body: %s", recorder.Body.String())
	}
}

func TestHealthzReportsAnUnhealthyDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New(&stubService{}, stubHealth{err: context.DeadlineExceeded}, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.Healthz)

	recorder := do(t, api.Middleware(mux), http.MethodGet, "/healthz", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// TestUnknownRouteIsNotFound checks that the router does not fall through to
// some catch-all handler.
func TestUnknownRouteIsNotFound(t *testing.T) {
	stub := &stubService{}

	recorder := do(t, newTestRouter(t, stub), http.MethodGet, "/nope", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// TestMethodNotAllowed checks that the method is part of the route.
func TestMethodNotAllowed(t *testing.T) {
	stub := &stubService{}

	recorder := do(t, newTestRouter(t, stub), http.MethodDelete, "/appointments", "")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
