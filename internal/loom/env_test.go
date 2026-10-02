package loom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type envRoundTripFunc func(*http.Request) (*http.Response, error)

func (f envRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Real TLS/httptest protocol behavior over pipes: no Docker, SSH, Proxmox or
// listening sockets are required, including inside network-restricted sandboxes.
type envPipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *envPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *envPipeListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *envPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8006}
}
func (l *envPipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	a, b := net.Pipe()
	select {
	case l.connections <- b:
		return a, nil
	case <-l.done:
		_ = a.Close()
		_ = b.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = a.Close()
		_ = b.Close()
		return nil, ctx.Err()
	}
}

func envTestRequest(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestEnvDockerJSONLines(t *testing.T) {
	rows, err := parseEnvDocker([]byte(`{"Names":"app","Image":"app:v1","State":"running","Status":"Up 1 hour","Ports":"127.0.0.1:80->80/tcp"}` + "\n\n" + `{"Names":"old","Image":"db","State":"exited","Status":"Exited (0)","Ports":""}` + "\n"))
	if err != nil || len(rows) != 2 || rows[0].Name != "app" || rows[0].Ports != "127.0.0.1:80->80/tcp" || rows[1].State != "exited" {
		t.Fatalf("%+v %v", rows, err)
	}
	if _, err := parseEnvDocker([]byte("permission denied")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	rows, err = parseEnvDocker(nil)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty list: %+v %v", rows, err)
	}
}

func TestEnvDockerOptInAndMachineErrors(t *testing.T) {
	testHome(t)
	e := newEnvironment()
	e.machines = func() []RemoteMachine { return []RemoteMachine{{ID: "box", User: "alice", Host: "box", Port: 2222}} }
	e.sshKey = func() (string, string, error) { return "/fake-key", "public", nil }
	calls := 0
	e.run = func(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
		calls++
		if name == "docker" {
			if !reflect.DeepEqual(args, []string{"ps", "-a", "--format", "{{json .}}"}) || stdin != "" {
				t.Fatalf("local invocation: %s %+v %q", name, args, stdin)
			}
			return nil, errors.New("docker: permission denied")
		}
		if name != "ssh" || !strings.HasPrefix(stdin, remotePathPreamble) || !strings.Contains(stdin, shellQuote("{{json .}}")) || !strings.Contains(strings.Join(args, " "), "-p 2222 alice@box sh -s") {
			t.Fatalf("SSH invocation: %s %+v %q", name, args, stdin)
		}
		return []byte(`{"Names":"remote","Image":"app","State":"running"}`), nil
	}
	w := envTestRequest(e.docker, "GET", "/api/env/docker", "")
	if w.Code != 200 || calls != 0 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("default observation: %s", w.Body)
	}
	for _, machine := range []string{"local", "box"} {
		w = envTestRequest(e.docker, "POST", "/api/env/docker", fmt.Sprintf(`{"machine":%q,"enabled":true}`, machine))
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		w = envTestRequest(e.docker, "GET", "/api/env/docker?machine="+machine, "")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		if machine == "local" && (!strings.Contains(w.Body.String(), "permission denied") || !strings.Contains(w.Body.String(), `"ok":false`)) {
			t.Fatal(w.Body)
		}
		if machine == "box" && !strings.Contains(w.Body.String(), `"name":"remote"`) {
			t.Fatal(w.Body)
		}
	}
	e.run = func(context.Context, string, []string, string) ([]byte, error) {
		return nil, errors.New("executable file not found")
	}
	w = envTestRequest(e.docker, "GET", "/api/env/docker", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "not found") {
		t.Fatal(w.Body)
	}
	w = envTestRequest(e.docker, "POST", "/api/env/docker", `{"enabled":false}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	e.run = func(context.Context, string, []string, string) ([]byte, error) {
		t.Fatal("disabled provider executed")
		return nil, nil
	}
	if w = envTestRequest(e.docker, "GET", "/api/env/docker", ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func TestEnvServiceCRUDAndProtection(t *testing.T) {
	testHome(t)
	e := newEnvironment()
	e.machines = func() []RemoteMachine { return nil }
	mux := http.NewServeMux()
	e.register(func(path string, h http.HandlerFunc) { mux.HandleFunc(path, requireWebAuth(h)) })
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"services", "services/delete", "docker", "proxmox", "proxmox/resources", "check", "matrix"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/env/"+path, nil))
		if w.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/env/services", nil)
	r.Header.Set("Authorization", "Bearer control")
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = envTestRequest(e.services, "POST", "/", `{"name":"DB","url":"localhost:5432","kind":"db","notes":"test"}`)
	var saved struct {
		Service EnvService `json:"service"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || w.Code != 200 || saved.Service.ID == "" || saved.Service.Machine != "local" {
		t.Fatalf("%s %v", w.Body, err)
	}
	saved.Service.Name = "Updated"
	body, _ := json.Marshal(saved.Service)
	if w = envTestRequest(e.services, "POST", "/", string(body)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	list, err := envServices()
	if err != nil || len(list) != 1 || list[0].Name != "Updated" {
		t.Fatalf("%+v %v", list, err)
	}
	for _, bad := range []string{
		`{"name":"x","url":"ftp://host","kind":"web"}`,
		`{"name":"x","url":"http://user:password@host","kind":"web"}`,
		`{"name":"x","url":"host:99","kind":"web","machine":"unknown"}`,
		`{"name":"x","url":"host:99","kind":"invented"}`,
	} {
		if w = envTestRequest(e.services, "POST", "/", bad); w.Code != 400 {
			t.Fatalf("accepted %s", bad)
		}
	}
	w = envTestRequest(e.deleteService, "POST", "/", fmt.Sprintf(`{"id":%q}`, saved.Service.ID))
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = envTestRequest(e.deleteService, "POST", "/", fmt.Sprintf(`{"id":%q}`, saved.Service.ID))
	if w.Code != 404 {
		t.Fatal(w.Body)
	}
	// An unreadable record must never be overwritten by an empty fallback.
	if err := putStr(bkState, envServicesState, "broken JSON"); err != nil {
		t.Fatal(err)
	}
	w = envTestRequest(e.services, "POST", "/", `{"name":"x","url":"host:99","kind":"web"}`)
	if w.Code != 503 || getStr(bkState, envServicesState) != "broken JSON" {
		t.Fatal("unreadable state overwritten")
	}
}

const envResourcesFixture = `{"data":[{"type":"node","node":"n1","status":"online","cpu":0.2,"maxmem":16000,"mem":8000,"uptime":500},{"type":"qemu","vmid":101,"name":"vm","node":"n1","status":"running","cpu":0.5,"maxmem":4096,"mem":1024,"uptime":100},{"type":"lxc","vmid":102,"name":"ct","node":"n1","status":"stopped"},{"type":"storage","node":"n1"}]}`

func TestEnvProxmoxResourcesJSON(t *testing.T) {
	rows, err := parseEnvProxmox([]byte(envResourcesFixture))
	if err != nil || len(rows) != 3 || rows[0].Name != "n1" || rows[0].VMID != nil || *rows[1].VMID != 101 || *rows[1].CPU != 0.5 || *rows[1].MaxMem != 4096 || rows[2].Mem != nil {
		t.Fatalf("%+v %v", rows, err)
	}
	for _, bad := range []string{`{}`, `{"data":null}`, `{"data":[{"cpu":"unknown"}]}`, `invalid`} {
		if _, err := parseEnvProxmox([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestEnvProxmoxSecretAndPinConfirmation(t *testing.T) {
	testHome(t)
	e := newEnvironment()
	vault := map[string]string{}
	e.setSecret = func(id, secret string) error { vault[id] = secret; return nil }
	e.getSecret = func(id string) (string, error) { return vault[id], nil }
	pin := strings.Repeat("ab", 32)
	body := fmt.Sprintf(`{"enabled":true,"url":"https://pve.example:8006","token_id":"user@pve!loom","token_secret":"TOP-SECRET","fingerprint":%q}`, pin)
	w := envTestRequest(e.proxmox, "POST", "/", body)
	if w.Code != 400 || len(vault) != 0 {
		t.Fatal("pin saved without confirmation")
	}
	body = strings.TrimSuffix(body, "}") + `,"confirm_fingerprint":true}`
	w = envTestRequest(e.proxmox, "POST", "/", body)
	if w.Code != 200 || vault[envProxmoxKey] != "TOP-SECRET" {
		t.Fatalf("%s %+v", w.Body, vault)
	}
	if strings.Contains(w.Body.String(), "TOP-SECRET") || strings.Contains(getStr(bkState, envProvidersState), "TOP-SECRET") {
		t.Fatal("secret persisted/returned")
	}
	w = envTestRequest(e.proxmox, "GET", "/", "")
	if strings.Contains(w.Body.String(), "TOP-SECRET") || strings.Contains(w.Body.String(), "token_secret") {
		t.Fatal(w.Body)
	}
	e.getSecret = func(string) (string, error) { return "", errors.New("TOP-SECRET backend error") }
	w = envTestRequest(e.proxmoxResources, "GET", "/", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "TOP-SECRET") {
		t.Fatal(w.Body)
	}
	e.setSecret = func(string, string) error { return errors.New("TOP-SECRET backend error") }
	w = envTestRequest(e.proxmox, "POST", "/", body)
	if w.Code != 503 || strings.Contains(w.Body.String(), "TOP-SECRET") {
		t.Fatal(w.Body)
	}
	for _, bad := range []string{`{"enabled":true,"url":"http://pve","token_id":"u@pve!t"}`, `{"url":"https://pve","token_id":"u@pve!t","fingerprint":"bad"}`} {
		if w = envTestRequest(e.proxmox, "POST", "/", bad); w.Code != 400 {
			t.Fatal(w.Body)
		}
	}
}

func TestEnvProxmoxTLSPinAndNoTokenLeak(t *testing.T) {
	var calls atomic.Int32
	var redirect atomic.Bool
	listener := &envPipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
	srv := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api2/json/cluster/resources" || r.Header.Get("Authorization") != "PVEAPIToken=u@pve!t=TOP-SECRET" {
			t.Errorf("unexpected request: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		if redirect.Load() {
			w.Header().Set("Location", "https://other.example/?TOP-SECRET")
			w.WriteHeader(302)
			_, _ = io.WriteString(w, "TOP-SECRET")
			return
		}
		_, _ = io.WriteString(w, envResourcesFixture)
	})}}
	srv.StartTLS()
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	c := EnvProxmoxConfig{URL: srv.URL, TokenID: "u@pve!t", Fingerprint: hex.EncodeToString(sum[:])}
	rows, err := fetchEnvProxmox(context.Background(), c, "TOP-SECRET", listener.dial)
	if err != nil || len(rows) != 3 {
		t.Fatalf("pin rejected: %v", err)
	}
	c.Fingerprint = strings.Repeat("00", 32)
	if _, err = fetchEnvProxmox(context.Background(), c, "TOP-SECRET", listener.dial); err == nil || strings.Contains(err.Error(), "TOP-SECRET") {
		t.Fatalf("mismatch accepted/leaked: %v", err)
	}
	c.Fingerprint = ""
	if _, err = fetchEnvProxmox(context.Background(), c, "TOP-SECRET", listener.dial); err == nil {
		t.Fatal("untrusted certificate accepted without pin")
	}
	if calls.Load() != 1 {
		t.Fatal("token sent before TLS trust verified")
	}
	// A provider may echo credentials in an error or redirect. Neither body nor
	// redirect is exposed/followed.
	redirect.Store(true)
	c.Fingerprint = hex.EncodeToString(sum[:])
	if _, err = fetchEnvProxmox(context.Background(), c, "TOP-SECRET", listener.dial); err == nil || strings.Contains(err.Error(), "TOP-SECRET") {
		t.Fatalf("redirect/error leak: %v", err)
	}
}

func TestEnvReachabilityResultsAndCommands(t *testing.T) {
	e := newEnvironment()
	e.machines = func() []RemoteMachine { return []RemoteMachine{{ID: "box", User: "u", Host: "h", Port: 22}} }
	e.transport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal(r.Method)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("HTTP deadline missing")
		}
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	r := e.check(context.Background(), "https://service.example", "")
	if !r.OK || r.From != "local" || r.Status != 204 || r.Error != "" || r.LatencyMS < 0 {
		t.Fatalf("%+v", r)
	}
	e.transport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if r = e.check(context.Background(), "http://h", "local"); r.OK || r.Status != 503 || r.Error == "" {
		t.Fatalf("%+v", r)
	}
	e.transport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	if r = e.check(context.Background(), "http://h", "local"); r.OK || !strings.Contains(r.Error, "deadline") {
		t.Fatalf("%+v", r)
	}
	e.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != "[::1]:5432" {
			t.Fatalf("dial %s %s", network, addr)
		}
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	if r = e.check(context.Background(), "[::1]:5432", "local"); !r.OK || r.Status != 0 {
		t.Fatalf("%+v", r)
	}
	e.dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("connection refused") }
	if r = e.check(context.Background(), "host:5432", "local"); r.OK || r.Error != "connection refused" {
		t.Fatalf("%+v", r)
	}
	e.sshKey = func() (string, string, error) { return "fake", "", nil }
	e.run = func(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
		if name != "ssh" || !strings.HasPrefix(stdin, remotePathPreamble) || !strings.Contains(stdin, "curl -sS -o /dev/null -w '%{http_code}' --max-time 5 --") || !strings.Contains(stdin, "command -v bash") {
			t.Fatalf("remote script: %s %+v %s", name, args, stdin)
		}
		if !strings.Contains(stdin, shellQuote("http://host/path?q=';echo pwned")) {
			t.Fatal("unquoted URL")
		}
		return []byte("noise\nLOOM-HTTP 200\n"), nil
	}
	if r = e.check(context.Background(), "http://host/path?q=';echo pwned", "box"); !r.OK || r.Status != 200 || r.From != "box" {
		t.Fatalf("%+v", r)
	}
	e.run = func(context.Context, string, []string, string) ([]byte, error) { return []byte("LOOM-TCP\n"), nil }
	if r = e.check(context.Background(), "http://host", "box"); !r.OK || r.Status != 0 {
		t.Fatalf("fallback %+v", r)
	}
	e.run = func(context.Context, string, []string, string) ([]byte, error) { return []byte("LOOM-HTTP 000\n"), nil }
	if r = e.check(context.Background(), "http://host", "box"); r.OK || r.Error == "" {
		t.Fatalf("malformed status %+v", r)
	}
	for _, target := range []string{"ftp://host", "host:0", "http://host:99999", "http://u:p@host", "host:99\n", ""} {
		if r = e.check(context.Background(), target, "local"); r.OK || r.Error == "" {
			t.Fatalf("accepted %q", target)
		}
	}
	if r = e.check(context.Background(), "http://host", "unknown"); r.OK || r.Error == "" {
		t.Fatal("unknown machine accepted")
	}
}

