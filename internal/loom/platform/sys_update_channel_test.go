package platform

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFetchReleaseChannels(t *testing.T) {
	edgeName := "edge 0.2.10-dev.20261007171200"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.9","name":"v0.2.9"}`))
		case "/tags/edge":
			_, _ = w.Write([]byte(`{"tag_name":"edge","name":"` + edgeName + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := releasesAPI
	releasesAPI = srv.URL + "/"
	defer func() { releasesAPI = old }()
	forgetReleases()
	if rel, err := FetchRelease(ChannelStable); err != nil || rel.TagName != "v0.2.9" {
		t.Fatalf("stable: %v %v", rel, err)
	}
	if rel, err := FetchRelease(ChannelEdge); err != nil || rel.TagName != "0.2.10-dev.20261007171200" {
		t.Fatalf("edge: %v %v", rel, err)
	}
	edgeName = "something else"
	forgetReleases()
	if _, err := FetchRelease(ChannelEdge); err == nil {
		t.Fatal("an edge release without a development version was accepted")
	}
}

// A lookup is reused for a while, and once GitHub reports the anonymous
// hourly limit Loom stops asking until the reset and says when to retry.
func TestFetchReleaseCacheAndRateLimit(t *testing.T) {
	calls, limited := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(20*time.Minute).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.9","name":"v0.2.9"}`))
	}))
	defer srv.Close()
	old := releasesAPI
	releasesAPI = srv.URL + "/"
	defer func() { releasesAPI = old; forgetReleases() }()
	forgetReleases()
	for i := 0; i < 3; i++ {
		if _, err := FetchRelease(ChannelStable); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("GitHub asked %d times, want 1", calls)
	}
	forgetReleases()
	limited = true
	_, err := FetchRelease(ChannelStable)
	var rl RateLimitError
	if !errors.As(err, &rl) || !strings.Contains(err.Error(), "try again after") {
		t.Fatalf("rate limit not reported: %v", err)
	}
	before := calls
	if _, err := FetchRelease(ChannelEdge); !errors.As(err, &rl) || calls != before {
		t.Fatalf("asked GitHub again during the limit: %v (%d calls)", err, calls-before)
	}
}
