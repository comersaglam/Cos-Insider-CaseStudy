package contract_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/comersaglam/Cos-Insider-CaseStudy/api/contract"
)

// TestTheContractIsValid: api/openapi.yaml parses, is valid OpenAPI 3.1, and its examples match their schemas.
func TestTheContractIsValid(t *testing.T) {
	ops, err := contract.Operations()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ops, "GET /v1/notifications/{id}") {
		t.Errorf("operations = %v, want GET /v1/notifications/{id} among them", ops)
	}
}

func TestValidate(t *testing.T) {
	problem := http.Header{"Content-Type": {"application/problem+json"}}
	notFound := `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found","detail":"no such notification","correlationId":"c"}`
	for _, tt := range []struct {
		name, method, path string
		status             int
		header             http.Header
		body               string
		wantErr            bool
	}{
		{"a documented answer", http.MethodGet, "/v1/notifications/01a209e9-78c5-7b0e-acd2-ccaacbc2ba85", 404, problem, notFound, false},
		{"a status the operation does not document", http.MethodGet, "/v1/notifications/01a209e9-78c5-7b0e-acd2-ccaacbc2ba85", http.StatusTeapot, problem, notFound, true},
		{"a body against its schema", http.MethodGet, "/v1/notifications/01a209e9-78c5-7b0e-acd2-ccaacbc2ba85", 404, problem, `{"status":"404"}`, true},
		{"a path the contract does not describe", http.MethodGet, "/v1/unknown", 404, problem, notFound, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			if err := contract.Validate(r, tt.status, tt.header, []byte(tt.body)); (err != nil) != tt.wantErr {
				t.Errorf("Validate = %v, want an error: %v", err, tt.wantErr)
			}
		})
	}
}
