package loom

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func writeHarnessFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessInstallationChannels(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ id, path, channel, prefix, formula string }{
		{"codex", ".codex/packages/standalone/releases/1.0.0-x86_64-unknown-linux-musl/bin/codex", "native", "", ""},
		{"claude-code", ".local/share/claude/versions/2.1.295", "native", "", ""},
		{"opencode", ".opencode/bin/opencode", "native", "", ""},
		{"antigravity", ".local/bin/agy", "native", "", ""},
		{"hermes", ".hermes/venv/bin/hermes", "native", "", ""},
		{"pi", ".local/lib/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js", "npm", ".local", ""},
		{"codex", ".npm-global/lib/node_modules/@openai/codex/bin/codex.js", "npm", ".npm-global", ""},
		{"codex", ".nvm/versions/node/v22/lib/node_modules/@openai/codex/bin/codex.js", "npm", ".nvm/versions/node/v22", ""},
		{"opencode", ".linuxbrew/Cellar/opencode/1.0/bin/opencode", "homebrew", "", "opencode"},
		{"codex", ".linuxbrew/Caskroom/codex/1.0/codex", "homebrew", "", "codex"},
		{"codex", "manual/bin/codex", "unknown", "", ""},
		{"codex", ".local/lib/node_modules/unrelated/codex", "unknown", "", ""},
	} {
		t.Run(tc.id+"/"+tc.path, func(t *testing.T) {
			spec, _ := harnessInspectSpec(tc.id)
			target := filepath.Join(home, tc.path)
			writeHarnessFixture(t, target, "#!/bin/sh\necho 1.0.0\n")
			launcher := filepath.Join(home, "launchers", tc.id)
			if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(launcher)
			if err := os.Symlink(target, launcher); err != nil {
				t.Fatal(err)
			}
			got, err := localHarnessInstallation(t.Context(), spec, launcher)
			if err != nil || got.Channel != tc.channel || got.Formula != tc.formula || got.Path != launcher {
				t.Fatal(got, err)
			}
			if tc.prefix != "" && got.Prefix != filepath.Join(home, tc.prefix) {
				t.Fatal(got)
			}
			// A binary reached directly inside a package has the same channel.
			direct, err := localHarnessInstallation(t.Context(), spec, target)
			if err != nil || direct.Channel != got.Channel {
				t.Fatal(direct, err)
			}
		})
	}
	spec, _ := harnessInspectSpec("codex")
	win := classifyHarnessInstallation(spec, harnessInstallationFiles{Home: `C:\Users\person`, Path: `C:\Users\person\npm\codex.cmd`, Shim: `@echo off
node "%dp0%\node_modules\@openai\codex\bin\codex.js" %*`})
	if win.Channel != "npm" || win.Prefix != "C:/Users/person/npm" {
		t.Fatal(win)
	}
	if got := classifyHarnessInstallation(spec, harnessInstallationFiles{Path: win.Path, Shim: `node "%dp0%\node_modules\another-package\codex.js" %*`}); got.Channel != "unknown" {
		t.Fatal(got)
	}
}

func TestHarnessChannelUpdateCommands(t *testing.T) {
	for _, tc := range []struct {
		id, channel, formula string
		cask                 bool
		want                 []string
	}{
		{"codex", "native", "", false, []string{"/selected/codex", "update"}},
		{"claude-code", "native", "", false, []string{"/selected/claude", "update"}},
		{"opencode", "native", "", false, []string{"/selected/opencode", "upgrade"}},
		{"antigravity", "native", "", false, []string{"/selected/agy", "update"}},
		{"codex", "npm", "", false, []string{"npm", "install", "-g", "@openai/codex@latest"}},
		{"claude-code", "npm", "", false, []string{"npm", "install", "-g", "@anthropic-ai/claude-code@latest"}},
		{"pi", "npm", "", false, []string{"npm", "install", "-g", "@earendil-works/pi-coding-agent@latest"}},
		{"opencode", "npm", "", false, []string{"npm", "install", "-g", "opencode-ai@latest"}},
		{"opencode", "homebrew", "opencode", false, []string{"brew", "upgrade", "opencode"}},
		{"codex", "homebrew", "codex", true, []string{"brew", "upgrade", "--cask", "codex"}},
	} {
		t.Run(tc.id+"/"+tc.channel, func(t *testing.T) {
			spec, _ := harnessInspectSpec(tc.id)
			s := newHarnessLifecycleService()
			got, err := s.installationUpdateCommand(t.Context(), nil, spec, harnessInstallation{Path: "/selected/" + spec.Binary, Channel: tc.channel, Formula: tc.formula, Cask: tc.cask})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, err)
			}
		})
	}
	spec, _ := harnessInspectSpec("pi")
	for _, help := range []string{"Usage: pi [options]\n  self-update   Update pi", "Usage: pi [options]\n  --update-check   Check for updates", "Usage: pi [options]"} {
		s := newHarnessLifecycleService()
		s.run = func(context.Context, *RemoteMachine, []string) (string, error) { return help, nil }
		cmd, err := s.installationUpdateCommand(t.Context(), nil, spec, harnessInstallation{Path: "/selected/pi", Channel: "native"})
		if strings.Contains(help, "self-update") {
			if err != nil || !reflect.DeepEqual(cmd, []string{"/selected/pi", "self-update"}) {
				t.Fatal(cmd, err)
			}
		} else if err == nil {
			t.Fatal("invented Pi updater", cmd)
		}
	}
}

