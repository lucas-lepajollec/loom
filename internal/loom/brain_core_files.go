package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// Core notes as readable files in the primary brain (opt-in): Loom/Toi.md for
// the profile and Loom/Projets/<project>.md per project. Both directions: a
// file edited elsewhere (Obsidian, Git) replaces the item; an item changed by
// Loom or an agent rewrites its file. The last written hash tells them apart.

type coreFilesConfig struct {
	Enabled bool   `json:"enabled"`
	Folder  string `json:"folder"`
}
type coreFilesState struct {
	Hashes map[string]string `json:"hashes"`
}

var coreFilesMu sync.Mutex

const coreFilesStateKey = "core_files_state"

var coreFileName = regexp.MustCompile(`[^\p{L}\p{N} ._-]+`)

func (s *brainService) coreFilesConfig() coreFilesConfig {
	cfg := coreFilesConfig{Folder: "Loom"}
	if b, err := s.storage.read("core_files.json", 4096); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.Folder == "" {
		cfg.Folder = "Loom"
	}
	return cfg
}

func validCoreFolder(folder string) bool {
	clean := filepath.Clean(folder)
	return folder != "" && len(folder) <= 120 && !filepath.IsAbs(folder) && clean != "." && !strings.HasPrefix(clean, "..") && !strings.HasPrefix(clean, ".loom") && !strings.ContainsAny(folder, "\x00\r\n")
}

func coreFileText(item brain.MemoryItem) string { return strings.TrimSpace(item.Text) + "\n" }
func coreHash(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// syncCoreFiles reconciles the core notes with their files; it is cheap and
// safe to call after every memory write and periodically.
func (s *brainService) syncCoreFiles() error {
	cfg := s.coreFilesConfig()
	if !cfg.Enabled || !validCoreFolder(cfg.Folder) {
		return nil
	}
	e, err := s.get()
	if err != nil {
		return err
	}
	var primary *brain.Source
	for _, src := range e.Sources() {
		if src.Primary && !src.ReadOnly && src.Permission == "write" {
			primary = &src
			break
		}
	}
	if primary == nil {
		return errors.New("no writable primary brain")
	}
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	list, err := store.List(brain.MemoryFilter{Classes: []string{"semantic"}, Limit: 5000})
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(primary.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	coreFilesMu.Lock()
	defer coreFilesMu.Unlock()
	state := coreFilesState{Hashes: map[string]string{}}
	_ = getStoreJSON(bkState, coreFilesStateKey, &state)
	if state.Hashes == nil {
		state.Hashes = map[string]string{}
	}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, item := range list.Items {
		rel := ""
		switch {
		case item.Scope == "global" && slices.Contains(item.Tags, brain.ProfileTag):
			rel = filepath.Join(cfg.Folder, "Toi.md")
		case strings.HasPrefix(item.Scope, "project:") && slices.Contains(item.Tags, brain.ProjectNotesTag):
			name := strings.TrimPrefix(item.Scope, "project:")
			if p, ok := getProject(name); ok && strings.TrimSpace(p.Name) != "" {
				name = p.Name
			}
			name = strings.TrimSpace(coreFileName.ReplaceAllString(name, "-"))
			if name == "" || strings.HasPrefix(name, ".") {
				continue
			}
			rel = filepath.Join(cfg.Folder, "Projets", name+".md")
		default:
			continue
		}
		current, readErr := root.ReadFile(rel)
		written := state.Hashes[rel]
		switch {
		case readErr == nil && coreHash(string(current)) != written && coreHash(string(current)) != coreHash(item.Text) && written != "":
			// Edited outside Loom since the last write: the file wins.
			text := strings.TrimSpace(string(current))
			if _, err := store.Update(brain.UpdateMemoryRequest{ID: item.ID, Patch: brain.MemoryPatch{Text: &text}}); err != nil {
				keep(err)
				continue
			}
			state.Hashes[rel] = coreHash(text)
		case readErr != nil || coreHash(string(current)) != coreHash(item.Text):
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				keep(err)
				continue
			}
			if err := root.WriteFile(rel, []byte(coreFileText(item)), 0o644); err != nil {
				keep(err)
				continue
			}
			state.Hashes[rel] = coreHash(item.Text)
		default:
			state.Hashes[rel] = coreHash(item.Text)
		}
	}
	keep(putStoreJSON(bkState, coreFilesStateKey, state))
	return firstErr
}

// The core folder is mirrored memory, not notes: keep it out of the index.
func (s *brainService) coreFilesIndexExclusions() []string {
	cfg := s.coreFilesConfig()
	if !cfg.Enabled || !validCoreFolder(cfg.Folder) {
		return nil
	}
	e, err := s.get()
	if err != nil {
		return nil
	}
	for _, src := range e.Sources() {
		if src.Primary {
			if dir, err := filepath.EvalSymlinks(filepath.Join(src.Path, cfg.Folder)); err == nil {
				return []string{dir}
			}
		}
	}
	return nil
}

// GET/POST /api/brain/core-files {enabled, folder}.
func (s *brainService) coreFilesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req coreFilesConfig
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Folder == "" {
			req.Folder = "Loom"
		}
		if !validCoreFolder(req.Folder) {
			brainResponse(w, nil, errors.New("folder must be a relative path inside the primary brain"))
			return
		}
		b, _ := json.Marshal(req)
		if err := s.storage.write("core_files.json", b); err != nil {
			brainResponse(w, nil, err)
			return
		}
		if req.Enabled {
			if err := s.syncCoreFiles(); err != nil {
				brainResponse(w, nil, err)
				return
			}
		}
	} else if !workspaceMethod(w, r, "GET") {
		return
	}
	brainResponse(w, s.coreFilesConfig(), nil)
}
