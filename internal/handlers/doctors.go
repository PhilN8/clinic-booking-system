package handlers

import (
	"net/http"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/models"
)

// doctorResponse is the JSON representation of a doctor.
type doctorResponse struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	StartWorkingHour string `json:"start_working_hour"`
	EndWorkingHour   string `json:"end_working_hour"`
}

func newDoctorResponse(d models.Doctor) doctorResponse {
	return doctorResponse{
		ID:               d.ID,
		Name:             d.Name,
		StartWorkingHour: d.StartWorkingHour.Format("15:04"),
		EndWorkingHour:   d.EndWorkingHour.Format("15:04"),
	}
}

// listDoctors handles GET /doctors.
func (a *API) ListDoctors(w http.ResponseWriter, r *http.Request) {
	doctors, err := a.svc.ListDoctors(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}

	response := make([]doctorResponse, 0, len(doctors))
	for _, doctor := range doctors {
		response = append(response, newDoctorResponse(doctor))
	}
	writeJSON(w, http.StatusOK, response)
}

// getDoctor handles GET /doctors/{id}.
func (a *API) GetDoctor(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	doctor, err := a.svc.DoctorByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newDoctorResponse(*doctor))
}

// availability handles GET /doctors/{id}/availability?date=YYYY-MM-DD.
func (a *API) Availability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httperr.Write(w, err)
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		httperr.Write(w, httperr.InvalidDate("the date query parameter is required and must be in YYYY-MM-DD format"))
		return
	}

	availability, err := a.svc.Availability(r.Context(), id, date)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, availability)
}
