package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHarnessDeviceHelper(t *testing.T) {
	mode := os.Getenv("LOOM_TEST_DEVICE_LOGIN")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg struct {
			ID     int
			Method string
			Params map[string]any
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(10)
		}
		switch msg.Method {
		case "initialize":
			fmt.Println(`{"id":1,"result":{}}`)
		case "initialized":
		case "account/login/start":
			if msg.Params["type"] != "chatgptDeviceCode" {
				os.Exit(11)
			}
			if mode == "unsupported" {
				fmt.Println(`{"id":2,"error":{"message":"SYNTHETIC-PRIVATE-ERROR"}}`)
				continue
			}
			url := "https://auth.openai.com/codex/device"
			if mode == "unsafe" {
				url = "https://unexpected.example/?secret=synthetic"
			}
			fmt.Printf("{\"id\":2,\"result\":{\"type\":\"chatgptDeviceCode\",\"loginId\":\"test-login\",\"verificationUrl\":%q,\"userCode\":\"ABCD-1234\"}}\n", url)
			if mode == "wait" {
				time.Sleep(time.Minute)
				os.Exit(12)
			}
			fmt.Println(`{"method":"account/login/completed","params":{"loginId":"different-login","success":false}}`)
			fmt.Printf("{\"method\":\"account/login/completed\",\"params\":{\"loginId\":\"test-login\",\"success\":%t,\"error\":\"SYNTHETIC-PRIVATE-ERROR\"}}\n", mode != "denied")
		default:
			os.Exit(13) // Login must never request an inference/thread/turn.
		}
	}
	os.Exit(0)
}

func deviceHelper(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHarnessDeviceHelper$")
	cmd.Env = append(os.Environ(), "LOOM_TEST_DEVICE_LOGIN="+mode)
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	cmd.WaitDelay = time.Second
	return cmd
}

func TestHarnessDeviceLoginProtocolAndSanitization(t *testing.T) {
	for _, mode := range []string{"success", "unsupported", "unsafe", "denied"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			published := false
			err := runCodexAccount(ctx, deviceHelper(ctx, mode), func(url, code string) {
				published = true
				if url != "https://auth.openai.com/codex/device" || code != "ABCD-1234" {
					t.Error("unexpected login data")
				}
			})
			if (mode == "success") != (err == nil) {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if err != nil && strings.Contains(err.Error(), "SYNTHETIC-PRIVATE") {
				t.Fatal("private native error forwarded")
			}
			if (mode == "success" || mode == "denied") != published {
				t.Fatal("unsafe/unsupported response published")
			}
		})
	}
}

func TestHarnessDeviceLoginCancellationStopsOwnedProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := deviceHelper(ctx, "wait")
	published, done := make(chan struct{}), make(chan error, 1)
	go func() { done <- runCodexAccount(ctx, cmd, func(string, string) { close(published) }) }()
	select {
	case <-published:
	case <-ctx.Done():
		t.Fatal("login did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled login succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled process retained")
	}
	if cmd.ProcessState == nil {
		t.Fatal("owned process not reaped")
	}
	j := &harnessAccountJob{harnessAccountState: harnessAccountState{State: "waiting", Code: "ABCD-1234", URL: "synthetic"}}
	j.finish("cancelled", "")
	j.finish("completed", "")
	if s := j.snapshot(); s.State != "cancelled" || s.Code != "" || s.URL != "" {
		t.Fatal("cancelled state overwritten or code retained")
	}
}

