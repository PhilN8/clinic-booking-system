package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

// patientCookie remembers which patient the site is acting as.
//
// The API has no authentication, so the site cannot know who is browsing.
// Rather than pretend otherwise, the patient is an explicit choice that is
// remembered between pages. See design.md section 15.
const patientCookie = "clinic_patient"

// dateStripDays is how many days the date strip shows.
const dateStripDays = 14

// LandingPage renders the front page.
func (w *Web) LandingPage(rw http.ResponseWriter, r *http.Request) {
	data := landingPage{
		base: w.base(r, "World-class care, and a booking that takes a minute",
			"Book an appointment with one of our doctors at Nairobi Clinic."),
	}
	w.render(rw, http.StatusOK, "landing", data)
}

// DoctorsPage renders the list of doctors.
func (w *Web) DoctorsPage(rw http.ResponseWriter, r *http.Request) {
	doctors, err := w.svc.ListDoctors(r.Context())
	if err != nil {
		w.serverError(rw, r, err)
		return
	}

	cards := make([]doctorCard, 0, len(doctors))
	for _, doctor := range doctors {
		cards = append(cards, doctorCard{
			ID:       doctor.ID,
			Name:     doctor.Name,
			Initials: Initials(doctor.Name),
			Hours:    FormatTimeRange(doctor.StartWorkingHour, doctor.EndWorkingHour),
			Detail:   "Available " + FormatTimeRange(doctor.StartWorkingHour, doctor.EndWorkingHour),
		})
	}

	data := doctorsPage{
		base:    w.base(r, "Book an appointment", "Choose a doctor to see the times they are available."),
		Doctors: cards,
	}
	data.NavDoctors = true

	w.render(rw, http.StatusOK, "doctors", data)
}

// DoctorPage renders a doctor's day and the booking form.
func (w *Web) DoctorPage(rw http.ResponseWriter, r *http.Request) {
	doctorID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}
	w.renderDoctor(rw, r, doctorID, r.URL.Query().Get("date"), "", nil)
}

// BookAppointment handles the booking form.
func (w *Web) BookAppointment(rw http.ResponseWriter, r *http.Request) {
	doctorID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.serverError(rw, r, err)
		return
	}

	date := r.PostFormValue("date")
	startTime := r.PostFormValue("start_time")
	patientID, _ := strconv.ParseInt(r.PostFormValue("patient_id"), 10, 64)

	if patientID <= 0 {
		w.renderDoctor(rw, r, doctorID, date, startTime,
			&alertView{Tone: "danger", Title: "Who is it for?", Message: "Choose who the appointment is for, then book again."})
		return
	}

	created, err := w.svc.BookAppointment(r.Context(), service.BookRequest{
		DoctorID:  doctorID,
		PatientID: patientID,
		Date:      date,
		StartTime: startTime,
	})
	if err != nil {
		// The slot may have been taken between the page load and the
		// submit. Re-render the day: the schedule now shows the slot as
		// taken, and the patient chooses another one without losing their
		// place in the day.
		message, code := formatError(err)
		w.renderDoctor(rw, r, doctorID, date, startTime,
			&alertView{Tone: "danger", Message: message, Title: alertTitle(code)})
		return
	}

	w.rememberPatient(rw, patientID)
	http.Redirect(rw, r, fmt.Sprintf("/appointments/%d/confirmation", created.ID), http.StatusSeeOther)
}

