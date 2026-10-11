package loom

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPBridgeRelaysResponsesAndSkipsNotifications(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Header.Get("Authorization")+" "+string(b))
		if strings.Contains(string(b), `"id"`) {
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
			return
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()
	var out bytes.Buffer
	in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n")
	if err := mcpBridge(in, &out, srv.URL, "tok", srv.Client()); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n" || len(seen) != 2 || !strings.HasPrefix(seen[0], "Bearer tok ") {
		t.Fatalf("%q %q", out.String(), seen)
	}
}
