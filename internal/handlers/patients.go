package handlers

import (
	"net/http"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// patientResponse is the JSON representation of a patient.
type patientResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func newPatientResponse(p models.Patient) patientResponse {
	return patientResponse{ID: p.ID, Name: p.Name}
}

// patientAppointmentsResponse is the body of GET /patients/{id}/appointments.
type patientAppointmentsResponse struct {
	PatientID    int64                 `json:"patient_id"`
	Appointments []appointmentResponse `json:"appointments"`
}

// getPatient handles GET /patients/{id}.
func (a *API) getPatient(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	patient, err := a.svc.PatientByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newPatientResponse(*patient))
}

// patientAppointments handles GET /patients/{id}/appointments.
//
// By default the list contains only booked appointments that have not started
// yet. Passing ?include_cancelled=true also returns cancelled appointments,
// which is what makes a reschedule's audit trail visible.
func (a *API) patientAppointments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	appointments, err := a.svc.PatientAppointments(r.Context(), id, queryBool(r, "include_cancelled"))
	if err != nil {
		a.fail(w, r, err)
		return
	}

	response := make([]appointmentResponse, 0, len(appointments))
	for _, appointment := range appointments {
		response = append(response, newAppointmentResponse(appointment))
	}
	writeJSON(w, http.StatusOK, patientAppointmentsResponse{
		PatientID:    id,
		Appointments: response,
	})
}
