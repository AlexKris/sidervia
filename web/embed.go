package web

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var distribution embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(distribution, "dist")
	if err != nil {
		return unavailableHandler()
	}
	index, indexErr := fs.ReadFile(root, "index.html")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(clean, "/")
		if name != "" && name != "." {
			if info, statErr := fs.Stat(root, name); statErr == nil && info.Mode().IsRegular() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".") {
				http.NotFound(w, r)
				return
			}
		}
		if indexErr != nil {
			unavailableHandler().ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(".html"))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

func unavailableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, errors.New("Sidervia web assets are not built").Error(), http.StatusServiceUnavailable)
	})
}
