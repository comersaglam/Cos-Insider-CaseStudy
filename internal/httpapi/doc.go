// Package httpapi is the central HTTP router: it mounts every module's routes on one
// ServeMux and wraps it in the shared middleware chain. It is the only package that
// assembles several modules for HTTP.
package httpapi
