package loom

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The second brain is one folder the user owns: agents work in it and the
// Files page reads and edits it. Every access goes through os.Root, so a path
// (or a symlink inside the folder) can never reach outside it.

const (
	secondBrainKey   = "SECOND_BRAIN_DIR"
	filesMaxText     = 2 << 20 // editable text, bytes
	filesMaxRaw      = 32 << 20
	filesMaxEntries  = 5000
	filesRecentAfter = 24 * time.Hour
	filesTrash       = ".trash"
)

// secondBrainDir is the configured folder, or ~/second-brain by default.
func secondBrainDir() string {
	if dir := strings.TrimSpace(ReadConfig()[secondBrainKey]); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "second-brain")
}

// Files written through Loom, with the modification time Loom left them at.
// A file changed since (or never written by Loom) and changed recently is
// flagged as modified outside the editor: usually by an agent.
var (
	filesWrittenMu sync.Mutex
	filesWritten   = map[string]time.Time{}
)

func noteLoomWrite(rel string, at time.Time) {
	filesWrittenMu.Lock()
	filesWritten[rel] = at
	filesWrittenMu.Unlock()
}

func changedOutside(rel string, mod time.Time) bool {
	if time.Since(mod) > filesRecentAfter {
		return false
	}
	filesWrittenMu.Lock()
	at, ok := filesWritten[rel]
	filesWrittenMu.Unlock()
	return !ok || mod.After(at.Add(time.Second))
}

// cleanRel validates a path relative to the root: no absolute paths, no
// parent segments, no hidden segments (the trash and dot folders stay out).
func cleanRel(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || p == "." || p == "/" {
		return ".", nil
	}
	if strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errors.New("invalid path")
	}
	clean := path.Clean(p)
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." || strings.HasPrefix(seg, ".") {
			return "", errors.New("invalid path")
		}
	}
	return clean, nil
}

// machineRootDir is where whole-machine browsing starts: the filesystem root
// on Unix, the user's home elsewhere. Access stays limited by the account's
// own permissions; dot entries are not listed.
func machineRootDir() string {
	if runtime.GOOS == "windows" {
		home, _ := os.UserHomeDir()
		return home
	}
	return "/"
}

func openFilesScope(r *http.Request) (*os.Root, string, error) {
	if r != nil && r.URL.Query().Get("scope") == "machine" {
		dir := machineRootDir()
		root, err := os.OpenRoot(dir)
		return root, dir, err
	}
	return openSecondBrain()
}

func openSecondBrain() (*os.Root, string, error) {
	dir := secondBrainDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, dir, err
	}
	return root, dir, nil
}

type fileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Dir      bool        `json:"dir,omitempty"`
	Size     int64       `json:"size,omitempty"`
	Modified int64       `json:"modified"`
	Outside  bool        `json:"outside,omitempty"`
	Children []*fileNode `json:"children,omitempty"`
}

func filesTree(root *os.Root) ([]*fileNode, bool, error) {
	byDir := map[string]*fileNode{".": {Path: ".", Dir: true}}
	count, truncated := 0, false
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if count >= filesMaxEntries {
			truncated = true
			return fs.SkipAll
		}
		count++
		info, err := d.Info()
		if err != nil {
			return nil
		}
		n := &fileNode{Name: name, Path: p, Dir: d.IsDir(), Modified: info.ModTime().UnixMilli()}
		if !n.Dir {
			n.Size = info.Size()
			n.Outside = changedOutside(p, info.ModTime())
		}
		if parent := byDir[path.Dir(p)]; parent != nil {
			parent.Children = append(parent.Children, n)
		}
		if n.Dir {
			byDir[p] = n
		}
		return nil
	})
	var sortNodes func([]*fileNode)
	sortNodes = func(list []*fileNode) {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Dir != list[j].Dir {
				return list[i].Dir
			}
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
		for _, n := range list {
			if n.Dir {
				// A folder shows that something inside changed outside the editor.
				sortNodes(n.Children)
				for _, c := range n.Children {
					if c.Outside {
						n.Outside = true
					}
				}
			}
		}
	}
	top := byDir["."].Children
	sortNodes(top)
	return top, truncated, err
}