func TestHarnessUnknownChannelRefusesMutations(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	path := filepath.Join(home, ".local/bin/codex")
	writeHarnessFixture(t, path, "#!/bin/sh\necho 1.0.0\n")
	s := newHarnessLifecycleService()
	s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
		if argv[0] == "npm" && argv[1] == "view" {
			return "2.0.0", nil
		}
		if argv[0] == "command" || argv[1] == "--version" {
			return runHarnessLifecycleCommand(ctx, m, argv)
		}
		t.Fatalf("unknown installation executed mutation %q", argv)
		return "", nil
	}
	state, err := s.action(t.Context(), "local", "codex", "check")
	if err != nil || state.Channel != "unknown" || state.CanUpdate || !state.UpdateAvailable {
		t.Fatal(state, err)
	}
	for _, action := range []string{"install", "update"} {
		state, err = s.action(t.Context(), "local", "codex", action)
		if err == nil || !strings.Contains(err.Error(), unknownHarnessChannel) || state.Channel != "unknown" {
			t.Fatal(state, err)
		}
	}
	if _, err := lifecycleNPMPrefix(t.Context(), []string{"npm", "install", "-g", "@openai/codex@latest"}); err == nil {
		t.Fatal("npm accepted unowned binary")
	}
}

func TestHarnessNativeUpdateVerificationAndRollback(t *testing.T) {
	for _, failure := range []string{"", "update", "version", "empty"} {
		t.Run(failure, func(t *testing.T) {
			testHome(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", "/usr/bin:/bin")
			launcher := filepath.Join(home, ".local/bin/codex")
			old := filepath.Join(home, ".codex/packages/standalone/releases/1.0.0/bin/codex")
			next := filepath.Join(home, ".codex/packages/standalone/releases/2.0.0/bin/codex")
			versionBody := "echo codex-cli 2.0.0"
			if failure == "version" {
				versionBody = "echo broken >&2; exit 9"
			}
			if failure == "empty" {
				versionBody = "exit 0"
			}
			writeHarnessFixture(t, next, "#!/bin/sh\n"+versionBody+"\n")
			updater := "ln -sfn " + shellQuote(next) + " " + shellQuote(launcher) + "\nrm -rf " + shellQuote(filepath.Dir(old)) + "\nprintf 'native update verbatim\\n'\n"
			if failure == "update" {
				updater += "exit 8\n"
			}
			writeHarnessFixture(t, old, "#!/bin/sh\nif [ \"$1\" = update ]; then\n"+updater+"else echo codex-cli 1.0.0; fi\n")
			if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(old, launcher); err != nil {
				t.Fatal(err)
			}
			s := newHarnessLifecycleService()
			refreshes := 0
			s.refresh = func(context.Context, *RemoteMachine, string) error { refreshes++; return nil }
			run := s.run
			s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
				if argv[0] == "npm" {
					if argv[1] != "view" {
						t.Fatal("npm touched standalone")
					}
					return "2.0.0", nil
				}
				return run(ctx, m, argv)
			}
			state, err := s.action(t.Context(), "local", "codex", "update")
			want := "codex-cli 2.0.0"
			if failure != "" {
				want = "codex-cli 1.0.0"
			}
			if (err != nil) != (failure != "") || state.Channel != "native" || state.Version != want || state.Log != "native update verbatim\n" || refreshes != 1 {
				t.Fatal(state, err, refreshes)
			}
			target, _ := os.Readlink(launcher)
			if failure != "" && target != old {
				t.Fatal("launcher was not restored", target)
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(old), ".loom-agent-backup-*"))
			if len(backups) > 0 {
				t.Fatal(backups)
			}
		})
	}
}

