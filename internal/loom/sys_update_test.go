package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseUpdatePreservesBinaryUntilVerified(t *testing.T) {
	content := []byte("synthetic new executable")
	hash := sha256.Sum256(content)
	for _, tc := range []struct {
		name, manifest string
		size           int64
		ok             bool
	}{
		{"valid", hex.EncodeToString(hash[:]), int64(len(content)), true},
		{"checksum mismatch", strings.Repeat("0", 64), int64(len(content)), false},
		{"missing checksum", "", int64(len(content)), false},
		{"size mismatch", hex.EncodeToString(hash[:]), int64(len(content) + 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "loom")
			if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/binary" {
					w.Write(content)
				} else {
					fmt.Fprintf(w, "%s  %s\n", tc.manifest, updateAssetName())
				}
			}))
			defer server.Close()
			rel := &ghRelease{TagName: "v99.0.0"}
			asset := struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Size               int64  `json:"size"`
			}{updateAssetName(), server.URL + "/binary", tc.size}
			rel.Assets = append(rel.Assets, asset)
			if tc.manifest != "" {
				asset.Name = "SHA256SUMS.txt"
				asset.BrowserDownloadURL = server.URL + "/sums"
				rel.Assets = append(rel.Assets, asset)
			}
			version, err := installReleaseUpdate(rel, exe)
			got, _ := os.ReadFile(exe)
			if tc.ok {
				if err != nil || version != "99.0.0" || string(got) != string(content) {
					t.Fatalf("update: %s %v %q", version, err, got)
				}
				previous, _ := os.ReadFile(exe + ".previous")
				if string(previous) != "old binary" {
					t.Fatal("rollback binary missing")
				}
			} else if err == nil || string(got) != "old binary" {
				t.Fatalf("unverified update changed binary: %v %q", err, got)
			}
			leftovers, _ := filepath.Glob(filepath.Join(dir, ".loom-update-*"))
			if len(leftovers) != 0 {
				t.Fatal("temporary download remains", leftovers)
			}
		})
	}
}

func TestUpdateAPIRequiresAuthenticationAndMethods(t *testing.T) {
	mux := loginTestMux(t)
	if err := saveWebPassword(testAccessPassword, nil); err != nil {
		t.Fatal(err)
	}
	webAPI(mux)("/api/update", handleUpdateCheck)
	webAPI(mux)("/api/update/apply", handleUpdateApply)
	for _, path := range []string{"/api/update", "/api/update/apply"} {
		if got := authCall(mux, "POST", path, "{}", nil, "", "").Code; got != 401 {
			t.Fatal("unauthenticated update", got)
		}
	}
	if err := storeWebKey("synthetic-updater-key"); err != nil {
		t.Fatal(err)
	}
	if got := authCall(mux, "GET", "/api/update/apply", "", nil, "synthetic-updater-key", "").Code; got != 405 {
		t.Fatal("GET can update", got)
	}
	if got := authCall(mux, "POST", "/api/update", "{}", nil, "synthetic-updater-key", "").Code; got != 405 {
		t.Fatal("wrong check method", got)
	}
	if got := authCall(mux, "POST", "/api/update/apply", "broken-json", nil, "synthetic-updater-key", "").Code; got != 400 {
		t.Fatal("invalid update body accepted", got)
	}
}
