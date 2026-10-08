package brain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Migrate is restart-safe: deterministic filenames are checked before import;
// the report is the completion marker, written only after all writes and rename.
// LegacyRoot/LegacyBase also support the original encrypted fallback layout.
func (s *MarkdownStore) Migrate(legacyOpts MemoryStoreOptions, extra []MemoryItem, others ...MemoryStoreOptions) error {
	s.mu.Lock()
	root, _, _, err := s.open()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	_, err = s.read(root, ".loom/migration-memory.md", 1<<20)
	root.Close()
	s.mu.Unlock()
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	inputs := append([]MemoryStoreOptions{legacyOpts}, others...)
	items := append([]MemoryItem{}, extra...)
	type source struct {
		opts      MemoryStoreOptions
		root      *os.Root
		malformed int
	}
	sources := []source{}
	for _, opts := range inputs {
		legacyRoot, e := os.OpenRoot(opts.Dir)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		defer legacyRoot.Close()
		legacy := NewMemoryStore(opts)
		if e = legacy.scan(legacyRoot); e != nil {
			return e
		}
		// A crash after archive rename but before the report resumes from the
		// archived files instead of losing the migration audit.
		if len(legacy.items) == 0 {
			if _, e = legacyRoot.Stat(filepath.Join(opts.Base, "memory.legacy")); e == nil {
				archiveRoot, e := legacyRoot.OpenRoot(filepath.Join(opts.Base, "memory.legacy"))
				if e != nil {
					return e
				}
				for _, class := range memoryClasses {
					f, e := archiveRoot.Open(class)
					if e != nil {
						continue
					}
					entries, e := f.ReadDir(-1)
					f.Close()
					if e != nil {
						archiveRoot.Close()
						return e
					}
					for _, entry := range entries {
						if !entry.Type().IsRegular() {
							continue
						}
						b, e := legacy.read(archiveRoot, filepath.Join(class, entry.Name()))
						if e != nil {
							continue
						}
						item, e := unmarshalMemory(b)
						if e == nil {
							legacy.items[item.ID] = item
						}
					}
				}
				archiveRoot.Close()
			}
		}
		for _, item := range legacy.items {
			items = append(items, item)
		}
		// Archive before creating Memory/: on case-insensitive filesystems the
		// fallback's old memory/ and new Memory/ are the same path.
		old := filepath.Join(opts.Base, "memory")
		archived := filepath.Join(opts.Base, "memory.legacy")
		if _, e = legacyRoot.Lstat(old); e == nil {
			if e = noSymlinks(legacyRoot, old); e != nil {
				return e
			}
			if _, e = legacyRoot.Lstat(archived); e == nil {
				return errors.New("legacy archive already exists; old memory retained for manual reconciliation")
			} else if !os.IsNotExist(e) {
				return e
			}
			if e = legacyRoot.Rename(old, archived); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		sources = append(sources, source{opts, legacyRoot, legacy.malformed})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	var report strings.Builder
	report.WriteString("# Markdown memory migration\n\nOld memory is retained. Pending candidates, handoffs and session state are skipped.\n\n")
	for _, item := range items {
		skip := item.Status == "candidate" || item.Status == "expired" || item.Status == "superseded" || item.Class == "session"
		for _, tag := range []string{"handoff", "discussion-state", "project-state", "session-summary", "discussion-refinement"} {
			skip = skip || contains(item.Tags, tag)
		}
		if item.Scope != "global" && !strings.HasPrefix(item.Scope, "project:") {
			skip = true
		}
		if skip {
			fmt.Fprintf(&report, "- Skipped `%s` (%s).\n", item.ID, item.Status)
			continue
		}
		kind := "project"
		switch item.Class {
		case "reflex", "procedural":
			kind = "feedback"
		case "semantic":
			if item.Scope == "global" {
				kind = "user"
			}
		}
		name := strings.Join(strings.Fields(item.Text), " ")
		r := []rune(name)
		name = string(r[:min(len(r), 80)])
		if contains(item.Tags, ProfileTag) {
			name, kind = "Profile", "user"
		}
		if contains(item.Tags, ProjectNotesTag) {
			name, kind = "Project notes", "project"
		}
		file := "legacy-" + item.ID + ".md"
		if !memoryFilename(file) {
			file = "legacy-" + shortIdentity(item.ID) + ".md"
		}
		if _, err = s.Read(MemoryRead{Scope: item.Scope, File: file}); err == nil {
			fmt.Fprintf(&report, "- Already imported `%s` → `%s`.\n", item.ID, file)
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		text := item.Text
		if kind == "feedback" || kind == "project" {
			text += "\n\n**Why:** Retained from Loom's legacy memory; no additional rationale recorded.\n\n**How to apply:** Use when relevant; check against current facts.\n"
		}
		// Legacy text already fits 8 KiB; format additions must not discard it.
		if len(text) > 8<<10 {
			text = item.Text
		}
		// Distinct legacy records are preserved, even when they share a title.
		// Deterministic IDs make an interrupted migration safe to resume.
		_, err = s.writeMemory(MemoryWrite{Scope: item.Scope, File: file, Name: name, Description: "Imported Loom memory", Type: kind, Text: text}, map[string]any{"legacy_id": item.ID, "discussion_id": item.Provenance.DiscussionID}, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(&report, "- Imported `%s` → `%s` (%s, %s).\n", item.ID, file, item.Scope, kind)
	}
	for _, source := range sources {
		fmt.Fprintf(&report, "\nLegacy source %s: malformed files retained: %d.\n", source.opts.Base, source.malformed)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	root, layout, raw, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	if err = s.saveLayout(root, layout, raw); err != nil {
		return err
	}
	return s.write(root, ".loom/migration-memory.md", []byte(report.String()))
}
