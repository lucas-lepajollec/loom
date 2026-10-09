package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

var releaseVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?`)

func versionNumber(text string) string {
	version := "v" + releaseVersion.FindString(text)
	if !semver.IsValid(version) {
		return ""
	}
	return version
}

// Find metadata next to an npm shim/symlink without invoking npm or accessing a
// user's native configuration. --install supplies the exact package directory.
func installedPackageDir(c candidate) string {
	binary, err := exec.LookPath(c.Binary)
	if err != nil {
		return ""
	}
	target, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return ""
	}
	for dir := filepath.Dir(target); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		var pkg struct {
			Name string `json:"name"`
		}
		if err == nil && json.Unmarshal(data, &pkg) == nil && pkg.Name == c.Package {
			return dir
		}
	}
	return ""
}
func notesRepository(c candidate) string {
	switch c.ID {
	case "codex":
		return "openai/codex"
	case "pi":
		return "earendil-works/pi"
	case "claude-acp":
		return "agentclientprotocol/claude-agent-acp"
	case "opencode":
		data, _ := os.ReadFile(filepath.Join(c.PackageDir, "package.json"))
		var pkg struct {
			Repository json.RawMessage `json:"repository"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			var repository string
			if json.Unmarshal(pkg.Repository, &repository) != nil {
				var field struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal(pkg.Repository, &field)
				repository = field.URL
			}
			if repo := githubRepository(repository); repo != "" {
				return repo
			}
		}
		return "sst/opencode"
	}
	return ""
}
func githubRepository(repository string) string {
	repository = strings.TrimPrefix(repository, "git+")
	repository = strings.Replace(repository, "git@github.com:", "https://github.com/", 1)
	if strings.HasPrefix(repository, "github:") {
		repository = "https://github.com/" + strings.TrimPrefix(repository, "github:")
	}
	u, err := url.Parse(repository)
	if err != nil || u.Hostname() != "github.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return strings.Join(parts, "/")
}

type githubRelease struct {
	Tag   string `json:"tag_name"`
	Name  string `json:"name"`
	Body  string `json:"body"`
	URL   string `json:"html_url"`
	Draft bool   `json:"draft"`
}

func fetchReleaseRange(client *http.Client, api, repository, previous, current string) (string, error) {
	upper, lower := versionNumber(current), versionNumber(previous)
	if upper == "" || (previous != "" && lower == "") {
		return "", fmt.Errorf("unrecognized release range")
	}
	releases := []githubRelease{}
	complete := false
	for page := 1; page <= 10; page++ {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", api, repository, page), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		// Public metadata only: never forward the publishing token to fetches.
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("release notes HTTP %d", resp.StatusCode)
		}
		if readErr != nil {
			return "", readErr
		}
		if len(data) > 4<<20 {
			return "", fmt.Errorf("release notes response too large")
		}
		var batch []githubRelease
		if err := json.Unmarshal(data, &batch); err != nil {
			return "", err
		}
		for _, release := range batch {
			if release.Draft {
				continue
			}
			v := versionNumber(release.Tag)
			if v == "" {
				continue
			}
			if lower != "" && semver.Compare(v, lower) <= 0 {
				complete = true
				continue
			}
			if semver.Compare(v, upper) <= 0 {
				releases = append(releases, release)
			}
		}
		if len(batch) < 100 {
			complete = true
		}
		if complete {
			break
		}
	}
	if !complete {
		return "", fmt.Errorf("release range exceeds pagination bound")
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("no release notes in range")
	}
	sort.SliceStable(releases, func(i, j int) bool {
		return semver.Compare(versionNumber(releases[i].Tag), versionNumber(releases[j].Tag)) > 0
	})
	var text strings.Builder
	for _, release := range releases {
		link := release.URL
		if !strings.HasPrefix(link, "https://github.com/"+repository+"/releases/") {
			link = "https://github.com/" + repository + "/releases"
		}
		fmt.Fprintf(&text, "### [%s](%s)\n%s\n", release.Tag, link, release.Body)
	}
	excerpt := notesExcerpt(text.String(), 80)
	if excerpt == "" {
		return "", fmt.Errorf("release notes contain no headings or bullets")
	}
	return excerpt, nil
}

// Excerpts deliberately include headings and bullet lines only. Fenced examples
// cannot masquerade as notes. Bound both lines and line lengths for issue bodies.
func notesExcerpt(body string, limit int) string {
	lines := []string{}
	fenced := false
	truncated := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		heading := strings.HasPrefix(line, "#")
		bullet := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ")
		if !heading && !bullet {
			continue
		}
		if len(lines) >= limit {
			truncated = true
			break
		}
		if utf8.RuneCountInString(line) > 160 {
			line = string([]rune(line)[:159]) + "…"
		}
		lines = append(lines, line)
	}
	if truncated && limit > 0 {
		lines[limit-1] = "- … excerpt truncated; see linked release notes."
	}
	return strings.Join(lines, "\n")
}
func changelogRange(body, previous, current string) string {
	lower, upper := versionNumber(previous), versionNumber(current)
	if upper == "" || (previous != "" && lower == "") {
		return ""
	}
	active := false
	lines := []string{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			if v := versionNumber(line); v != "" {
				active = semver.Compare(v, upper) <= 0 && (lower == "" || semver.Compare(v, lower) > 0)
			}
		}
		if active {
			lines = append(lines, line)
		}
	}
	return notesExcerpt(strings.Join(lines, "\n"), 80)
}
func releaseNotes(c candidate, previous string, client *http.Client, api string) string {
	repository := notesRepository(c)
	link := "https://github.com/" + repository + "/releases"
	intro := fmt.Sprintf("Release notes (%s → %s): [%s](%s).", previous, c.Version, repository, link)
	notes, err := fetchReleaseRange(client, api, repository, previous, c.Version)
	if err == nil {
		return intro + "\n\n" + notes
	}
	// The installed files come from the npm tarball used for this exact version.
	for _, name := range []string{"CHANGELOG.md", "changelog.md"} {
		if c.PackageDir == "" {
			break
		}
		file, err := os.Open(filepath.Join(c.PackageDir, name))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, 4<<20+1))
		_ = file.Close()
		if err != nil || len(data) > 4<<20 {
			continue
		}
		if notes := changelogRange(string(data), previous, c.Version); notes != "" {
			return intro + " Source: package `" + name + "` from the installed npm tarball.\n\n" + notes
		}
	}
	return intro + " Release notes unavailable."
}
