package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLinkedEngineNeverFollowsCredentialRedirect(t *testing.T) {
	calls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	r, _ := http.NewRequest("GET", redirect.URL, nil)
	r.Header.Set("Authorization", "Bearer synthetic-node-token")
	resp, err := nodeClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 307 || calls != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
}

func TestLegacyControlBodyFailureCannotDecryptOrToggle(t *testing.T) {
	testHome(t)
	mux := http.NewServeMux()
	webAPI(mux)("/api/agent/toggle", handleAgentToggle)
	if err := setAgentEnabled(true); err != nil {
		t.Fatal(err)
	}
	before := agentEnabled()
	for _, body := range []string{`{"on":`, `{"on":42}`, `{"on":"false"}`, `null`, `[]`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, localTestRequest("POST", "/api/agent/toggle", strings.NewReader(body)))
		if w.Code != 400 || agentEnabled() != before {
			t.Fatal("invalid request changed agent mode", w.Code)
		}
	}
}

func TestTerminalPendingAndOpenConnectionsRevokeWithOwner(t *testing.T) {
	testHome(t)
	if err := saveWebPassword(testAccessPassword, nil); err != nil {
		t.Fatal(err)
	}
	_, credential, err := readWebPassword()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	owner := localTestRequest("POST", "/", nil)
	if err = issueWebSession(w, owner, credential); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	owner.AddCookie(cookie)
	grant, err := controlOwner(owner)
	if err != nil || !grant.valid() {
		t.Fatal("grant missing")
	}
	// A synthetic terminal needs no process. These pipes observe input only.
	proc := &auditTerminalProcess{input: make(chan []byte, 1)}
	id := "audit-" + randomID(8)
	term := &Terminal{TerminalInfo: TerminalInfo{ID: id, Running: true}, proc: proc, clients: map[chan []byte]struct{}{}, done: make(chan struct{})}
	terminals.Lock()
	terminals.byID[id] = term
	terminals.Unlock()
	t.Cleanup(func() { terminals.Lock(); delete(terminals.byID, id); terminals.Unlock() })
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleTerminalWS)
	server := httptest.NewServer(mux)
	defer server.Close()
	ticketBody := `{"id":"` + id + `"}`
	issue := func() string {
		r := localTestRequest("POST", "/api/terminals/ticket", strings.NewReader(ticketBody))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		out := httptest.NewRecorder()
		handleTerminalTicket(out, r)
		var data struct {
			Ticket string `json:"ticket"`
		}
		_ = json.Unmarshal(out.Body.Bytes(), &data)
		if out.Code != 200 || data.Ticket == "" {
			t.Fatal("ticket missing")
		}
		return data.Ticket
	}
	opened, pending := issue(), issue()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws?ticket="+opened, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err = conn.Write(ctx, websocket.MessageText, []byte("before-revocation")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.input:
	case <-ctx.Done():
		t.Fatal("authorized input not delivered")
	}
	logout := localTestRequest("POST", "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	handleWebLogout(httptest.NewRecorder(), logout)
	if grant.valid() || useTicket(pending) != "" {
		t.Fatal("logout left a grant or pending ticket valid")
	}
	if _, err = controlOwner(owner); err == nil {
		t.Fatal("stale browser authorization became a fresh grant")
	}
	if _, _, err = conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("open terminal did not close promptly on logout")
	}
}

type auditTerminalProcess struct{ input chan []byte }

func (*auditTerminalProcess) Read([]byte) (int, error) { return 0, io.EOF }
func (p *auditTerminalProcess) Write(b []byte) (int, error) {
	p.input <- append([]byte{}, b...)
	return len(b), nil
}
func (*auditTerminalProcess) Resize(uint16, uint16) error { return nil }
func (*auditTerminalProcess) Close() error                { return nil }
func (*auditTerminalProcess) Wait() (int, error)          { return 0, nil }

func TestShellOutputIsBoundedDuringExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper invocation")
	}
	testHome(t)
	t.Setenv("LOOM_TEST_SHELL_NOISE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := runShell(context.Background(), shellQuote(exe)+" -test.run=^TestNoisyShellHelper$", 10)
	if len(out) > toolMaxOutput+100 || !strings.HasSuffix(out, "final useful line") {
		t.Fatal("noisy command lost bounded useful output")
	}
}

func TestNoisyShellHelper(t *testing.T) {
	if os.Getenv("LOOM_TEST_SHELL_NOISE") != "1" {
		return
	}
	chunk := []byte(strings.Repeat("x", 8192))
	for i := 0; i < 8192; i++ {
		_, _ = os.Stdout.Write(chunk)
	}
	_, _ = os.Stdout.Write([]byte("final useful line"))
	os.Exit(0)
}

func TestEstablishedDiscussionStreamRevokesOnCredentialChange(t *testing.T) {
	testHome(t)
	if err := storeWebKey("synthetic-stream-owner"); err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	h := requireWebAuth(revocableControlStream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(ended)
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	req.Header.Set("Authorization", "Bearer synthetic-stream-owner")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err = storeWebKey("synthetic-stream-replacement"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("accepted discussion stream retained revoked access")
	}
}
