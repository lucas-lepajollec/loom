package loom

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func pairTestNode(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	testHome(t)
	if err := initEngineWorker("", ""); err != nil {
		t.Fatal(err)
	}
	token, err := readNodeToken()
	if err != nil {
		t.Fatal(err)
	}
	return newEngineWorkerMux(token), token
}

func pairTestRequest(code, id, name, remote string, force bool) *http.Request {
	req := nodePairRequest{Code: code, Force: force}
	req.Main.ID, req.Main.Name, req.Main.Version = id, name, "fixture"
	body, _ := json.Marshal(req)
	r := httptest.NewRequest("POST", "/api/node/pair", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote
	return r
}

func pairTestExchange(mux http.Handler, code, id, remote string, force bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, pairTestRequest(code, id, "Main "+id, remote, force))
	return w
}

func pairTestCode(t *testing.T, first bool, now time.Time) string {
	t.Helper()
	code, err := issueNodePairCode(first, now)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestNodePairCodeFormatHashAndFirstStart(t *testing.T) {
	pairTestNode(t)
	format := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)
	for range 100 {
		code, err := generatePairCode()
		if err != nil || !format.MatchString(code) {
			t.Fatalf("bad code format %q: %v", code, err)
		}
	}
	now := time.Now()
	code := pairTestCode(t, true, now)
	if code == "" || pairTestCode(t, true, now.Add(11*time.Minute)) != "" {
		t.Fatal("first-start code was repeated")
	}
	raw := getBytes(bkState, nodePairingKey)
	if strings.Contains(string(raw), code) || strings.Contains(string(raw), normalizePairCode(code)) {
		t.Fatal("plaintext code persisted")
	}
	var state nodePairState
	if json.Unmarshal(raw, &state) != nil || len(state.Hash) != 32 || state.Expires != now.Add(10*time.Minute).UnixMilli() {
		t.Fatal("hash/expiry missing")
	}
	newCode := pairTestCode(t, false, now)
	if newCode == code || pairTestCode(t, true, now) != "" {
		t.Fatal("explicit pair did not replace/suppress startup code")
	}
}

