package loom

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeHarnessNPM(t *testing.T, layout string) (string, string, string) {
	t.Helper()
	testHome(t)
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixtures")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	prefix := filepath.Join(home, layout)
	bin := filepath.Join(prefix, "bin")
	pkg := filepath.Join(prefix, "lib", "node_modules", "@openai", "codex")
	for _, dir := range []string{bin, pkg} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pkg, "cli"), "#!/bin/sh\nif [ \"$1\" = --help ]; then echo app-server; else echo 'codex-cli 1.0.0'; fi\n")
	exe := filepath.Join(bin, "codex")
	if err := os.Symlink("../lib/node_modules/@openai/codex/cli", exe); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(home, "npm-args")
	t.Setenv("LOOM_TEST_NPM_ARGS", args)
	write(filepath.Join(bin, "npm"), `#!/bin/sh
case "$1" in
 prefix) echo /proc/loom-readonly-prefix; exit 0 ;;
 view) echo 2.0.0; exit 0 ;;
esac
printf '%s\n' "$@" > "$LOOM_TEST_NPM_ARGS"
[ "$3" = --prefix ] || exit 90
stage="$4"
mkdir -p "$stage/bin" "$stage/lib/node_modules/@openai/codex"
cat > "$stage/lib/node_modules/@openai/codex/cli" <<'CLI'
#!/bin/sh
if [ "$LOOM_TEST_NPM_FAIL" = version ]; then echo broken >&2; exit 8; fi
if [ "$LOOM_TEST_NPM_FAIL" = promoted ]; then
 case "$0" in */.loom-npm-*) ;; *) echo broken-after-promotion >&2; exit 9 ;; esac
fi
if [ "$1" = --help ]; then echo app-server; else echo 'codex-cli 2.0.0'; fi
CLI
chmod +x "$stage/lib/node_modules/@openai/codex/cli"
ln -s ../lib/node_modules/@openai/codex/cli "$stage/bin/codex"
printf 'npm output verbatim\n' >&2
if [ "$LOOM_TEST_NPM_FAIL" = install ]; then exit 7; fi
`)
	return prefix, exe, args
}

func TestHarnessNPMExistingLayoutsAndRollback(t *testing.T) {
	for _, layout := range []string{".npm-global", ".local", ".nvm/versions/node/v22.0.0"} {
		for _, failure := range []string{"", "install", "version", "promoted"} {
			t.Run(layout+"/"+failure, func(t *testing.T) {
				prefix, exe, argsFile := fakeHarnessNPM(t, layout)
				t.Setenv("LOOM_TEST_NPM_FAIL", failure)
				argv := []string{"npm", "install", "-g", "@openai/codex@latest"}
				got, err := lifecycleNPMPrefix(t.Context(), argv)
				if err != nil || got != prefix {
					t.Fatalf("prefix %q: %v", got, err)
				}
				out, err := runHarnessLifecycleCommand(t.Context(), nil, argv)
				if (err != nil) != (failure != "") || out != "npm output verbatim\n" {
					t.Fatalf("%q %v", out, err)
				}
				args, err := os.ReadFile(argsFile)
				if err != nil {
					t.Fatal(err)
				}
				words := strings.Split(strings.TrimSpace(string(args)), "\n")
				// npm receives a staging prefix beneath the current installation.
				if len(words) != 5 || words[2] != "--prefix" || filepath.Dir(words[3]) != prefix {
					t.Fatalf("argv %q", words)
				}
				path, err := lifecycleLookPath("codex")
				if err != nil || path != exe {
					t.Fatalf("lost previous executable: %q %v", path, err)
				}
				version, err := runHarnessLifecycleCommand(t.Context(), nil, []string{exe, "--version"})
				want := "codex-cli 2.0.0\n"
				if failure != "" {
					want = "codex-cli 1.0.0\n"
				}
				if err != nil || version != want {
					t.Fatalf("version %q %v", version, err)
				}
				stages, _ := filepath.Glob(filepath.Join(prefix, ".loom-npm-*"))
				if len(stages) != 0 {
					t.Fatalf("staging files retained: %q", stages)
				}
			})
		}
	}
}

func TestHarnessNPMPrefixFromModuleAndReadOnlyInstall(t *testing.T) {
	prefix, exe, _ := fakeHarnessNPM(t, ".local")
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil || npmExecutablePrefix(resolved) != prefix {
		t.Fatal(resolved, err)
	}
	if err := os.Chmod(prefix, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(prefix, 0755) })
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory modes")
	}
	if _, err := lifecycleNPMPrefix(t.Context(), []string{"npm", "install", "-g", "@openai/codex@latest"}); err == nil {
		t.Fatal("must not move an existing read-only installation")
	}
}

