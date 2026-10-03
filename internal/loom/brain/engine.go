package brain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type posting struct{ doc, frequency int }
type indexedChunk struct {
	chunk  *Chunk
	length int
}
type Engine struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	opts      Options
	sources   []Source
	files     map[string]File
	docs      []indexedChunk
	postings  map[string][]posting
}

func New(opts Options) (*Engine, error) {
	e := &Engine{opts: opts, files: map[string]File{}}
	if opts.Storage != nil {
		sources, err := opts.Storage.LoadSources()
		if err != nil {
			return nil, err
		}
		if len(sources) > MaxSources {
			return nil, errors.New("too many Brain sources")
		}
		seen := map[string]bool{}
		scopes := map[string]string{}
		for _, s := range sources {
			if err := validateSource(s); err != nil {
				return nil, err
			}
			if seen[s.ID] {
				return nil, errors.New("duplicate source id")
			}
			seen[s.ID] = true
			scopes[s.ID] = sourceScope(s)
			s.ReadOnly = false
			s.Include = append([]string{}, s.Include...)
			s.Exclude = append([]string{}, s.Exclude...)
			e.sources = append(e.sources, s)
		}
		snapshot, err := opts.Storage.LoadIndex()
		if err != nil {
			return nil, err
		}
		if snapshot.Version == 1 {
			bytes, chunks := 0, 0
			fileCounts := map[string]int{}
			for _, f := range snapshot.Files {
				if !seen[f.Source] || snapshot.Scopes[f.Source] != scopes[f.Source] || !safeRelative(f.Path) || f.Size > MaxFileBytes {
					continue
				}
				fileCounts[f.Source]++
				if fileCounts[f.Source] > MaxFiles {
					return nil, errors.New("Brain cache exceeds file limit")
				}
				bytes += fileBytes(f)
				chunks += len(f.Chunks)
				if bytes > MaxIndexBytes || chunks > MaxChunks {
					return nil, errors.New("Brain cache exceeds index limits")
				}
				e.files[fileKey(f.Source, f.Path)] = f
			}
			for i := range e.sources {
				if snapshot.Scopes[e.sources[i].ID] == scopes[e.sources[i].ID] {
					e.sources[i].LastIndexed = snapshot.IndexedAt
				}
				for _, f := range e.files {
					if f.Source == e.sources[i].ID {
						e.sources[i].Files++
						e.sources[i].Chunks += len(f.Chunks)
					}
				}
			}
		}
	}
	e.sources = append(e.sources, Source{ID: "conversations", Label: "Conversations", Kind: "conversations", ReadOnly: true}, Source{ID: "memory", Label: "Memory", Kind: "memory", ReadOnly: true})
	if opts.Distilled != nil {
		e.sources = append(e.sources, Source{ID: "distilled", Label: "Distilled", Kind: "distilled", ReadOnly: true})
	}
	e.buildIndexLocked()
	return e, nil
}

