package loom

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Model-driven page reads never reach this machine or the local network.
func TestWebToolsRefusePrivateTargets(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8080/", "http://localhost/", "http://192.168.0.1/admin", "http://[::1]/", "http://100.100.1.1/", "file:///etc/passwd"} {
		if publicTarget(u) == nil {
			t.Errorf("%s accepted", u)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer srv.Close()
	if _, err := goHTTPClient.Get(srv.URL); err == nil {
		t.Fatal("the web client reached a loopback server")
	}
}
