package httpx

import "net/http"

// Mux is what a module mounts its routes on: an *http.ServeMux, or a wrapper around one that records them, as the
// router does for its contract test.
type Mux interface {
	Handle(pattern string, handler http.Handler)
}