func TestNodePairExchangeSingleUseAndInfo(t *testing.T) {
	mux, token := pairTestNode(t)
	code := pairTestCode(t, false, time.Now())
	w := pairTestExchange(mux, strings.ToLower(strings.ReplaceAll(code, "-", "")), "one", "192.168.1.2:1234", false)
	var exchange nodePairExchange
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &exchange) != nil {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	if exchange.MachineToken != token || exchange.InferenceKey != readAPIKey() || exchange.Node.Handshake != 1 || !hasEngineModule(exchange.Node.Modules) {
		t.Fatal("wrong exchange")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentials can be cached")
	}
	var state nodePairState
	if !getJSON(bkState, nodePairingKey, &state) || state.Main == nil || state.Main.ID != "one" || state.Main.Name != "Main one" || state.Main.PairedAt == 0 || len(state.Hash) != 0 {
		t.Fatal("pair was not recorded/consumed")
	}
	if w := pairTestExchange(mux, code, "one", "192.168.1.2:3456", false); w.Code != 401 {
		t.Fatal("code used twice")
	}
	if pairTestCode(t, true, time.Now()) != "" {
		t.Fatal("paired node logged a startup code")
	}
	r := httptest.NewRequest("GET", "/api/node/info", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("info no longer authenticated")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var info struct {
		ID        string   `json:"id"`
		Modules   []string `json:"modules"`
		Handshake int      `json:"handshake"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || info.ID != exchange.Node.ID || info.Handshake != 1 || !hasEngineModule(info.Modules) {
		t.Fatal("info handshake/identity missing")
	}
}

func TestNodePairExpiryReplacementAndAttemptLimit(t *testing.T) {
	mux, _ := pairTestNode(t)
	expired := pairTestCode(t, false, time.Now().Add(-pairCodeLifetime))
	if w := pairTestExchange(mux, expired, "one", "192.168.1.2:1", false); w.Code != 401 {
		t.Fatal("expired code accepted")
	}
	old := pairTestCode(t, false, time.Now())
	code := pairTestCode(t, false, time.Now())
	if w := pairTestExchange(mux, old, "one", "192.168.1.3:1", false); w.Code != 401 {
		t.Fatal("replaced code accepted")
	}
	for i := 0; i < 4; i++ {
		if w := pairTestExchange(mux, "INVALID!", "one", "192.168.1."+string(rune('4'+i))+":1", false); w.Code != 401 {
			t.Fatal("invalid attempt accepted")
		}
	}
	if w := pairTestExchange(mux, code, "one", "192.168.1.9:1", false); w.Code != 401 {
		t.Fatal("five failed attempts did not invalidate code")
	}
	if w := pairTestExchange(mux, pairTestCode(t, false, time.Now()), "one", "192.168.1.9:2", false); w.Code != 200 {
		t.Fatal("fresh code unusable")
	}
}

func TestNodePairRemoteRateLimitSurvivesCodeReplacement(t *testing.T) {
	mux, _ := pairTestNode(t)
	pairTestCode(t, false, time.Now())
	for range 5 {
		if w := pairTestExchange(mux, "INVALID!", "one", "192.168.1.2:1", false); w.Code != 401 {
			t.Fatal("wrong failure status")
		}
	}
	code := pairTestCode(t, false, time.Now())
	r := pairTestRequest(code, "one", "Main one", "192.168.1.2:999", false)
	r.Header.Set("X-Forwarded-For", "192.168.1.9")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") != "600" {
		t.Fatal("per-address limit bypassed by new port/code/header")
	}
	if err := updateNodePairState(func(s *nodePairState) error {
		s.Remotes["192.168.1.2"] = nodePairRate{Failures: 5, Expires: time.Now().Add(-time.Second).UnixMilli()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w := pairTestExchange(mux, code, "one", "192.168.1.2:3", false); w.Code != 200 {
		t.Fatal("expired rate limit retained")
	}
}

func TestNodePairConflictForceAndInvalidRequests(t *testing.T) {
	mux, _ := pairTestNode(t)
	code := pairTestCode(t, false, time.Now())
	if w := pairTestExchange(mux, code, "one", "192.168.1.2:1", false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	code = pairTestCode(t, false, time.Now())
	if w := pairTestExchange(mux, "INVALID!", "two", "192.168.1.2:1", true); w.Code != 401 || strings.Contains(w.Body.String(), "Main one") {
		t.Fatal("force bypassed code or disclosed owner")
	}
	if w := pairTestExchange(mux, code, "two", "192.168.1.2:1", false); w.Code != 409 || !strings.Contains(w.Body.String(), "Main one") {
		t.Fatal("owner conflict missing")
	}
	if w := pairTestExchange(mux, code, "two", "192.168.1.2:1", true); w.Code != 200 {
		t.Fatal("valid forced re-pair refused")
	}
	var state nodePairState
	if !getJSON(bkState, nodePairingKey, &state) || state.Main.ID != "two" {
		t.Fatal("new owner not stored")
	}
	code = pairTestCode(t, false, time.Now())
	for _, tc := range []struct {
		method, body, origin string
		status               int
	}{
		{"GET", "{}", "", 405}, {"POST", "{", "", 400}, {"POST", `{"code":"test"}`, "", 400},
		{"POST", "{}", "http://browser", 403}, {"POST", `{"code":"x","extra":true}`, "", 400},
	} {
		r := httptest.NewRequest(tc.method, "/api/node/pair", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("bad request status %d != %d", w.Code, tc.status)
		}
	}
	if w := pairTestExchange(mux, code, "two", "192.168.1.2:1", false); w.Code != 200 {
		t.Fatal("invalid metadata consumed code")
	}
}

func TestNodePairConcurrentSingleUse(t *testing.T) {
	mux, _ := pairTestNode(t)
	code := pairTestCode(t, false, time.Now())
	results := make(chan int, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { results <- pairTestExchange(mux, code, "one", "192.168.1.2:1", false).Code })
	}
	workers.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("code consumed %d times", success)
	}
}

func TestNodePairCLIPrintsReplacementCode(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("node CLI requires a non-root Linux user")
	}
	mux, _ := pairTestNode(t)
	t.Setenv("LOOM_SERVICE", os.Getenv("LOOM_SERVICE"))
	t.Setenv("LOOM_UI_SERVICE", os.Getenv("LOOM_UI_SERVICE"))
	old := pairTestCode(t, false, time.Now())
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	previous := os.Stdout
	os.Stdout = writer
	err = cmdNode([]string{"pair", "--home", LoomHome()})
	os.Stdout = previous
	writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}`).FindString(string(output))
	if code == "" || !strings.Contains(string(output), "10 minutes") {
		t.Fatal("CLI did not print code and lifetime")
	}
	if w := pairTestExchange(mux, old, "one", "192.168.1.2:1", false); w.Code != 401 {
		t.Fatal("CLI did not replace active code")
	}
	if w := pairTestExchange(mux, code, "one", "192.168.1.2:1", false); w.Code != 200 {
		t.Fatal("CLI code not accepted by existing mux")
	}
}