func TestHarnessNPMVoltaShimResolution(t *testing.T) {
	prefix, exe, _ := fakeHarnessNPM(t, ".volta/tools/image/packages/codex")
	// Only the manager's shims are visible; the real package remains in its image.
	home, _ := os.UserHomeDir()
	shimDir := filepath.Join(home, ".volta", "bin")
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"codex": "#!/bin/sh\nexec " + shellQuote(exe) + " \"$@\"\n",
		"npm":   "#!/bin/sh\necho 'must not install through the manager shim' >&2\nexit 97\n",
		"volta": "#!/bin/sh\ncase \"$2\" in codex) echo " + shellQuote(exe) + " ;; npm) echo " + shellQuote(filepath.Join(prefix, "bin", "npm")) + " ;; esac\n",
	} {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	argv := []string{"npm", "install", "-g", "@openai/codex@latest"}
	got, err := lifecycleNPMPrefix(t.Context(), argv)
	if err != nil || got != prefix {
		t.Fatal(got, err)
	}
	out, err := runHarnessLifecycleCommand(t.Context(), nil, argv)
	if err != nil || out != "npm output verbatim\n" {
		t.Fatal(out, err)
	}
	version, err := runHarnessLifecycleCommand(t.Context(), nil, []string{"codex", "--version"})
	if err != nil || version != "codex-cli 2.0.0\n" {
		t.Fatal(version, err)
	}
	shim, _ := os.ReadFile(filepath.Join(shimDir, "codex"))
	if !strings.Contains(string(shim), "exec") {
		t.Fatal("managed shim replaced")
	}
}

func TestHarnessNPMFreshInstallFallbackAndSavedPath(t *testing.T) {
	prefix, exe, _ := fakeHarnessNPM(t, ".local")
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	argv := []string{"npm", "install", "-g", "@openai/codex"}
	out, err := runHarnessLifecycleCommand(t.Context(), nil, argv)
	if err != nil || out != "npm output verbatim\n" {
		t.Fatal(out, err)
	}
	var saved string
	if !getStoreJSON(bkState, "harness_executable:codex", &saved) || saved != filepath.Join(prefix, "bin", "codex") {
		t.Fatal(saved)
	}
	// A configured writable global prefix need not be on the service's PATH.
	other := filepath.Join(prefix, "custom-prefix", "bin")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho codex-cli 3.0.0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, "harness_executable:codex", path); err != nil {
		t.Fatal(err)
	}
	if got, err := lifecycleLookPath("codex"); err != nil || got != path {
		t.Fatal(got, err)
	}
	m := nodeHarnessSavedPaths(t.Context(), RemoteMachine{Tools: []RemoteTool{{ID: "codex", Path: exe, Version: "old"}}})
	if len(m.Tools) != 1 || m.Tools[0].Path != path || m.Tools[0].Version != "codex-cli 3.0.0" {
		t.Fatal(m.Tools)
	}
}

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
	got, err := lifecycleNPMPrefix(context.Background(), []string{"npm", "install", "-g", "prefix-fixture"})
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
	got, err = lifecycleNPMPrefix(context.Background(), []string{"npm", "install", "-g", "prefix-fixture"})
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
	for _, id := range []string{"claude-code", "codex", "pi", "opencode"} {
		spec, _ := harnessInspectSpec(id)
		for _, action := range []string{"install", "update"} {
			argv, err := lifecycleActionCommand(spec, "unix", action)
			if action == "update" {
				argv, err = newHarnessLifecycleService().installationUpdateCommand(t.Context(), nil, spec, harnessInstallation{Channel: "npm"})
			}
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
	testHome(t)
	manifest, err := os.ReadFile(filepath.Join(pkg, "package.json"))
	var fixture struct {
		Name string `json:"name"`
	}
	if err != nil || json.Unmarshal(manifest, &fixture) != nil || fixture.Name == "" {
		t.Fatal("acceptance requires a local package directory with package.json")
	}
	harnessInspectSpec("")
	inspectSpecs["loom-harness-fixture"] = inspectSpec{Binary: "loom-harness-fixture", Version: []string{"loom-harness-fixture", "--version"}, Install: map[string][]string{"unix": {"npm", "install", "-g", pkg}}, Latest: &harnessLatestSpec{NPM: fixture.Name}}
	t.Cleanup(func() { delete(inspectSpecs, "loom-harness-fixture") })
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
