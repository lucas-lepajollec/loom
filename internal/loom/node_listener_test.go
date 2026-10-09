package loom

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNodeLANSelectionNeverUsesPublicAddress(t *testing.T) {
	for _, tc := range []struct{ route, addresses, want string }{
		{"1.1.1.1 via 192.168.1.1 src 192.168.1.20", "10.0.0.2", "192.168.1.20"},
		{"1.1.1.1 src 203.0.113.8", "203.0.113.8 100.64.1.2 192.168.1.5", "100.64.1.2"},
		{"", "172.31.4.2", "172.31.4.2"},
		{"src 172.32.0.1", "100.128.0.1 127.0.0.1 169.254.1.2 ::1", ""},
		{"src 8.8.8.8", "203.0.113.4", ""},
	} {
		got, err := nodeLANAddress(tc.route, tc.addresses)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("selection %q %q: %q %v", tc.route, tc.addresses, got, err)
		}
	}
	for _, invalid := range []string{"192.168.1.1:0", "192.168.1.1:65536", "lan:bad", "foo:2511"} {
		if _, err := resolveNodeListener(invalid); err == nil {
			t.Fatal("invalid listener accepted", invalid)
		}
	}
	if got, err := resolveNodeListener("local"); err != nil || got != defaultNodeListen {
		t.Fatal(got, err)
	}
}
func capturePairDetails(t *testing.T, ifUnpaired, asJSON bool) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	err = printNodePairDetails("192.168.1.20:2511", ifUnpaired, asJSON)
	os.Stdout = previous
	writer.Close()
	defer reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestNodeInstallerPairSummaryFreshAndAlreadyPaired(t *testing.T) {
	mux, _ := pairTestNode(t)
	old := pairTestCode(t, false, time.Now())
	output := capturePairDetails(t, true, true)
	var details nodePairDetails
	if err := json.Unmarshal([]byte(output), &details); err != nil {
		t.Fatal(err)
	}
	if details.Address != "http://192.168.1.20:2511" || details.Code == "" || details.Expires == "" || details.Code == old {
		t.Fatal(output)
	}
	if w := pairTestExchange(mux, old, "main", "192.168.1.2:1", false); w.Code != 401 {
		t.Fatal("old code still valid")
	}
	if w := pairTestExchange(mux, details.Code, "main", "192.168.1.2:1", false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	before, _ := getBytesErr(bkState, nodePairingKey)
	output = capturePairDetails(t, true, false)
	if !strings.Contains(output, "already paired with ") || strings.Contains(output, "Pairing code:") || !strings.Contains(output, "In Loom: Machines › Add a machine") {
		t.Fatal(output)
	}
	after, _ := getBytesErr(bkState, nodePairingKey)
	if string(after) != string(before) {
		t.Fatal("installer changed pairing association")
	}
	// Explicit pair remains available for repairing/replacing an association.
	output = capturePairDetails(t, false, true)
	if !strings.Contains(output, `"code":`) {
		t.Fatal("explicit repair code missing")
	}
}

func TestNodeListenPreservesInstalledExecutableAndRollsBack(t *testing.T) {
	testHome(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	config, _ := os.UserConfigDir()
	unitDir := filepath.Join(config, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(unitDir, "loom-node.service")
	original, err := engineWorkerUnit("/custom/node/loom", LoomHome(), defaultNodeListen)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	_ = putStr(bkState, "node_listener", defaultNodeListen)
	bin := t.TempDir()
	flag := filepath.Join(bin, "failed-once")
	script := "#!/bin/sh\nif [ \"$2\" = restart ] && [ -n \"$FAIL_NODE_RESTART\" ] && [ ! -f " + shellQuote(flag) + " ]; then touch " + shellQuote(flag) + "; echo 'exact restart failure' >&2; exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := changeNodeListener(LoomHome(), "192.168.1.20:2511"); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.ReadFile(path)
	if !strings.Contains(string(previous), `ExecStart="/custom/node/loom"`) || !strings.Contains(string(previous), `--listen "192.168.1.20:2511"`) {
		t.Fatal("listener change replaced executable", string(previous))
	}
	t.Setenv("FAIL_NODE_RESTART", "1")
	err = changeNodeListener(LoomHome(), "100.64.1.2:2511")
	if err == nil || !strings.Contains(err.Error(), "exact restart failure") {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(previous) || getStr(bkState, "node_listener") != "192.168.1.20:2511" {
		t.Fatal("failed listener change not restored")
	}
}
