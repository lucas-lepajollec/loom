package loom

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func localTestRequest(method, target string, body io.Reader) *http.Request {
	if strings.HasPrefix(target, "/") {
		target = "http://127.0.0.1" + target
	}
	r := httptest.NewRequest(method, target, body)
	r.RemoteAddr = "127.0.0.1:43210"
	return r
}

func TestAnonymousControlAccessRejectsRebindingAndRemotePeers(t *testing.T) {
	testHome(t)
	calls := 0
	h := requireWebAuth(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) })
	for _, tc := range []struct {
		host, peer string
		want       int
	}{
		{"127.0.0.1:2510", "127.0.0.1:1234", 204},
		{"localhost:2510", "[::1]:1234", 204},
		{"[::1]:2510", "[::1]:1234", 204},
		{"rebind.example:2510", "127.0.0.1:1234", 403},
		{"localhost.attacker.example", "127.0.0.1:1234", 403},
		{"127.0.0.1:2510", "192.0.2.1:1234", 403},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/test", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("Origin", "http://"+tc.host)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		before := calls
		h(w, r)
		if w.Code != tc.want || (tc.want == 403 && calls != before) {
			t.Fatalf("host %s peer %s: %d", tc.host, tc.peer, w.Code)
		}
	}
	// A listener already opened to the LAN also stays closed if its last access
	// credential is removed later. Explicit machine credentials still work.
	if err := storeWebKey("synthetic-audit-control"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://control.example/api/test", nil)
	r.Header.Set("Authorization", "Bearer synthetic-audit-control")
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != 204 {
		t.Fatal("authenticated deployment refused")
	}
	if err := storeWebKey(""); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h(w, r)
	if w.Code != 403 {
		t.Fatal("credential removal opened network control")
	}
}
