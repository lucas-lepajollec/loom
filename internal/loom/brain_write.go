package loom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxSecondBrainWrite = 1 << 20

func primarySecondBrain() (string, string, error) {
	e, err := theBrain().get()
	if err != nil {
		return "", "", err
	}
	for _, source := range e.Sources() {
		if source.Primary && !source.ReadOnly && source.Permission == "write" {
			return source.Label, source.Path, nil
		}
	}
	return "", "", errors.New("no writable primary second brain is configured")
}

func writableSecondBrain(sourceID string) (string, string, error) {
	e, err := theBrain().get()
	if err != nil {
		return "", "", err
	}
	for _, source := range e.Sources() {
		selected := source.ID == sourceID || (sourceID == "" && source.Primary)
		if selected && !source.ReadOnly {
			if source.Permission != "write" {
				return "", "", errors.New("this second brain is read-only; explicitly allow writing in Brain settings first")
			}
			return source.Label, source.Path, nil
		}
	}
	if sourceID != "" {
		return "", "", errors.New("writable second brain not found")
	}
	return "", "", errors.New("no writable primary second brain is configured")
}

func hasWritableSecondBrain() bool {
	e, err := theBrain().get()
	if err != nil {
		return false
	}
	for _, source := range e.Sources() {
		if !source.ReadOnly && source.Permission == "write" {
			return true
		}
	}
	return false
}

func secondBrainPath(sourceID, relative string) (string, error) {
	_, root, err := writableSecondBrain(sourceID)
	if err != nil {
		return "", err
	}
	relative = filepath.Clean(strings.TrimSpace(relative))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("use a relative Markdown path inside the primary second brain")
	}
	if ext := strings.ToLower(filepath.Ext(relative)); ext != ".md" && ext != ".markdown" {
		return "", errors.New("second-brain writes are limited to Markdown files")
	}
	full := filepath.Join(root, relative)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the primary second brain")
	}
	return full, nil
}

func refreshSecondBrainAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if e, err := theBrain().get(); err == nil {
			_ = e.Refresh(ctx)
		}
	}()
}

func secondBrainWriteSource(sourceID, relative, content string) error {
	if len(content) > maxSecondBrainWrite {
		return errors.New("Markdown file exceeds 1 MiB")
	}
	full, err := secondBrainPath(sourceID, relative)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_, root, err := writableSecondBrain(sourceID)
	if err != nil {
		return err
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedDir, dirErr := filepath.EvalSymlinks(dir)
	within, relErr := filepath.Rel(resolvedRoot, resolvedDir)
	if rootErr != nil || dirErr != nil || relErr != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return errors.New("resolved path escapes the primary second brain")
	}
	full = filepath.Join(resolvedDir, filepath.Base(full))
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(full); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to replace a symlink")
		}
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".loom-brain-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.WriteString(content)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, full)
	}
	if err == nil {
		refreshSecondBrainAsync()
	}
	return err
}

func secondBrainWrite(relative, content string) error {
	return secondBrainWriteSource("", relative, content)
}

func secondBrainEditSource(sourceID, relative, oldText, newText string) error {
	full, err := secondBrainPath(sourceID, relative)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	count := strings.Count(string(b), oldText)
	if oldText == "" || count != 1 {
		return fmt.Errorf("old text must occur exactly once (found %d)", count)
	}
	return secondBrainWriteSource(sourceID, relative, strings.Replace(string(b), oldText, newText, 1))
}

func secondBrainEdit(relative, oldText, newText string) error {
	return secondBrainEditSource("", relative, oldText, newText)
}

func brainWriteTool() Tool {
	return Tool{Type: "function", Function: ToolFunction{Name: "brain_write", Description: "Create or replace a Markdown file in a write-authorized second brain. Omit source for the primary. Use another source only when the user explicitly asks to update it.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"source": map[string]any{"type": "string", "description": "optional second-brain source id; omit for the primary"}, "file": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"file", "content"}}}}
}

func brainEditTool() Tool {
	return Tool{Type: "function", Function: ToolFunction{Name: "brain_edit", Description: "Update a write-authorized second brain by replacing one exact unique Markdown snippet. Omit source for the primary; use another source only on an explicit user request.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"source": map[string]any{"type": "string", "description": "optional second-brain source id; omit for the primary"}, "file": map[string]any{"type": "string"}, "old": map[string]any{"type": "string"}, "new": map[string]any{"type": "string"}}, "required": []string{"file", "old", "new"}}}}
}
