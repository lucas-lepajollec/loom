package loom

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/lucas-lepajollec/loom/internal/loom/harness"
)

// This list is deliberately independent of the catalog. Every family must be
// represented in every consumer as supported or explicitly non-executable.
func TestHarnessCatalogFamilyMatrix(t *testing.T) {
	testHome(t)
	families := []struct {
		id, binary                      string
		local, inspect, remote, preview bool
	}{
		{"codex", "codex", true, true, true, false},
		{"claude-code", "claude", true, true, true, false},
		{"pi", "pi", true, true, true, false},
		{"opencode", "opencode", true, true, true, false},
		{"antigravity", "agy", true, true, false, false},
		{"deepseek-harness", "dsh", true, true, false, true},
		{"hermes", "hermes", true, true, true, false},
		{"openclaw", "openclaw", true, true, true, false},
		{"custom-acp", "", false, false, false, false},
	}
	records := harness.Catalog()
	if len(records) != len(families) {
		t.Fatalf("family matrix needs updating: catalog=%d matrix=%d", len(records), len(families))
	}
	seen := map[string]bool{}
	for _, r := range records {
		if seen[r.ID] {
			t.Fatalf("duplicate family %s", r.ID)
		}
		seen[r.ID] = true
	}
	builtins := map[string]acpAgent{}
	for _, a := range builtinACPAgents() {
		builtins[a.ID] = a
	}
	recipes := map[string]harness.RemoteRecipe{}
	for _, d := range remoteHarnessDefs {
		recipes[d.ID] = d
	}
	for _, tc := range families {
		t.Run(tc.id, func(t *testing.T) {
			r, ok := harness.Lookup(tc.id)
			if !ok {
				t.Fatal("missing family from shared catalog")
			}
			a, launchable := builtinCatalogAgent(r, "loom-fixture")
			if launchable != tc.local {
				t.Fatal("missing or unexpected builtin ACP projection", a)
			}
			if launchable {
				if a.ID != r.ID || a.Name != r.Name || a.Logo != r.Logo || a.Docs != r.Docs || !reflect.DeepEqual(a.Args, r.Launcher.Args) || !reflect.DeepEqual(a.Detect, r.Launcher.Detect) {
					t.Fatal("identity/launcher drift", a)
				}
				if r.Launcher.Self {
					if tc.id != "antigravity" || a.Command != "loom-fixture" {
						t.Fatal(a)
					}
				} else {
					if !reflect.DeepEqual(a, builtins[tc.id]) {
						t.Fatal("family missing from builtin ACP consumer")
					}
				}
			} else if _, ok := builtins[tc.id]; ok {
				t.Fatal("user-defined family gained an executable default")
			}
			spec, inspectable := harnessInspectSpec(tc.id)
			if inspectable != tc.inspect || (r.Inspect != nil) != tc.inspect {
				t.Fatal("family missing from inspect consumer")
			}
			if inspectable && (spec.Binary != tc.binary || spec.Unverified != tc.preview || !reflect.DeepEqual(spec, *r.Inspect)) {
				t.Fatal("inspect metadata drift", spec)
			}
			d, remote := recipes[tc.id]
			if remote != tc.remote || (r.Remote != nil) != tc.remote {
				t.Fatal("family missing from remote consumer")
			}
			if remote && (d.Name != r.Name || d.Logo != r.Logo || !reflect.DeepEqual(d.Launch, append([]string{a.Command}, a.Args...))) {
				t.Fatal("remote argv/identity drift", d)
			}
			for _, transport := range []string{"ssh-posix", "ssh-windows", "node"} {
				t.Run(transport, func(t *testing.T) {
					m := RemoteMachine{ID: "matrix", Host: "host.example", User: "fixture", Home: "/fixture", OS: "linux"}
					if transport == "ssh-windows" {
						m.OS = "windows"
						m.Home = "C:/fixture"
					}
					if transport == "node" {
						m.NodeID = "paired"
						m.User = ""
						m.Modules = []string{"harness"}
					}
					remoteAgent, err := remoteAgent(m, tc.id, "")
					if (err == nil) != tc.remote || knownRemoteHarness(tc.id) != tc.remote {
						t.Fatalf("transport support drift: %v", err)
					}
					if !tc.remote {
						return
					}
					if remoteAgent.Name != r.Name || remoteAgent.Logo != r.Logo || !remoteAgent.Remote || !remoteAgent.Custom {
						t.Fatal("remote identity lost", remoteAgent)
					}
					if transport == "node" {
						for _, need := range d.Needs {
							m.Tools = append(m.Tools, RemoteTool{ID: need})
						}
						cmd, err := nodeHarnessCommand(m, tc.id, t.TempDir())
						if err != nil {
							t.Fatal(err)
						}
						for _, arg := range d.Launch {
							if !strings.Contains(strings.Join(cmd.Args, " "), shellQuote(arg)) {
								t.Fatal("Node recipe lost shared argv", cmd.Args, arg)
							}
						}
						m.Modules = nil
						if _, err := nodeAgent(m, tc.id); err == nil {
							t.Fatal("pure Node without harness module became executable")
						}
					} else {
						script := strings.Join(remoteAgent.Args, " ")
						if transport == "ssh-windows" {
							payload, err := base64.StdEncoding.DecodeString(remoteAgent.Args[len(remoteAgent.Args)-1])
							if err != nil || len(payload)%2 != 0 {
								t.Fatal("invalid PowerShell launch encoding", err)
							}
							words := make([]uint16, len(payload)/2)
							for i := range words {
								words[i] = binary.LittleEndian.Uint16(payload[2*i:])
							}
							script = string(utf16.Decode(words))
						}
						for _, arg := range d.Launch {
							if !strings.Contains(script, arg) {
								t.Fatal("SSH recipe lost shared argv", remoteAgent.Args, arg)
							}
						}
					}
					tools := []RemoteTool{}
					for _, need := range d.Needs {
						tools = append(tools, RemoteTool{ID: need, Version: "fixture"})
					}
					m.Tools = tools
					found := false
					for _, offer := range remoteOffers(m) {
						if offer["id"] != tc.id {
							continue
						}
						found = true
						if offer["ready"] != (transport != "node") || offer["name"] != r.Name || offer["logo"] != r.Logo {
							t.Fatal("remote availability/module/identity drift", offer)
						}
					}
					if !found {
						t.Fatal("family missing from remote offers consumer")
					}
				})
			}
		})
	}
	// Custom launchers remain user configuration, locally or over their chosen
	// remote transport; none can inherit a builtin or preview executable.
	for _, remote := range []bool{false, true} {
		a, err := validCustomACPAgent(acpAgent{Name: "Fixture ACP", Command: "fixture-acp", Args: []string{"--acp"}, Remote: remote})
		if err != nil || a.Command != "fixture-acp" || !a.Custom || a.Remote != remote {
			t.Fatal(a, err)
		}
	}
}

