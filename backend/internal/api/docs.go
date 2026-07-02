package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openapiSpec []byte

//go:embed swagger-ui.html
var swaggerUIHTML []byte

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
