// Package openapi serves the OpenAPI description of the JSON API.
//
// The document is embedded in the binary so the deployed service always
// carries the description of the API it is actually serving.
package openapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var spec []byte

// Spec returns the OpenAPI document.
func Spec() []byte { return spec }

// Handler serves the OpenAPI document as JSON.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(spec)
	})
}