// Every dimension is required even when the explicit answer is unsupported.
// This helper accepts snapshots so omission sensitivity needs no source mutation.
func harnessCoverageErrors(ids []string, coverage map[string]map[string]bool) []string {
	var errors []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			errors = append(errors, "duplicate family: "+id)
		}
		seen[id] = true
		for _, dimension := range []string{"features", "inspect", "lifecycle", "model-source", "docs"} {
			if !coverage[id][dimension] {
				errors = append(errors, id+": missing "+dimension+" coverage")
			}
		}
	}
	for id := range coverage {
		if !seen[id] {
			errors = append(errors, "obsolete family coverage: "+id)
		}
	}
	return errors
}

func TestHarnessContractCoverage(t *testing.T) {
	testHome(t)
	// Independent support decisions. Empty sink/native updater means unsupported,
	// never an invented executable or provider compatibility claim.
	contracts := []struct {
		id, protocol, sink, native string
		npm                        bool
	}{
		{"codex", "app-server", "env", "update", true},
		{"claude-code", "acp", "env", "update", true},
		{"pi", "pi-rpc", "pi", "", true},
		{"opencode", "opencode-http", "opencode", "upgrade", true},
		{"antigravity", "agy-stream-json", "", "update", false},
		{"deepseek-harness", "acp", "dsh", "", true},
		{"hermes", "acp", "hermes", "update", false},
		{"openclaw", "acp", "", "", true},
		{"custom-acp", "acp", "", "", false},
	}
	docs, err := os.ReadFile("../../docs/agents/acp-implementation.md")
	if err != nil {
		t.Fatal(err)
	}
	coverage := map[string]map[string]bool{}
	for _, c := range contracts {
		coverage[c.id] = map[string]bool{}
		for _, f := range harnessFeaturePathCases() {
			if f.id == c.id && f.protocol == c.protocol {
				coverage[c.id]["features"] = true
			}
		}
		sink, supported := modelSinkFor(c.id)
		if supported != (c.sink != "") || sink.Format != c.sink {
			t.Fatalf("%s model-source support drift: %+v", c.id, sink)
		}
		coverage[c.id]["model-source"] = true
		spec, inspectable := harnessInspectSpec(c.id)
		if inspectable != (c.id != "custom-acp") {
			t.Fatalf("%s inspect support drift", c.id)
		}
		coverage[c.id]["inspect"] = true
		if inspectable {
			service := newHarnessLifecycleService()
			// No subprocess, network, account, real HOME or updater is ever used.
			service.run = func(context.Context, *RemoteMachine, []string) (string, error) {
				return "Usage: fixture (no self-updater)", nil
			}
			for _, channel := range []string{"native", "npm", "homebrew", "unknown"} {
				install := harnessInstallation{Channel: channel, Path: "/fixture/" + spec.Binary, Formula: spec.BrewNames[0]}
				cmd, err := service.installationUpdateCommand(t.Context(), nil, spec, install)
				want := channel == "homebrew" || channel == "npm" && c.npm || channel == "native" && c.native != ""
				if (err == nil) != want || want && len(cmd) == 0 {
					t.Fatalf("%s/%s update-channel support drift: %v %v", c.id, channel, cmd, err)
				}
				if channel == "native" && want && !reflect.DeepEqual(cmd, []string{install.Path, c.native}) {
					t.Fatalf("%s native updater drift: %v", c.id, cmd)
				}
			}
		}
		coverage[c.id]["lifecycle"] = true
		source := c.sink
		if source == "" {
			source = "unsupported"
		}
		lifecycle := "native=" + c.native + "; npm=" + strconv.FormatBool(c.npm) + "; homebrew=true; unknown=refuse"
		inspect := "yes"
		if !inspectable {
			inspect, lifecycle = "unsupported", "unsupported"
		}
		row := "| `" + c.id + "` | " + c.protocol + " | " + inspect + " | " + lifecycle + " | " + source + " |"
		coverage[c.id]["docs"] = strings.Contains(string(docs), row)
	}
	ids := []string{}
	for _, record := range harness.Catalog() {
		ids = append(ids, record.ID)
	}
	if errors := harnessCoverageErrors(ids, coverage); len(errors) != 0 {
		t.Fatal(errors)
	}
}