// AppointmentsPage renders a patient's appointments.
func (w *Web) AppointmentsPage(rw http.ResponseWriter, r *http.Request) {
	patientID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	patient, err := w.svc.PatientByID(r.Context(), patientID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	appointments, err := w.svc.PatientAppointments(r.Context(), patientID, true)
	if err != nil {
		w.serverError(rw, r, err)
		return
	}

	doctors, err := w.svc.ListDoctors(r.Context())
	if err != nil {
		w.serverError(rw, r, err)
		return
	}
	names := make(map[int64]*models.Doctor, len(doctors))
	for _, doctor := range doctors {
		names[doctor.ID] = &doctor
	}

	cards := make([]appointmentCard, 0, len(appointments))
	for _, appointment := range appointments {
		cards = append(cards, w.appointmentCard(appointment, names[appointment.DoctorID]))
	}

	data := appointmentsPage{
		base:         w.base(r, "Your appointments", "Your upcoming and past appointments."),
		Patient:      *patient,
		Appointments: cards,
	}
	data.NavAppointments = true
	data.PatientID = patientID

	w.render(rw, http.StatusOK, "appointments", data)
}

// CancelPage renders the cancellation confirmation.
//
// Cancelling is destructive, so it takes a second deliberate step rather
// than happening on the first click.
func (w *Web) CancelPage(rw http.ResponseWriter, r *http.Request) {
	appointmentID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	appointment, doctor, err := w.appointment(r.Context(), appointmentID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	data := cancelPage{
		base:        w.base(r, "Cancel this appointment", "Confirm you want to cancel this appointment."),
		Appointment: *appointment,
		Doctor:      w.doctorView(doctor),
		DateLabel:   FormatDateLong(appointment.AppointmentDate),
		TimeLabel:   FormatTimeRange(appointment.StartTime, appointment.EndTime),
	}
	data.PatientID = appointment.PatientID

	w.render(rw, http.StatusOK, "cancel", data)
}

// CancelAppointment handles the cancellation form.
func (w *Web) CancelAppointment(rw http.ResponseWriter, r *http.Request) {
	appointmentID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.serverError(rw, r, err)
		return
	}

	var reason *string
	if raw := strings.TrimSpace(r.PostFormValue("reason")); raw != "" {
		reason = &raw
	}

	updated, err := w.svc.CancelAppointment(r.Context(), appointmentID, reason)
	if err != nil {
		message, code := formatError(err)
		status := statusForCode(code)
		if status == http.StatusNotFound {
			w.notFound(rw, r)
			return
		}
		w.renderCancelError(rw, r, appointmentID, &alertView{Tone: "danger", Message: message})
		return
	}

	http.Redirect(rw, r, fmt.Sprintf("/patients/%d/appointments", updated.PatientID), http.StatusSeeOther)
}

// ReschedulePage renders the reschedule form.
func (w *Web) ReschedulePage(rw http.ResponseWriter, r *http.Request) {
	appointmentID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	appointment, _, err := w.appointment(r.Context(), appointmentID)
	if err != nil {
		w.notFound(rw, r)
		return
	}
	if appointment.Status == models.StatusCancelled {
		w.renderCancelError(rw, r, appointmentID, &alertView{
			Tone:    "danger",
			Message: "This appointment has been cancelled, so it cannot be moved.",
		})
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = dateValue(appointment.AppointmentDate)
	}
	w.renderReschedule(rw, r, appointmentID, date, "", nil)
}

// RescheduleAppointment handles the reschedule form.
func (w *Web) RescheduleAppointment(rw http.ResponseWriter, r *http.Request) {
	appointmentID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.serverError(rw, r, err)
		return
	}

	newDate := r.PostFormValue("date")
	newStart := r.PostFormValue("start_time")

	replacement, err := w.svc.RescheduleAppointment(r.Context(), appointmentID, service.RescheduleRequest{
		NewDate:      newDate,
		NewStartTime: newStart,
	})
	if err != nil {
		message, code := formatError(err)
		status := statusForCode(code)
		if status == http.StatusNotFound {
			w.notFound(rw, r)
			return
		}
		w.renderReschedule(rw, r, appointmentID, newDate, newStart,
			&alertView{Tone: "danger", Message: message, Title: alertTitle(code)})
		return
	}

	http.Redirect(rw, r, fmt.Sprintf("/patients/%d/appointments", replacement.PatientID), http.StatusSeeOther)
}

