package brain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const maxMemoryFile = 64 << 10

// MemoryStoreOptions confines all IO to Dir. Base is .loom for a user vault,
// or loom-memory for Loom's private fallback. Codecs apply only to the fallback.
type MemoryStoreOptions struct {
	Dir             string
	Base            string
	Encode          func([]byte) ([]byte, error)
	Decode          func([]byte) ([]byte, error)
	Available       func() error
	ImportDistilled func() ([]MemoryItem, error)
}

// MemoryStore is retained for legacy migration and fixtures. Runtime memory
// operations use MarkdownStore; this store no longer participates in context.
type MemoryStore struct {
	mu        sync.Mutex
	opts      MemoryStoreOptions
	items     map[string]MemoryItem
	malformed int
}

func NewMemoryStore(opts MemoryStoreOptions) *MemoryStore { return &MemoryStore{opts: opts} }
func (s *MemoryStore) read(root *os.Root, path string) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("memory file is not regular")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxMemoryFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMemoryFile {
		return nil, errors.New("memory file exceeds 64 KiB")
	}
	if s.opts.Decode != nil {
		return s.opts.Decode(b)
	}
	return b, nil
}
func (s *MemoryStore) write(root *os.Root, path string, data []byte) error {
	if len(data) > maxMemoryFile-1024 {
		return errors.New("memory file exceeds storage limit")
	}
	if s.opts.Available != nil {
		if err := s.opts.Available(); err != nil {
			s.items = nil
			return err
		}
	}
	if s.opts.Encode != nil {
		var err error
		data, err = s.opts.Encode(data)
		if err != nil {
			return err
		}
	}
	if err := root.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, err := root.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular memory file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".memory-"+newMemoryID())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(tmp, path)
	}
	// A failed multi-file operation must also discard its cached snapshot.
	s.items = nil
	return err
}
func (s *MemoryStore) itemPath(item MemoryItem) string {
	return filepath.Join(s.opts.Base, "memory", item.Class, item.ID+".md")
}
func marshalMemory(item MemoryItem) ([]byte, error) {
	header, err := yaml.Marshal(item)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(header) + "---\n" + item.Text), nil
}
func unmarshalMemory(data []byte) (MemoryItem, error) {
	var item MemoryItem
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return item, errors.New("missing memory frontmatter")
	}
	header, body, ok := bytes.Cut(data[4:], []byte("\n---\n"))
	if !ok {
		return item, errors.New("unterminated memory frontmatter")
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal(header, &fields); err != nil {
		return item, err
	}
	for _, key := range []string{"id", "class", "scope", "tags", "importance", "confidence", "created_at", "updated_at", "last_used_at", "provenance", "supersedes", "status"} {
		field, ok := fields[key]
		if !ok || field.Tag == "!!null" {
			return item, fmt.Errorf("missing memory field %s", key)
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(header))
	dec.KnownFields(true)
	if err := dec.Decode(&item); err != nil {
		return item, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return item, errors.New("multiple frontmatter documents")
	}
	item.Text = string(body)
	return item, validateMemory(item)
}
func (s *MemoryStore) save(root *os.Root, item MemoryItem) error {
	data, err := marshalMemory(cloneMemory(item))
	if err != nil {
		return err
	}
	return s.write(root, s.itemPath(item), data)
}
func (s *MemoryStore) scan(root *os.Root) error {
	s.items = map[string]MemoryItem{}
	s.malformed = 0
	for _, class := range memoryClasses {
		dir := filepath.Join(s.opts.Base, "memory", class)
		f, err := root.Open(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			s.items = nil
			return err
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			s.items = nil
			return err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if !entry.Type().IsRegular() {
				s.malformed++
				continue
			}
			data, err := s.read(root, filepath.Join(dir, entry.Name()))
			var item MemoryItem
			if err == nil {
				item, err = unmarshalMemory(data)
			}
			if err != nil || item.Class != class || entry.Name() != item.ID+".md" {
				s.malformed++
				continue
			}
			if _, exists := s.items[item.ID]; exists {
				s.malformed++
				continue
			}
			s.items[item.ID] = item
		}
	}
	return nil
}

// open initializes/imports once, while preserving unrelated brain.yaml keys.
// The marker is written only after every imported item is durably saved; stable
// import IDs make retries safe even if a previous attempt was interrupted.
func (s *MemoryStore) open() (*os.Root, error) {
	if s.opts.Available != nil {
		if err := s.opts.Available(); err != nil {
			s.items = nil
			return nil, err
		}
	}
	if s.opts.Base != ".loom" && s.opts.Base != "loom-memory" {
		return nil, errors.New("invalid memory base")
	}
	root, err := os.OpenRoot(s.opts.Dir)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.Root, error) { s.items = nil; root.Close(); return nil, err }
	if s.items != nil {
		return root, nil
	}
	metadataPath := filepath.Join(s.opts.Base, "brain.yaml")
	metadata := map[string]any{}
	data, err := s.read(root, metadataPath)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return fail(err)
	}
	if !missing {
		if err := yaml.Unmarshal(data, &metadata); err != nil || metadata == nil {
			return fail(errors.New("invalid memory brain.yaml"))
		}
	}
	if version, exists := metadata["format"]; exists && version != 1 {
		return fail(errors.New("unsupported memory format"))
	}
	_, hasFormat := metadata["format"]
	_, hasCreated := metadata["created_at"]
	metadata["format"] = 1
	if _, exists := metadata["created_at"]; !exists {
		metadata["created_at"] = time.Now().UnixMilli()
	}
	if err := s.scan(root); err != nil {
		return fail(err)
	}
	if metadata["imported_distilled"] != true {
		var items []MemoryItem
		if s.opts.ImportDistilled != nil {
			items, err = s.opts.ImportDistilled()
			if err != nil {
				s.items = nil
				return fail(err)
			}
		}
		existing := s.items
		for _, item := range items {
			if _, exists := existing[item.ID]; exists {
				continue
			}
			if err := validateMemory(item); err != nil {
				s.items = nil
				return fail(fmt.Errorf("distilled memory: %w", err))
			}
			if err := s.save(root, item); err != nil {
				return fail(err)
			}
			existing[item.ID] = item
		}
		metadata["imported_distilled"] = true
		data, err := yaml.Marshal(metadata)
		if err != nil {
			s.items = nil
			return fail(err)
		}
		if err := s.write(root, metadataPath, data); err != nil {
			return fail(err)
		}
		if err := s.scan(root); err != nil {
			return fail(err)
		}
	} else if !hasFormat || !hasCreated {
		data, err := yaml.Marshal(metadata)
		if err != nil {
			return fail(err)
		}
		if err := s.write(root, metadataPath, data); err != nil {
			return fail(err)
		}
		if err := s.scan(root); err != nil {
			return fail(err)
		}
	}
	return root, nil
}
func (s *MemoryStore) Remember(req RememberRequest) (MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := MemoryItem{ID: req.ID, Class: req.Class, Scope: req.Scope, Text: req.Text, Tags: req.Tags, Importance: .5, Confidence: .7, Provenance: req.Provenance, Supersedes: req.Supersedes, Status: req.Status}
	if item.ID == "" {
		item.ID = newMemoryID()
	}
	if item.Status == "" {
		item.Status = "active"
	}
	if req.Importance != nil {
		item.Importance = *req.Importance
	}
	if req.Confidence != nil {
		item.Confidence = *req.Confidence
	}
	now := time.Now().UnixMilli()
	item.CreatedAt = now
	item.UpdatedAt = now
	if err := validateMemory(item); err != nil {
		return MemoryItem{}, err
	}
	if err := coreNoteLimit(item); err != nil {
		return MemoryItem{}, err
	}
	root, err := s.open()
	if err != nil {
		return MemoryItem{}, err
	}
	defer root.Close()
	// Deterministic choice if external tools have introduced duplicate texts.
	var match MemoryItem
	for _, old := range s.items {
		if item.Status != "candidate" && old.Status == "active" && old.Scope == item.Scope && old.Class == item.Class && normalizedMemoryText(old.Text) == normalizedMemoryText(item.Text) && sameContinuityIdentity(old, item) && (match.ID == "" || old.ID < match.ID) {
			match = old
		}
	}
	if match.ID != "" {
		match.UpdatedAt = max(now, match.UpdatedAt+1)
		match.Importance = max(item.Importance, match.Importance)
		item = match
	} else if _, exists := s.items[item.ID]; exists {
		return MemoryItem{}, errors.New("memory id already exists; use update")
	}
	err = s.save(root, item)
	return cloneMemory(item), err
}
func (s *MemoryStore) Update(req UpdateMemoryRequest) (MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.open()
	if err != nil {
		return MemoryItem{}, err
	}
	defer root.Close()
	old, exists := s.items[req.ID]
	if !exists {
		return MemoryItem{}, errors.New("memory item not found")
	}
	item := cloneMemory(old)
	if req.Patch.Text != nil {
		item.Text = *req.Patch.Text
	}
	if req.Patch.Tags != nil {
		item.Tags = *req.Patch.Tags
	}
	if req.Patch.Importance != nil {
		item.Importance = *req.Patch.Importance
	}
	if req.Patch.Confidence != nil {
		item.Confidence = *req.Patch.Confidence
	}
	if req.Patch.Status != nil {
		item.Status = *req.Patch.Status
	}
	if req.Patch.Scope != nil {
		item.Scope = *req.Patch.Scope
	}
	item.UpdatedAt = max(time.Now().UnixMilli(), old.UpdatedAt+1)
	supersede := req.Supersede && normalizedMemoryText(item.Text) != normalizedMemoryText(old.Text)
	if supersede {
		item.ID = newMemoryID()
		item.CreatedAt = item.UpdatedAt
		item.LastUsedAt = 0
		item.Status = "active"
		if old.Status == "candidate" {
			item.Status = "candidate"
		}
		if req.Patch.Status != nil {
			item.Status = *req.Patch.Status
		}
		item.Supersedes = append(item.Supersedes, old.ID)
	}
	if err := validateMemory(item); err != nil {
		return MemoryItem{}, err
	}
	if err := coreNoteLimit(item); err != nil {
		return MemoryItem{}, err
	}
	if err := s.save(root, item); err != nil {
		return MemoryItem{}, err
	}
	if supersede {
		old.Status = "superseded"
		old.UpdatedAt = item.UpdatedAt
		if err := s.save(root, old); err != nil {
			_ = root.Remove(s.itemPath(item))
			return MemoryItem{}, err
		}
	}
	return cloneMemory(item), nil
}
func (s *MemoryStore) Forget(id string) (MemoryItem, error) {
	status := "expired"
	return s.Update(UpdateMemoryRequest{ID: id, Patch: MemoryPatch{Status: &status}})
}
func (s *MemoryStore) List(filter MemoryFilter) (MemoryList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if filter.Status == "" {
		filter.Status = "active"
	}
	if !contains([]string{"active", "candidate", "superseded", "uncertain", "expired", "all"}, filter.Status) || filter.Limit < 0 {
		return MemoryList{}, errors.New("invalid memory status or limit")
	}
	for _, class := range filter.Classes {
		if !contains(memoryClasses, class) {
			return MemoryList{}, errors.New("invalid memory class filter")
		}
	}
	scopes := append([]string{}, filter.Scopes...)
	for _, scope := range filter.Scopes {
		if !validScope(scope) {
			return MemoryList{}, errors.New("invalid memory scope filter")
		}
		if strings.HasPrefix(scope, "project:") {
			scopes = append(scopes, "global")
		}
	}
	root, err := s.open()
	if err != nil {
		return MemoryList{}, err
	}
	defer root.Close()
	out := MemoryList{OK: true, Items: []MemoryItem{}, Malformed: s.malformed}
	for _, item := range s.items {
		if filter.Status != "all" && filter.Status != item.Status || len(filter.Classes) > 0 && !contains(filter.Classes, item.Class) || len(scopes) > 0 && !contains(scopes, item.Scope) || !strings.Contains(strings.ToLower(item.Text), strings.ToLower(filter.Query)) {
			continue
		}
		out.Items = append(out.Items, cloneMemory(item))
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.Importance != b.Importance {
			return a.Importance > b.Importance
		}
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID < b.ID
	})
	if filter.Limit > 0 && len(out.Items) > filter.Limit {
		out.Items = out.Items[:filter.Limit]
	}
	return out, nil
}
func (s *MemoryStore) Touch(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	items := s.items
	for _, id := range ids {
		if _, exists := items[id]; !exists {
			return errors.New("memory item not found")
		}
	}
	now := time.Now().UnixMilli()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		item := items[id]
		item.LastUsedAt = max(now, item.LastUsedAt+1)
		if err := s.save(root, item); err != nil {
			return err
		}
	}
	return nil
}
