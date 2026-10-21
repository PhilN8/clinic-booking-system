package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// bookAppointmentRequest is the body of POST /appointments.
type bookAppointmentRequest struct {
	DoctorID        int64  `json:"doctor_id"`
	PatientID       int64  `json:"patient_id"`
	AppointmentDate string `json:"appointment_date"`
	StartTime       string `json:"start_time"`
}

// cancelAppointmentRequest is the body of PATCH /appointments/{id}/cancel.
type cancelAppointmentRequest struct {
	// AppointmentID is accepted for convenience but must match the path.
	AppointmentID int64   `json:"appointment_id"`
	Reason        *string `json:"reason"`
}

// rescheduleAppointmentRequest is the body of PATCH /appointments/{id}/reschedule.
type rescheduleAppointmentRequest struct {
	// AppointmentID is accepted for convenience but must match the path.
	AppointmentID      int64  `json:"appointment_id"`
	NewAppointmentDate string `json:"new_appointment_date"`
	NewStartTime       string `json:"new_start_time"`
}

// appointmentResponse is the JSON representation of an appointment.
type appointmentResponse struct {
	ID                 int64   `json:"id"`
	DoctorID           int64   `json:"doctor_id"`
	PatientID          int64   `json:"patient_id"`
	AppointmentDate    string  `json:"appointment_date"`
	StartTime          string  `json:"start_time"`
	EndTime            string  `json:"end_time"`
	Status             string  `json:"status"`
	CancellationReason *string `json:"cancellation_reason"`
	CancelledAt        *string `json:"cancelled_at"`
	CreatedAt          string  `json:"created_at"`
	RescheduledFromID  *int64  `json:"rescheduled_from_id"`
}

func newAppointmentResponse(a models.Appointment) appointmentResponse {
	response := appointmentResponse{
		ID:                 a.ID,
		DoctorID:           a.DoctorID,
		PatientID:          a.PatientID,
		AppointmentDate:    a.AppointmentDate.Format("2006-01-02"),
		StartTime:          a.StartTime.Format("15:04"),
		EndTime:            a.EndTime.Format("15:04"),
		Status:             a.Status,
		CancellationReason: a.CancellationReason,
		CreatedAt:          a.CreatedAt.UTC().Format(time.RFC3339),
		RescheduledFromID:  a.RescheduledFromID,
	}
	if a.CancelledAt != nil {
		cancelledAt := a.CancelledAt.UTC().Format(time.RFC3339)
		response.CancelledAt = &cancelledAt
	}
	return response
}

// bookAppointment handles POST /appointments.
func (a *API) BookAppointment(w http.ResponseWriter, r *http.Request) {
	var request bookAppointmentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		httperr.Write(w, err)
		return
	}

	created, err := a.svc.BookAppointment(r.Context(), service.BookRequest{
		DoctorID:  request.DoctorID,
		PatientID: request.PatientID,
		Date:      request.AppointmentDate,
		StartTime: request.StartTime,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newAppointmentResponse(*created))
}

// cancelAppointment handles PATCH /appointments/{id}/cancel.
func (a *API) CancelAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	var request cancelAppointmentRequest
	if err := decodeOptionalJSON(w, r, &request); err != nil {
		httperr.Write(w, err)
		return
	}
	if request.AppointmentID != 0 && request.AppointmentID != id {
		httperr.Write(w, httperr.InvalidRequest(
			"appointment_id in the body must match the id in the path"))
		return
	}

	updated, err := a.svc.CancelAppointment(r.Context(), id, request.Reason)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newAppointmentResponse(*updated))
}

// rescheduleAppointment handles PATCH /appointments/{id}/reschedule.
func (a *API) RescheduleAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	var request rescheduleAppointmentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		httperr.Write(w, err)
		return
	}
	if request.AppointmentID != 0 && request.AppointmentID != id {
		httperr.Write(w, httperr.InvalidRequest(
			"appointment_id in the body must match the id in the path"))
		return
	}

	replacement, err := a.svc.RescheduleAppointment(r.Context(), id, service.RescheduleRequest{
		NewDate:      request.NewAppointmentDate,
		NewStartTime: request.NewStartTime,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newAppointmentResponse(*replacement))
}

// fail logs an unexpected error and renders the standard error envelope.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var typed *httperr.Error
	if !errors.As(err, &typed) {
		a.logger.Error("request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err.Error(),
		)
	}
	httperr.Write(w, err)
}
