// Package httperr defines the single error type used across the service and
// transport layers, together with the JSON envelope the API returns for every
// failure.
//
// The service layer returns *Error values so that a rule can be expressed
// once and rendered identically no matter which endpoint hit it. Handlers
// never build error bodies by hand.
package httperr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Error codes returned by the API.
const (
	CodeInvalidJSON          = "INVALID_JSON"
	CodeInvalidRequest       = "INVALID_REQUEST"
	CodeInvalidDate          = "INVALID_DATE"
	CodeInvalidTime          = "INVALID_TIME"
	CodeDoctorNotFound       = "DOCTOR_NOT_FOUND"
	CodePatientNotFound      = "PATIENT_NOT_FOUND"
	CodeAppointmentNotFound  = "APPOINTMENT_NOT_FOUND"
	CodeSlotNotAligned       = "SLOT_NOT_ALIGNED"
	CodeOutsideWorkingHours  = "OUTSIDE_WORKING_HOURS"
	CodeTooSoon              = "TOO_SOON"
	CodeAppointmentInPast    = "APPOINTMENT_IN_PAST"
	CodeSlotTaken            = "SLOT_TAKEN"
	CodeAppointmentCancelled = "APPOINTMENT_ALREADY_CANCELLED"
	CodeInternalError        = "INTERNAL_ERROR"
)

// Error is an API error with an HTTP status, a stable machine-readable code
// and a human-readable message.
type Error struct {
	Status  int
	Code    string
	Message string
	// Details is optional structured context for the client.
	Details map[string]any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// WithDetails attaches structured context and returns the error for chaining.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

// WithCause attaches an underlying error for logging.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

// New builds an error with an explicit status.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// InvalidJSON reports a body that could not be read as a JSON object.
func InvalidJSON(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidJSON, message)
}

// InvalidRequest reports a well-formed request that is missing or has an
// invalid field.
func InvalidRequest(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidRequest, message)
}

// InvalidDate reports an unparseable calendar date.
func InvalidDate(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidDate, message)
}

// InvalidTime reports an unparseable time of day.
func InvalidTime(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidTime, message)
}

// DoctorNotFound reports a doctor id that does not exist.
func DoctorNotFound(id int64) *Error {
	return New(http.StatusNotFound, CodeDoctorNotFound, fmt.Sprintf("doctor %d does not exist", id))
}

// PatientNotFound reports a patient id that does not exist.
func PatientNotFound(id int64) *Error {
	return New(http.StatusNotFound, CodePatientNotFound, fmt.Sprintf("patient %d does not exist", id))
}

// AppointmentNotFound reports an appointment id that does not exist.
func AppointmentNotFound(id int64) *Error {
	return New(http.StatusNotFound, CodeAppointmentNotFound, fmt.Sprintf("appointment %d does not exist", id))
}

// SlotNotAligned reports a start time that is not on the doctor's 30-minute grid.
func SlotNotAligned(message string) *Error {
	return New(http.StatusUnprocessableEntity, CodeSlotNotAligned, message)
}

// OutsideWorkingHours reports a start time outside the doctor's working window.
func OutsideWorkingHours(message string) *Error {
	return New(http.StatusUnprocessableEntity, CodeOutsideWorkingHours, message)
}

// TooSoon reports a booking that is closer than the minimum lead time.
func TooSoon(message string) *Error {
	return New(http.StatusUnprocessableEntity, CodeTooSoon, message)
}

// AppointmentInPast reports an operation on an appointment that has started.
func AppointmentInPast(message string) *Error {
	return New(http.StatusUnprocessableEntity, CodeAppointmentInPast, message)
}

// SlotTaken reports that the requested slot is already booked.
func SlotTaken(message string) *Error {
	return New(http.StatusConflict, CodeSlotTaken, message)
}

// AppointmentAlreadyCancelled reports an operation on a cancelled appointment.
func AppointmentAlreadyCancelled(id int64) *Error {
	return New(http.StatusConflict, CodeAppointmentCancelled,
		fmt.Sprintf("appointment %d is already cancelled", id))
}

// Internal reports an unexpected server-side failure. The message is
// deliberately generic; the underlying cause is logged, not returned.
func Internal() *Error {
	return New(http.StatusInternalServerError, CodeInternalError, "an unexpected error occurred")
}

// Body is the JSON body returned for every non-2xx response.
type Body struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the error member of the response envelope.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Write renders err as the standard error envelope. Any error that is not an
// *Error is reported as a generic 500 so that internal details never leak.
func Write(w http.ResponseWriter, err error) {
	typed := &Error{Status: http.StatusInternalServerError, Code: CodeInternalError, Message: "an unexpected error occurred"}
	var known *Error
	if errors.As(err, &known) {
		typed = known
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(typed.Status)
	_ = json.NewEncoder(w).Encode(Body{Error: ErrorBody{
		Code:    typed.Code,
		Message: typed.Message,
		Details: typed.Details,
	}})
}
