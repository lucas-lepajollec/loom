package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const offlineCSP = "default-src 'self'; script-src 'none'; style-src 'self' 'unsafe-inline'; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// importmapHash computes the CSP 'sha256-...' source for an inline
// <script type="importmap"> element in html, or "" when absent.
func importmapHash(html []byte) string {
	const openTag = `<script type="importmap">`
	const closeTag = `</script>`
	i := bytes.Index(html, []byte(openTag))
	if i < 0 {
		return ""
	}
	rest := html[i+len(openTag):]
	j := bytes.Index(rest, []byte(closeTag))
	if j < 0 {
		return ""
	}
	sum := sha256.Sum256(rest[:j])
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// UIContentSecurityPolicy builds the restrictive CSP for the Loom UI HTML,
// pinning the inline importmap hash when present without allowing unsafe-inline scripts.
func UIContentSecurityPolicy(html []byte) string {
	scriptSrc := "'self'"
	if h := importmapHash(html); h != "" {
		scriptSrc += " " + h
	}
	return "default-src 'self'; script-src " + scriptSrc +
		"; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' ws: wss:; media-src 'self' blob:; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

// Assets serves the public UI from supplied filesystems and brand icon bytes.
// It owns no conversation data, application state or mutable package globals.
type Assets struct {
	UI     fs.FS
	NextFS fs.FS
	Icons  func() map[string][]byte
}

func nextContentType(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

func (a Assets) Next(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/next")), "/")
	name := "ui/next/" + rel
	if rel == "" || rel == "." {
		name = "ui/next/index.html"
	}
	b, err := fs.ReadFile(a.NextFS, name)
	ext := path.Ext(name)
	if err != nil || nextContentType(ext) == "" {
		// Routes de l'application (#/… n'arrive pas ici, mais /next/chat oui).
		if ext != "" {
			http.NotFound(w, r)
			return
		}
		b, err = fs.ReadFile(a.NextFS, "ui/next/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ext = ".html"
	}
	ApplySecurityHeaders(w)
	w.Header().Set("Content-Type", nextContentType(ext))
	if ext == ".html" {
		// Developer previews have their own origin and must not embed the
		// control plane to trick the owner into interacting with it.
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", UIContentSecurityPolicy(b))
	}
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodGet {
		_, _ = w.Write(b)
	}
}

// handleIndex sert l'interface à la racine.
func (a Assets) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/next/"
	a.Next(w, r2)
}

func (a Assets) RegisterPWA(mux *http.ServeMux) {
	for path, b := range a.Icons() {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			ApplySecurityHeaders(w)
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		})
	}
	// Polices Geist (OFL) embarquées : Loom reste utilisable hors ligne et
	// n'appelle aucun CDN de polices.
	for path, file := range map[string]string{"/fonts/geist.woff2": "ui/fonts/geist.woff2", "/fonts/geist-mono.woff2": "ui/fonts/geist-mono.woff2"} {
		file := file
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			b, err := fs.ReadFile(a.UI, file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			ApplySecurityHeaders(w)
			w.Header().Set("Content-Type", "font/woff2")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		})
	}
	mux.HandleFunc("/offline.html", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/offline.html")
		ApplySecurityHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", offlineCSP)
		w.Write(b)
	})
}

// Register installs every historical public UI route and its cache policy.
func (a Assets) Register(mux *http.ServeMux) {
	// Pages publiques : le HTML et le JS ne contiennent aucun secret. Toute la
	// donnée et toutes les actions passent par /api/* qui, lui, exige la clé.
	mux.HandleFunc("/", a.Index)
	mux.HandleFunc("/next/", a.Next)
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/next/", http.StatusFound) })
	a.RegisterPWA(mux)
	mux.HandleFunc("/marked.min.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/marked.min.js")
		ApplySecurityHeaders(w)
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(b)
	})
	// Service worker et manifeste PWA, avec notifications Web Push (push.go / sw.js).
	// PUBLICS (aucun secret) et servis en clair à la RACINE : un service worker doit
	// venir de l'origine même, et son scope est celui de son URL. no-store sur le SW
	// pour qu'une mise à jour du worker soit toujours reprise (pas de cache figé).
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/sw.js")
		ApplySecurityHeaders(w)
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Write(b)
	})
	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		b, _ := fs.ReadFile(a.UI, "ui/manifest.webmanifest")
		ApplySecurityHeaders(w)
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(b)
	})
}
