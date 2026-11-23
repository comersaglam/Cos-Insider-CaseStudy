package httpx

import "net/http"

// ProblemFallback answers the requests mux has no route for as problems, so they share the error envelope of every
// other error: 404 not_found for an unknown path, and 405 method_not_allowed, with its Allow header, for a known
// path asked with another method. ServeMux alone answers both in plain text.
func ProblemFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r) // ServeHTTP, not h: it also sets the request's path values
			return
		}

		// The mux has no route. Let its own handler decide between 404 and 405 and compute Allow, without
		// writing to the client.
		seen := &captured{header: http.Header{}}
		h.ServeHTTP(seen, r)
		switch seen.status {
		case http.StatusNotFound:
			WriteProblem(w, r, NewProblem(http.StatusNotFound, "not_found", "nothing is served at this path"))
		case http.StatusMethodNotAllowed:
			w.Header().Set("Allow", seen.header.Get("Allow"))
			WriteProblem(w, r, NewProblem(http.StatusMethodNotAllowed, "method_not_allowed",
				"this path does not accept "+r.Method))
		default:
			mux.ServeHTTP(w, r)
		}
	})
}

// captured is a response writer that keeps the status and headers and discards the body.
type captured struct {
	header http.Header
	status int
}

func (c *captured) Header() http.Header         { return c.header }
func (c *captured) Write(b []byte) (int, error) { return len(b), nil }
func (c *captured) WriteHeader(status int)      { c.status = status }
