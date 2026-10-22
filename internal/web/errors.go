package web

import (
	"net/http"
)

// notFound renders the "not found" page.
//
// A patient who follows a stale link should get a page, not a bare status
// code, and the page should offer a way back into the site.
func (w *Web) notFound(rw http.ResponseWriter, r *http.Request) {
	data := doctorsPage{base: w.base(r, "Page not found", "That page does not exist.")}
	data.Notice = &alertView{
		Tone:    "info",
		Title:   "We could not find that page",
		Message: "The link may be out of date. Head back to the doctors and pick up where you left off.",
	}
	w.render(rw, http.StatusNotFound, "doctors", data)
}

// serverError records an unexpected failure and renders a plain error page.
//
// The message is deliberately vague: an internal error must never leak a
// database message or a stack trace to a patient.
func (w *Web) serverError(rw http.ResponseWriter, r *http.Request, err error) {
	w.logger.Error("request failed",
		"method", r.Method,
		"path", r.URL.Path,
		"error", err.Error(),
	)

	data := doctorsPage{base: w.base(r, "Something went wrong", "Something went wrong.")}
	data.Error = &alertView{
		Tone:    "danger",
		Title:   "Something went wrong on our side",
		Message: "We could not load that page just now. Please try again in a moment, or call the clinic and we will sort it out.",
	}
	w.render(rw, http.StatusInternalServerError, "doctors", data)
}
