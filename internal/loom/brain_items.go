package loom

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func (s *brainService) memoryStore() (*brain.MarkdownStore, error) {
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	sources, err := s.storage.LoadSources()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.storage.dir, "loom-memory")
	primary := false
	for _, source := range sources {
		if source.Primary && source.Permission == "write" {
			// An unavailable primary uses the existing encrypted fallback, never a
			// different user vault. Probe metadata writability without touching notes.
			root, openErr := os.OpenRoot(source.Path)
			if openErr == nil {
				if info, e := root.Lstat(".loom"); e == nil && info.Mode()&os.ModeSymlink != 0 {
					openErr = errors.New("primary metadata is a symlink")
				}
				if openErr == nil {
					openErr = root.MkdirAll(".loom", 0700)
				}
				if openErr == nil {
					f, e := root.OpenFile(".loom/.writable-"+fmt.Sprint(time.Now().UnixNano()), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if e == nil {
						name := f.Name()
						f.Close()
						_ = root.Remove(filepath.Join(".loom", filepath.Base(name)))
					}
					openErr = e
				}
				root.Close()
			}
			if openErr == nil {
				dir, primary = source.Path, true
			}
			break
		}
	}
	encrypted := !primary && memEncActive()
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	if s.items != nil && s.itemsDir == dir && s.itemsEncrypted == encrypted {
		return s.items, nil
	}
	if !primary {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	opts := brain.MarkdownOptions{Dir: dir, Available: brainAvailable, ProjectName: func(id string) (string, error) {
		if p, ok := getProject(id); ok {
			return p.Name, nil
		}
		return id, nil
	}}
	legacy := brain.MemoryStoreOptions{Dir: dir, Base: ".loom"}
	if !primary {
		opts.Encode, opts.Decode = encodeMemContent, decodeMemContent
		legacy.Dir, legacy.Base = s.storage.dir, "loom-memory"
		legacy.Decode = decodeMemContent
	}
	store := brain.NewMarkdownStore(opts)
	imported, err := s.importDistilledMemory()
	if err != nil {
		return nil, err
	}
	others := []brain.MemoryStoreOptions{}
	if primary {
		others = append(others, brain.MemoryStoreOptions{Dir: s.storage.dir, Base: "loom-memory", Decode: decodeMemContent})
	}
	if err = store.Migrate(legacy, imported, others...); err != nil {
		return nil, err
	}
	s.items, s.itemsDir, s.itemsEncrypted = store, dir, encrypted
	return store, nil
}
func (s *brainService) importDistilledMemory() ([]brain.MemoryItem, error) {
	distilled, err := s.loadDistilledLocked()
	if err != nil {
		return nil, err
	}
	out := []brain.MemoryItem{}
	classes := map[string]string{"decision": "episodic", "fact": "semantic", "preference": "semantic", "todo": "working"}
	for _, old := range distilled {
		if old.Review != "accepted" {
			continue
		}
		class, ok := classes[old.Kind]
		if !ok {
			return nil, errors.New("invalid distilled memory kind")
		}
		// Include the full identity when legacy records lack an ID.
		identity := old.ID
		if identity == "" {
			identity = fmt.Sprintf("%s\x00%d\x00%s\x00%s", old.Source.DiscussionID, old.Source.MessageIndex, old.Kind, old.Text)
		}
		hash := sha256.Sum256([]byte(identity))
		status := "active"
		if old.Review == "" {
			status = "uncertain"
		}
		created := old.Date.UnixMilli()
		if old.Date.IsZero() || created < 0 {
			created = time.Now().UnixMilli()
		}
		index := old.Source.MessageIndex
		out = append(out, brain.MemoryItem{ID: fmt.Sprintf("distilled_%x", hash), Class: class, Scope: "global", Text: old.Text, Importance: .5, Confidence: .7, CreatedAt: created, UpdatedAt: created, Provenance: brain.MemoryProvenance{Kind: "distilled", DiscussionID: old.Source.DiscussionID, MessageIndex: &index}, Status: status})
	}
	return out, nil
}

func (s *brainService) MemoryIndex(req brain.MemoryIndexRequest) (brain.MemoryIndexes, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryIndexes{}, err
	}
	out := brain.MemoryIndexes{}
	out.Global, err = store.List("global")
	if err == nil && req.ProjectID != "" {
		var project brain.MemoryFiles
		project, err = store.List("project:" + req.ProjectID)
		out.Project = &project
	}
	return out, err
}
func (s *brainService) MemoryRead(req brain.MemoryRead) (brain.MemoryFile, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryFile{}, err
	}
	return store.Read(req)
}
func (s *brainService) MemoryWrite(req brain.MemoryWrite) (brain.MemoryFile, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryFile{}, err
	}
	item, err := store.Write(req, nil)
	if err == nil {
		s.refreshMemoryIndex()
	}
	return item, err
}
func (s *brainService) MemoryDelete(req brain.MemoryRead) error {
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	if err = store.Delete(req); err == nil {
		s.refreshMemoryIndex()
	}
	return err
}
func (s *brainService) refreshMemoryIndex() {
	s.writeRefresh.Add(1)
	go func() {
		defer s.writeRefresh.Done()
		if e, err := s.get(); err == nil {
			_ = e.Refresh(context.Background())
		}
	}()
}
func (s *brainService) brainIndexExclusions() []string {
	out := s.skillsIndexExclusions()
	sources, err := s.storage.LoadSources()
	if err != nil {
		return out
	}
	for _, source := range sources {
		if source.Primary {
			store := brain.NewMarkdownStore(brain.MarkdownOptions{Dir: source.Path, Available: brainAvailable})
			if paths, err := store.OwnedDirectories(); err == nil {
				out = append(out, paths...)
			}
		}
	}
	return out
}
