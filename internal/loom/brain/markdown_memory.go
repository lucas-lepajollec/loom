package brain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const MemoryIndexLines, MemoryIndexBytes = 200, 25 << 10

// MemoryFile is also the native Claude Code auto-memory format. Unknown YAML
// fields are preserved on updates so agents and people can extend the format.
type MemoryFile struct {
	File        string         `json:"file" yaml:"-"`
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description" yaml:"description"`
	Type        string         `json:"type" yaml:"type"`
	Text        string         `json:"text" yaml:"-"`
	UpdatedAt   int64          `json:"updated_at" yaml:"-"`
	Metadata    map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Malformed   bool           `json:"malformed,omitempty" yaml:"-"`
}
type MemoryFiles struct {
	Index   string       `json:"index"`
	Items   []MemoryFile `json:"items"`
	Path    string       `json:"path"`
	Warning string       `json:"warning,omitempty"`
}
type MemoryWrite struct {
	Scope       string `json:"scope"`
	File        string `json:"file,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Text        string `json:"text"`
}
type MemoryRead struct {
	Scope string `json:"scope"`
	File  string `json:"file"`
}
type MemoryIndexRequest struct {
	ProjectID string `json:"project_id,omitempty"`
}
type MemoryIndexes struct {
	Global  MemoryFiles  `json:"global"`
	Project *MemoryFiles `json:"project,omitempty"`
}

type MarkdownOptions struct {
	Dir            string
	Encode, Decode func([]byte) ([]byte, error)
	Available      func() error
	ProjectName    func(string) (string, error)
}
type MarkdownStore struct {
	mu   sync.Mutex
	opts MarkdownOptions
}

func NewMarkdownStore(opts MarkdownOptions) *MarkdownStore { return &MarkdownStore{opts: opts} }

// OwnedDirectories prevents duplicate indexing of memory and transcripts as
// ordinary notes (and persisting their text in the disposable file cache).
func (s *MarkdownStore) OwnedDirectories() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, layout, _, err := s.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	paths := []string{filepath.Join(s.opts.Dir, layout.Memory), filepath.Join(s.opts.Dir, layout.Discussions)}
	for _, folder := range layout.ProjectFolders {
		paths = append(paths, filepath.Join(s.opts.Dir, layout.Projects, folder, layout.ProjectMemory))
	}
	return paths, nil
}

// Slugs are chosen once and recorded by project ID, independent of later names.
func Slug(name string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			separator = false
			b.WriteRune(r)
		} else {
			separator = true
		}
		if b.Len() >= 80 {
			break
		}
	}
	if b.Len() == 0 {
		return "untitled"
	}
	return b.String()
}
func shortIdentity(id string) string {
	hash := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", hash[:6])
}
func relativeFolder(path string) bool {
	if path == "" || len(path) > 256 || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00\r\n[]()") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
func memoryFilename(file string) bool {
	return !strings.EqualFold(file, "MEMORY.md") && len(file) <= 180 && relativeFolder(file) && !strings.Contains(file, "/") && strings.HasSuffix(file, ".md")
}

// Even in-root symlinks are refused: writes must stay in the configured tree.
func noSymlinks(root *os.Root, path string) error {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Brain paths must not contain symlinks")
		}
	}
	return nil
}
func (s *MarkdownStore) read(root *os.Root, path string, limit int64) ([]byte, error) {
	if err := noSymlinks(root, path); err != nil {
		return nil, err
	}
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
		return nil, errors.New("Brain file is not regular")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1025))
	if err != nil {
		return nil, err
	}
	if s.opts.Decode != nil {
		b, err = s.opts.Decode(b)
		if err != nil {
			return nil, err
		}
	}
	if int64(len(b)) > limit {
		return nil, errors.New("Brain file exceeds limit")
	}
	return b, nil
}
func (s *MarkdownStore) write(root *os.Root, path string, b []byte) error {
	if s.opts.Available != nil {
		if err := s.opts.Available(); err != nil {
			return err
		}
	}
	if err := noSymlinks(root, path); err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := root.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace non-regular Brain file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if s.opts.Encode != nil {
		var err error
		b, err = s.opts.Encode(b)
		if err != nil {
			return err
		}
	}
	tmp := filepath.Join(filepath.Dir(path), ".loom-write-"+newMemoryID())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(tmp, path)
	}
	return err
}

type brainLayout struct {
	Memory          string            `json:"memory_folder"`
	Projects        string            `json:"projects_folder"`
	ProjectMemory   string            `json:"project_memory_folder"`
	Discussions     string            `json:"discussions_folder"`
	ProjectFolders  map[string]string `json:"project_folders"`
	DiscussionFiles map[string]string `json:"discussion_files"`
}

func (s *MarkdownStore) open() (*os.Root, brainLayout, map[string]json.RawMessage, error) {
	l := brainLayout{Memory: "Memory", Projects: "Projects", ProjectMemory: "memory", Discussions: "Discussions", ProjectFolders: map[string]string{}, DiscussionFiles: map[string]string{}}
	if s.opts.Available != nil {
		if err := s.opts.Available(); err != nil {
			return nil, l, nil, err
		}
	}
	root, err := os.OpenRoot(s.opts.Dir)
	if err != nil {
		return nil, l, nil, err
	}
	raw := map[string]json.RawMessage{}
	b, err := s.read(root, ".loom/brain.json", 1<<20)
	if err == nil {
		err = json.Unmarshal(b, &raw)
		if raw == nil {
			err = errors.New("invalid Brain layout JSON")
		}
		if err == nil {
			err = json.Unmarshal(b, &l)
		}
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err == nil {
		for _, p := range []string{l.Memory, l.Projects, l.ProjectMemory, l.Discussions} {
			if !relativeFolder(p) {
				err = errors.New("invalid Brain layout folder")
			}
		}
		folders := []string{l.Memory, l.Projects, l.Discussions}
		for i, a := range folders {
			for j, b := range folders {
				a, b := strings.ToLower(a), strings.ToLower(b)
				if i != j && (a == b || strings.HasPrefix(a, b+"/")) {
					err = errors.New("Brain layout folders overlap")
				}
			}
		}
		for _, p := range l.ProjectFolders {
			if !relativeFolder(p) || strings.Contains(p, "/") {
				err = errors.New("invalid project folder")
			}
		}
		for _, p := range l.DiscussionFiles {
			if !strings.HasPrefix(p, l.Discussions+"/") || !relativeFolder(p) || !strings.HasSuffix(p, ".md") {
				err = errors.New("invalid discussion path")
			}
		}
	}
	if err != nil {
		root.Close()
		return nil, l, nil, err
	}
	if l.ProjectFolders == nil {
		l.ProjectFolders = map[string]string{}
	}
	if l.DiscussionFiles == nil {
		l.DiscussionFiles = map[string]string{}
	}
	return root, l, raw, nil
}
func (s *MarkdownStore) saveLayout(root *os.Root, l brainLayout, raw map[string]json.RawMessage) error {
	b, _ := json.Marshal(l)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for k, v := range fields {
		raw[k] = v
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return s.write(root, ".loom/brain.json", append(b, '\n'))
}
func (s *MarkdownStore) scopeFolder(root *os.Root, l *brainLayout, raw map[string]json.RawMessage, scope string) (string, error) {
	if scope == "global" {
		return l.Memory, nil
	}
	id, ok := strings.CutPrefix(scope, "project:")
	if !ok || id == "" || len(id) > 200 || strings.ContainsAny(id, "\x00\r\n") {
		return "", errors.New("scope must be global or project:<id>")
	}
	folder := l.ProjectFolders[id]
	if folder == "" {
		name := id
		if s.opts.ProjectName != nil {
			var err error
			name, err = s.opts.ProjectName(id)
			if err != nil {
				return "", err
			}
		}
		folder = Slug(name)
		for other, p := range l.ProjectFolders {
			if other != id && strings.EqualFold(p, folder) {
				folder += "-" + shortIdentity(id)
				break
			}
		}
		l.ProjectFolders[id] = folder
		if err := s.saveLayout(root, *l, raw); err != nil {
			return "", err
		}
	}
	return filepath.ToSlash(filepath.Join(l.Projects, folder, l.ProjectMemory)), nil
}

func parseMemoryFile(file string, b []byte, at int64) MemoryFile {
	m := MemoryFile{File: file, Text: string(b), UpdatedAt: at}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if strings.HasPrefix(text, "---\n") {
		if head, body, ok := strings.Cut(text[4:], "\n---\n"); ok {
			if yaml.Unmarshal([]byte(head), &m) == nil {
				m.Text = body
			} else {
				m.Malformed = true
			}
		} else {
			m.Malformed = true
		}
	} else {
		m.Malformed = true
	}
	if strings.TrimSpace(m.Description) == "" {
		m.Malformed = true
	}
	if m.Name == "" {
		m.Malformed = true
		m.Name = strings.TrimSuffix(file, ".md")
	}
	if !contains([]string{"user", "feedback", "project", "reference"}, m.Type) {
		m.Malformed = true
	}
	return m
}
func (s *MarkdownStore) list(root *os.Root, folder string) (MemoryFiles, error) {
	out := MemoryFiles{Items: []MemoryFile{}, Path: filepath.Join(s.opts.Dir, filepath.FromSlash(folder))}
	b, err := s.readIndex(root, folder+"/MEMORY.md")
	if err == nil {
		out.Index, out.Warning = BoundMemoryIndex(string(b))
	} else if !os.IsNotExist(err) {
		return out, err
	}
	if err := noSymlinks(root, folder); err != nil {
		return out, err
	}
	f, err := root.Open(folder)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		if !memoryFilename(entry.Name()) {
			continue
		}
		b, err := s.read(root, folder+"/"+entry.Name(), maxMemoryFile)
		if err != nil {
			out.Items = append(out.Items, MemoryFile{File: entry.Name(), Name: entry.Name(), Malformed: true})
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, parseMemoryFile(entry.Name(), b, info.ModTime().UnixMilli()))
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].File < out.Items[j].File })
	return out, nil
}

func (s *MarkdownStore) readIndex(root *os.Root, path string) ([]byte, error) {
	if s.opts.Decode != nil {
		return s.read(root, path, maxMemoryFile)
	}
	if err := noSymlinks(root, path); err != nil {
		return nil, err
	}
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
		return nil, errors.New("Brain index is not a regular file")
	}
	// Human-edited indexes may be arbitrarily long. Read only enough to bound
	// the startup context and report overflow, without refusing the collection.
	return io.ReadAll(io.LimitReader(f, MemoryIndexBytes+1))
}
func BoundMemoryIndex(text string) (string, string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i >= MemoryIndexLines || b.Len()+len(line)+1 > MemoryIndexBytes {
			return b.String(), "MEMORY.md exceeds 200 lines or 25 KB; condense the index"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), ""
}
func singleLine(text string) string { return strings.Join(strings.Fields(text), " ") }
func (s *MarkdownStore) index(root *os.Root, folder string) error {
	list, err := s.list(root, folder)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, m := range list.Items {
		name := strings.NewReplacer("[", "", "]", "", "(", "", ")", "").Replace(singleLine(m.Name))
		fmt.Fprintf(&b, "- [%s](%s) — %s\n", name, m.File, singleLine(m.Description))
	}
	text, _ := BoundMemoryIndex(b.String())
	return s.write(root, folder+"/MEMORY.md", []byte(text))
}
func (s *MarkdownStore) List(scope string) (MemoryFiles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, raw, err := s.open()
	if err != nil {
		return MemoryFiles{}, err
	}
	defer root.Close()
	folder, err := s.scopeFolder(root, &l, raw, scope)
	if err != nil {
		return MemoryFiles{}, err
	}
	out, err := s.list(root, folder)
	required := 0
	for _, item := range out.Items {
		required += len(item.Name) + len(item.Description) + len(item.File) + 15
	}
	if len(out.Items) > MemoryIndexLines || required > MemoryIndexBytes {
		out.Warning = "More than 200 memory files; condense the index"
	}
	return out, err
}
func validateMemoryWrite(req MemoryWrite) error {
	if req.File != "" && !memoryFilename(req.File) {
		return errors.New("file must be a relative Markdown filename other than MEMORY.md")
	}
	if !contains([]string{"user", "feedback", "project", "reference"}, req.Type) {
		return errors.New("invalid memory type")
	}
	for _, text := range []string{req.Name, req.Description, req.Text} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return errors.New("memory must be UTF-8 without NUL")
		}
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Description) == "" || len(req.Name) > 300 || len(req.Description) > 1000 || strings.ContainsAny(req.Name+req.Description, "\r\n") || strings.TrimSpace(req.Text) == "" || len(req.Text) > 8<<10 {
		return errors.New("memory requires name, description and short text (at most 8 KiB)")
	}
	return nil
}
func (s *MarkdownStore) Write(req MemoryWrite, metadata map[string]any) (MemoryFile, error) {
	return s.writeMemory(req, metadata, true)
}

func (s *MarkdownStore) writeMemory(req MemoryWrite, metadata map[string]any, deduplicate bool) (MemoryFile, error) {
	if err := validateMemoryWrite(req); err != nil {
		return MemoryFile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, raw, err := s.open()
	if err != nil {
		return MemoryFile{}, err
	}
	defer root.Close()
	folder, err := s.scopeFolder(root, &l, raw, req.Scope)
	if err != nil {
		return MemoryFile{}, err
	}
	list, err := s.list(root, folder)
	if err != nil {
		return MemoryFile{}, err
	}
	exists := false
	for _, m := range list.Items {
		if strings.EqualFold(m.File, req.File) {
			req.File, exists = m.File, true
		}
	}
	if !exists {
		if deduplicate {
			for _, m := range list.Items {
				if !m.Malformed && (normalizedMemoryText(m.Name) == normalizedMemoryText(req.Name) || normalizedMemoryText(m.Text) == normalizedMemoryText(req.Text)) {
					req.File = m.File
					break
				}
			}
		}
		if req.File == "" {
			req.File = Slug(req.Name) + ".md"
			for _, m := range list.Items {
				if m.File == req.File {
					req.File = Slug(req.Name) + "-" + shortIdentity(newMemoryID()) + ".md"
					break
				}
			}
		}
	}
	head := map[string]any{}
	if old, err := s.read(root, folder+"/"+req.File, maxMemoryFile); err == nil {
		m := parseMemoryFile(req.File, old, 0)
		if m.Malformed {
			return MemoryFile{}, errors.New("malformed memory must be repaired manually before replacement")
		}
		_, body, _ := strings.Cut(strings.ReplaceAll(string(old), "\r\n", "\n"), "---\n")
		h, _, _ := strings.Cut(body, "\n---\n")
		_ = yaml.Unmarshal([]byte(h), &head)
	} else if !os.IsNotExist(err) {
		return MemoryFile{}, err
	}
	head["name"], head["description"], head["type"] = req.Name, req.Description, req.Type
	if metadata != nil {
		existing, _ := head["metadata"].(map[string]any)
		if existing == nil {
			existing = map[string]any{}
		}
		for k, v := range metadata {
			existing[k] = v
		}
		head["metadata"] = existing
	}
	b, err := yaml.Marshal(head)
	if err != nil {
		return MemoryFile{}, err
	}
	b = append(append([]byte("---\n"), b...), []byte("---\n"+req.Text)...)
	if err = s.write(root, folder+"/"+req.File, b); err == nil {
		err = s.index(root, folder)
	}
	return parseMemoryFile(req.File, b, time.Now().UnixMilli()), err
}
func (s *MarkdownStore) Read(req MemoryRead) (MemoryFile, error) {
	if !memoryFilename(req.File) {
		return MemoryFile{}, errors.New("invalid memory filename")
	}
	list, err := s.List(req.Scope)
	if err != nil {
		return MemoryFile{}, err
	}
	for _, m := range list.Items {
		if m.File == req.File {
			return m, nil
		}
	}
	return MemoryFile{}, os.ErrNotExist
}
func (s *MarkdownStore) Delete(req MemoryRead) error {
	if !memoryFilename(req.File) {
		return errors.New("invalid memory filename")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, raw, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	folder, err := s.scopeFolder(root, &l, raw, req.Scope)
	if err != nil {
		return err
	}
	b, err := s.read(root, folder+"/"+req.File, maxMemoryFile)
	if err != nil {
		return err
	}
	if parseMemoryFile(req.File, b, 0).Malformed {
		return errors.New("malformed memories are never deleted")
	}
	if err = root.Remove(folder + "/" + req.File); err != nil {
		return err
	}
	return s.index(root, folder)
}

// Documents reads the canonical trees, including agent-written transcripts.
func (s *MarkdownStore) Documents(ctxDone <-chan struct{}, discussions bool, emit func(Document) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, l, _, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	folders := []string{l.Memory}
	for _, folder := range l.ProjectFolders {
		folders = append(folders, filepath.ToSlash(filepath.Join(l.Projects, folder, l.ProjectMemory)))
	}
	if discussions {
		folders = []string{l.Discussions}
	}
	for _, folder := range folders {
		err = fs.WalkDir(root.FS(), folder, func(path string, d fs.DirEntry, walkErr error) error {
			select {
			case <-ctxDone:
				return errors.New("Brain document read cancelled")
			default:
			}
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			limit := int64(maxMemoryFile)
			if discussions {
				limit = 16 << 20
			}
			var b []byte
			var err error
			if !discussions && strings.EqualFold(filepath.Base(path), "MEMORY.md") {
				b, err = s.readIndex(root, path)
				index, _ := BoundMemoryIndex(string(b))
				b = []byte(index)
			} else {
				b, err = s.read(root, path, limit)
			}
			if err != nil {
				return err
			}
			if !emit(Document{Path: filepath.ToSlash(path), Text: string(b)}) {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