func TestHarnessAccountHTTPConsentAndRuntimeBoundary(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	a.agent.ID = "codex"
	b := fakeACPAdapter(t)
	b.agent.ID, b.agent.Machine, b.agent.Remote = "custom-fixture-machine-codex", "fixture-machine", true
	isolateRuntimeRegistry(t, a, b)
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{http.MethodPost, `{}`, 400}, {http.MethodPost, `{"cancel":true}`, 400},
		{http.MethodPost, `{"consent":true,"unknown":true}`, 400},
		{http.MethodGet, "", 400}, {http.MethodDelete, "", 405},
	} {
		r := httptest.NewRequest(tc.method, "/api/runtimes/codex/account", strings.NewReader(tc.body))
		r.SetPathValue("id", "codex")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleHarnessAccount(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.body, w.Code, w.Body.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := &harnessAccountJob{harnessAccountState: harnessAccountState{ID: "synthetic-job", Runtime: "codex", State: "waiting", Code: "ABCD-1234", Expires: time.Now().Add(time.Minute).UnixMilli()}, cancel: cancel}
	harnessAccounts.Lock()
	harnessAccounts.jobs[j.ID] = j
	harnessAccounts.Unlock()
	t.Cleanup(func() { harnessAccounts.Lock(); delete(harnessAccounts.jobs, j.ID); harnessAccounts.Unlock() })
	r := httptest.NewRequest(http.MethodGet, "/api/runtimes/codex/account?job=synthetic-job", nil)
	r.SetPathValue("id", "codex")
	w := httptest.NewRecorder()
	handleHarnessAccount(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ABCD-1234") {
		t.Fatal("active login missing")
	}
	r = httptest.NewRequest(http.MethodGet, "/api/runtimes/other/account?job=synthetic-job", nil)
	r.SetPathValue("id", b.agent.ID)
	w = httptest.NewRecorder()
	handleHarnessAccount(w, r)
	if w.Code != 404 || strings.Contains(w.Body.String(), "ABCD-1234") {
		t.Fatal("another runtime accessed this login")
	}
	r = httptest.NewRequest(http.MethodPost, "/api/runtimes/codex/account", strings.NewReader(`{"job":"synthetic-job","cancel":true}`))
	r.SetPathValue("id", "codex")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handleHarnessAccount(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "ABCD-1234") || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("login not cancelled/cleared")
	}
	if s := j.snapshot(); s.State != "cancelled" {
		t.Fatal("wrong final state")
	}
}

func TestHarnessMissingCLIDistinguishedFromACPLauncher(t *testing.T) {
	a := acpAgent{Command: os.Args[0], Detect: []string{"loom-synthetic-absent-cli"}}
	if !strings.Contains(a.unavailableReason(), "Native CLI") {
		t.Fatal("missing native CLI not identified")
	}
	a.Command, a.Detect = "loom-synthetic-absent-launcher", []string{os.Args[0]}
	if !strings.Contains(a.unavailableReason(), "ACP launcher") {
		t.Fatal("missing ACP launcher not identified")
	}
	testHome(t)
	adapter := &acpAdapter{agent: a}
	adapter.agent.ID = "codex"
	isolateRuntimeRegistry(t, adapter)
	if err := putStoreJSON(bkState, acpProbeKey+"codex", acpProbe{At: 1, Error: "harness CLI or ACP launcher unavailable"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/runtimes/codex/probe", nil)
	r.SetPathValue("id", "codex")
	w := httptest.NewRecorder()
	handleACPProbe(w, r)
	if !strings.Contains(w.Body.String(), "ACP launcher not installed") {
		t.Fatal("cached error hid current missing dependency")
	}
}

func TestControlPlanePingDoesNotIdentifyRemoteEngine(t *testing.T) {
	testHome(t)
	defer setEngineNode(nil)
	if err := setEngineNode(&engineNode{URL: "http://127.0.0.1:1", Hostname: "synthetic-engine", Version: "0.1.4"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	nodeAware("/api/ping", handlePing)(w, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	var got struct{ Version, Hostname string }
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Version != Version || got.Hostname == "synthetic-engine" {
		t.Fatal("ping returned remote identity")
	}
}

func TestBrowserAccountCodeAcceptedOnceEvenAfterRepeatedPrompt(t *testing.T) {
	testHome(t)
	agent := fakeACPAdapter(t)
	agent.agent.ID = "claude-code"
	isolateRuntimeRegistry(t, agent)
	job := &harnessAccountJob{harnessAccountState: harnessAccountState{ID: "fixture-once", Runtime: "claude-code", State: "waiting", Input: true, URL: "fixture-url", Expires: time.Now().Add(time.Minute).UnixMilli()}, cancel: func() {}, codes: make(chan string, 1)}
	harnessAccounts.Lock()
	harnessAccounts.jobs[job.ID] = job
	harnessAccounts.Unlock()
	t.Cleanup(func() { harnessAccounts.Lock(); delete(harnessAccounts.jobs, job.ID); harnessAccounts.Unlock() })
	submit := func() int {
		r := httptest.NewRequest("POST", "/api/runtimes/claude-code/account", strings.NewReader(`{"job":"fixture-once","code":"TEST-CODE"}`))
		r.SetPathValue("id", "claude-code")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleHarnessAccount(w, r)
		return w.Code
	}
	if submit() != 200 || len(job.codes) != 1 {
		t.Fatal("first code not accepted")
	}
	// A repeated prompt must not permit a second send into the occupied channel.
	job.mu.Lock()
	job.Input = true
	job.mu.Unlock()
	if submit() != 400 {
		t.Fatal("duplicate authorization code accepted")
	}
	job.finish("cancelled", "")
	if len(job.codes) != 0 || job.snapshot().URL != "" || job.snapshot().Input {
		t.Fatal("cancelled job retained queued authorization data")
	}
}
