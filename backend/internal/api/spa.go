package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// webDist holds the built Vue 3 SPA (frontend/, built via `npm run build`,
// which Vite is configured to output straight into web/dist so go:embed —
// which cannot reach outside its package directory — can pick it up).
//
//go:embed web/dist
var webDistRaw embed.FS

// webDist strips the "web/dist" prefix so /assets/foo.js maps to
// web/dist/assets/foo.js on disk, matching Vite's build output layout.
var webDist = mustSub(webDistRaw, "web/dist")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // build-time invariant: web/dist always exists before `go build`
	}
	return sub
}

// spaIndex serves the built SPA shell for /, /admin, and /floor. Vue Router's
// history mode takes over client-side routing from there.
func (s *Server) spaIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, webDist, "index.html")
}
