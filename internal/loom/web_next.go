package loom

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Nouvelle interface (ui/next) : modules ES servis tels quels, sans étape de
// build. Publique comme l'ancienne page : aucune donnée ici, tout passe par
// /api/* protégé par la clé de pilotage.
//
//go:embed ui/next
var nextFS embed.FS

var nextTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".txt":  "text/plain; charset=utf-8",
}

func handleNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/next")), "/")
	name := "ui/next/" + rel
	if rel == "" || rel == "." {
		name = "ui/next/index.html"
	}
	b, err := fs.ReadFile(nextFS, name)
	ext := path.Ext(name)
	if err != nil || nextTypes[ext] == "" {
		// Routes de l'application (#/… n'arrive pas ici, mais /next/chat oui).
		if ext != "" {
			http.NotFound(w, r)
			return
		}
		b, err = fs.ReadFile(nextFS, "ui/next/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ext = ".html"
	}
	w.Header().Set("Content-Type", nextTypes[ext])
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		_, _ = w.Write(b)
	}
}
