package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
)

func TestAgentProbeRecoveryIsCapabilitySpecific(t *testing.T) {
	testHome(t)
	old := degradedCapabilities
	degradedCapabilities = &capability.State{}
	t.Cleanup(func() { degradedCapabilities = old })
	for _, id := range []string{"codex", "claude-code", "pi", "opencode", "antigravity", "hermes", "openclaw", "deepseek-harness", "registry-fixture", "custom-ssh-codex", "custom-node-hermes"} {
		t.Run(id, func(t *testing.T) {
			owner := "agent:" + id
			observeCapability(owner, "approvals", false, "approval_failed")
			observeAgentProbe(id, acpProbe{CapabilityChecks: []capability.Probe{{Capability: "models", OK: true}}})
			if !capabilityDisabled(owner, "approvals") {
				t.Fatal("models-only probe restored unchecked approvals")
			}
			observeAgentProbe(id, acpProbe{})
			if !capabilityDisabled(owner, "approvals") {
				t.Fatal("empty probe restored unchecked approvals")
			}
			observeAgentProbe(id, acpProbe{Compatibility: &runtimeCompatibilityRecord{CapabilityChecks: []capability.Probe{{Capability: "approvals", OK: true}}}})
			if capabilityDisabled(owner, "approvals") {
				t.Fatal("successful approvals check did not restore approvals")
			}
		})
	}
}

func TestHarnessAutomaticChecksNeverUpdateWorkingInstallation(t *testing.T) {
	s, version, updates, _ := fakeHarnessLifecycle(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }
	if err := s.setAuto("local", "codex", true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s.autoCycle(context.Background())
		now = now.Add(harnessAutoInterval)
	}
	if *updates != 0 || *version != "codex-cli 1.0.0" {
		t.Fatalf("automatic check updated working harness: updates=%d version=%s", *updates, *version)
	}
}

func TestAgentCapabilityEvidenceFixtures(t *testing.T) {
	testHome(t)
	raw, err := os.ReadFile("testdata/agents/capability-evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name       string
		Protocol   string
		Caps       map[string]any
		Checks     []capability.Probe
		Capability string
		Discovered bool
		Verified   bool
		Degraded   bool
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			a := acpAgent{ID: fixture.Name, Command: "fixture", Custom: true}
			r := &runtimeCompatibilityRecord{Protocol: fixture.Protocol, Version: "999.0.0", AdapterVersion: "fixture"}
			p := acpProbe{At: 1000, Caps: fixture.Caps, Compatibility: r, CapabilityChecks: fixture.Checks}
			recordProbeEvidence(a, &p)
			e := r.Evidence[fixture.Capability]
			if (e.Discovered != nil) != fixture.Discovered || (e.ProtocolVerified != nil) != fixture.Verified || e.ObservedWorking != nil || (e.Health == "degraded") != fixture.Degraded {
				t.Fatalf("evidence %+v", e)
			}
			// Version acceptance never silently gives evidence to another capability.
			if r.Evidence["approvals"].ObservedWorking != nil {
				t.Fatal("probe claimed working approvals")
			}
		})
	}
}