func validateSource(s Source) error {
	ok, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`, s.ID)
	if !ok || s.ID == "memory" || s.ID == "conversations" || s.ID == "distilled" {
		return errors.New("invalid or reserved source id")
	}
	if strings.TrimSpace(s.Label) == "" || len(s.Label) > 200 {
		return errors.New("label required (200 bytes maximum)")
	}
	if s.Kind != "context" && s.Kind != "personal" && s.Kind != "repo" {
		return errors.New("kind must be context, personal or repo")
	}
	switch s.Connector {
	case "", "folder", "git", "obsidian", "webdav-mount":
	default:
		return errors.New("unsupported second-brain connection; use a local checkout or mounted folder")
	}
	if len(s.Exclude) > 64 {
		return errors.New("maximum 64 exclude globs")
	}
	for _, g := range s.Exclude {
		if len(g) > 256 {
			return errors.New("exclude glob too long")
		}
		if _, err := globRegex(g); err != nil {
			return err
		}
	}
	if !filepath.IsAbs(s.Path) {
		return errors.New("source path must be absolute")
	}
	if len(s.Include) > 64 {
		return errors.New("maximum 64 include globs")
	}
	for _, g := range s.Include {
		if len(g) > 256 {
			return errors.New("include glob too long")
		}
		if _, err := globRegex(g); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) available() error {
	if e.opts.Available != nil {
		return e.opts.Available()
	}
	return nil
}
func (e *Engine) Sources() []Source {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := append([]Source{}, e.sources...)
	for i := range out {
		out[i].Include = append([]string{}, out[i].Include...)
		out[i].Exclude = append([]string{}, out[i].Exclude...)
	}
	return out
}

// Update creates or replaces a file source. Built-ins cannot be modified.
// Persistence succeeds before the new definition becomes visible.
func (e *Engine) Update(s Source) error {
	if err := e.available(); err != nil {
		return err
	}
	if err := validateSource(s); err != nil {
		return err
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("source must be a directory")
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	s.ReadOnly = false
	s.Path = filepath.Clean(s.Path)
	s.Include = append([]string{}, s.Include...)
	s.Exclude = append([]string{}, s.Exclude...)
	s.Files, s.Chunks, s.LastIndexed, s.Error = 0, 0, time.Time{}, ""
	next := append([]Source{}, e.sources...)
	found := false
	scopeChanged := true
	for i := range next {
		if next[i].ID == s.ID {
			prior := next[i]
			scopeChanged = prior.Path != s.Path || prior.Kind != s.Kind || !equalHeading(prior.Include, s.Include) || !equalHeading(prior.Exclude, s.Exclude)
			if !scopeChanged {
				s.Files, s.Chunks, s.LastIndexed, s.Error = prior.Files, prior.Chunks, prior.LastIndexed, prior.Error
			}
			next[i] = s
			found = true
		}
	}
	if !found {
		count := 0
		for _, source := range next {
			if !source.ReadOnly {
				count++
			}
		}
		if count >= MaxSources {
			return errors.New("maximum 100 sources")
		}
		next = append(next, s)
	}
	if err := e.saveSources(next); err != nil {
		return err
	}
	// Clear cached chunks on scope changes, including transitions to personal.
	for key, f := range e.files {
		if scopeChanged && f.Source == s.ID {
			delete(e.files, key)
		}
	}
	e.sources = next
	e.buildIndexLocked()
	return nil
}
func (e *Engine) Remove(id string) error {
	if err := e.available(); err != nil {
		return err
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	next := []Source{}
	found := false
	for _, s := range e.sources {
		if s.ID == id {
			if s.ReadOnly {
				return errors.New("built-in source is read-only")
			}
			found = true
		} else {
			next = append(next, s)
		}
	}
	if !found {
		return errors.New("source not found")
	}
	if err := e.saveSources(next); err != nil {
		return err
	}
	for key, f := range e.files {
		if f.Source == id {
			delete(e.files, key)
		}
	}
	e.sources = next
	e.buildIndexLocked()
	return nil
}

// Relabel updates metadata even when a source directory is temporarily absent.
func (e *Engine) Relabel(id, label string) error {
	if err := e.available(); err != nil {
		return err
	}
	if strings.TrimSpace(label) == "" || len(label) > 200 {
		return errors.New("label required (200 bytes maximum)")
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	next := append([]Source{}, e.sources...)
	for i := range next {
		if next[i].ID != id {
			continue
		}
		if next[i].ReadOnly {
			return errors.New("built-in source is read-only")
		}
		next[i].Label = label
		if err := e.saveSources(next); err != nil {
			return err
		}
		e.sources = next
		return nil
	}
	return errors.New("source not found")
}
func (e *Engine) saveSources(sources []Source) error {
	if e.opts.Storage == nil {
		return nil
	}
	saved := []Source{}
	for _, s := range sources {
		if !s.ReadOnly {
			s.Files = 0
			s.Chunks = 0
			s.LastIndexed = time.Time{}
			s.Error = ""
			saved = append(saved, s)
		}
	}
	return e.opts.Storage.SaveSources(saved)
}
func fileKey(source, path string) string { return source + "\x00" + path }
func fileBytes(f File) int {
	n := 64 + len(f.Source) + len(f.Path)
	for _, c := range f.Chunks {
		n += len(c.Text) + len(c.ID) + len(c.Source) + len(c.Path)
		for _, h := range c.Heading {
			n += len(h)
		}
	}
	return n
}
func sourceScope(s Source) string {
	digest := sha256.Sum256([]byte(s.Path + "\x00" + s.Kind + "\x00" + strings.Join(s.Include, "\x00") + "\x01" + strings.Join(s.Exclude, "\x00")))
	return fmt.Sprintf("%x", digest[:])
}
func safeRelative(p string) bool {
	return p != "." && p != "" && !strings.Contains(p, "\\") && !path.IsAbs(p) && p != ".." && !strings.HasPrefix(path.Clean(p), "../")
}

// globRegex supports slash-relative *, ?, character classes and ** segments.
func globRegex(g string) (*regexp.Regexp, error) {
	if !safeRelative(g) {
		return nil, errors.New("include globs must be relative to the source")
	}
	if _, err := path.Match(strings.ReplaceAll(g, "**", "*"), ""); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch g[i] {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(g[i+1:], ']')
			if end < 0 {
				return nil, errors.New("invalid glob")
			}
			end += i + 1
			b.WriteString(g[i : end+1])
			i = end
		default:
			_, size := utf8.DecodeRuneInString(g[i:])
			b.WriteString(regexp.QuoteMeta(g[i : i+size]))
			i += size - 1
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
func eligible(s Source, rel string, excludes []*regexp.Regexp) bool {
	// Credential files never become context just because they live under docs/.
	name := strings.ToLower(path.Base(rel))
	if name == ".env" || strings.HasPrefix(name, ".env.") || name == "credentials" || name == "credentials.json" || name == "auth.json" || name == "id_rsa" || name == "id_ed25519" || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
		return false
	}
	for _, re := range excludes {
		if re.MatchString(rel) {
			return false
		}
	}
	ext := path.Ext(name)
	switch ext {
	case ".pdf", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".zip", ".gz", ".tar", ".7z", ".exe", ".dll", ".so", ".dylib", ".o", ".a", ".wasm", ".gguf", ".woff", ".woff2", ".ttf", ".mp3", ".mp4", ".docx", ".xlsx", ".pptx":
		return false
	}
	text := ext == ".md" || ext == ".mdx" || ext == ".txt" || ext == ".rst" || ext == ".org"
	if s.Kind == "repo" {
		text = text || strings.HasPrefix(name, "readme") || strings.HasPrefix(rel, "docs/")
	}
	return text
}
func skipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".ssh", ".gnupg", ".aws", ".codex", ".claude", ".project-local", "node_modules", "vendor", "dist", "build":
		return true
	}
	return false
}

// Refresh serializes indexing and source changes, then atomically publishes a
// fresh index. Unchanged files reuse cached chunks; removed/unreadable files
// disappear. Individual source errors are reported on the source, not hidden.
func (e *Engine) Refresh(ctx context.Context) error {
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	if err := e.available(); err != nil {
		return err
	}
	e.mu.RLock()
	sources := append([]Source{}, e.sources...)
	old := e.files
	e.mu.RUnlock()
	// old is immutable until publication under refreshMu.
	next := map[string]File{}
	bytes, chunks := 0, 0
	var failures []error
	for i := range sources {
		s := &sources[i]
		s.Files = 0
		s.Chunks = 0
		s.Error = ""
		add := func(f File) bool {
			n := fileBytes(f)
			if s.Files >= MaxFiles || bytes+n > MaxIndexBytes || chunks+len(f.Chunks) > MaxChunks {
				s.Error = "index limit reached"
				return false
			}
			if _, ok := next[fileKey(s.ID, f.Path)]; ok {
				return true
			}
			bytes += n
			chunks += len(f.Chunks)
			s.Files++
			s.Chunks += len(f.Chunks)
			next[fileKey(s.ID, f.Path)] = f
			return true
		}
		var err error
		if s.ReadOnly {
			provider := e.opts.Memory
			if s.ID == "conversations" {
				provider = e.opts.Conversations
			}
			if s.ID == "distilled" {
				provider = e.opts.Distilled
			}
			if provider != nil {
				err = provider(ctx, func(d Document) bool {
					if ctx.Err() != nil {
						return false
					}
					if len(d.Text) > MaxFileBytes || !utf8.ValidString(d.Text) || strings.ContainsRune(d.Text, 0) || !safeRelative(d.Path) {
						return true
					}
					return add(File{Source: s.ID, Path: d.Path, Size: int64(len(d.Text)), Chunks: ChunkText(s.ID, d.Path, d.Text)})
				})
			}
		} else {
			var root *os.Root
			root, err = os.OpenRoot(s.Path)
			if err == nil {
				patterns := []*regexp.Regexp{}
				for _, g := range s.Include {
					re, _ := globRegex(g)
					patterns = append(patterns, re)
				}
				excludes := []*regexp.Regexp{}
				for _, g := range s.Exclude {
					re, _ := globRegex(g)
					if re != nil {
						excludes = append(excludes, re)
					}
				}
				err = walkRoot(root, func(rel string, d fs.DirEntry, walkErr error) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if walkErr != nil {
						s.Error = walkErr.Error()
						if d != nil && d.IsDir() {
							return fs.SkipDir
						}
						return nil
					}
					if d.IsDir() {
						if rel != "." && skipDir(d.Name()) {
							return fs.SkipDir
						}
						return nil
					}
					// Conservatively skip all symlinks; os.Root also confines reads
					// when an entry is replaced concurrently by an escaping link.
					if d.Type()&os.ModeSymlink != 0 || !eligible(*s, rel, excludes) {
						return nil
					}
					if len(patterns) > 0 {
						matched := false
						for _, re := range patterns {
							if re.MatchString(rel) {
								matched = true
								break
							}
						}
						if !matched {
							return nil
						}
					}
					info, err := d.Info()
					if err != nil {
						s.Error = err.Error()
						return nil
					}
					if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
						return nil
					}
					f, ok := old[fileKey(s.ID, rel)]
					if !ok || f.Mtime != info.ModTime().UnixNano() || f.Size != info.Size() {
						file, err := root.Open(rel)
						if err != nil {
							s.Error = err.Error()
							return nil
						}
						data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
						file.Close()
						if err != nil {
							s.Error = err.Error()
							return nil
						}
						if len(data) > MaxFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
							return nil
						}
						f = File{s.ID, rel, info.ModTime().UnixNano(), info.Size(), ChunkText(s.ID, rel, string(data))}
					}
					if !add(f) {
						return fs.SkipAll
					}
					return nil
				})
				root.Close()
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			s.Error = err.Error()
		}
		if s.Error != "" {
			failures = append(failures, fmt.Errorf("%s: %s", s.ID, s.Error))
		}
		s.LastIndexed = time.Now().UTC()
	}
	if err := e.available(); err != nil {
		return err
	}
	if e.opts.Storage != nil {
		snap := Snapshot{Version: 1, Files: []File{}, IndexedAt: time.Now().UTC(), Scopes: map[string]string{}}
		for _, source := range sources {
			if !source.ReadOnly {
				snap.Scopes[source.ID] = sourceScope(source)
			}
		}
		for _, f := range next {
			if f.Source != "memory" && f.Source != "conversations" && f.Source != "distilled" {
				snap.Files = append(snap.Files, f)
			}
		}
		sort.Slice(snap.Files, func(i, j int) bool {
			return fileKey(snap.Files[i].Source, snap.Files[i].Path) < fileKey(snap.Files[j].Source, snap.Files[j].Path)
		})
		if err := e.opts.Storage.SaveIndex(snap); err != nil {
			failures = append(failures, err)
			for i := range sources {
				sources[i].Error = err.Error()
			}
		}
	}
	e.mu.Lock()
	e.files = next
	e.sources = sources
	e.buildIndexLocked()
	e.mu.Unlock()
	return errors.Join(failures...)
}

// Read directory entries in bounded batches instead of WalkDir's unbounded
// per-directory listing. No symlink directory is traversed.
func walkRoot(root *os.Root, visit func(string, fs.DirEntry, error) error) error {
	var walk func(string, fs.DirEntry) error
	walk = func(rel string, entry fs.DirEntry) error {
		if entry != nil {
			if err := visit(rel, entry, nil); err != nil {
				if err == fs.SkipDir {
					return nil
				}
				return err
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
		}
		dir, err := root.Open(filepath.FromSlash(rel))
		if err != nil {
			err = visit(rel, entry, err)
			if err == fs.SkipDir {
				return nil
			}
			return err
		}
		defer dir.Close()
		for {
			entries, err := dir.ReadDir(128)
			for _, child := range entries {
				if err := walk(path.Join(rel, child.Name()), child); err != nil {
					return err
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				err = visit(rel, entry, err)
				if err == fs.SkipDir {
					return nil
				}
				return err
			}
		}
	}
	err := walk(".", nil)
	if err == fs.SkipAll {
		return nil
	}
	return err
}
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	_ = e.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.Refresh(ctx)
		}
	}
}
func (e *Engine) buildIndexLocked() {
	e.docs = nil
	e.postings = map[string][]posting{}
	keys := []string{}
	for key := range e.files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f := e.files[key]
		for i := range f.Chunks {
			c := &f.Chunks[i]
			ws := words(c.Text)
			freq := map[string]int{}
			for _, w := range ws {
				freq[w.text]++
			}
			for _, w := range words(c.Path + " " + strings.Join(c.Heading, " ")) {
				freq[w.text] += 3
			}
			doc := len(e.docs)
			e.docs = append(e.docs, indexedChunk{c, max(1, len(ws))})
			for term, count := range freq {
				e.postings[term] = append(e.postings[term], posting{doc, count})
			}
		}
	}
}
