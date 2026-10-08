package loom

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// The application boundary chooses the primary writable vault, just as
// primarySecondBrain does, without changing retrieval/source selection.
func (s *brainService) memoryStore() (*brain.MemoryStore, error) {
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	e, err := s.get()
	if err != nil {
		return nil, err
	}
	dir, base := s.storage.dir, "loom-memory"
	for _, source := range e.Sources() {
		if source.Primary && !source.ReadOnly && source.Permission == "write" {
			dir, base = source.Path, ".loom"
			break
		}
	}
	encrypted := base == "loom-memory" && memEncActive()
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	if s.items == nil || s.itemsDir != dir || s.itemsBase != base || s.itemsEncrypted != encrypted {
		if base == "loom-memory" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
		}
		opts := brain.MemoryStoreOptions{Dir: dir, Base: base, Available: brainAvailable, ImportDistilled: s.importDistilledMemory}
		if base == "loom-memory" {
			opts.Encode, opts.Decode = encodeMemContent, decodeMemContent
		}
		s.items = brain.NewMemoryStore(opts)
		s.itemsDir, s.itemsBase, s.itemsEncrypted = dir, base, encrypted
	}
	return s.items, nil
}
func (s *brainService) importDistilledMemory() ([]brain.MemoryItem, error) {
	s.distillMu.Lock()
	defer s.distillMu.Unlock()
	distilled, err := s.loadDistilledLocked()
	if err != nil {
		return nil, err
	}
	out := []brain.MemoryItem{}
	classes := map[string]string{"decision": "episodic", "fact": "semantic", "preference": "semantic", "todo": "working"}
	for _, old := range distilled {
		if old.Review != "" && old.Review != "accepted" {
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
func (s *brainService) Remember(req brain.RememberRequest) (brain.MemoryItem, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryItem{}, err
	}
	item, err := store.Remember(req)
	s.afterCoreWrite(item, err)
	return item, err
}
func (s *brainService) UpdateMemory(req brain.UpdateMemoryRequest) (brain.MemoryItem, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryItem{}, err
	}
	item, err := store.Update(req)
	s.afterCoreWrite(item, err)
	return item, err
}

// A core note changed through Loom (UI or agent): mirror it now, off the path.
func (s *brainService) afterCoreWrite(item brain.MemoryItem, err error) {
	if err == nil && (slices.Contains(item.Tags, brain.ProfileTag) || slices.Contains(item.Tags, brain.ProjectNotesTag)) {
		go s.syncCoreFiles()
	}
}
func (s *brainService) ForgetMemory(req brain.ForgetMemoryRequest) (brain.MemoryItem, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryItem{}, err
	}
	return store.Forget(req.ID)
}
func (s *brainService) ListMemory(filter brain.MemoryFilter) (brain.MemoryList, error) {
	store, err := s.memoryStore()
	if err != nil {
		return brain.MemoryList{}, err
	}
	return store.List(filter)
}
func (s *brainService) TouchMemory(ids []string) error {
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	return store.Touch(ids)
}
func memoryQueryValues(r *http.Request, keys ...string) []string {
	out := []string{}
	for _, key := range keys {
		for _, value := range r.URL.Query()[key] {
			for _, part := range strings.Split(value, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		}
	}
	return out
}
func (s *brainService) itemsHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if r.Method == "POST" {
		var req brain.RememberRequest
		if !workspaceDecode(w, r, &req) {
			return
		}
		item, err := s.Remember(req)
		brainResponse(w, brain.MemoryResult{OK: true, Item: item}, err)
		return
	}
	q := r.URL.Query()
	filter := brain.MemoryFilter{Classes: memoryQueryValues(r, "class", "classes"), Scopes: memoryQueryValues(r, "scope", "scopes"), Status: q.Get("status"), Query: q.Get("query")}
	if q.Get("limit") != "" {
		var err error
		filter.Limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			brainResponse(w, nil, errors.New("invalid limit"))
			return
		}
	}
	result, err := s.ListMemory(filter)
	brainResponse(w, result, err)
}
func (s *brainService) updateMemoryHTTP(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req brain.UpdateMemoryRequest
	if !workspaceDecode(w, r, &req) {
		return
	}
	item, err := s.UpdateMemory(req)
	brainResponse(w, brain.MemoryResult{OK: true, Item: item}, err)
}
func (s *brainService) forgetMemoryHTTP(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req brain.ForgetMemoryRequest
	if !workspaceDecode(w, r, &req) {
		return
	}
	item, err := s.ForgetMemory(req)
	brainResponse(w, brain.MemoryResult{OK: true, Item: item}, err)
}