func TestAgentEvidenceIdentityHistoryAndPrivacy(t *testing.T) {
	testHome(t)
	a := acpAgent{ID: "fixture", Command: "fixture", Args: []string{"PRIVATE_PATH"}, Custom: true}
	r := &runtimeCompatibilityRecord{Protocol: "acp", Version: "1.0.0", AdapterVersion: "adapter-1"}
	recordSessionEvidence(a, r, []string{"approvals"}, 1000)
	p := acpProbe{At: 2000, Compatibility: r, CapabilityChecks: []capability.Probe{{Capability: "approvals", OK: false, Reason: "PRIVATE_REASON"}}}
	recordProbeEvidence(a, &p)
	e := r.Evidence["approvals"]
	if e.ObservedWorking == nil || e.Health != "degraded" || e.ProtocolVerified != nil {
		t.Fatalf("history lost or failure verified: %+v", e)
	}
	for _, mutate := range []func(*acpAgent, *runtimeCompatibilityRecord){
		func(a *acpAgent, r *runtimeCompatibilityRecord) { r.Version = "2.0.0" },
		func(a *acpAgent, r *runtimeCompatibilityRecord) { r.AdapterVersion = "adapter-2" },
		func(a *acpAgent, r *runtimeCompatibilityRecord) { r.Protocol = "pi-rpc" },
		func(a *acpAgent, r *runtimeCompatibilityRecord) {
			a.Machine, a.Remote, a.Command = "remote", true, "ssh"
		},
		func(a *acpAgent, r *runtimeCompatibilityRecord) { a.Args = []string{"changed"} },
	} {
		changedA, changedR := a, *r
		mutate(&changedA, &changedR)
		readAgentEvidence(changedA, &changedR)
		if len(changedR.Evidence) != 0 {
			t.Fatal("different installation inherited evidence")
		}
	}
	readAgentEvidence(a, r)
	if r.Evidence["approvals"].ObservedWorking == nil {
		t.Fatal("historical partition lost")
	}
	updateAgentEvidence(a, r, 3000, "observed_working", "PRIVATE_CHECK", []string{"approvals"}, true)
	for _, value := range allKV(bkCapabilities) {
		for _, secret := range []string{"PRIVATE_PATH", "PRIVATE_REASON", "PRIVATE_CHECK"} {
			if strings.Contains(value, secret) {
				t.Fatalf("evidence retained %s", secret)
			}
		}
	}
	for version := range 6 {
		next := *r
		next.Version = fmt.Sprint(version)
		recordSessionEvidence(a, &next, []string{"chat"}, 4000+int64(version))
	}
	var partitions []agentEvidencePartition
	if !getStoreJSON(bkCapabilities, evidenceKey(a.ID), &partitions) || len(partitions) != 4 {
		t.Fatalf("unbounded partitions: %d", len(partitions))
	}
}

