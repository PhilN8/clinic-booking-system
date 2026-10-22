package openapi_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/PhilN8/clinic-booking-system/internal/httperr"
	"github.com/PhilN8/clinic-booking-system/internal/openapi"
	"github.com/PhilN8/clinic-booking-system/internal/server"
)

// document is the parts of an OpenAPI document this test inspects.
type document struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title       string `json:"title"`
		Version     string `json:"version"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Contact     struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"contact"`
		License struct {
			Name string `json:"name"`
		} `json:"license"`
	} `json:"info"`
	Servers []struct {
		URL         string `json:"url"`
		Description string `json:"description"`
	} `json:"servers"`
	Tags []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tags"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas    map[string]json.RawMessage `json:"schemas"`
		Parameters map[string]json.RawMessage `json:"parameters"`
		Responses  map[string]json.RawMessage `json:"responses"`
	} `json:"components"`
}

// operation is one path and method.
type operation struct {
	Tags        []string          `json:"tags"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	OperationID string            `json:"operationId"`
	Parameters  []json.RawMessage `json:"parameters"`
	Responses   map[string]json.RawMessage
	RequestBody *json.RawMessage `json:"requestBody"`
}

func load(t *testing.T) document {
	t.Helper()

	var doc document
	if err := json.Unmarshal(openapi.Spec(), &doc); err != nil {
		t.Fatalf("the OpenAPI document does not parse: %v", err)
	}
	return doc
}

// TestDocumentIsValid checks the parts of the document that a linter would.
func TestDocumentIsValid(t *testing.T) {
	doc := load(t)

	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Errorf("openapi = %q, want a 3.1 document", doc.OpenAPI)
	}
	if doc.Info.Title == "" || doc.Info.Version == "" {
		t.Error("the info block needs a title and a version")
	}
	if doc.Info.Summary == "" {
		t.Error("the info block needs a summary")
	}
	if doc.Info.Description == "" {
		t.Error("the info block needs a description")
	}
	if doc.Info.Contact.Name == "" || doc.Info.Contact.URL == "" {
		t.Error("the info block needs contact details")
	}
	if doc.Info.License.Name == "" {
		t.Error("the info block needs a licence")
	}
	if len(doc.Servers) == 0 {
		t.Error("the document needs at least one server")
	}
	if len(doc.Tags) == 0 {
		t.Error("the document needs tags")
	}
	for _, tag := range doc.Tags {
		if tag.Description == "" {
			t.Errorf("the tag %q has no description", tag.Name)
		}
	}
}

// TestEveryRouteIsDocumented is the drift test. The route table drives the
// router, so a route that is not in the document fails here.
func TestEveryRouteIsDocumented(t *testing.T) {
	doc := load(t)
	routes := server.APIRoutes()

	for _, route := range routes {
		methods, ok := doc.Paths[route.Path]
		if !ok {
			t.Errorf("the path %q is routed but not documented", route.Path)
			continue
		}

		operation, ok := methods[strings.ToLower(route.Method)]
		if !ok {
			t.Errorf("the path %q is routed for %s but that method is not documented",
				route.Path, route.Method)
			continue
		}

		if operation.OperationID != route.OperationID {
			t.Errorf("the path %q has operationId %q, want %q",
				route.Path, operation.OperationID, route.OperationID)
		}
		if operation.Summary == "" {
			t.Errorf("the operation %q has no summary", route.OperationID)
		}
		if operation.Description == "" {
			t.Errorf("the operation %q has no description", route.OperationID)
		}
		if len(operation.Tags) == 0 {
			t.Errorf("the operation %q has no tags", route.OperationID)
		}
		if len(operation.Responses) == 0 {
			t.Errorf("the operation %q documents no responses", route.OperationID)
		}
	}
}

// TestNoUndocumentedPaths catches the other direction: a path in the
// document that nothing serves.
func TestNoUndocumentedPaths(t *testing.T) {
	doc := load(t)

	routed := make(map[string]bool)
	for _, route := range server.APIRoutes() {
		routed[route.Path] = true
	}

	for path := range doc.Paths {
		if !routed[path] {
			t.Errorf("the path %q is documented but nothing routes to it", path)
		}
	}
}

// TestOperationIdsAreUnique catches a copy and paste in the document.
func TestOperationIdsAreUnique(t *testing.T) {
	doc := load(t)

	seen := make(map[string]string)
	for path, methods := range doc.Paths {
		for method, operation := range methods {
			id := operation.OperationID
			if id == "" {
				t.Errorf("%s %s has no operationId", method, path)
				continue
			}
			if previous, exists := seen[id]; exists {
				t.Errorf("the operationId %q is used by both %s and %s", id, previous, path)
				continue
			}
			seen[id] = method + " " + path
		}
	}
}

// TestEveryErrorCodeIsDocumented keeps the error vocabulary in step with the
// code, so a new error code cannot go undocumented.
func TestEveryErrorCodeIsDocumented(t *testing.T) {
	doc := load(t)

	codes := []string{
		httperr.CodeInvalidJSON,
		httperr.CodeInvalidRequest,
		httperr.CodeInvalidDate,
		httperr.CodeInvalidTime,
		httperr.CodeDoctorNotFound,
		httperr.CodePatientNotFound,
		httperr.CodeAppointmentNotFound,
		httperr.CodeSlotNotAligned,
		httperr.CodeOutsideWorkingHours,
		httperr.CodeTooSoon,
		httperr.CodeAppointmentInPast,
		httperr.CodeSlotTaken,
		httperr.CodeAppointmentCancelled,
		httperr.CodeInternalError,
	}

	documented := string(doc.Components.Schemas["Error"])
	for _, code := range codes {
		if !strings.Contains(documented, `"`+code+`"`) {
			t.Errorf("the error code %q is not in the documented vocabulary", code)
		}
	}
}

// TestEveryRefResolves catches a reference to a component that does not
// exist, which would break every tool that reads the document.
func TestEveryRefResolves(t *testing.T) {
	doc := load(t)

	components := map[string]bool{}
	for name := range doc.Components.Schemas {
		components["#/components/schemas/"+name] = true
	}
	for name := range doc.Components.Parameters {
		components["#/components/parameters/"+name] = true
	}
	for name := range doc.Components.Responses {
		components["#/components/responses/"+name] = true
	}

	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "$ref" {
					ref, ok := child.(string)
					if !ok {
						t.Errorf("a $ref is not a string: %v", child)
						continue
					}
					if !components[ref] {
						t.Errorf("the reference %q does not resolve to a component", ref)
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}

	var generic any
	if err := json.Unmarshal(openapi.Spec(), &generic); err != nil {
		t.Fatalf("parse: %v", err)
	}
	walk(generic)
}

// TestComponentsAreUsed catches a schema that was written and then never
// referenced.
func TestComponentsAreUsed(t *testing.T) {
	doc := load(t)

	body := string(openapi.Spec())
	for name := range doc.Components.Schemas {
		if name == "Error" || name == "Health" {
			continue
		}
		if !strings.Contains(body, `"#/components/schemas/`+name+`"`) {
			t.Errorf("the schema %q is never referenced", name)
		}
	}
	for name := range doc.Components.Responses {
		if !strings.Contains(body, `"#/components/responses/`+name+`"`) {
			t.Errorf("the response %q is never referenced", name)
		}
	}
	for name := range doc.Components.Parameters {
		if !strings.Contains(body, `"#/components/parameters/`+name+`"`) {
			t.Errorf("the parameter %q is never referenced", name)
		}
	}
}

// TestSpecFileMatchesTheEmbeddedCopy catches an edited file that was not
// rebuilt into the binary.
func TestSpecFileMatchesTheEmbeddedCopy(t *testing.T) {
	onDisk, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}

	if string(onDisk) != string(openapi.Spec()) {
		t.Error("the embedded document differs from openapi.json")
	}
}
