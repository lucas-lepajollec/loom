//go:build !windows

package loom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityReadFindsUserInstallOutsideServicePATH(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\nprintf 'fixture-model\\tFixture model\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	out, err := agyRead(context.Background(), "models")
	if err != nil || !strings.Contains(string(out), "fixture-model") || !agyInstalled() {
		t.Fatalf("user-installed CLI not found: %v", err)
	}
}
