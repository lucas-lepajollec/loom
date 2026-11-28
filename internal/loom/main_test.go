package loom

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

// Isolate the OS keychain and the workspace cached on first use by the package.
func TestMain(m *testing.M) {
	keyring.MockInit()
	dir, err := os.MkdirTemp("", "loom-test-workspace-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := 1
	if err := os.Setenv(workspaceEnv, filepath.Join(dir, "workspace")); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
