package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/comersaglam/Cos-Insider-CaseStudy/api"
)

// docsPage renders the contract with Swagger UI, loaded from a CDN at a pinned version with integrity hashes.
//
//go:embed docs.html
var docsPage []byte

// serveSpec answers GET /openapi.yaml with the contract, under YAML's registered media type (RFC 9512).
func serveSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(api.Spec)
}

// serveDocs answers GET /docs with the documentation page.
func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(docsPage)
}
