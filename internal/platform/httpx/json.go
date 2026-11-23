package httpx

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// DecodeJSON reads the request body into dst, strictly: the body must be declared as application/json, be at
// most maxBytes long, hold exactly one JSON value, and use only the member names dst declares, in their exact
// case. Any failure is returned as a *Problem that says what was wrong.
//
// It uses encoding/json/v2, which, unlike v1, matches member names case-sensitively and refuses duplicate
// members and invalid UTF-8.
func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return NewProblem(http.StatusUnsupportedMediaType, "unsupported_media_type",
			"the request body must be sent as application/json")
	}

	err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxBytes), dst, json.RejectUnknownMembers(true))
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return NewProblem(http.StatusRequestEntityTooLarge, "body_too_large",
			fmt.Sprintf("the request body must be at most %d bytes", maxBytes))
	}
	if serr, ok := errors.AsType[*json.SemanticError](err); ok {
		if errors.Is(serr.Err, json.ErrUnknownName) {
			return NewProblem(http.StatusBadRequest, "unknown_field",
				fmt.Sprintf("the member %q is not part of the request", serr.JSONPointer.LastToken()))
		}
		return NewProblem(http.StatusBadRequest, "invalid_json",
			fmt.Sprintf("the value at %q has the wrong type", serr.JSONPointer))
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return NewProblem(http.StatusBadRequest, "invalid_json", "the request body is empty or cut short")
	}
	if serr, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		return NewProblem(http.StatusBadRequest, "invalid_json",
			fmt.Sprintf("the request body is not valid JSON at byte %d", serr.ByteOffset))
	}
	return NewProblem(http.StatusBadRequest, "invalid_json", "the request body is not valid JSON")
}
