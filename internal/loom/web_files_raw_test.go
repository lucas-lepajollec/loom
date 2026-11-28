package loom

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRawTypesRangesAndConfinement(t *testing.T) {
	dir := secondBrainFixture(t)
	read := func(name, subdir, byteRange string) *httptest.ResponseRecorder {
		q := url.Values{"path": {name}, "dir": {subdir}}
		req := httptest.NewRequest("GET", "/api/files/raw?"+q.Encode(), nil)
		req.Header.Set("Range", byteRange)
		w := httptest.NewRecorder()
		handleFilesRaw(w, req)
		return w
	}
	for ext, typ := range previewFileTypes {
		name := "fixture" + ext
		if err := os.WriteFile(filepath.Join(dir, "projets", name), []byte("0123456789"), 0600); err != nil {
			t.Fatal(err)
		}
		w := read(name, filepath.Join(dir, "projets"), "bytes=2-4")
		if w.Code != 206 || w.Body.String() != "234" || w.Header().Get("Content-Range") != "bytes 2-4/10" || w.Header().Get("Content-Type") != typ || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(ext, w.Code, w.Header())
		}
		if (ext == ".html" || ext == ".svg") && !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatal(ext, "missing sandbox")
		}
	}
	for name, want := range map[string]string{"fixture.svg": "image/svg+xml", "fixture.htm": "text/html; charset=utf-8", "fixture.glb": "model/gltf-binary", "fixture.wav": "audio/wav", "fixture.mp4": "video/mp4", "fixture.pdf": "application/pdf"} {
		if got := read(name, "projets", "").Header().Get("Content-Type"); got != want {
			t.Errorf("%s MIME %s, want %s", name, got, want)
		}
	}
	for _, p := range []string{"../other", "/absolute", ".private/file", "bad/../../note"} {
		if w := read(p, "projets", ""); w.Code < 400 {
			t.Fatal(p, w.Code)
		}
	}
	if w := read("plan.md", "../", ""); w.Code < 400 {
		t.Fatal("escaped directory")
	}
	if w := read("plan.md", "projets", ""); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("download compatibility", w.Header())
	}
	if err := os.Symlink(dir, filepath.Join(dir, "projets", "escape")); err == nil {
		if w := read("escape/projets/fixture.png", "projets", ""); w.Code < 400 {
			t.Fatal("escaped session directory via symlink")
		}
	}
	if err := os.Truncate(filepath.Join(dir, "projets", "fixture.mp4"), filesMaxRaw+1); err != nil {
		t.Fatal(err)
	}
	if w := read("fixture.mp4", "projets", "bytes=0-1"); w.Code < 400 {
		t.Fatal("oversized stream")
	}
}
