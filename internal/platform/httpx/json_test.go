package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

type body struct {
	To    string `json:"to"`
	Count int    `json:"count"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int // 0: no error
		wantCode    string
		wantDetail  string // a part of the detail
	}{
		{"valid", "application/json", `{"to":"a","count":2}`, 0, "", ""},
		{"valid with a charset", "application/json; charset=utf-8", `{"to":"a"}`, 0, "", ""},
		{"no content type", "", `{"to":"a"}`, 415, "unsupported_media_type", "application/json"},
		{"form content type", "application/x-www-form-urlencoded", `to=a`, 415, "unsupported_media_type", ""},
		{"too large", "application/json", `{"to":"` + strings.Repeat("a", 100) + `"}`, 413, "body_too_large", "64 bytes"},
		{"empty", "application/json", ``, 400, "invalid_json", ""},
		{"not JSON", "application/json", `{"to":}`, 400, "invalid_json", ""},
		{"two values", "application/json", `{"to":"a"} {"to":"b"}`, 400, "invalid_json", ""},
		{"duplicate member", "application/json", `{"to":"a","to":"b"}`, 400, "invalid_json", ""},
		{"unknown member", "application/json", `{"to":"a","cc":"b"}`, 400, "unknown_field", `"cc"`},
		// encoding/json v1 would match "TO" to the field "to"; the contract's names are case-sensitive.
		{"member in the wrong case", "application/json", `{"TO":"a"}`, 400, "unknown_field", `"TO"`},
		{"wrong type", "application/json", `{"to":"a","count":"two"}`, 400, "invalid_json", "/count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}
			var dst body
			err := DecodeJSON(httptest.NewRecorder(), r, 64, &dst)

			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("DecodeJSON = %v, want nil", err)
				}
				return
			}
			p, ok := errors.AsType[*Problem](err)
			if !ok {
				t.Fatalf("DecodeJSON = %v (%T), want a *Problem", err, err)
			}
			if p.Status != tt.wantStatus || p.Code != tt.wantCode || !strings.Contains(p.Detail, tt.wantDetail) {
				t.Errorf("problem = %d %s %q, want %d %s containing %q",
					p.Status, p.Code, p.Detail, tt.wantStatus, tt.wantCode, tt.wantDetail)
			}
		})
	}
}

// TestDecodeJSONWhenTheBodyCannotBeRead covers a connection that breaks mid-body: the request is answered as
// invalid, never as a server error.
func TestDecodeJSONWhenTheBodyCannotBeRead(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", iotest.ErrReader(errors.New("connection reset")))
	r.Header.Set("Content-Type", "application/json")
	var dst body
	err := DecodeJSON(httptest.NewRecorder(), r, 64, &dst)
	if p, ok := errors.AsType[*Problem](err); !ok || p.Status != http.StatusBadRequest || p.Code != "invalid_json" {
		t.Errorf("DecodeJSON = %v, want a 400 invalid_json problem", err)
	}
}
