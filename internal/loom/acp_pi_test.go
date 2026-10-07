package loom

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPiNoticesAndJournalError(t *testing.T) {
	for _, text := range []string{"Retrying (attempt 1/3, waiting 2s)...", "Retrying (attempt 1/3, waiting 2s)...Retrying (attempt 2/3, waiting 4s)...Retry finished, resuming.", "Retry finished, resuming."} {
		if !piRetryNotice(text) {
			t.Fatalf("retry notice not recognised: %q", text)
		}
	}
	if piRetryNotice("Retrying the build fixed it.") {
		t.Fatal("an answer was taken for a retry notice")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".pi", "agent", "sessions", "--work--")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Minute)
	journal := `{"type":"message","message":{"role":"user","content":"hi"}}
{"type":"message","message":{"role":"assistant","stopReason":"error","errorMessage":"502 engine unavailable; start an engine or load a model"}}
`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := piTurnError(since); got != "502 engine unavailable; start an engine or load a model" {
		t.Fatalf("%q", got)
	}
	if got := piTurnError(time.Now().Add(time.Minute)); got != "" {
		t.Fatalf("an older journal was read: %q", got)
	}
}
