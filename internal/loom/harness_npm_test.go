package loom

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHarnessNPMWritablePrefixAndUserPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix npm policy")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(home, "prefix with spaces")
	if err := os.MkdirAll(prefix, 0755); err != nil {
		t.Fatal(err)
	}
	npm := filepath.Join(dir, "npm")
	if err := os.WriteFile(npm, []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(prefix)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := lifecycleNPMPrefix(context.Background(), []string{"npm", "install", "-g", "@openai/codex@latest"})
	if err != nil || got != prefix {
		t.Fatalf("prefix: %q %v", got, err)
	}
	found, err := lifecycleLookPath("npm")
	if err != nil || found != npm {
		t.Fatal("user-installed npm not preferred")
	}
	if err := os.WriteFile(npm, []byte("#!/bin/sh\nprintf '%s\\n' /proc/loom-readonly-prefix\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err = lifecycleNPMPrefix(context.Background(), []string{"npm", "install", "-g", "@openai/codex@latest"})
	if err != nil || got != filepath.Join(home, ".local") {
		t.Fatalf("fallback: %q %v", got, err)
	}
	if err := os.WriteFile(npm, []byte("#!/bin/sh\nprintf '%s\\n' relative-prefix\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycleNPMPrefix(context.Background(), []string{"npm", "install", "-g", "pkg"}); err == nil {
		t.Fatal("invalid prefix accepted")
	}
}

func TestHarnessNPMAllUnixInstallUpdateCommands(t *testing.T) {
	for _, id := range []string{"claude-code", "codex", "gemini", "pi", "opencode"} {
		spec, _ := harnessInspectSpec(id)
		for _, action := range []string{"install", "update"} {
			argv, err := lifecycleActionCommand(spec, "unix", action)
			if err != nil || !lifecycleGlobalNPM(argv) {
				t.Fatalf("%s %s: %q %v", id, action, argv, err)
			}
			remote, err := buildHarnessLifecycleCommand(&RemoteMachine{Host: "box.invalid", User: "user", OS: "Linux"}, "", argv)
			if err != nil || !strings.Contains(remote[len(remote)-1], "--prefix") || strings.Contains(remote[len(remote)-1], "sudo") {
				t.Fatal("unsafe npm action")
			}
		}
	}
}

// Optional isolated-container acceptance uses real npm with a local tiny
// package, never a native account or globally installed user's harness.
func TestHarnessNPMNonRootAcceptance(t *testing.T) {
	pkg := os.Getenv("LOOM_NPM_ACCEPTANCE_PACKAGE")
	if pkg == "" {
		t.Skip("container acceptance only")
	}
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Fatal("acceptance must run as a Linux non-root user")
	}
	output, err := runHarnessLifecycleCommand(context.Background(), nil, []string{"npm", "install", "-g", pkg})
	if err != nil {
		t.Fatalf("npm failed: %v %s", err, output)
	}
	p, err := lifecycleLookPath("loom-harness-fixture")
	if err != nil || !strings.Contains(p, "/.local/bin/") {
		t.Fatalf("new user binary not discovered: %q %v", p, err)
	}
	output, err = runHarnessLifecycleCommand(context.Background(), nil, []string{"loom-harness-fixture", "--version"})
	if err != nil || strings.TrimSpace(output) != "1.0.0" {
		t.Fatal("wrong executable/version")
	}
	// Execute the actual Unix SSH script body as the target user (no remote
	// machine changed). This verifies target-home expansion, quoting and prefix.
	argv := []string{"npm", "install", "-g", pkg}
	script := remotePathPreamble + remoteNPMPrefixScript(argv) + `exec npm install -g --prefix "$loom_npm_prefix" ` + shellQuote(pkg)
	if output, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("target npm script: %v %s", err, output)
	}
	// Installing again exercises the same update prefix without configuration writes.
	if _, err := runHarnessLifecycleCommand(context.Background(), nil, []string{"npm", "install", "-g", pkg}); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".npmrc")); !os.IsNotExist(err) {
		t.Fatal("npm user configuration was changed")
	}
	if installDirWritable("/usr/lib") {
		t.Fatal("test prefix is not root-only")
	}
	if err := requireInstallWritable("/usr/lib/loom-engine"); err == nil {
		t.Fatal("engine permissions failure not caught")
	}
}