// ConfirmationPage renders the confirmation shown after booking.
func (w *Web) ConfirmationPage(rw http.ResponseWriter, r *http.Request) {
	appointmentID, err := pathID(r)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	appointment, doctor, err := w.appointment(r.Context(), appointmentID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	data := confirmationPage{
		base:        w.base(r, "Your appointment is booked", "Your appointment is confirmed."),
		Appointment: *appointment,
		Doctor:      w.doctorView(doctor),
		DateLabel:   FormatDateLong(appointment.AppointmentDate),
		TimeLabel:   FormatTimeRange(appointment.StartTime, appointment.EndTime),
	}
	data.PatientID = appointment.PatientID

	w.render(rw, http.StatusOK, "confirmation", data)
}

// renderDoctor renders the doctor's day, with an optional error.
func (w *Web) renderDoctor(rw http.ResponseWriter, r *http.Request, doctorID int64, date, selectedSlot string, alert *alertView) {
	doctor, err := w.svc.DoctorByID(r.Context(), doctorID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	day := w.resolveDate(date)
	schedule, err := w.svc.Schedule(r.Context(), doctorID, dateValue(day))
	if err != nil {
		w.serverError(rw, r, err)
		return
	}

	data := doctorPage{
		base:           w.base(r, doctor.Name, "Choose a time for your appointment with "+doctor.Name),
		Doctor:         w.doctorView(doctor),
		Date:           dateValue(day),
		DateLabel:      FormatDateLong(day),
		SelectedSlot:   selectedSlot,
		LeadTimeNote:   "Appointments must be booked at least 1 hour in advance.",
		DateFormAction: fmt.Sprintf("/doctors/%d", doctorID),
		Action:         fmt.Sprintf("/doctors/%d/appointments", doctorID),
	}
	data.NavDoctors = true
	data.Error = alert

	today := clinicDate(w.now(), w.loc)
	for _, candidate := range dateRange(w.stripStart(day, today), dateStripDays) {
		data.Dates = append(data.Dates, dateChip{
			Value:    dateValue(candidate),
			Weekday:  FormatWeekday(candidate),
			DayMonth: FormatDayMonth(candidate),
			Selected: candidate.Equal(day),
			Today:    candidate.Equal(today),
		})
	}

	for _, slot := range schedule.Slots {
		data.Slots = append(data.Slots, w.slotView(slot))
		if slot.Bookable() {
			data.AvailableCount++
		}
	}
	data.HasSlots = len(schedule.Slots) > 0

	w.render(rw, http.StatusOK, "doctor", data)
}

// renderReschedule renders the reschedule form, with an optional error.
func (w *Web) renderReschedule(rw http.ResponseWriter, r *http.Request, appointmentID int64, date, selectedSlot string, alert *alertView) {
	appointment, doctor, err := w.appointment(r.Context(), appointmentID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	day := w.resolveDate(date)
	schedule, err := w.svc.Schedule(r.Context(), appointment.DoctorID, dateValue(day))
	if err != nil {
		w.serverError(rw, r, err)
		return
	}

	data := doctorPage{
		base:             w.base(r, "Move this appointment", "Choose a new time for your appointment."),
		Doctor:           w.doctorView(doctor),
		Date:             dateValue(day),
		DateLabel:        FormatDateLong(day),
		SelectedSlot:     selectedSlot,
		LeadTimeNote:     "Appointments must be booked at least 1 hour in advance.",
		DateFormAction:   fmt.Sprintf("/appointments/%d/reschedule", appointmentID),
		Action:           fmt.Sprintf("/appointments/%d/reschedule", appointmentID),
		Rescheduling:     appointment,
		ReschedulingDate: FormatDateLong(appointment.AppointmentDate),
		ReschedulingTime: FormatTimeRange(appointment.StartTime, appointment.EndTime),
	}
	data.Error = alert

	today := clinicDate(w.now(), w.loc)
	for _, candidate := range dateRange(w.stripStart(day, today), dateStripDays) {
		data.Dates = append(data.Dates, dateChip{
			Value:    dateValue(candidate),
			Weekday:  FormatWeekday(candidate),
			DayMonth: FormatDayMonth(candidate),
			Selected: candidate.Equal(day),
			Today:    candidate.Equal(today),
		})
	}

	for _, slot := range schedule.Slots {
		view := w.slotView(slot)
		// The slot the appointment already occupies is the one being moved,
		// so it must not look like an available alternative.
		if appointment.Status == models.StatusBooked &&
			appointment.AppointmentDate.Equal(day) &&
			appointment.StartTime.Equal(slot.Start) {
			view.Bookable = false
			view.Status = string(service.SlotTaken)
			view.Note = "Current"
			view.AccessibleName = view.Label + ", your current appointment"
		}
		data.Slots = append(data.Slots, view)
		if view.Bookable {
			data.AvailableCount++
		}
	}
	data.HasSlots = len(schedule.Slots) > 0

	w.render(rw, http.StatusOK, "reschedule", data)
}

// renderCancelError re-renders the cancellation page with an error.
func (w *Web) renderCancelError(rw http.ResponseWriter, r *http.Request, appointmentID int64, alert *alertView) {
	appointment, doctor, err := w.appointment(r.Context(), appointmentID)
	if err != nil {
		w.notFound(rw, r)
		return
	}

	data := cancelPage{
		base:        w.base(r, "Cancel this appointment", "Confirm you want to cancel this appointment."),
		Appointment: *appointment,
		Doctor:      w.doctorView(doctor),
		DateLabel:   FormatDateLong(appointment.AppointmentDate),
		TimeLabel:   FormatTimeRange(appointment.StartTime, appointment.EndTime),
	}
	data.Error = alert
	data.PatientID = appointment.PatientID

	w.render(rw, http.StatusConflict, "cancel", data)
}

// slotView converts a service slot into the view the template renders.
func (w *Web) slotView(slot service.DaySlot) slotView {
	view := slotView{
		Value:    slot.Start.Format("15:04"),
		Label:    FormatTime(slot.Start),
		EndLabel: FormatTime(slot.End),
		Status:   string(slot.Status),
		Bookable: slot.Bookable(),
	}

	switch slot.Status {
	case service.SlotTaken:
		view.Note = "Taken"
		view.AccessibleName = view.Label + ", already booked"
	case service.SlotTooSoon:
		view.Note = "Too soon"
		view.AccessibleName = view.Label + ", too soon to book"
	case service.SlotPast:
		view.Note = "Past"
		view.AccessibleName = view.Label + ", in the past"
	default:
		view.AccessibleName = view.Label
	}
	return view
}

// appointmentCard converts an appointment into the card the template renders.
func (w *Web) appointmentCard(appointment models.Appointment, doctor *models.Doctor) appointmentCard {
	card := appointmentCard{
		ID:             appointment.ID,
		DoctorName:     doctor.Name,
		DoctorInitials: Initials(doctor.Name),
		DateLabel:      FormatDateLong(appointment.AppointmentDate),
		TimeLabel:      FormatTimeRange(appointment.StartTime, appointment.EndTime),
		Status:         appointment.Status,
	}

	if appointment.Status == models.StatusCancelled {
		card.StatusLabel = "Cancelled"
		card.StatusTone = "danger"
		card.StatusIcon = "✕"
		if appointment.CancellationReason != nil {
			card.CancellationReason = *appointment.CancellationReason
		}
		return card
	}

	card.StatusLabel = "Booked"
	card.StatusTone = "brand"
	card.StatusIcon = "✓"
	card.IsBooked = true
	return card
}

// doctorView converts a doctor into the view the template renders.
func (w *Web) doctorView(doctor *models.Doctor) doctorView {
	return doctorView{
		ID:       doctor.ID,
		Name:     doctor.Name,
		Initials: Initials(doctor.Name),
		Hours:    FormatTimeRange(doctor.StartWorkingHour, doctor.EndWorkingHour),
	}
}

// appointment loads an appointment and its doctor.
func (w *Web) appointment(ctx context.Context, id int64) (*models.Appointment, *models.Doctor, error) {
	appointment, err := w.svc.AppointmentByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	doctor, err := w.svc.DoctorByID(ctx, appointment.DoctorID)
	if err != nil {
		return nil, nil, err
	}
	return appointment, doctor, nil
}

// base builds the fields every page shares.
func (w *Web) base(r *http.Request, title, description string) base {
	patients, err := w.svc.ListPatients(r.Context())
	if err != nil {
		w.logger.Error("list patients", "error", err.Error())
		patients = nil
	}

	current := w.currentPatientID(r, patients)
	options := make([]patientOption, 0, len(patients))
	for _, patient := range patients {
		options = append(options, patientOption{
			ID:       patient.ID,
			Name:     patient.Name,
			Selected: patient.ID == current,
		})
	}

	return base{
		Title:        title,
		Description:  description,
		SiteName:     SiteName,
		PatientID:    current,
		Patients:     options,
		CSSVariables: w.cssVariables,
	}
}

// currentPatientID works out which patient the site is acting as.
func (w *Web) currentPatientID(r *http.Request, patients []models.Patient) int64 {
	if cookie, err := r.Cookie(patientCookie); err == nil {
		if id, err := strconv.ParseInt(cookie.Value, 10, 64); err == nil {
			for _, patient := range patients {
				if patient.ID == id {
					return id
				}
			}
		}
	}
	if len(patients) > 0 {
		return patients[0].ID
	}
	return 1
}

// rememberPatient stores the chosen patient in a cookie.
func (w *Web) rememberPatient(rw http.ResponseWriter, patientID int64) {
	http.SetCookie(rw, &http.Cookie{
		Name:     patientCookie,
		Value:    strconv.FormatInt(patientID, 10),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// resolveDate turns a date string into a calendar date, falling back to
// today when it is missing or unreadable.
func (w *Web) resolveDate(value string) time.Time {
	today := clinicDate(w.now(), w.loc)
	if value == "" {
		return today
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return today
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
}

// stripStart returns the earlier of the selected day and today, so that the
// strip always contains the day being looked at.
func (w *Web) stripStart(selected, today time.Time) time.Time {
	if selected.Before(today) {
		return today
	}
	return selected
}

// now returns the current instant. It is a method so that tests can be
// written against a fixed clock if the site ever grows one.
func (w *Web) now() time.Time {
	return time.Now()
}

// pathID reads a positive integer id from the request path.
func pathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("web: id %q must be a positive integer", raw)
	}
	return id, nil
}

// alertTitle gives the short headline for an error alert.
func alertTitle(code string) string {
	switch code {
	case "SLOT_TAKEN":
		return "That time is no longer free"
	case "TOO_SOON":
		return "Too soon to book"
	case "OUTSIDE_WORKING_HOURS":
		return "Outside the doctor's hours"
	case "APPOINTMENT_ALREADY_CANCELLED":
		return "Already cancelled"
	case "APPOINTMENT_IN_PAST":
		return "That appointment has started"
	default:
		return ""
	}
}
