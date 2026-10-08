package brain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type TranscriptEntry struct{ Role, Text, Tool string }
type Transcript struct {
	ID        string            `yaml:"discussion_id"`
	Title     string            `yaml:"title"`
	ProjectID string            `yaml:"project_id"`
	Project   string            `yaml:"project"`
	Executor  string            `yaml:"executor"`
	Model     string            `yaml:"model"`
	CreatedAt int64             `yaml:"created_at"`
	UpdatedAt int64             `yaml:"updated_at"`
	Entries   []TranscriptEntry `yaml:"-"`
}

func RenderTranscript(t Transcript) ([]byte, error) {
	head, err := yaml.Marshal(t)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n" + string(head) + "---\n# " + singleLine(t.Title) + "\n")
	for _, entry := range t.Entries {
		if entry.Tool != "" {
			b.WriteString("\n- Tool: " + singleLine(entry.Tool) + "\n")
			continue
		}
		if entry.Role == "user" || entry.Role == "assistant" {
			b.WriteString("\n## " + entry.Role + "\n\n" + entry.Text + "\n")
		}
	}
	if b.Len() > 16<<20 {
		return nil, errors.New("discussion transcript exceeds 16 MiB")
	}
	return []byte(b.String()), nil
}
func (s *MarkdownStore) WriteTranscript(t Transcript) (string, error) {
	if t.ID == "" {
		return "", errors.New("discussion id is required")
	}
	b, err := RenderTranscript(t)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, raw, err := s.open()
	if err != nil {
		return "", err
	}
	defer root.Close()
	folder := "_"
	if t.ProjectID != "" {
		if _, err = s.scopeFolder(root, &l, raw, "project:"+t.ProjectID); err != nil {
			return "", err
		}
		folder = l.ProjectFolders[t.ProjectID]
	}
	old := l.DiscussionFiles[t.ID]
	name := filepath.Base(old)
	if old == "" {
		date := time.UnixMilli(t.CreatedAt).UTC().Format("2006-01-02")
		name = date + "-" + Slug(t.Title) + "-" + shortIdentity(t.ID) + ".md"
	}
	path := filepath.ToSlash(filepath.Join(l.Discussions, folder, name))
	if err = s.write(root, path, b); err != nil {
		return "", err
	}
	l.DiscussionFiles[t.ID] = path
	if err = s.saveLayout(root, l, raw); err != nil {
		return "", err
	}
	if old != "" && old != path {
		if err = noSymlinks(root, old); err == nil {
			err = root.Remove(old)
		}
		if os.IsNotExist(err) {
			err = nil
		}
	}
	return path, err
}
func (s *MarkdownStore) DiscussionPath(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, _, err := s.open()
	if err != nil {
		return "", err
	}
	defer root.Close()
	return l.DiscussionFiles[id], nil
}
