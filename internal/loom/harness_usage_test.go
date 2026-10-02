package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func harnessUsageFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "harness_usage", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func usageFixtureTime() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
func TestHarnessClaudeQuota(t *testing.T) {
	raw := harnessUsageFixture(t, "claude_usage.txt")
	windows, note := parseClaudeUsage(raw, usageFixtureTime())
	if len(windows) != 4 || note != raw || *windows[0].Remaining != 34 || *windows[1].Remaining != 16 || *windows[2].Remaining != 0 || windows[3].ResetAt != nil {
		t.Fatalf("%+v %q", windows, note)
	}
	if *windows[0].ResetAt != time.Date(2026, 10, 2, 14, 19, 0, 0, time.UTC).Unix() {
		t.Fatal("zone ignored")
	}
	windows, note = parseClaudeUsage("Current session: 25.5% used · resets Jan 2, 3:04pm (Europe/Paris)", usageFixtureTime())
	if note != "" || *windows[0].Remaining != 74.5 || time.Unix(*windows[0].ResetAt, 0).Year() != 2027 {
		t.Fatal("year rollover", windows, note)
	}
	for _, raw := range []string{"unknown", "Window: 20% used · resets nonsense (UTC)", "Window: 101% used · resets Oct 7, 1:59pm (UTC)"} {
		_, note := parseClaudeUsage(raw, usageFixtureTime())
		if note != raw {
			t.Fatal("unknown discarded")
		}
	}
	b, err := json.Marshal(windows[0])
	if err != nil || !strings.Contains(string(b), `"remaining":0.745`) || !strings.Contains(string(b), `"remaining_percent":74.5`) {
		t.Fatal(string(b), err)
	}
	b, _ = json.Marshal(QuotaWindow{})
	if !strings.Contains(string(b), `"remaining":null`) {
		t.Fatal(string(b))
	}
}
func TestHarnessHermesQuota(t *testing.T) {
	windows, note := parseHermesQuota(harnessUsageFixture(t, "hermes_usage.txt"))
	if len(windows) != 2 || note != "" || windows[0].Group != "openai-codex (Plus)" || *windows[0].Remaining != 53 || *windows[0].ResetAt != time.Date(2026, 10, 7, 14, 47, 0, 0, time.UTC).Unix() {
		t.Fatalf("%+v %q", windows, note)
	}
	raw := "Provider: unknown\nWeekly: 20% remaining • resets someday\nodd line"
	windows, note = parseHermesQuota(raw)
	if len(windows) != 1 || windows[0].ResetAt != nil || note != raw {
		t.Fatal(windows, note)
	}
}
func TestHarnessStatsFixtures(t *testing.T) {
	for _, tc := range []struct {
		harness, file                string
		sessions                     int
		input, output, cached, total int64
		cost                         float64
		modelTokens                  int64
	}{
		{"hermes", "hermes_insights.txt", 12, 1200, 300, 0, 1500, .42, 200},
		{"opencode", "opencode_stats.txt", 3, 1200, 300, 500, 2100, 1.25, 700},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			q := HarnessUsage{ByModel: []HarnessModelUsage{}}
			err := parseHarnessStats(harnessUsageFixture(t, tc.file), tc.harness, &q)
			if err != nil || q.Sessions != tc.sessions || q.InputTokens != tc.input || q.OutputTokens != tc.output || q.CacheReadTokens != tc.cached || q.TotalTokens != tc.total || q.CostUSD == nil || *q.CostUSD != tc.cost || len(q.ByModel) != 2 || q.ByModel[0].Tokens != tc.modelTokens {
				t.Fatalf("%+v %v", q, err)
			}
			if tc.harness == "opencode" && q.ByModel[0].Sessions != nil {
				t.Fatal("message counts became sessions")
			}
		})
	}
	q := HarnessUsage{}
	raw := "Sessions: 1\nInput: 10\nOutput: 2\nCache Read: 3\nTotal Cost: unknown\nModels\nModel | Input | Output | Cache Read\nmodel-test | 10 | 2 | 3"
	if err := parseHarnessStats(raw, "opencode", &q); err != nil || q.CostUSD != nil || q.TotalTokens != 15 || len(q.ByModel) != 1 || q.ByModel[0].Tokens != 15 {
		t.Fatalf("%+v %v", q, err)
	}
	if parseHarnessStats("unknown", "hermes", &HarnessUsage{}) == nil {
		t.Fatal("unknown format accepted as zero")
	}
}
func TestHarnessNativeJSONL(t *testing.T) {
	for _, tc := range []struct {
		harness, file                string
		days                         int
		input, output, cached, total int64
		models                       int
	}{
		{"claude-code", "claude.jsonl", 7, 100, 15, 40, 160, 1},
		{"claude-code", "claude.jsonl", 30, 120, 20, 44, 190, 2},
		{"codex", "codex.jsonl", 7, 150, 50, 100, 200, 2},
		{"codex", "codex.jsonl", 30, 230, 70, 150, 300, 2},
		{"pi", "pi.jsonl", 7, 53, 21, 30, 109, 1},
		{"pi", "pi.jsonl", 30, 63, 23, 33, 125, 2},
	} {
		t.Run(tc.harness+"/"+time.Duration(tc.days).String(), func(t *testing.T) {
			now := usageFixtureTime()
			s, err := parseHarnessJSONL(context.Background(), strings.NewReader(harnessUsageFixture(t, tc.file)), tc.harness, now.AddDate(0, 0, -tc.days), now)
			q := HarnessUsage{ByModel: []HarnessModelUsage{}}
			mergeNativeSession(&q, s)
			if err != nil || q.Sessions != 1 || q.InputTokens != tc.input || q.OutputTokens != tc.output || q.CacheReadTokens != tc.cached || q.TotalTokens != tc.total || len(q.ByModel) != tc.models || q.CostUSD != nil {
				t.Fatalf("%+v %v", q, err)
			}
			var total int64
			for _, m := range q.ByModel {
				total += m.Tokens
				if m.Sessions == nil || *m.Sessions != 1 {
					t.Fatal("model sessions")
				}
			}
			if total != q.TotalTokens {
				t.Fatal("model attribution", q)
			}
			b, _ := json.Marshal(q)
			if strings.Contains(string(b), "synthetic private text") {
				t.Fatal("content retained")
			}
		})
	}
	raw := `{"type":"message","timestamp":"2026-09-30T10:00:00Z","message":{"role":"assistant","model":"pi","usage":{"input":0,"output":0,"totalTokens":0,"cost":{"total":0}}}}`
	s, err := parseHarnessJSONL(context.Background(), strings.NewReader(raw), "pi", usageFixtureTime().AddDate(0, 0, -7), usageFixtureTime())
	if err != nil || !s.counted || s.cost == nil || *s.cost != 0 || s.costUnknown {
		t.Fatal("real zero lost", s, err)
	}
	raw = `{"type":"message","timestamp":"2026-09-30T10:00:00Z","message":{"role":"assistant","usage":{"input":-1,"output":2}}}`
	s, err = parseHarnessJSONL(context.Background(), strings.NewReader(raw), "pi", usageFixtureTime().AddDate(0, 0, -7), usageFixtureTime())
	if err == nil || s.counted {
		t.Fatal("invalid tokens accepted")
	}
}
func TestHarnessNativeFileBounds(t *testing.T) {
	root := t.TempDir()
	now := usageFixtureTime()
	raw := harnessUsageFixture(t, "claude.jsonl")
	for _, name := range []string{"recent.jsonl", "old.jsonl"} {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := now
		if name == "old.jsonl" {
			stamp = now.AddDate(0, 0, -31)
		}
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	q := HarnessUsage{}
	if err := readHarnessSessionFiles(context.Background(), root, "claude-code", now, 7, &q); err != nil || q.Sessions != 1 || q.TotalTokens != 160 {
		t.Fatal(q, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if readHarnessSessionFiles(ctx, root, "claude-code", now, 7, &HarnessUsage{}) == nil {
		t.Fatal("cancellation ignored")
	}
	if readHarnessSessionFiles(context.Background(), filepath.Join(root, "missing"), "pi", now, 7, &HarnessUsage{}) == nil {
		t.Fatal("missing source became zero")
	}
	root = t.TempDir()
	for i := 0; i <= nativeUsageMaxFiles; i++ {
		p := filepath.Join(root, time.Duration(i).String()+".jsonl")
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := readHarnessSessionFiles(context.Background(), root, "pi", time.Now(), 7, &HarnessUsage{})
	if err == nil || !strings.Contains(err.Error(), "2000") {
		t.Fatal("file limit ignored", err)
	}
}
func TestHarnessQuotaCapabilities(t *testing.T) {
	testHome(t)
	for _, tc := range []struct {
		agent acpAgent
		quota bool
	}{
		{acpAgent{ID: "claude-code"}, true}, {acpAgent{ID: "hermes"}, true},
		{acpAgent{ID: "custom-lab-hermes", Machine: "lab", Remote: true, Custom: true}, true},
		{acpAgent{ID: "custom-lab-claude-code", Machine: "lab", Remote: true, Custom: true}, true},
		{acpAgent{ID: "custom-pretend-hermes", Custom: true}, false},
		{acpAgent{ID: "pi"}, false}, {acpAgent{ID: "opencode"}, false}, {acpAgent{ID: "gemini"}, false},
	} {
		a := &acpAdapter{agent: tc.agent}
		if hasRuntimeCapability(a.Descriptor(), "quota") != tc.quota {
			t.Fatal(tc)
		}
	}
	a := hermesUsageAdapter{}
	if !hasRuntimeCapability(a.Descriptor(), "quota") || hasRuntimeCapability(a.Descriptor(), "chat") {
		t.Fatal("Hermes observation enabled chat")
	}
	if _, err := a.Run(context.Background(), RuntimeTurn{}, nil); err == nil {
		t.Fatal("preview executed")
	}
}
func resetNativeUsageCache(t *testing.T) {
	t.Helper()
	nativeUsageCache.Lock()
	old := nativeUsageCache.items
	nativeUsageCache.items = map[string]HarnessUsage{}
	nativeUsageCache.Unlock()
	t.Cleanup(func() { nativeUsageCache.Lock(); nativeUsageCache.items = old; nativeUsageCache.Unlock() })
}
func TestHarnessNativeHTTP(t *testing.T) {
	testHome(t)
	resetNativeUsageCache(t)
	isolateRuntimeRegistry(t, &acpAdapter{agent: acpAgent{ID: "gemini"}}, &acpAdapter{agent: acpAgent{ID: "antigravity"}})
	if err := storeWebKey("fixture-key"); err != nil {
		t.Fatal(err)
	}
	handlers := map[string]http.HandlerFunc{"/api/usage/native": handleNativeUsage, "/api/usage/native/refresh": handleNativeUsageRefresh}
	for _, tc := range []struct {
		method, path, body, key string
		status                  int
	}{
		{"GET", "/api/usage/native", "", "", 401},
		{"GET", "/api/usage/native?days=7", "", "fixture-key", 200},
		{"GET", "/api/usage/native?days=30", "", "fixture-key", 200},
		{"GET", "/api/usage/native?days=8", "", "fixture-key", 400},
		{"GET", "/api/usage/native?days=oops", "", "fixture-key", 400},
		{"POST", "/api/usage/native", "{}", "fixture-key", 405},
		{"POST", "/api/usage/native/refresh", `{"runtime_id":"gemini","days":7}`, "", 401},
		{"POST", "/api/usage/native/refresh", `{"runtime_id":"gemini","days":7}`, "fixture-key", 200},
		{"POST", "/api/usage/native/refresh", `{"runtime_id":"missing"}`, "fixture-key", 404},
		{"POST", "/api/usage/native/refresh", `{"runtime_id":"gemini","days":1}`, "fixture-key", 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.key != "" {
			r.Header.Set("Authorization", "Bearer "+tc.key)
		}
		if tc.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		requireWebAuth(handlers[r.URL.Path])(w, r)
		if w.Code != tc.status || (tc.status != 401 && w.Header().Get("Cache-Control") != "no-store") {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.status == 200 && tc.method == "GET" {
			var result struct {
				OK        bool           `json:"ok"`
				Harnesses []HarnessUsage `json:"harnesses"`
			}
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.OK || len(result.Harnesses) != 2 {
				t.Fatal(w.Body.String())
			}
			for _, q := range result.Harnesses {
				if q.Error != "non disponible" || q.CostUSD != nil || q.FetchedAt == 0 {
					t.Fatal(q)
				}
			}
		}
	}
}
func TestHarnessNativeCacheAndCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	testHome(t)
	resetNativeUsageCache(t)
	isolateRuntimeRegistry(t, hermesUsageAdapter{})
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(count) + "\ncat <<'STATS'\n" + harnessUsageFixture(t, "hermes_insights.txt") + "STATS\n"
	if err := os.WriteFile(filepath.Join(dir, "hermes"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := cachedHarnessUsage(context.Background(), "hermes", 7, false)
			if q.Error != "" || q.TotalTokens != 1500 {
				t.Errorf("%+v", q)
			}
		}()
	}
	wg.Wait()
	calls, _ := os.ReadFile(count)
	if strings.Count(string(calls), "insights --days 7") != 1 {
		t.Fatal("cache/single flight", string(calls))
	}
	cachedHarnessUsage(context.Background(), "hermes", 30, false)
	cachedHarnessUsage(context.Background(), "hermes", 7, true)
	nativeUsageCache.Lock()
	q := nativeUsageCache.items["hermes:7"]
	q.FetchedAt = time.Now().Add(-6 * time.Minute).Unix()
	nativeUsageCache.items["hermes:7"] = q
	nativeUsageCache.Unlock()
	cachedHarnessUsage(context.Background(), "hermes", 7, false)
	calls, _ = os.ReadFile(count)
	if strings.Count(string(calls), "insights --days 7") != 3 || strings.Count(string(calls), "insights --days 30") != 1 {
		t.Fatal("refresh/expiry/window cache", string(calls))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := harnessUsageCommand(ctx, acpAgent{ID: "hermes"}, []string{"hermes", "insights", "--days", "7"}); err == nil {
		t.Fatal("command cancellation ignored")
	}
}

func TestHarnessRemoteHermesReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX SSH fixture")
	}
	testHome(t)
	dir := t.TempDir()
	captured := filepath.Join(dir, "script")
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(args) + "\ncat > " + shellQuote(captured) + "\nif /bin/grep -q 'exec hermes usage' " + shellQuote(captured) + "; then\ncat <<'QUOTA'\n" + harnessUsageFixture(t, "hermes_usage.txt") + "QUOTA\nelse\ncat <<'STATS'\n" + harnessUsageFixture(t, "hermes_insights.txt") + "STATS\nfi\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	sshDir := filepath.Join(LoomHome(), "ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"loom_ed25519", "loom_ed25519.pub"} {
		if err := os.WriteFile(filepath.Join(sshDir, name), []byte("synthetic-test-key"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	machine := RemoteMachine{ID: "lab", Name: "Lab", Host: "test.invalid", User: "fixture", Port: 2222, Tools: []RemoteTool{{ID: "hermes", Path: "/opt/test tools/hermes"}}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{machine}); err != nil {
		t.Fatal(err)
	}
	agent := acpAgent{ID: "custom-lab-hermes", Name: "Hermes", Machine: "lab", Remote: true, Custom: true, Command: "ssh"}
	adapter := &acpAdapter{agent: agent}
	isolateRuntimeRegistry(t, adapter)
	quota, err := adapter.Quota(context.Background())
	if err != nil || quota.RuntimeID != agent.ID || len(quota.Windows) != 2 {
		t.Fatal(quota, err)
	}
	q := readNativeHarnessUsage(context.Background(), agent.ID, 30)
	if q.Error != "" || q.TotalTokens != 1500 || q.RuntimeID != agent.ID || !strings.Contains(q.Source, "SSH") {
		t.Fatal(q)
	}
	body, _ := os.ReadFile(captured)
	argv, _ := os.ReadFile(args)
	if !strings.Contains(string(body), remotePathPreamble) || !strings.Contains(string(body), "exec hermes insights --days 30") || !strings.Contains(string(body), "'/opt/test tools'") || !strings.Contains(string(argv), "fixture@test.invalid") || !strings.Contains(string(argv), "2222") || !strings.Contains(string(argv), "BatchMode=yes") {
		t.Fatal("remote helpers not used", string(body), string(argv))
	}
}
func TestHarnessClaudeQuotaCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	result, _ := json.Marshal(map[string]string{"result": harnessUsageFixture(t, "claude_usage.txt")})
	write := func(output string) {
		t.Helper()
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(args) + "\ncat <<'JSON'\n" + output + "\nJSON\n"
		if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	write(string(result))
	q, err := (&acpAdapter{agent: acpAgent{ID: "claude-code", Name: "Claude Code"}}).Quota(context.Background())
	if err != nil || len(q.Windows) != 4 || q.Note == "" {
		t.Fatal(q, err)
	}
	argv, _ := os.ReadFile(args)
	if string(argv) != "-p\n/usage\n--output-format\njson\n--no-session-persistence\n" {
		t.Fatal("quota argv", string(argv))
	}
	write("odd native response")
	q, err = readHarnessQuota(context.Background(), acpAgent{ID: "claude-code"})
	if err != nil || q.Note != "odd native response\n" || len(q.Windows) != 0 {
		t.Fatal("raw format lost", q, err)
	}
	var out usageOutput
	if _, err := out.Write(make([]byte, (2<<20)+1)); err == nil {
		t.Fatal("output bound ignored")
	}
}

func TestHermesInsightsTwoColumns(t *testing.T) {
	raw := "  📊 Overview\n  Sessions:          26            Messages:        1,869\n  Input tokens:      2,656,262     Output tokens:   466,039\n  Total tokens:      56,172,925\n\n  💰 Cost\n  Estimated:          ~$0.0077\n  Included:           24 session(s)\n\n  🤖 Models Used\n  Model                          Sessions       Tokens\n  gpt-5.6-sol                          23   56,127,955\n  mimo-v2.6-pro                         1       17,439\n\n  📱 Platforms\n  Platform       Sessions   Messages         Tokens\n  tui                  23      1,861     50,388,168\n"
	q := HarnessUsage{}
	if err := parseHarnessStats(raw, "hermes", &q); err != nil || q.Sessions != 26 || q.InputTokens != 2656262 || q.OutputTokens != 466039 || q.TotalTokens != 56172925 || q.CostUSD == nil || len(q.ByModel) != 2 || q.ByModel[0].Tokens != 56127955 {
		t.Fatalf("%+v %v", q, err)
	}
}
