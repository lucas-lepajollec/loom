package resources

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMCPFileCodecPreservesExtensionsAndRedactsDecoderErrors(t *testing.T) {
	top, entries, servers, err := ParseMCPFile([]byte(`{"version":17,"vendor":{"preserve":true},"mcpServers":{"local":{"command":"node","custom":{"flag":true}},"remote":{"url":"https://example.test/mcp"}}}`))
	if err != nil || !servers["local"].Enabled {
		t.Fatalf("default enabled: %v", err)
	}
	cfg := servers["local"]
	cfg.Enabled = false
	servers["local"] = cfg
	delete(servers, "remote")
	data, err := EncodeMCPFile(top, entries, servers)
	if err != nil {
		t.Fatal(err)
	}
	next, raw, got, err := ParseMCPFile(data)
	if err != nil || string(next["version"]) != "17" || next["vendor"] == nil || !bytes.Contains(raw["local"], []byte(`"custom"`)) || got["local"].Enabled || len(got) != 1 {
		t.Fatalf("round trip: %s %v", data, err)
	}
	for _, bad := range []string{`{"private-field":`, `{"mcpServers":{"x":{"command":{"private-field":"secret"}}}}`} {
		_, _, _, err := ParseMCPFile([]byte(bad))
		if err == nil || bytes.Contains([]byte(err.Error()), []byte("private-field")) || bytes.Contains([]byte(err.Error()), []byte("secret")) {
			t.Fatalf("decoder error: %v", err)
		}
	}
	encoded, _ := json.Marshal(MCPServerConfig{Command: "node", Enabled: false})
	if string(encoded) != `{"command":"node","enabled":false}` {
		t.Fatalf("wire: %s", encoded)
	}
}
