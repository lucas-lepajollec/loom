package loom

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func resetCodemanQuotas() {
	codemanQuotas.Lock()
	codemanQuotas.loaded, codemanQuotas.samples, codemanQuotas.latest, codemanQuotas.probed = false, nil, quotaSample{}, time.Time{}
	codemanQuotas.Unlock()
}

func TestCodemanQuotasAreSampledAndKeptAsPercentagesOnly(t *testing.T) {
	_, loom := codemanTestServers(t)
	resetCodemanQuotas()
	t.Cleanup(resetCodemanQuotas)
	if err := sealProviderSecret(codemanSecretID, "synthetic-codeman"); err != nil {
		t.Fatal(err)
	}
	resp, out := codemanDo(t, "GET", loom.URL+"/api/codeman/quotas", "", nil)
	if resp.StatusCode != 200 || out["ok"] != true {
		t.Fatalf("quotas: %d %v", resp.StatusCode, out)
	}
	latest, _ := out["latest"].(map[string]any)
	claude, _ := latest["claude"].(map[string]any)
	five, _ := claude["5h"].(map[string]any)
	if five["used"] != 12.0 || five["reset_at"] != 1792714800000.0 {
		t.Fatalf("claude 5h: %v", latest)
	}
	codex, _ := latest["codex"].(map[string]any)
	if w, _ := codex["7d"].(map[string]any); w["used"] != 7.0 {
		t.Fatalf("codex 7d: %v", latest)
	}
	if samples, _ := out["samples"].([]any); len(samples) != 1 {
		t.Fatalf("samples: %v", out["samples"])
	}
	// Seuls pourcentages et remises à zéro sont écrits sur disque.
	b, err := os.ReadFile(codemanQuotaPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved []map[string]any
	if json.Unmarshal(b, &saved) != nil || len(saved) != 1 {
		t.Fatalf("saved: %s", b)
	}
	for k := range saved[0] {
		if k != "at" && k != "claude" && k != "codex" {
			t.Fatalf("unexpected field %q in %s", k, b)
		}
	}
	if info, _ := os.Stat(codemanQuotaPath()); info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestCodemanQuotaHistoryDedupesAndExpires(t *testing.T) {
	testHome(t)
	resetCodemanQuotas()
	t.Cleanup(resetCodemanQuotas)
	s := parsePlanUsage(map[string]any{"fiveHour": map[string]any{"usedPercentage": 150.0}, "sevenDay": map[string]any{"usedPercentage": "x"}})
	if s.Claude["5h"].Used != 100 || len(s.Claude) != 1 || s.Codex != nil {
		t.Fatalf("parse: %+v", s)
	}
	if !parsePlanUsage(nil).empty() || !parsePlanUsage(map[string]any{"codex": nil}).empty() {
		t.Fatal("an empty reading must stay empty")
	}
	base := time.Now().Add(-20 * 24 * time.Hour)
	recordQuotaSample(s, base)
	recordQuotaSample(s, base.Add(5*time.Minute)) // inchangé : ignoré
	recordQuotaSample(s, base.Add(40*time.Minute))
	if n := len(codemanQuotas.samples); n != 2 {
		t.Fatalf("dedupe: %d samples", n)
	}
	recordQuotaSample(parsePlanUsage(map[string]any{"fiveHour": map[string]any{"usedPercentage": 20.0}}), time.Now())
	if n := len(codemanQuotas.samples); n != 1 {
		t.Fatalf("expiry kept %d samples", n)
	}
	recordQuotaSample(quotaSample{}, time.Now())
	if codemanQuotas.latest.Claude["5h"].Used != 20 {
		t.Fatal("an empty reading must not replace the latest one")
	}
}
