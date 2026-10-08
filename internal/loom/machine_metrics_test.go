package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func metricsFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "machine_metrics", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func fixtureMachineMetrics(t *testing.T) MachineMetrics {
	t.Helper()
	return parseProcMachineMetrics(metricsFixture(t, "stat1"), metricsFixture(t, "stat2"), metricsFixture(t, "meminfo"), metricsFixture(t, "loadavg"), metricsFixture(t, "uptime"))
}
func TestMachineMetricsProcFixtures(t *testing.T) {
	m := fixtureMachineMetrics(t)
	// Delta is 100 ticks, of which idle+iowait is 35. Guest ticks are excluded.
	if m.CPU != 65 || m.Cores != 2 || m.Load1 != 1.25 || m.UptimeSeconds != 12345 || m.RAMTotal != 16384000*1024 || m.RAMUsed != 12288000*1024 || m.Partial {
		t.Fatalf("unexpected metrics: %+v", m)
	}
	for _, tc := range []struct{ name, stat1, stat2, meminfo, load, uptime string }{
		{"empty", "", "", "", "", ""},
		{"bad counters", "cpu a 1 2 3", "cpu 1 2 3 4", "MemTotal: 200 kB\nMemAvailable: 300 kB", "NaN", "Inf"},
		{"reset counters", metricsFixture(t, "stat2"), metricsFixture(t, "stat1"), "MemTotal: 100 kB", "-1", "-1"},
		{"unchanged", metricsFixture(t, "stat1"), metricsFixture(t, "stat1"), "MemTotal: 100 kB\nMemAvailable: 50 kB", "0", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseProcMachineMetrics(tc.stat1, tc.stat2, tc.meminfo, tc.load, tc.uptime)
			if !got.Partial || got.CPU != 0 {
				t.Fatal(got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMachineMetricsCacheExpiryAndErrors(t *testing.T) {
	for _, ttl := range []time.Duration{2 * time.Second, 10 * time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			now := time.Unix(100, 0)
			cache := &machineMetricsCache{now: func() time.Time { return now }}
			calls := 0
			fetch := func(context.Context) (any, error) { calls++; return calls, errors.New("offline") }
			for i := 0; i < 2; i++ {
				v, err := cache.get(context.Background(), "machine", ttl, fetch)
				if v != 1 || err == nil {
					t.Fatal(v, err)
				}
			}
			now = now.Add(ttl)
			v, err := cache.get(context.Background(), "machine", ttl, fetch)
			if calls != 2 || v != 2 || err == nil {
				t.Fatal(calls, v, err)
			}
		})
	}
}
func TestMachineMetricsCacheSharesInflightAndCancellation(t *testing.T) {
	cache := &machineMetricsCache{}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fetch := func(context.Context) (any, error) { calls.Add(1); close(started); <-release; return "sample", nil }
	done := make(chan any, 1)
	go func() { v, _ := cache.get(context.Background(), "local", 2*time.Second, fetch); done <- v }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.get(ctx, "local", 2*time.Second, fetch); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if v := <-done; v != "sample" {
		t.Fatal(v)
	}
	if v, err := cache.get(context.Background(), "local", 2*time.Second, fetch); v != "sample" || err != nil || calls.Load() != 1 {
		t.Fatal(v, err, calls.Load())
	}
}

func seedLocalMetrics(t *testing.T) MachineMetrics {
	t.Helper()
	old := localMachineMetricsCache
	localMachineMetricsCache = &machineMetricsCache{}
	t.Cleanup(func() { localMachineMetricsCache = old })
	m := fixtureMachineMetrics(t)
	_, err := localMachineMetricsCache.get(context.Background(), "local", 2*time.Second, func(context.Context) (any, error) { return m, nil })
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestMachineMetricsLocalRouteAndCollector(t *testing.T) {
	testHome(t)
	seedLocalMetrics(t)
	w := httptest.NewRecorder()
	handleLocalMachineMetrics(w, httptest.NewRequest("GET", "/api/machines/local/metrics", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"cpu":65`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := collectLocalMachineMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(MachineMetrics)
	if m.OS != runtime.GOOS || m.At <= 0 || m.Cores <= 0 || m.GPUs == nil {
		t.Fatal(m)
	}
	if runtime.GOOS == "linux" && (m.RAMTotal == 0 || m.Disk.Total == 0 || m.UptimeSeconds <= 0) {
		t.Fatal(m)
	}
	if runtime.GOOS != "linux" && !m.Partial {
		t.Fatal("non-Linux must be partial", m)
	}
}
func TestNodeObserveAuthDisabledAndModules(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 8)
	seedLocalMetrics(t)
	for _, credential := range []string{"", "inference-key"} {
		if w := nodeHarnessGet(mux, "/api/node/observe", credential); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/node/observe?token="+token, nil)
	r.AddCookie(&http.Cookie{Name: "loom_session", Value: token})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("URL/cookie credential accepted", w.Code)
	}
	if w := nodeHarnessGet(mux, "/api/node/observe", token); w.Code != 200 || !strings.Contains(w.Body.String(), `"cpu":65`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if !hasNodeModule(nodeModules(), "observe") {
		t.Fatal("observe not advertised")
	}
	if err := putBool(bkState, nodeObserveDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	if hasNodeModule(nodeModules(), "observe") {
		t.Fatal("disabled observe advertised")
	}
	if w := nodeHarnessGet(mux, "/api/node/observe", token); w.Code != 409 || !strings.Contains(w.Body.String(), nodeObserveDisabled) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestNodeObserveInitOptOut(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("node CLI requires non-root Linux")
	}
	home := testHome(t)
	t.Setenv("LOOM_SERVICE", os.Getenv("LOOM_SERVICE"))
	t.Setenv("LOOM_UI_SERVICE", os.Getenv("LOOM_UI_SERVICE"))
	for _, args := range [][]string{{"init", "--home", home, "--no-observe", "--no-harness"}, {"init", "--home", home}, {"init", "--home", home, "--no-observe=false"}} {
		if err := cmdNode(args); err != nil {
			t.Fatal(err)
		}
		enabled := strings.HasSuffix(args[len(args)-1], "=false")
		if nodeObserveEnabled() != enabled || nodeHarnessEnabled() {
			t.Fatal("module choices not retained", args)
		}
	}
}
func saveMetricsNode(t *testing.T, id string) RemoteMachine {
	t.Helper()
	m := RemoteMachine{ID: id, NodeID: id, Modules: []string{"engine", "observe"}, Handshake: 1}
	n := &engineNode{URL: "https://" + id + ".example", WebKey: "metrics-machine-token", NodeID: id, Role: "engine-node", Modules: m.Modules, Handshake: 1}
	saved, err := savePairedMachine(m, n)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func TestMachineMetricsPairedNodeProxy(t *testing.T) {
	testHome(t)
	m := saveMetricsNode(t, "worker")
	original := nodeClient.Transport
	t.Cleanup(func() { nodeClient.Transport = original })
	want := fixtureMachineMetrics(t)
	body, _ := json.Marshal(want)
	status := 200
	nodeClient.Transport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/node/observe" || r.Header.Get("Authorization") != "Bearer metrics-machine-token" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
			t.Error("wrong proxy request", r.URL, r.Header)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	r := httptest.NewRequest("GET", "/api/machines/worker/metrics", nil)
	r.SetPathValue("id", m.ID)
	r.Header.Set("Authorization", "Bearer browser-key")
	r.Header.Set("Cookie", "browser-cookie")
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	handleMachineMetrics(w, r)
	var got MachineMetrics
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.CPU != 65 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A disabled module response does not depend on the separate harness module.
	status = 409
	if _, err := remoteMachineMetrics(context.Background(), m); err == nil || err.Error() != nodeObserveDisabled {
		t.Fatal(err)
	}
	m.Modules = []string{"engine"}
	if _, err := remoteMachineMetrics(context.Background(), m); err == nil {
		t.Fatal("missing module accepted")
	}
}

func sshMetricsFixture(t *testing.T) string {
	t.Helper()
	out := "login banner\n" + remoteOutputMarker + "\n"
	for _, name := range []string{"stat1", "stat2", "meminfo", "loadavg", "uptime"} {
		out += machineMetricsMarker + name + "\n" + metricsFixture(t, name)
	}
	return out + machineMetricsMarker + "home\n/home/fixture\n" + machineMetricsMarker + "disk\n4096 1000 400\n" + machineMetricsMarker + "nvidia\nTest GPU 0, 100, 8000, 20, 40\nTest GPU 1, 200, 16000, 75, 50\n" + machineMetricsMarker + "end\n"
}
func TestMachineMetricsSSHRunnerParserAndCache(t *testing.T) {
	testHome(t)
	oldRunner, oldCache := runMachineMetricsSSH, sshMachineMetricsCache
	sshMachineMetricsCache = &machineMetricsCache{}
	t.Cleanup(func() { runMachineMetricsSSH = oldRunner; sshMachineMetricsCache = oldCache })
	calls := 0
	fixture := sshMetricsFixture(t)
	runMachineMetricsSSH = func(ctx context.Context, m RemoteMachine, script string) ([]byte, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || !strings.Contains(script, "cat /proc/stat") || !strings.Contains(script, `stat -f -c '%S %b %f' -- "$HOME"`) {
			t.Error("unbounded or incomplete SSH command", script)
		}
		return []byte(fixture), nil
	}
	machine := RemoteMachine{ID: "ssh", Host: "worker.example", User: "fixture", Port: 22, OS: "Linux"}
	for i := 0; i < 2; i++ {
		v, err := remoteMachineMetrics(context.Background(), machine)
		if err != nil {
			t.Fatal(err)
		}
		m := v.(MachineMetrics)
		if m.CPU != 65 || m.Partial || m.Disk.Path != "/home/fixture" || m.Disk.Total != 4096000 || m.Disk.Used != 2457600 || len(m.GPUs) != 2 || m.GPUs[1].Util != 75 || m.GPUs[1].VRAMTotal != 16000<<20 {
			t.Fatal(m)
		}
	}
	if calls != 1 {
		t.Fatal("SSH storm", calls)
	}
	machine.OS = "Darwin"
	v, err := remoteMachineMetrics(context.Background(), machine)
	if err != nil || v.(map[string]any)["supported"] != false || calls != 1 {
		t.Fatal(v, err, calls)
	}
	machine.OS = ""
	machine.ID = "unknown"
	runMachineMetricsSSH = func(context.Context, RemoteMachine, string) ([]byte, error) {
		return []byte(machineMetricsMarker + "unsupported\n"), nil
	}
	v, err = remoteMachineMetrics(context.Background(), machine)
	if err != nil || v.(map[string]any)["supported"] != false {
		t.Fatal(v, err)
	}
	if _, err := parseSSHMachineMetrics("banner without metrics"); err == nil {
		t.Fatal("invalid output accepted")
	}
}
func TestMachineMetricsAggregateIsConcurrentAndKeepsFailures(t *testing.T) {
	testHome(t)
	seedLocalMetrics(t)
	saveMetricsNode(t, "healthy")
	saveMetricsNode(t, "failed")
	old := nodeClient.Transport
	t.Cleanup(func() { nodeClient.Transport = old })
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	body, _ := json.Marshal(fixtureMachineMetrics(t))
	nodeClient.Transport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		if r.URL.Hostname() == "failed.example" {
			return nil, errors.New("offline")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		handleMachinesMetrics(w, httptest.NewRequest("GET", "/api/machines/metrics", nil))
		done <- w
	}()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-timer.C:
			close(release)
			t.Fatal("machines were not fetched concurrently")
		}
	}
	close(release)
	w := <-done
	var got struct {
		Metrics map[string]json.RawMessage `json:"metrics"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Metrics) != 3 || !strings.Contains(string(got.Metrics["healthy"]), `"cpu":65`) || !strings.Contains(string(got.Metrics["local"]), `"cpu":65`) || !strings.Contains(string(got.Metrics["failed"]), `"error":`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "metrics-machine-token") {
		t.Fatal("credential exposed")
	}
}

func TestMachineMetricsMainRoutesAndAuthentication(t *testing.T) {
	testHome(t)
	seedLocalMetrics(t)
	m := RemoteMachine{ID: "mac", OS: "Darwin", Host: "mac.example", User: "fixture", Port: 22}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct{ path, contains string }{{"/api/machines/local/metrics", `"cpu":65`}, {"/api/machines/mac/metrics", `"supported":false`}, {"/api/machines/metrics", `"local":`}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, localTestRequest("GET", tc.path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.contains) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, localTestRequest("POST", tc.path, strings.NewReader(`{}`)))
		if w.Code != 405 {
			t.Fatal(tc.path, "POST accepted", w.Code, w.Body.String())
		}
		r := httptest.NewRequest("GET", tc.path, nil)
		r.RemoteAddr = "192.0.2.1:1234"
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(tc.path, "unauthenticated network request accepted", w.Code)
		}
	}
}

// Execute the generated remote script locally to verify its wire format too.
func TestMachineMetricsSSHScriptOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SSH metrics script is Linux-only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(machineMetricsSSHScript())
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	value, err := parseSSHMachineMetrics(string(out))
	if err != nil {
		t.Fatal(err, string(out))
	}
	m := value.(MachineMetrics)
	if m.Cores <= 0 || m.RAMTotal == 0 || m.Disk.Total == 0 || m.Disk.Path == "" || m.UptimeSeconds <= 0 {
		t.Fatal(m)
	}
}

func TestMachineMetricsCacheRequesterCancellationKeepsSample(t *testing.T) {
	cache := &machineMetricsCache{}
	started, release := make(chan struct{}), make(chan struct{})
	fetch := func(ctx context.Context) (any, error) { close(started); <-release; return "sample", ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := cache.get(ctx, "local", 2*time.Second, fetch); first <- err }()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if value, err := cache.get(context.Background(), "local", 2*time.Second, fetch); err != nil || value != "sample" {
		t.Fatal(value, err)
	}
}