func TestAgentNormalSessionFeedsEvidenceWithoutTranscript(t *testing.T) {
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOOM_STEP2_ACP_FIXTURE", "opencode-acp")
	t.Setenv("LOOM_STEP2_ACP_FIXTURE_ROOT", filepath.Join(mustWorkingDir(t), "testdata/agents"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := newRuntimeSessions()
	defer m.shutdownACP()
	a := &acpAdapter{agent: acpAgent{ID: "evidence-fixture", Name: "Fixture", Command: exe, Args: []string{"-test.run=^TestStep2ACPFixtureProcess$"}, Custom: true}, sessions: m,
		session: RuntimeSession{ID: "fixture-discussion", Model: "default", ACPState: ACPState{Workdir: t.TempDir()}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = a.Run(ctx, RuntimeTurn{Messages: []Message{{Role: "user", Content: "PRIVATE_PROMPT"}}}, func(StreamEvent) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	r := agentCompatibility(a.agent)
	if r.Evidence["chat"].ObservedWorking == nil || r.Evidence["stream"].ObservedWorking == nil || r.Evidence["approvals"].ObservedWorking != nil {
		t.Fatalf("session evidence: %+v", r.Evidence)
	}
	for _, value := range allKV(bkCapabilities) {
		for _, private := range []string{"PRIVATE_PROMPT", "Hello from OpenCode", "fixture-discussion", "test.run", "sessionId"} {
			if strings.Contains(value, private) {
				t.Fatalf("evidence retained %s", private)
			}
		}
	}
}

func TestAgentProbeConflictsFailClosed(t *testing.T) {
	testHome(t)
	old := degradedCapabilities
	degradedCapabilities = &capability.State{}
	t.Cleanup(func() { degradedCapabilities = old })
	for _, checks := range [][]capability.Probe{
		{{Capability: "approvals", OK: false}, {Capability: "approvals", OK: true}},
		{{Capability: "approvals", OK: true}, {Capability: "approvals", OK: false}},
	} {
		observeAgentProbe("fixture", acpProbe{CapabilityChecks: checks})
		if !capabilityDisabled("agent:fixture", "approvals") {
			t.Fatal("conflicting success restored failed capability")
		}
	}
}

func TestHarnessUpdateChecksLeaveModelSinksUntouched(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"providers":{}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, modelSinkState, map[string]bool{"pi": true}); err != nil {
		t.Fatal(err)
	}
	a := fakeACPAdapter(t).agent
	a.ID, a.Custom = "pi", true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := refreshHarnessAgentProbe(ctx, a); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("update check mutated Pi model sink: %s (%v)", after, err)
	}
}

func TestAgentEvidenceRemoteInstallationAndTransportIsolation(t *testing.T) {
	testHome(t)
	machine := RemoteMachine{ID: "box", NodeID: "installation-1", OS: "linux", Tools: []RemoteTool{{ID: "codex", Path: "PRIVATE_REMOTE_PATH", Version: "1.0.0"}}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{machine}); err != nil {
		t.Fatal(err)
	}
	a := acpAgent{ID: "custom-box-codex", Machine: "box", Remote: true, Custom: true, Command: "loom", Args: []string{"node-bridge", "box", "codex", nodeBridgeCwd}}
	r := &runtimeCompatibilityRecord{Protocol: "acp", Version: "1.0.0"}
	recordSessionEvidence(a, r, []string{"chat"}, 1000)
	readAgentEvidence(a, r)
	if r.Evidence["chat"].ObservedWorking == nil {
		t.Fatal("missing node observation")
	}
	ssh := a
	ssh.Command, ssh.Args = "ssh", []string{"PRIVATE_HOST"}
	changed := *r
	readAgentEvidence(ssh, &changed)
	if len(changed.Evidence) != 0 {
		t.Fatal("SSH inherited Node evidence")
	}
	machine.NodeID = "installation-2"
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{machine}); err != nil {
		t.Fatal(err)
	}
	readAgentEvidence(a, r)
	if len(r.Evidence) != 0 {
		t.Fatal("replacement node inherited installation evidence")
	}
	for _, value := range allKV(bkCapabilities) {
		if strings.Contains(value, "PRIVATE_REMOTE_PATH") || strings.Contains(value, "PRIVATE_HOST") {
			t.Fatal("private machine data in evidence")
		}
	}
}

func TestAgentEvidenceUsesPrivateVaultStore(t *testing.T) {
	testHome(t)
	previous, _ := currentDEK()
	t.Cleanup(func() {
		if len(previous) > 0 {
			setMemDEK(previous)
		} else {
			clearMemDEK()
		}
	})
	key, err := randBytes(32)
	if err != nil {
		t.Fatal(err)
	}
	setMemDEK(key)
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	a := acpAgent{ID: "vault-fixture", Command: "fixture", Custom: true}
	r := &runtimeCompatibilityRecord{Protocol: "acp", Version: "fixture"}
	recordSessionEvidence(a, r, []string{"chat"}, 1000)
	before := getBytes(bkCapabilities, evidenceKey(a.ID))
	if !looksEncrypted(before) {
		t.Fatal("evidence bypassed vault encryption")
	}
	clearMemDEK()
	recordSessionEvidence(a, r, []string{"chat"}, 2000)
	if string(getBytes(bkCapabilities, evidenceKey(a.ID))) != string(before) {
		t.Fatal("locked observation overwrote evidence")
	}
	readAgentEvidence(a, r)
	if len(r.Evidence) != 0 {
		t.Fatal("locked evidence exposed")
	}
	setMemDEK(key)
	readAgentEvidence(a, r)
	if e := r.Evidence["chat"]; e.ObservedWorking == nil || e.ObservedWorking.Count != 1 {
		t.Fatalf("vault evidence lost: %+v", e)
	}
}
