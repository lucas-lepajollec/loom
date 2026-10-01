package loom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudModelsIsBoundedCredentialOnlyAndNoRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"catalog", `{"data":[{"id":"z"},{"id":"a"},{"id":"a"},{"id":""},{"id":"bad\nline"}]}`, 200, false},
		{"denied", "sensitive-test-key and private prompt", 401, true},
		{"invalid", `{"models":["z"]}`, 200, true},
		{"truncated", `{"data":[`, 200, true},
		{"oversized", strings.Repeat("x", (3<<20)+1), 200, true},
		{"empty", `{"data":[]}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/models" || r.URL.RawQuery != "" || r.ContentLength > 0 || r.Header.Get("Authorization") != "Bearer sensitive-test-key" {
					t.Error("probe sent something besides model-list request and key")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			models, err := cloudModels(context.Background(), srv.URL+"/v1", "sensitive-test-key", nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("models=%v error=%v", models, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-test-key") {
				t.Fatal("credential reflected")
			}
			if tc.name == "catalog" && strings.Join(models, ",") != "a,z" {
				t.Fatalf("not normalized: %v", models)
			}
		})
	}
	var forwarded bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer source.Close()
	if _, err := cloudModels(context.Background(), source.URL, "test-key", nil); err == nil || forwarded {
		t.Fatal("probe followed redirect")
	}
}

func TestProviderProbeRequiresConsentAndDoesNotSaveKey(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	defer func() { workspaceSessions = old }()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; fmt.Fprint(w, `{"data":[{"id":"fixture"}]}`) }))
	defer srv.Close()
	for _, consent := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/api/providers/models", strings.NewReader(fmt.Sprintf(`{"endpoint":%q,"key":"test-key","consent":%v}`, srv.URL, consent)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleProviderModels(w, r)
		if consent && w.Code != 200 || !consent && w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "test-key") {
			t.Fatal("key returned")
		}
	}
	if requests != 1 || len(workspaceSessions.keys) != 0 || len(allKV(bkProviders)) != 0 {
		t.Fatal("probe persisted or ran without consent")
	}
}

func TestCloudUsageOptionCanBeDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		if strings.Contains(b.String(), "stream_options") {
			t.Error("unsupported option sent")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Test")+"data: [DONE]\n\n")
	}))
	defer srv.Close()
	a := cloudRuntimeAdapter{provider: CloudProvider{Endpoint: srv.URL, Model: "fixture", UsageMode: "none"}, key: "test-key"}
	if _, err := a.Run(context.Background(), RuntimeTurn{}, func(StreamEvent) bool { return true }); err != nil {
		t.Fatal(err)
	}
}