func TestEnvMatrixBoundsAndCancellation(t *testing.T) {
	e := newEnvironment()
	machines := []RemoteMachine{{ID: "box", Host: "h", User: "u", Port: 22}}
	e.machines = func() []RemoteMachine { return machines }
	e.sshKey = func() (string, string, error) { return "fake", "", nil }
	var active, peak atomic.Int32
	probe := func(ctx context.Context) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-time.After(10 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.transport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := probe(r.Context()); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	e.run = func(ctx context.Context, _ string, _ []string, _ string) ([]byte, error) {
		return []byte("LOOM-HTTP 200\n"), probe(ctx)
	}
	services := []EnvService{}
	for n := 0; n < 24; n++ {
		services = append(services, EnvService{ID: fmt.Sprint(n), URL: "http://h"})
	}
	// Two simultaneous requests still share the owner's eight-check limit.
	done := make(chan []EnvMatrixRow, 1)
	go func() { done <- e.matrix(context.Background(), services, machines) }()
	rows := e.matrix(context.Background(), services, machines)
	other := <-done
	if len(rows) != 48 || len(other) != 48 || peak.Load() > envConcurrency || peak.Load() < 2 || active.Load() != 0 {
		t.Fatalf("rows=%d peak=%d active=%d", len(rows), peak.Load(), active.Load())
	}
	for i, row := range rows {
		if !row.OK || row.ServiceID != fmt.Sprint(i/2) || row.From != []string{"local", "box"}[i%2] {
			t.Fatalf("%+v", row)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	rows = e.matrix(ctx, services, machines)
	if time.Since(start) > time.Second || len(rows) != 48 {
		t.Fatal("cancellation did not bound matrix")
	}
	for _, row := range rows {
		if row.OK || row.Error == "" {
			t.Fatalf("cancelled row %+v", row)
		}
	}
	// Saturated checks respect the incoming deadline without starting a probe.
	for n := 0; n < envConcurrency; n++ {
		e.checks <- struct{}{}
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	r := e.check(ctx, "http://h", "local")
	if r.OK || !strings.Contains(r.Error, "deadline") {
		t.Fatalf("queued %+v", r)
	}
	for n := 0; n < envConcurrency; n++ {
		<-e.checks
	}
}

func TestProxmoxFingerprintProbeMatchesPin(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/env/proxmox/fingerprint", strings.NewReader(`{"url":"`+srv.URL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	handleProxmoxFingerprint(rec, req)
	var out struct {
		OK          bool   `json:"ok"`
		Fingerprint string `json:"fingerprint"`
		Trusted     bool   `json:"trusted"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	sum := sha256.Sum256(srv.Certificate().Raw)
	pin, _ := envFingerprint(out.Fingerprint)
	if !out.OK || out.Trusted || pin != hex.EncodeToString(sum[:]) {
		t.Fatalf("%s", rec.Body.String())
	}
}
