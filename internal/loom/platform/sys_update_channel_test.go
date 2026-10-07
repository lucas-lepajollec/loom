package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
	if rel, err := FetchRelease(ChannelStable); err != nil || rel.TagName != "v0.2.9" {
		t.Fatalf("stable: %v %v", rel, err)
	}
	if rel, err := FetchRelease(ChannelEdge); err != nil || rel.TagName != "0.2.10-dev.20261007171200" {
		t.Fatalf("edge: %v %v", rel, err)
	}
	edgeName = "something else"
	if _, err := FetchRelease(ChannelEdge); err == nil {
		t.Fatal("an edge release without a development version was accepted")
	}
}