func TestHarnessRepairStandaloneAndHiddenNPM(t *testing.T) {
	for _, layout := range []string{"standalone", "native-opencode", "npm"} {
		t.Run(layout, func(t *testing.T) {
			testHome(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", "/usr/bin:/bin")
			id := "codex"
			path := filepath.Join(home, ".codex/packages/standalone/current/bin/codex")
			channel := "native"
			if layout == "native-opencode" {
				id = "opencode"
				path = filepath.Join(home, ".opencode/bin/opencode")
			}
			if layout == "npm" {
				channel = "npm"
				prefix := filepath.Join(home, "prefix outside path")
				path = filepath.Join(prefix, "lib/node_modules/@openai/codex/cli")
				writeHarnessFixture(t, filepath.Join(prefix, "bin/codex"), "#!/bin/sh\nexec "+shellQuote(path)+" \"$@\"\n")
				// npm launchers must prove ownership via symlinks or Windows shim content.
				_ = os.Remove(filepath.Join(prefix, "bin/codex"))
				_ = os.Symlink(path, filepath.Join(prefix, "bin/codex"))
				writeHarnessFixture(t, filepath.Join(home, ".local/bin/npm"), "#!/bin/sh\necho "+shellQuote(prefix)+"\n")
			}
			writeHarnessFixture(t, path, "#!/bin/sh\necho 1.0.0\n")
			s := newHarnessLifecycleService()
			s.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
			run := s.run
			s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
				if argv[0] == "npm" && argv[1] == "view" {
					return "2.0.0", nil
				}
				return run(ctx, m, argv)
			}
			state, err := s.action(t.Context(), "local", id, "check")
			if err != nil || state.Installed || !state.CanRepair || state.Channel != channel {
				t.Fatal(state, err)
			}
			if _, err := s.action(t.Context(), "local", id, "install"); err == nil {
				t.Fatal("installed a second copy")
			}
			state, err = s.action(t.Context(), "local", id, "repair")
			if err != nil || !state.Installed || state.CanRepair || state.Result != "repaired" || state.Channel != channel || state.Version != "1.0.0" {
				t.Fatal(state, err)
			}
			spec, _ := harnessInspectSpec(id)
			saved, err := lifecycleLookPath(spec.Binary)
			if err != nil || saved != filepath.Join(home, ".local/bin", spec.Binary) {
				t.Fatal(saved, err)
			}
		})
	}
}

func TestHarnessSSHChannelAndNPMPrefixObservations(t *testing.T) {
	prefix, path, _ := fakeHarnessNPM(t, ".npm-global")
	spec, _ := harnessInspectSpec("codex")
	script := unixHarnessInstallationScript(spec, path)
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	got := classifyHarnessInstallation(spec, harnessInstallationFiles{Home: lines[0], Path: lines[1], Resolved: lines[2]})
	if got.Channel != "npm" || got.Prefix != prefix {
		t.Fatal(got, string(out))
	}
	argv := []string{"npm", "install", "-g", "@openai/codex@latest"}
	out, err = exec.Command("sh", "-c", remotePathPreamble+remoteNPMPrefixScript(argv)+`printf '%s\n' "$loom_npm_prefix"`).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != prefix {
		t.Fatal(string(out), err)
	}
	home, _ := os.UserHomeDir()
	native := filepath.Join(home, ".codex/packages/standalone/current/bin/codex")
	writeHarnessFixture(t, native, "#!/bin/sh\necho 1.0.0\n")
	_ = os.Remove(path)
	_ = os.Symlink(native, path)
	out, err = exec.Command("sh", "-c", remotePathPreamble+remoteNPMPrefixScript(argv)).CombinedOutput()
	if err == nil || !strings.Contains(string(out), unknownHarnessChannel) {
		t.Fatal("SSH npm accepted standalone", string(out), err)
	}
	_ = os.Remove(path)
	out, err = exec.Command("sh", "-c", unixHarnessInstallationScript(spec, "")).Output()
	if err != nil || !strings.Contains(string(out), native) {
		t.Fatal("SSH recovery missed standalone", string(out), err)
	}
}

