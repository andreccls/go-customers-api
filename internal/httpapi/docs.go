package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

// The OpenAPI document and Swagger UI ship inside the binary: /docs works offline,
// with no CDN. Swagger UI is Apache-2.0 (see swagger/LICENSE, swagger/VERSION).
var (
	//go:embed openapi.json
	openAPISpec []byte

	//go:embed swagger
	swaggerFS embed.FS
)

// OpenAPI returns the embedded OpenAPI document (used by the documentation tests).
func OpenAPI() []byte { return openAPISpec }

func mountDocs(mux *http.ServeMux) {
	mux.Handle("GET /openapi.json", named("GET /openapi.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPISpec)
	})))

	ui, _ := fs.Sub(swaggerFS, "swagger") // the directory is embedded: cannot fail
	files := http.StripPrefix("/docs/", http.FileServerFS(ui))
	mux.Handle("GET /docs/", named("GET /docs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Swagger UI needs inline styles; scripts and everything else stay same-origin.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'")
		files.ServeHTTP(w, r)
	})))
}
