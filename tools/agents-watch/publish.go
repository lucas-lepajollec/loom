package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func gh(argv ...string) ([]byte, error) {
	out, err := run(time.Minute, nil, append([]string{"gh"}, argv...)...)
	if err != nil {
		return out, fmt.Errorf("gh %s: %w (%s)", argv[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func attentionIssue(title, bodyPath string) error {
	out, err := gh("issue", "list", "--state", "open", "--search", title+" in:title", "--json", "number,title", "--limit", "100")
	if err != nil {
		return err
	}
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &issues); err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.Title == title {
			_, err := gh("issue", "edit", fmt.Sprint(issue.Number), "--body-file", bodyPath)
			return err
		}
	}
	_, err = gh("issue", "create", "--title", title, "--body-file", bodyPath)
	return err
}
func publishReview(outDir, reportPath string, registry []byte, failed bool) error {
	if os.Getenv("GITHUB_TOKEN") == "" && os.Getenv("GH_TOKEN") == "" {
		return errors.New("--publish requires GITHUB_TOKEN or GH_TOKEN")
	}
	if failed {
		opened := false
		for _, c := range candidates {
			if len(c.Problems) == 0 {
				continue
			}
			title := "Agents watch: " + c.ID + " " + c.Version + " needs attention"
			if err := attentionIssue(title, reportPath); err != nil {
				return err
			}
			opened = true
		}
		if !opened {
			return attentionIssue("Agents watch: registry latest needs attention", reportPath)
		}
		return nil
	}
	// Publish from a temporary worktree. Local reports and unrelated changes
	// cannot be included in the bot's deliberately small review commit.
	worktree, err := os.MkdirTemp("", "loom-watch-review-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(worktree)
	if out, err := run(time.Minute, nil, "git", "worktree", "add", "--detach", worktree, "HEAD"); err != nil {
		return fmt.Errorf("worktree: %w (%s)", err, out)
	}
	defer func() { _, _ = run(time.Minute, nil, "git", "worktree", "remove", "--force", worktree) }()
	versionsPath := filepath.Join(worktree, "internal/loom/harness/tested_versions.json")
	data, err := os.ReadFile(versionsPath)
	if err != nil {
		return err
	}
	var versions map[string][]string
	if err := json.Unmarshal(data, &versions); err != nil {
		return err
	}
	for _, c := range candidates {
		if !versionIncluded(versions[c.ID], c.Version) {
			versions[c.ID] = append(versions[c.ID], c.Version)
		}
	}
	if err := writeJSON(versionsPath, versions); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(worktree, "internal/loom/harness/acp_registry_snapshot.json"), registry, 0644); err != nil {
		return err
	}
	git := func(argv ...string) ([]byte, error) {
		return run(time.Minute, nil, append([]string{"git", "-C", worktree}, argv...)...)
	}
	status, err := git("status", "--porcelain")
	if err != nil {
		return err
	}
	if len(status) == 0 {
		fmt.Println("No new versions or registry changes to publish.")
		return nil
	}
	date := time.Now().UTC().Format("2006-01-02")
	branch := "agents-watch/" + date
	if _, err := git("checkout", "-b", branch); err != nil {
		return err
	}
	if _, err := git("add", "internal/loom/harness/tested_versions.json", "internal/loom/harness/acp_registry_snapshot.json"); err != nil {
		return err
	}
	if _, err := git("-c", "user.name=github-actions[bot]", "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com", "commit", "-m", "Agents watch: accept checked versions "+date); err != nil {
		return err
	}
	// Re-runs on the same date may update only this dated automation branch.
	remote, err := git("ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	lease := "--force-with-lease=refs/heads/" + branch + ":"
	if fields := strings.Fields(string(remote)); len(fields) > 0 {
		lease += fields[0]
	}
	if out, err := git("push", lease, "origin", "HEAD:refs/heads/"+branch); err != nil {
		return fmt.Errorf("push: %w (%s)", err, out)
	}
	// gh runs in the original checkout so repository resolution stays stable.
	out, err := gh("pr", "list", "--state", "open", "--head", branch, "--json", "number")
	if err != nil {
		return err
	}
	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return err
	}
	if len(prs) > 0 {
		_, err = gh("pr", "edit", fmt.Sprint(prs[0].Number), "--body-file", reportPath)
		return err
	}
	_, err = gh("pr", "create", "--head", branch, "--title", "Agents watch: checked versions "+date, "--body-file", reportPath)
	return err
}
func versionIncluded(versions []string, version string) bool {
	for _, v := range versions {
		if v == version {
			return true
		}
	}
	return false
}
