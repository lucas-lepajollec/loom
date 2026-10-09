package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type notesTransport func(*http.Request) (*http.Response, error)

func (f notesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func notesResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestReleaseNotesRangeAndPagination(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: notesTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || !strings.HasPrefix(r.URL.Path, "/repos/openai/codex/releases") {
			t.Fatal(r)
		}
		releases := []githubRelease{}
		if calls == 1 {
			for i := 0; i < 100; i++ {
				releases = append(releases, githubRelease{Tag: "rust-v0.163.0", Body: "- outside range"})
			}
		} else {
			releases = []githubRelease{{Tag: "rust-v0.162.0", Body: "## Features\n- New capability"}, {Tag: "rust-v0.161.0", Body: "- Intermediate"}, {Tag: "rust-v0.160.0", Body: "- Draft", Draft: true}, {Tag: "rust-v0.159.2", Body: "- Already tested"}}
		}
		data, _ := json.Marshal(releases)
		return notesResponse(string(data), 200), nil
	})}
	notes, err := fetchReleaseRange(client, "https://api.github.com", "openai/codex", "codex-cli 0.159.2", "codex-cli 0.162.0")
	if err != nil || calls != 2 || !strings.Contains(notes, "Intermediate") || strings.Contains(notes, "outside") || strings.Contains(notes, "Already tested") || strings.Contains(notes, "Draft") {
		t.Fatal(notes, calls, err)
	}
}
func TestReleaseNotesExcerptBound(t *testing.T) {
	body := "# Features\nplain prose\n```md\n- example only\n```\n"
	for i := 0; i < 90; i++ {
		body += fmt.Sprintf("- Change %d %s\n", i, strings.Repeat("界", 300))
	}
	notes := notesExcerpt(body, 80)
	if len(strings.Split(notes, "\n")) != 80 || strings.Contains(notes, "plain prose") || strings.Contains(notes, "example only") || !strings.Contains(notes, "truncated") || len(notes) > 80*500 {
		t.Fatal(notes)
	}
	if got := notesExcerpt(body, 0); got != "" {
		t.Fatal(got)
	}
}
func TestReleaseNotesNetworkFailureAndChangelogFallback(t *testing.T) {
	client := &http.Client{Transport: notesTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("sandbox network denied") })}
	c := candidate{ID: "pi", Version: "1.1.0"}
	got := releaseNotes(c, "1.0.0", client, "https://api.github.com")
	if !strings.Contains(got, "Release notes unavailable") || !strings.Contains(got, "https://github.com/earendil-works/pi/releases") {
		t.Fatal(got)
	}
	c.Notes = got
	plan := planReview([]candidate{c}, nil)
	if len(plan.Passing) != 1 || len(plan.Attention) != 0 {
		t.Fatal("notes failures must not be attention", plan)
	}
	c.PackageDir = t.TempDir()
	body := "# Changelog\n## 1.2.0\n- Future\n## 1.1.0\n### Added\n- Current\n## 1.0.1\n- Intermediate\n## 1.0.0\n- Already tested\n"
	if err := os.WriteFile(filepath.Join(c.PackageDir, "CHANGELOG.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got = releaseNotes(c, "1.0.0", client, "https://api.github.com")
	if !strings.Contains(got, "Current") || !strings.Contains(got, "Intermediate") || strings.Contains(got, "Future") || strings.Contains(got, "Already tested") || !strings.Contains(got, "npm tarball") {
		t.Fatal(got)
	}
}
func TestReleaseNotesHTTPAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{{"limited", 403}, {"invalid", 200}, {"[]", 200}} {
		client := &http.Client{Transport: notesTransport(func(*http.Request) (*http.Response, error) { return notesResponse(tc.body, tc.status), nil })}
		got := releaseNotes(candidate{ID: "codex", Version: "codex-cli 0.162.0"}, "codex-cli 0.159.2", client, "https://api.github.com")
		if !strings.Contains(got, "Release notes unavailable") {
			t.Fatal(got)
		}
	}
}
func TestOpenCodeRepositoryFromPackageMetadata(t *testing.T) {
	dir := t.TempDir()
	for _, repository := range []string{`"git+https://github.com/anomalyco/opencode.git"`, `{"type":"git","url":"https://github.com/sst/opencode.git","directory":"packages/opencode"}`} {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"repository":`+repository+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		got := notesRepository(candidate{ID: "opencode", PackageDir: dir})
		if got != "anomalyco/opencode" && got != "sst/opencode" {
			t.Fatal(got)
		}
	}
	if githubRepository("https://github.com.evil.test/owner/repo") != "" || githubRepository("https://example.com/owner/repo") != "" {
		t.Fatal("non-GitHub repository accepted")
	}
}
