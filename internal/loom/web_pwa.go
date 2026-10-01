package loom

import (
	"image"
	"image/draw"
	"net/http"
	"sync"
)

// Reuse the canonical tray/executable mark, including its antialiasing. The
// opaque 512px background meets maskable-icon requirements without a new logo.
func loomIcon(size int) []byte {
	if size != 512 {
		return BrandIconPNG(size)
	}
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(im, im.Bounds(), &image.Uniform{C: brandBlack}, image.Point{}, draw.Src)
	draw.Draw(im, im.Bounds(), brandIconImage(size, brandBlack, brandWhite), image.Point{}, draw.Over)
	return encodePNG(im)
}

var pwaIcons = sync.OnceValue(func() map[string][]byte {
	icons := map[string][]byte{}
	for path, size := range map[string]int{"/icons/loom-180.png": 180, "/icons/loom-192.png": 192, "/icons/loom-512.png": 512} {
		icons[path] = loomIcon(size)
	}
	return icons
})

func registerPWAAssets(mux *http.ServeMux) {
	for path, b := range pwaIcons() {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Header().Set("X-Content-Type-Options", "nosniff")
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
			b, err := uiFS.ReadFile(file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "font/woff2")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		})
	}
	mux.HandleFunc("/offline.html", func(w http.ResponseWriter, r *http.Request) {
		b, _ := uiFS.ReadFile("ui/offline.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(b)
	})
}
