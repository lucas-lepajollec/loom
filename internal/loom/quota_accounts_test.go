package loom

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeQuotaFake(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Each harness is read through its own CLI: Codex's app server (limits and
// reset credits), Antigravity's /quota and /credits, Claude Code's cached
// usage. Only this machine's CLIs are found through PATH.
func TestQuotaAccountReaders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures")
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "codex.log")
	writeQuotaFake(t, bin, "codex", `while read l; do
  case "$l" in
    *'"initialize"'*) echo '{"id":1,"result":{}}';;
    *rateLimits/read*) echo '{"id":2,"result":{"rateLimits":{"planType":"pro","primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1000},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":2000}},"rateLimitResetCredits":{"availableCount":2,"credits":[{"id":"x","resetType":"codexRateLimits","status":"available","title":"Full reset","grantedAt":100,"expiresAt":200},{"id":"y","resetType":"codexRateLimits","status":"redeemed","title":"Full reset","grantedAt":1}]}}}';;
    *consume*) echo "$l" > "`+log+`"; echo '{"id":2,"result":{"outcome":"reset"}}';;
  esac
done
`)
	writeQuotaFake(t, bin, "agy", `case "$2" in
  /quota) echo '{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":0.8,"reset_time":"2026-11-19T16:54:48Z"},{"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.95,"reset_time":"2026-11-26T11:54:48Z"}]}]}}}';;
  /credits) echo '{"status":"SUCCESS","command":{"name":"credits","data":{"remaining_credits":3}}}';;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	// Les dossiers d'installation habituels sont cherchés sous HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	codex := readCodexAccount(context.Background())
	if codex == nil || codex.Plan != "pro" || len(codex.Limits) != 2 || codex.Limits[0].Window != "5h" || codex.Limits[1].Window != "7d" || codex.Limits[1].ResetAt != 2000000 {
		t.Fatalf("codex limits: %+v", codex)
	}
	if r := codex.Resets; r == nil || r.Available != 2 || !r.Exact || r.Use != "direct" || len(r.Credits) != 1 || r.Credits[0].ID != "x" || r.Credits[0].ExpiresAt != 200000 {
		t.Fatalf("codex resets: %+v", codex.Resets)
	}
	if out, err := consumeCodexReset(context.Background(), "x"); err != nil || out != "reset" {
		t.Fatalf("consume: %q %v", out, err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), `"creditId":"x"`) || !strings.Contains(string(b), `"idempotencyKey":"loom-`) {
		t.Fatalf("consume request: %s", b)
	}

	agy := readAntigravityAccount(context.Background())
	if agy == nil || len(agy.Limits) != 2 || agy.Limits[0].ID != "gemini-5h" || agy.Limits[0].Group != "Gemini Models" || int(agy.Limits[0].Used+0.5) != 20 || agy.Limits[1].Window != "7d" || agy.Credits == nil || *agy.Credits != 3 {
		t.Fatalf("antigravity: %+v", agy)
	}

	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"cachedUsageUtilization":{"fetchedAtMs":5,"utilization":{"limits":[{"kind":"session","percent":69,"resets_at":"2026-11-19T14:10:00.5+00:00"},{"kind":"weekly_all","percent":38,"resets_at":"2026-11-25T12:00:00+00:00"},{"kind":"weekly_opus","percent":10}]}},"unusedLimitResetStatusCache":{"holdsUnusedReset":true,"checkedAt":7,"validUntil":9}}`), 0o600)
	claude := readClaudeAccount(context.Background())
	if claude == nil || len(claude.Limits) != 3 || claude.Limits[0].Window != "5h" || claude.Limits[2].Label != "Opus" || claude.CheckedAt != 5 {
		t.Fatalf("claude limits: %+v", claude)
	}
	if r := claude.Resets; r == nil || r.Available != 1 || r.Exact || r.Use != "cli" || !r.Stale || r.CheckedAt != 7 {
		t.Fatalf("claude resets: %+v", claude.Resets)
	}

	s := addAccountsToSample(quotaSample{}, []quotaAccount{*claude, *codex, *agy})
	if s.Claude["5h"].Used != 69 || s.Codex["7d"].Used != 40 || len(s.Antigravity) != 2 {
		t.Fatalf("sample: %+v", s)
	}
}

// A service started without the session's PATH still finds a CLI installed
// in ~/.local/bin.
func TestQuotaCLIFoundOutsidePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	writeQuotaFake(t, local, "agy", `[ "$2" = /quota ] && echo '{"command":{"data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-5h","window":"5h","remaining_fraction":0.5}]}]}}}'
`)
	t.Setenv("PATH", "/usr/bin:/bin")
	if a := readAntigravityAccount(context.Background()); a == nil || len(a.Limits) != 1 || a.Limits[0].Used != 50 {
		t.Fatalf("antigravity outside PATH: %+v", a)
	}
}
