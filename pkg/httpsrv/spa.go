package httpsrv

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler serves a single-page app from spaFS, falling back to index.html
// for any path that doesn't match an existing file (so client-side routing
// can take over).
//
// Mount via http.StripPrefix("/portal", SPAHandler(fsys)) so that requests to
// /portal/foo become /foo and resolve against the FS root.
func SPAHandler(spaFS fs.FS) http.Handler {
	indexBytes, _ := fs.ReadFile(spaFS, "index.html")
	fileServer := http.FileServer(http.FS(spaFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't fall through for asset requests that 404; return the real 404
		// so missing chunks are visible. Only fall back when the path looks
		// like a client route (no extension and not /assets/).
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if path == "index.html" {
			serveIndex(w, indexBytes)
			return
		}

		// Try to serve the file. If it doesn't exist and the path looks like a
		// client route, fall back to index.html.
		if f, err := spaFS.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		if isClientRoute(path) {
			serveIndex(w, indexBytes)
			return
		}
		http.NotFound(w, r)
	})
}

func serveIndex(w http.ResponseWriter, bytes []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(bytes)
}

func isClientRoute(path string) bool {
	if strings.HasPrefix(path, "assets/") {
		return false
	}
	// If there's a dot in the *last segment*, treat as a static asset.
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return !strings.Contains(path, ".")
}