func TestHarnessContractCoverageRejectsOmissions(t *testing.T) {
	dimensions := []string{"features", "inspect", "lifecycle", "model-source", "docs"}
	complete := map[string]map[string]bool{"fixture": {}}
	for _, dimension := range dimensions {
		complete["fixture"][dimension] = true
	}
	if errors := harnessCoverageErrors([]string{"fixture"}, complete); len(errors) != 0 {
		t.Fatal(errors)
	}
	for _, dimension := range dimensions {
		t.Run(dimension, func(t *testing.T) {
			delete(complete["fixture"], dimension)
			errors := harnessCoverageErrors([]string{"fixture"}, complete)
			if len(errors) != 1 || !strings.Contains(errors[0], dimension) {
				t.Fatal("omission did not fire", errors)
			}
			complete["fixture"][dimension] = true
		})
	}
	if len(harnessCoverageErrors([]string{"fixture", "new-family"}, complete)) != len(dimensions) {
		t.Fatal("new family escaped coverage")
	}
	if len(harnessCoverageErrors([]string{"fixture", "fixture"}, complete)) != 1 {
		t.Fatal("duplicate family escaped coverage")
	}
	if len(harnessCoverageErrors(nil, complete)) != 1 {
		t.Fatal("obsolete family escaped coverage")
	}
}