func filesError(w http.ResponseWriter, err error) {
	code := 400
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = 404
	case errors.Is(err, fs.ErrExist):
		code = 409
	}
	sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
}

// handleFilesRoot: GET → {root, exists}; POST {path, create} chooses the folder.
func handleFilesRoot(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Path   string `json:"path"`
			Create bool   `json:"create"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
			filesError(w, err)
			return
		}
		dir := strings.TrimSpace(body.Path)
		if strings.HasPrefix(dir, "~/") {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, dir[2:])
		}
		if dir != "" && !filepath.IsAbs(dir) {
			filesError(w, errors.New("absolute folder path required"))
			return
		}
		if dir != "" {
			if body.Create {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					filesError(w, err)
					return
				}
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				filesError(w, errors.New("folder not found"))
				return
			}
		}
		if err := SetConfigKey(secondBrainKey, dir); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	dir := secondBrainDir()
	st, err := os.Stat(dir)
	sendJSON(w, 200, map[string]any{"ok": true, "root": dir, "exists": err == nil && st.IsDir()})
}

func handleFilesTree(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	root, dir, err := openSecondBrain()
	if err != nil {
		sendJSON(w, 200, map[string]any{"ok": true, "root": dir, "exists": false, "tree": []any{}})
		return
	}
	defer root.Close()
	tree, truncated, err := filesTree(root)
	if err != nil {
		filesError(w, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "root": dir, "exists": true, "tree": tree, "truncated": truncated})
}

func handleFilesRead(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	rel, err := cleanRel(r.URL.Query().Get("path"))
	if err != nil || rel == "." {
		filesError(w, errors.New("invalid path"))
		return
	}
	root, _, err := openFilesScope(r)
	if err != nil {
		filesError(w, err)
		return
	}
	defer root.Close()
	f, err := confinedOpenRead(root, rel)
	if err != nil {
		filesError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		filesError(w, errors.New("not a file"))
		return
	}
	out := map[string]any{"ok": true, "path": rel, "size": info.Size(), "modified": info.ModTime().UnixMilli()}
	if info.Size() > filesMaxText {
		out["too_large"] = true
		sendJSON(w, 200, out)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, filesMaxText+1))
	if err != nil {
		filesError(w, err)
		return
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		out["binary"] = true
	} else {
		out["content"] = string(data)
	}
	sendJSON(w, 200, out)
}

// handleFilesRaw serves a file's bytes (images in notes). Never executable:
// served as an attachment unless it is an image.
func handleFilesRaw(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	rel, err := cleanRel(r.URL.Query().Get("path"))
	if err != nil || rel == "." {
		http.Error(w, "invalid path", 400)
		return
	}
	root, _, err := openFilesScope(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	f, err := confinedOpenRead(root, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() > filesMaxRaw {
		http.NotFound(w, r)
		return
	}
	kind := mime.TypeByExtension(strings.ToLower(path.Ext(rel)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	if !strings.HasPrefix(kind, "image/") || kind == "image/svg+xml" {
		kind = "application/octet-stream"
		w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(path.Base(rel), "\"", "")+"\"")
	}
	w.Header().Set("Content-Type", kind)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// handleFilesWrite saves a text file. With `modified`, the write is refused
// when the file changed since it was read (an agent may have edited it).
func handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	var body struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Modified int64  `json:"modified"`
		Create   bool   `json:"create"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, filesMaxText+4096)).Decode(&body); err != nil {
		filesError(w, err)
		return
	}
	rel, err := cleanRel(body.Path)
	if err != nil || rel == "." {
		filesError(w, errors.New("invalid path"))
		return
	}
	root, _, err := openFilesScope(r)
	if err != nil {
		filesError(w, err)
		return
	}
	defer root.Close()
	info, statErr := root.Stat(rel)
	switch {
	case statErr == nil && info.IsDir():
		filesError(w, errors.New("a folder has this name"))
		return
	case statErr == nil && body.Create:
		filesError(w, fs.ErrExist)
		return
	case statErr == nil && body.Modified > 0 && info.ModTime().UnixMilli() != body.Modified:
		sendJSON(w, 409, map[string]any{"ok": false, "conflict": true, "error": "the file changed since it was opened", "modified": info.ModTime().UnixMilli()})
		return
	case statErr != nil && !body.Create:
		filesError(w, fs.ErrNotExist)
		return
	}
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			filesError(w, err)
			return
		}
	}
	// Write next to the file, then rename: a reader never sees half a note.
	tmp := path.Join(path.Dir(rel), ".loom-"+randomID(8)+".tmp")
	if err := root.WriteFile(tmp, []byte(body.Content), 0o644); err != nil {
		filesError(w, err)
		return
	}
	if err := root.Rename(tmp, rel); err != nil {
		_ = root.Remove(tmp)
		filesError(w, err)
		return
	}
	info, err = root.Stat(rel)
	if err != nil {
		filesError(w, err)
		return
	}
	noteLoomWrite(rel, info.ModTime())
	sendJSON(w, 200, map[string]any{"ok": true, "path": rel, "modified": info.ModTime().UnixMilli()})
}