func TestHarnessRepairRejectsBrokenCandidate(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	path := filepath.Join(home, ".codex/packages/standalone/current/bin/codex")
	writeHarnessFixture(t, path, "#!/bin/sh\nexit 9\n")
	s := newHarnessLifecycleService()
	_, err := s.action(t.Context(), "local", "codex", "repair")
	if err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatal(err)
	}
	if _, err := lifecycleLookPath("codex"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatal("broken candidate became installed", err)
	}
}

func TestHarnessHomebrewUpdateAndVerification(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "version-failure"}[failure], func(t *testing.T) {
			testHome(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", "/usr/bin:/bin")
			binary := filepath.Join(home, ".linuxbrew/Cellar/opencode/1.0/bin/opencode")
			launcher := filepath.Join(home, ".local/bin/opencode")
			writeHarnessFixture(t, binary, "#!/bin/sh\necho 1.0.0\n")
			_ = os.MkdirAll(filepath.Dir(launcher), 0755)
			if err := os.Symlink(binary, launcher); err != nil {
				t.Fatal(err)
			}
			args := filepath.Join(home, "brew-args")
			replacement := "#!/bin/sh\necho 2.0.0\n"
			if failure {
				replacement = "#!/bin/sh\nexit 9\n"
			}
			writeHarnessFixture(t, filepath.Join(home, ".local/bin/brew"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+shellQuote(args)+"\nprintf '%s' "+shellQuote(replacement)+" > "+shellQuote(binary)+"\necho brew-updated\n")
			s := newHarnessLifecycleService()
			refreshes := 0
			s.refresh = func(context.Context, *RemoteMachine, string) error { refreshes++; return nil }
			run := s.run
			s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
				if argv[0] == "npm" {
					if argv[1] != "view" {
						t.Fatal("npm touched Homebrew")
					}
					return "2.0.0", nil
				}
				return run(ctx, m, argv)
			}
			state, err := s.action(t.Context(), "local", "opencode", "update")
			got, _ := os.ReadFile(args)
			if (err != nil) != failure || state.Channel != "homebrew" || string(got) != "upgrade\nopencode\n" || refreshes != 1 {
				t.Fatal(state, err, string(got))
			}
			if !failure && (state.Result != "updated" || state.Version != "2.0.0") {
				t.Fatal(state)
			}
		})
	}
}

func TestHarnessNativeSnapshotRestoresBinaryAndCurrentLink(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "releases/1.0.0/bin/codex")
	next := filepath.Join(home, "releases/2.0.0")
	current := filepath.Join(home, "current")
	original := "#!/bin/sh\necho 1.0.0\n"
	writeHarnessFixture(t, old, original)
	_ = os.MkdirAll(filepath.Join(next, "bin"), 0755)
	if err := os.Symlink(filepath.Dir(filepath.Dir(old)), current); err != nil {
		t.Fatal(err)
	}
	finish, err := preserveHarnessExecutable(filepath.Join(current, "bin/codex"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Dir(filepath.Dir(old)))
	_ = os.Remove(current)
	if err := os.Symlink(next, current); err != nil {
		t.Fatal(err)
	}
	if err := finish(false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(current, "bin/codex"))
	if err != nil || string(restored) != original {
		t.Fatal(string(restored), err)
	}
}

func TestWindowsSSHNPMPrefixGuards(t *testing.T) {
	argv := []string{"npm", "install", "-g", "@openai/codex@latest"}
	script := windowsRemoteNPMPrefixScript(argv)
	for _, guard := range []string{"$loom_existing", "Get-Content -Raw", "@openai/codex", "existing npm installation is not writable", powershellLiteral(unknownHarnessChannel)} {
		if !strings.Contains(script, guard) {
			t.Fatal("missing Windows target-side prefix guard", guard)
		}
	}
	built := windowsRemoteLifecycleCommand(RemoteMachine{Host: "win.example", User: "user", OS: "Windows"}, "", argv)
	// The helper encodes the generated PowerShell script for SSH. Decode it to
	// verify the actual invocation retains the target prefix and package argv.
	encoded := strings.Trim(built[len(built)-1], "'")
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	words := make([]uint16, len(bytes)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(bytes[i*2:])
	}
	decoded := string(utf16.Decode(words))
	if !strings.Contains(decoded, "& 'npm' 'install' '-g' '--prefix' $loom_npm_prefix '@openai/codex@latest'") {
		t.Fatal(decoded)
	}
}
