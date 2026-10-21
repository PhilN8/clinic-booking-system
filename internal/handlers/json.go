package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
)

// maxBodyBytes caps how much of a request body is read.
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON reads a JSON request body into dst.
//
// Unknown fields are rejected rather than ignored: a client that sends
// "start-time" instead of "start_time" gets told, instead of silently
// booking a slot at the wrong time.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType := strings.TrimSpace(strings.Split(contentType, ";")[0])
		if !strings.EqualFold(mediaType, "application/json") {
			return httperr.InvalidJSON("Content-Type must be application/json")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return httperr.InvalidJSON(fmt.Sprintf("request body is not valid JSON: %v", err))
	}
	// Reject trailing content so that "{} {}" is not silently accepted.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return httperr.InvalidJSON("request body must contain exactly one JSON object")
	}
	return nil
}

// decodeOptionalJSON reads a JSON request body into dst, tolerating an empty
// body. It is used by the cancel endpoint, whose reason is optional.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		if err == io.EOF {
			return nil
		}
		return httperr.InvalidJSON(fmt.Sprintf("request body is not valid JSON: %v", err))
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return httperr.InvalidJSON("request body must contain exactly one JSON object")
	}
	return nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so there is nothing to do but
		// record it. The body is truncated at this point.
		return
	}
}

// pathID reads a positive integer id from the request path.
func pathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, httperr.InvalidRequest(fmt.Sprintf("id %q must be a positive integer", raw))
	}
	return id, nil
}

// queryBool reads an optional boolean query parameter.
func queryBool(r *http.Request, name string) bool {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}
