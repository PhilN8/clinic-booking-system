// Package web is the patient facing site.
//
// The pages are rendered on the server with html/template and styled with a
// small stylesheet that references the design tokens. The site works without
// JavaScript: forms submit, the page reloads, and every interaction still
// functions. JavaScript only removes a click.
//
// The design decisions the site implements live in design.md. The colours
// come from design/tokens.json, so the stylesheet cannot drift away from the
// palette the test suite verifies.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/PhilN8/clinic-booking-system/design"
	"github.com/PhilN8/clinic-booking-system/internal/models"
	"github.com/PhilN8/clinic-booking-system/internal/service"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// SiteName is the name shown in the header and the footer.
const SiteName = "Nairobi Clinic"

// pages are the templates that can be rendered. Each is parsed with the
// layout into its own template set, so every page can define "content"
// without colliding with the others.
var pages = []string{"landing", "doctors", "doctor", "appointments", "cancel", "reschedule", "confirmation"}

// Service is the part of the service layer the site depends on.
//
// It is declared here so the site can be tested against a stub.
type Service interface {
	ListDoctors(ctx context.Context) ([]models.Doctor, error)
	DoctorByID(ctx context.Context, id int64) (*models.Doctor, error)
	ListPatients(ctx context.Context) ([]models.Patient, error)
	PatientByID(ctx context.Context, id int64) (*models.Patient, error)
	Schedule(ctx context.Context, doctorID int64, date string) (*service.Schedule, error)
	BookAppointment(ctx context.Context, req service.BookRequest) (*models.Appointment, error)
	CancelAppointment(ctx context.Context, id int64, reason *string) (*models.Appointment, error)
	RescheduleAppointment(ctx context.Context, id int64, req service.RescheduleRequest) (*models.Appointment, error)
	PatientAppointments(ctx context.Context, patientID int64, includeCancelled bool) ([]models.Appointment, error)
	AppointmentByID(ctx context.Context, id int64) (*models.Appointment, error)
}

// Web serves the site.
type Web struct {
	svc    Service
	loc    *time.Location
	logger *slog.Logger
	tokens design.Tokens
	// cssVariables are the palette as CSS custom properties, generated from
	// the token file so the stylesheet and the tokens cannot disagree.
	cssVariables template.CSS
	templates    map[string]*template.Template
}

// New builds the site.
func New(svc Service, loc *time.Location, logger *slog.Logger, tokens design.Tokens) (*Web, error) {
	variables, err := design.CSSVariables(tokens)
	if err != nil {
		return nil, fmt.Errorf("web: build css variables: %w", err)
	}

	templates, err := loadTemplates(templateFS)
	if err != nil {
		return nil, err
	}

	return &Web{
		svc:          svc,
		loc:          loc,
		logger:       logger,
		tokens:       tokens,
		cssVariables: template.CSS(variables),
		templates:    templates,
	}, nil
}

// loadTemplates parses the layout and each page into its own template set.
func loadTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template, len(pages))

	for _, page := range pages {
		tmpl, err := template.New(page).Funcs(templateFuncs).ParseFS(fsys,
			"templates/layout.html",
			"templates/partials.html",
			"templates/"+page+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse template %s: %w", page, err)
		}
		templates[page] = tmpl
	}

	return templates, nil
}

// templateFuncs are the formatting helpers the templates call.
var templateFuncs = template.FuncMap{
	"time":      FormatTime,
	"timeRange": FormatTimeRange,
	"dateLong":  FormatDateLong,
	"dateShort": FormatDateShort,
	"weekday":   FormatWeekday,
	"dayMonth":  FormatDayMonth,
	"initials":  Initials,
}

// Middleware wraps the site in request logging and panic recovery.
//
// The site is a separate set of routes from the API, so it gets its own
// middleware rather than sharing the API's.
func (w *Web) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				w.logger.Error("panic serving page",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				http.Error(rw, "Something went wrong", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// Assets serves the stylesheet and the script.
func (w *Web) Assets() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		w.logger.Error("static assets unavailable", "error", err.Error())
		return http.NotFoundHandler()
	}
	// The prefix has no trailing slash, so the file server is left with a
	// path that starts with a slash, which is what it expects.
	return http.StripPrefix("/assets", http.FileServerFS(sub))
}

// render writes a page.
func (w *Web) render(rw http.ResponseWriter, status int, page string, data any) {
	tmpl, ok := w.templates[page]
	if !ok {
		w.logger.Error("unknown template", "page", page)
		http.Error(rw, "page not found", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Referrer-Policy", "same-origin")
	rw.WriteHeader(status)

	if err := tmpl.ExecuteTemplate(rw, "layout.html", data); err != nil {
		// The status line is already sent, so there is nothing to do but
		// record it.
		w.logger.Error("render page", "page", page, "error", err.Error())
	}
}

// wantsHTML reports whether the client asked for an HTML page.
//
// A browser sends "text/html,...". curl sends "*/*", which means "anything",
// and must keep getting the JSON API so that the documented API examples
// still work unchanged.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	if !strings.Contains(accept, "text/html") {
		return false
	}
	return true
}

// Negotiate returns a handler that serves the API to API clients and the
// page to browsers.
//
// Three paths exist as both a JSON endpoint and an HTML page. Rather than
// moving either one, the same URL serves whichever the client asked for.
func Negotiate(api http.HandlerFunc, page http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if wantsHTML(r) {
			page(rw, r)
			return
		}
		api(rw, r)
	}
}
