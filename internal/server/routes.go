// Package server builds the HTTP router.
//
// It holds the route table, which is the single source of truth for the JSON
// API: the same table drives the router and the drift test that checks the
// OpenAPI document against it. A route cannot be added without either
// appearing in the document or failing the build.
package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/PhilN8/clinic-booking-system/internal/handlers"
	"github.com/PhilN8/clinic-booking-system/internal/web"
)

//go:embed templates/*.html
var templateFS embed.FS

// Route describes one JSON API endpoint.
type Route struct {
	// Method is the HTTP method.
	Method string
	// Path is the path in OpenAPI form, for example "/doctors/{id}".
	Path string
	// Summary is the one-line description of what the endpoint does.
	Summary string
	// OperationID is the OpenAPI operationId. It must be unique.
	OperationID string
	// Bind returns the handler for the route. It is a function so that one
	// table can hold handlers from two packages.
	Bind func(*handlers.API, *web.Web) http.HandlerFunc
}

// apiRoutes is every endpoint the JSON API exposes.
//
// The three routes with a page handler serve HTML to a browser and JSON to
// everything else. They are API routes as well, so they belong here and in
// the OpenAPI document.
var apiRoutes = []Route{
	{
		Method: "GET", Path: "/doctors",
		Summary:     "List the doctors",
		OperationID: "listDoctors",
		Bind: func(a *handlers.API, s *web.Web) http.HandlerFunc {
			return web.Negotiate(a.ListDoctors, s.DoctorsPage)
		},
	},
	{
		Method: "GET", Path: "/doctors/{id}",
		Summary:     "Get one doctor",
		OperationID: "getDoctor",
		Bind: func(a *handlers.API, s *web.Web) http.HandlerFunc {
			return web.Negotiate(a.GetDoctor, s.DoctorPage)
		},
	},
	{
		Method: "GET", Path: "/doctors/{id}/availability",
		Summary:     "List the free slots for a doctor on a date",
		OperationID: "getDoctorAvailability",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.Availability
		},
	},
	{
		Method: "GET", Path: "/patients/{id}",
		Summary:     "Get one patient",
		OperationID: "getPatient",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.GetPatient
		},
	},
	{
		Method: "GET", Path: "/patients/{id}/appointments",
		Summary:     "List a patient's appointments",
		OperationID: "listPatientAppointments",
		Bind: func(a *handlers.API, s *web.Web) http.HandlerFunc {
			return web.Negotiate(a.PatientAppointments, s.AppointmentsPage)
		},
	},
	{
		Method: "POST", Path: "/appointments",
		Summary:     "Book an appointment",
		OperationID: "bookAppointment",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.BookAppointment
		},
	},
	{
		Method: "PATCH", Path: "/appointments/{id}/cancel",
		Summary:     "Cancel an appointment",
		OperationID: "cancelAppointment",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.CancelAppointment
		},
	},
	{
		Method: "PATCH", Path: "/appointments/{id}/reschedule",
		Summary:     "Move an appointment to another slot",
		OperationID: "rescheduleAppointment",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.RescheduleAppointment
		},
	},
	{
		Method: "GET", Path: "/healthz",
		Summary:     "Check that the service is healthy",
		OperationID: "healthCheck",
		Bind: func(a *handlers.API, _ *web.Web) http.HandlerFunc {
			return a.Healthz
		},
	},
}

// APIRoutes returns the JSON API route table.
func APIRoutes() []Route {
	out := make([]Route, len(apiRoutes))
	copy(out, apiRoutes)
	return out
}

// New builds the router.
//
// spec serves the OpenAPI document. It is passed in rather than imported so
// that this package does not depend on the document it is checked against.
func New(api *handlers.API, site *web.Web, spec http.Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// The API under /api, where it is always JSON and never negotiates.
	apiMux := http.NewServeMux()
	for _, route := range apiRoutes {
		apiMux.HandleFunc(route.Method+" "+route.Path, route.Bind(api, site))
	}
	mux.Handle("/api/", http.StripPrefix("/api", apiMux))

	// The same routes at the root, negotiating between JSON and a page.
	for _, route := range apiRoutes {
		mux.HandleFunc(route.Method+" "+route.Path, route.Bind(api, site))
	}

	// Site only.
	mux.HandleFunc("GET /{$}", site.LandingPage)
	mux.HandleFunc("POST /doctors/{id}/appointments", site.BookAppointment)
	mux.HandleFunc("GET /appointments/{id}/cancel", site.CancelPage)
	mux.HandleFunc("POST /appointments/{id}/cancel", site.CancelAppointment)
	mux.HandleFunc("GET /appointments/{id}/reschedule", site.ReschedulePage)
	mux.HandleFunc("POST /appointments/{id}/reschedule", site.RescheduleAppointment)
	mux.HandleFunc("GET /appointments/{id}/confirmation", site.ConfirmationPage)

	// Documentation.
	mux.Handle("GET /openapi.json", spec)
	mux.HandleFunc("GET /docs", docsPage(apiRoutes, site.CSSVariables()))

	// Static assets.
	mux.Handle("/assets/", site.Assets())

	return api.Middleware(mux)
}

// docsPage renders the API reference.
//
// The endpoint table is rendered on the server so the page works without
// JavaScript. When the Swagger UI assets load, they take over the same
// section and the table is hidden.
func docsPage(routes []Route, cssVariables template.CSS) http.HandlerFunc {
	tmpl := template.Must(template.New("docs").ParseFS(templateFS, "templates/docs.html"))

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"Routes": routes, "CSSVariables": cssVariables}
		if err := tmpl.ExecuteTemplate(w, "docs.html", data); err != nil {
			http.Error(w, "could not render the API reference", http.StatusInternalServerError)
		}
	}
}
