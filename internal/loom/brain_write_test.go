package loom

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func TestManagedBrainReconnectReusesMatchingCheckout(t *testing.T) {
	testHome(t)
	dst := filepath.Join(managedBrainSourcesDir(), "knowledge")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", dst}, {"-C", dst, "remote", "add", "origin", "https://forge.example/user/brain.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	got, err := cloneBrainRemote(context.Background(), "knowledge", "https://forge.example/user/brain.git", "")
	if err != nil || got != dst {
		t.Fatalf("matching checkout was not reused: %q %v", got, err)
	}
	if _, err = cloneBrainRemote(context.Background(), "knowledge", "https://forge.example/other/brain.git", ""); err == nil {
		t.Fatal("checkout for a different remote was reused")
	}
}

func TestBrainRemoteValidationRejectsCredentialAndOptionInjection(t *testing.T) {
	for _, tc := range []struct {
		remote string
		branch string
		ok     bool
	}{
		{"https://forge.example/user/brain.git", "main", true},
		{"ssh://git@forge.example/user/brain.git", "notes/main", true},
		{"git@forge.example:user/brain.git", "", true},
		{"https://token@forge.example/user/brain.git", "", false},
		{"git@-oProxyCommand=bad:user/brain.git", "", false},
		{"https://forge.example/user/brain.git", "-upload-pack=bad", false},
	} {
		if err := validateBrainRemote(tc.remote, tc.branch); (err == nil) != tc.ok {
			t.Errorf("validateBrainRemote(%q, %q) = %v, want ok=%v", tc.remote, tc.branch, err, tc.ok)
		}
	}
}

func TestPrimarySecondBrainWriteIsConfined(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	brainSvc = nil
	brainSvcMu.Unlock()
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "primary", Label: "Primary", Path: dir, Kind: "context", Connector: "folder", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWrite("projects/loom.md", "# Loom\n\nCurrent decision.\n"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "projects", "loom.md"))
	if err != nil || !strings.Contains(string(b), "Current decision") {
		t.Fatalf("write missing: %q %v", b, err)
	}
	if err = secondBrainEdit("projects/loom.md", "Current decision", "Updated decision"); err != nil {
		t.Fatal(err)
	}
	secondary := t.TempDir()
	if err = e.Update(brain.Source{ID: "shared", Label: "Shared", Path: secondary, Kind: "context", Connector: "folder", Permission: "write"}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWriteSource("shared", "requested.md", "# Explicit update\n"); err != nil {
		t.Fatal(err)
	}
	readOnly := t.TempDir()
	if err = e.Update(brain.Source{ID: "reference", Label: "Reference", Path: readOnly, Kind: "context", Connector: "folder", Permission: "read"}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWriteSource("reference", "blocked.md", "no"); err == nil {
		t.Fatal("read-only second brain accepted a write")
	}
	if err = secondBrainWrite("../escape.md", "no"); err == nil {
		t.Fatal("path traversal accepted")
	}
	names := map[string]bool{}
	for _, tool := range EnabledTools(Caps{}) {
		names[tool.Function.Name] = true
	}
	if !names["brain_write"] || !names["brain_edit"] {
		t.Fatal("writable primary brain tools missing")
	}
}
