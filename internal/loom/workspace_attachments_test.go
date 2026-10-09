package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeTurnTextPerRoute(t *testing.T) {
	testHome(t)
	dir, err := uploadsDir()
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("notes.md", []byte("# Notes\nhello"))
	write("pic.png", []byte{0x89, 'P', 'N', 'G', 0xff, 0x00})
	write("big.txt", []byte(strings.Repeat("é", maxMessageBytes)))

	cloud := RuntimeSession{RuntimeID: "openai-compatible"}
	got, err := composeTurnText(cloud, "Résume", []string{"notes.md", "pic.png"})
	if err != nil || !strings.Contains(got, "<loom-file name=\"notes.md\">\n# Notes\nhello\n</loom-file>") || !strings.Contains(got, `omitted="binary"`) {
		t.Fatalf("cloud inline: %v\n%s", err, got)
	}
	got, err = composeTurnText(cloud, "Lis", []string{"big.txt"})
	if err != nil || len(got) > maxMessageBytes || !strings.Contains(got, "truncated=") {
		t.Fatalf("cloud truncation: %v len=%d", err, len(got))
	}
	agent := RuntimeSession{RuntimeID: "pi"}
	got, err = composeTurnText(agent, "", []string{"notes.md"})
	if err != nil || !strings.HasPrefix(got, "Read the attached file.") || !strings.Contains(got, `path="`+filepath.Join(dir, "notes.md")+`"`) || strings.Contains(got, "hello") {
		t.Fatalf("agent paths: %v\n%s", err, got)
	}
	if _, err := composeTurnText(cloud, "x", []string{"missing.txt"}); err == nil {
		t.Fatal("missing upload accepted")
	}
	if got, _ := composeTurnText(cloud, "same", nil); got != "same" {
		t.Fatal("text without files changed")
	}
}

func TestDiscussionTitleIgnoresAttachedFiles(t *testing.T) {
	if got := discussionTitle("Résume ce fichier\n\n<loom-file name=\"a.md\">\nx\n</loom-file>"); strings.Contains(got, "loom-file") || !strings.HasPrefix(got, "Résume") {
		t.Fatal(got)
	}
}

func TestCloudToolsOnlyRunEnabledTools(t *testing.T) {
	ctx := t.Context()
	if _, err := (cloudTools{web: false, mcp: false}).Execute(ctx, "web_search", map[string]any{"query": "x"}); err == nil {
		t.Fatal("disabled web_search ran")
	}
	if _, err := (cloudTools{web: true}).Execute(ctx, "mcp__srv__tool", nil); err == nil {
		t.Fatal("MCP tool ran while MCP is off")
	}
	defs := (cloudTools{web: true}).Definitions().([]Tool)
	if len(defs) != 1 || defs[0].Function.Name != "web_search" {
		t.Fatalf("definitions: %+v", defs)
	}
}
