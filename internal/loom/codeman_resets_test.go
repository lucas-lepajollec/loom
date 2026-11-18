package loom

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The reset credits come from Codex's app server (only available ones are
// kept, ids dropped) and Claude Code's cached hint.
func TestQuotaResetReaders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
read l1
echo '{"id":1,"result":{}}'
read l2
read l3
echo '{"id":2,"result":{"rateLimitResetCredits":{"availableCount":2,"credits":[{"id":"x","resetType":"codexRateLimits","status":"available","title":"Full reset","grantedAt":100,"expiresAt":200},{"id":"y","resetType":"codexRateLimits","status":"redeemed","title":"Full reset"},{"id":"z","resetType":"codexRateLimits","status":"available","title":"Full reset","grantedAt":150,"expiresAt":250}]}}}'
sleep 1
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	codex := readCodexResetCredits(context.Background())
	if codex == nil || codex.Available != 2 || len(codex.Credits) != 2 || codex.Credits[0].ExpiresAt != 200000 {
		t.Fatalf("codex credits: %+v", codex)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"unusedLimitResetStatusCache":{"holdsUnusedReset":true,"checkedAt":5,"validUntil":9}}`), 0o600)
	if c := readClaudeResetHint(); c == nil || !c.Available || c.CheckedAt != 5 {
		t.Fatalf("claude hint: %+v", c)
	}
}