// handleFilesOp: POST {op: mkdir|rename|delete, path, to}. Delete moves the
// entry to the folder's .trash, so nothing is lost by a misclick.
func handleFilesOp(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Op   string `json:"op"`
		Path string `json:"path"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		filesError(w, err)
		return
	}
	rel, err := cleanRel(body.Path)
	if err != nil || rel == "." {
		filesError(w, errors.New("invalid path"))
		return
	}
	root, _, err := openFilesScope(r)
	if err != nil {
		filesError(w, err)
		return
	}
	defer root.Close()
	switch body.Op {
	case "mkdir":
		if _, err := root.Stat(rel); err == nil {
			filesError(w, fs.ErrExist)
			return
		}
		err = root.MkdirAll(rel, 0o755)
	case "rename":
		to, terr := cleanRel(body.To)
		if terr != nil || to == "." {
			filesError(w, errors.New("invalid destination"))
			return
		}
		if _, serr := root.Stat(to); serr == nil {
			filesError(w, fs.ErrExist)
			return
		}
		if dir := path.Dir(to); dir != "." {
			if err = root.MkdirAll(dir, 0o755); err != nil {
				break
			}
		}
		err = root.Rename(rel, to)
	case "delete":
		if err = root.MkdirAll(filesTrash, 0o755); err == nil {
			err = root.Rename(rel, path.Join(filesTrash, time.Now().Format("20060102-150405")+"-"+path.Base(rel)))
		}
	default:
		err = errors.New("unknown operation")
	}
	if err != nil {
		filesError(w, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleFilesList lists one directory (dot entries and node_modules hidden),
// for browsing a whole machine without walking it.
func handleFilesList(w http.ResponseWriter, r *http.Request) {
	if proxyFiles(w, r) {
		return
	}
	rel, err := cleanRel(r.URL.Query().Get("path"))
	if err != nil {
		filesError(w, err)
		return
	}
	root, dir, err := openFilesScope(r)
	if err != nil {
		filesError(w, err)
		return
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		filesError(w, err)
		return
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		filesError(w, err)
		return
	}
	out := []*fileNode{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := name
		if rel != "." {
			p = rel + "/" + name
		}
		n := &fileNode{Name: name, Path: p, Dir: e.IsDir(), Modified: info.ModTime().UnixMilli()}
		if !n.Dir {
			n.Size = info.Size()
		}
		out = append(out, n)
		if len(out) >= filesMaxEntries {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	sendJSON(w, 200, map[string]any{"ok": true, "root": dir, "path": rel, "entries": out})
}
