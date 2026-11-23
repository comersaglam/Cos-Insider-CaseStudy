// Package contract checks the server against its contract, api/openapi.yaml, in tests (ADR 0029): the contract is
// valid OpenAPI, and an answer has a status and a body the contract documents for its operation. Only tests import
// it.
package contract

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/comersaglam/Cos-Insider-CaseStudy/api"
)

var (
	loadOnce sync.Once
	doc      *openapi3.T
	router   routers.Router
	errLoad  error
)

// load parses and validates the contract once per test binary.
func load() (*openapi3.T, routers.Router, error) {
	loadOnce.Do(func() {
		loader := openapi3.NewLoader()
		d, err := loader.LoadFromData(api.Spec)
		if err != nil {
			errLoad = fmt.Errorf("parse the contract: %w", err)
			return
		}
		if err := d.Validate(loader.Context, openapi3.EnableExamplesValidation()); err != nil {
			errLoad = fmt.Errorf("the contract is not valid: %w", err)
			return
		}
		// Tests serve from any host and port, which the contract's servers would not match.
		d.Servers = nil
		r, err := legacy.NewRouter(d)
		if err != nil {
			errLoad = fmt.Errorf("route the contract: %w", err)
			return
		}
		doc, router = d, r
	})
	return doc, router, errLoad
}

// Operations returns every operation the contract describes, sorted, each as an http.ServeMux pattern such as
// "GET /v1/notifications/{id}".
func Operations() ([]string, error) {
	d, _, err := load()
	if err != nil {
		return nil, err
	}
	var ops []string
	for path, item := range d.Paths.Map() {
		for method := range item.Operations() {
			ops = append(ops, method+" "+path)
		}
	}
	sort.Strings(ops)
	return ops, nil
}

// Validate returns nil when status, header and body, answered to r, are an answer the contract documents for r's
// operation. A request no operation matches returns nil: that every route is documented is checked against the
// router, in httpapi's tests.
func Validate(r *http.Request, status int, header http.Header, body []byte) error {
	_, rt, err := load()
	if err != nil {
		return err
	}
	route, params, err := rt.FindRoute(r)
	if _, unrouted := errors.AsType[*routers.RouteError](err); unrouted {
		return nil
	}
	if err != nil {
		return fmt.Errorf("route %s %s: %w", r.Method, r.URL.Path, err)
	}
	return openapi3filter.ValidateResponse(r.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route},
		Status:                 status,
		Header:                 header,
		Body:                   io.NopCloser(bytes.NewReader(body)),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	})
}

// Check fails t unless the answer is one the contract documents (Validate).
func Check(t testing.TB, r *http.Request, status int, header http.Header, body []byte) {
	t.Helper()
	if err := Validate(r, status, header, body); err != nil {
		t.Errorf("%s %s answered %d against the contract:\n%v\nbody: %.500s", r.Method, r.URL.Path, status, err, body)
	}
}
