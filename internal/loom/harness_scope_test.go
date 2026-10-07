package loom

import "testing"

func TestAgentScopeDefaultsAndToggles(t *testing.T) {
	testHome(t)
	m := RemoteMachine{ID: "tour", Name: "Tour", Host: "10.0.0.2", User: "u", Port: 22, OS: "Linux", Tools: []RemoteTool{{ID: "codex", Path: "/usr/bin/codex", Version: "0.161.0"}}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	find := func() agentInstallation {
		for _, i := range agentInstallations() {
			if i.Machine == "tour" && i.Harness == "codex" {
				return i
			}
		}
		t.Fatal("remote codex not listed")
		return agentInstallation{}
	}
	if i := find(); i.Managed || i.Enabled || i.Version != "0.161.0" {
		t.Fatalf("another machine's agent must start unmanaged: %+v", i)
	}
	if !harnessManaged("local", "codex") {
		t.Fatal("Loom's own machine agents are managed by default")
	}
	if err := setHarnessManaged("tour", "codex", true); err != nil {
		t.Fatal(err)
	}
	if i := find(); !i.Managed || i.Enabled {
		t.Fatalf("managed without being enabled: %+v", i)
	}
	if err := setHarnessManaged("tour", "codex", false); err != nil || find().Managed {
		t.Fatalf("unmanage failed: %v", err)
	}
}
