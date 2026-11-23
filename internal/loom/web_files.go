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
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || strings.HasPrefix(seg, ".") {
			return "", errors.New("invalid path")
		}
	}
	return path.Clean(p), nil
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
	top := &fileNode{Path: ".", Dir: true}
	queue := []*fileNode{top}
	count, truncated := 0, false
	for len(queue) > 0 && !truncated {
		dir := queue[0]
		queue = queue[1:]
		children, more, err := listFileNodes(root, dir.Path, filesMaxEntries-count, true)
		if err != nil {
			if dir == top {
				return nil, false, err
			}
			continue
		}
		dir.Children, truncated = children, more
		count += len(children)
		for _, n := range children {
			if n.Dir {
				queue = append(queue, n)
			}
		}
	}
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
	sortNodes(top.Children)
	return top.Children, truncated, nil
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
				if err := os.MkdirAll(dir, 0o700); err != nil {
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
	if err != nil || !info.Mode().IsRegular() {
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
	if len(data) > filesMaxText {
		out["too_large"] = true
	} else if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
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
	if err != nil || !info.Mode().IsRegular() || info.Size() > filesMaxRaw {
		http.NotFound(w, r)
		return
	}
	kind := mime.TypeByExtension(strings.ToLower(path.Ext(rel)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	if !strings.HasPrefix(kind, "image/") || kind == "image/svg+xml" {
		kind = "application/octet-stream"
		cd := mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(rel)})
		if cd == "" {
			cd = "attachment"
		}
		w.Header().Set("Content-Disposition", cd)
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
	case statErr == nil && !info.Mode().IsRegular():
		filesError(w, errors.New("destination is not a regular file"))
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
		if err := root.MkdirAll(dir, 0o700); err != nil {
			filesError(w, err)
			return
		}
	}
	// Write next to the file, then rename: a reader never sees half a note.
	tmp := path.Join(path.Dir(rel), ".loom-"+randomID(8)+".tmp")
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		filesError(w, err)
		return
	}
	defer root.Remove(tmp)
	_, err = io.WriteString(f, body.Content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		filesError(w, err)
		return
	}
	if body.Create {
		err = installNewNote(root, tmp, rel)
	} else {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
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

// Publish a complete note without overwriting an existing entry. On volumes
// without hard links, reserve the destination exclusively before renaming.
func installNewNote(root *os.Root, tmp, rel string) error {
	if err := root.Link(tmp, rel); err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	f, err := root.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err == nil {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
		_ = root.Remove(rel)
	}
	return err
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
		err = root.MkdirAll(rel, 0o700)
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
			if err = root.MkdirAll(dir, 0o700); err != nil {
				break
			}
		}
		err = root.Rename(rel, to)
	case "delete":
		if err = root.MkdirAll(filesTrash, 0o700); err == nil {
			err = root.Rename(rel, path.Join(filesTrash, time.Now().Format("20060102-150405")+"-"+randomID(8)+"-"+path.Base(rel)))
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
	out, _, err := listFileNodes(root, rel, filesMaxEntries, false)
	if err != nil {
		filesError(w, err)
		return
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	sendJSON(w, 200, map[string]any{"ok": true, "root": dir, "path": rel, "entries": out})
}

// Read directory batches so the listing limit also bounds allocation for a single large folder.
// Tree traversal skips symlink directories. The interactive listing lets the owner follow an in-root link.
func listFileNodes(root *os.Root, rel string, limit int, tree bool) ([]*fileNode, bool, error) {
	f, err := confinedOpenRead(root, rel)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	out := []*fileNode{}
	for {
		entries, err := f.ReadDir(128)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				continue
			}
			if len(out) >= limit {
				return out, true, nil
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
			if !tree && e.Type()&os.ModeSymlink != 0 {
				if target, err := root.Stat(p); err == nil && target.IsDir() {
					n.Dir = true
				}
			}
			if !n.Dir {
				n.Size = info.Size()
				if tree {
					n.Outside = changedOutside(p, info.ModTime())
				}
			}
			out = append(out, n)

		}
	}
	return out, false, nil
}
