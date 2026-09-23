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

func TestVerifyChecksumRequiresManifest(t *testing.T) {
	asset := "loom-linux"
	path := filepath.Join(t.TempDir(), asset)
	if err := os.WriteFile(path, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(relWith(asset), asset, path); err == nil {
		t.Fatal("an automatic update must reject a release without checksums")
	}
}

func TestVerifyChecksumMatchesExactAsset(t *testing.T) {
	asset := "loom-linux"
	content := []byte("test binary")
	path := filepath.Join(t.TempDir(), asset)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	manifest := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, manifest)
	}))
	defer server.Close()

	release := relWith(asset, "SHA256SUMS.txt")
	release.Assets[1].BrowserDownloadURL = server.URL
	if err := verifyChecksum(release, asset, path); err != nil {
		t.Fatalf("valid manifest was rejected: %v", err)
	}

	manifest = strings.Replace(manifest, asset, "another-platform", 1)
	if err := verifyChecksum(release, asset, path); err == nil {
		t.Fatal("manifest for another platform was accepted")
	}
	manifest = strings.Repeat("0", 64) + "  " + asset + "\n"
	if err := verifyChecksum(release, asset, path); err == nil {
		t.Fatal("mismatched binary was accepted")
	}
}
