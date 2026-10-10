package loom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgentCompatibilityConstructorFixtures(t *testing.T) {
	testHome(t)
	records := map[string]*runtimeCompatibilityRecord{}
	for _, version := range []string{"", "999.0.0", "0.162.0"} {
		for _, id := range []string{"codex", "pi", "opencode", "antigravity", "claude-code", "hermes", "openclaw", "deepseek-harness", "registry-fixture"} {
			a := acpAgent{ID: id, Name: id, Command: "fixture", Args: []string{"@agentclientprotocol/claude-agent-acp@0.88.0"}}
			if id == "registry-fixture" {
				a.RegistryID, a.RegistryPackage, a.RegistryVersion = "fixture", "fixture-package", "1.0.0"
			}
			records[id+"/acp/"+version] = acpCompatibility(a, map[string]any{"version": version}, []string{"models"})
			if id == "codex" || id == "pi" {
				records[id+"/native/"+version] = nativeCompatibility(a, "fixture-native", "/fixture/tool", version, []string{"models"})
			}
			if id == "opencode" {
				records[id+"/native/"+version] = openCodeCompatibility(a, version)
			}
			if id == "antigravity" {
				records[id+"/native/"+version] = antigravityCompatibility(a, version)
			}
		}
	}
	for _, record := range records {
		record.Executable = "/fixture/tool"
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "agents", "compatibility-constructors.json")
	if os.Getenv("LOOM_WRITE_COMPAT_FIXTURE") == "1" {
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	var actual any
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(data, &actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("compatibility constructor changed legacy behavior")
	}
}
