package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openapiSpec []byte

//go:embed swagger-ui.html
var swaggerUIHTML []byte

// Swagger UI assets are vendored (not loaded from a CDN) so /docs works behind
// any network — no unpkg/jsdelivr reachability needed. Pinned to swagger-ui-dist@5.17.14.
//
//go:embed swagger-ui.css
var swaggerCSS []byte

//go:embed swagger-ui-bundle.js
var swaggerBundleJS []byte

//go:embed swagger-ui-standalone-preset.js
var swaggerPresetJS []byte

// openapiYAML serves the hand-authored OpenAPI 3.1 spec.
func (s *Server) openapiYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapiSpec)
}

// docs serves Swagger UI, which renders the spec at /openapi.yaml.
func (s *Server) docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(swaggerUIHTML)
}

// docsAsset serves the vendored Swagger UI CSS/JS from /docs/…
func (s *Server) docsAsset(body []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(body)
	}
}
