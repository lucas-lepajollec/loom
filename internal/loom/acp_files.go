package loom

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const acpMaxFile = 1 << 20
const acpMaxBaselines = 16 << 20

func acpDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("choisissez un dossier de travail absolu existant")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("dossier inaccessible")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", errors.New("le chemin doit être un dossier existant")
	}
	return filepath.Clean(path), nil
}
func resolveACPCommand(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
func sortedStringKeys[V any](values map[string]V) []string {
	keys := []string{}
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func acpInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Canonicalization gives stable file identities; os.Root enforces confinement
// again during the operation, closing the symlink-swap window.
func acpScopedPath(roots []*os.Root, path string) (*os.Root, string, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", "", errors.New("chemin absolu requis")
	}
	clean := filepath.Clean(path)
	// Reject lexical escapes before resolving links as well.
	lexical := false
	for _, root := range roots {
		if acpInside(root.Name(), clean) {
			lexical = true
			break
		}
	}
	if !lexical {
		return nil, "", "", errors.New("chemin hors du dossier autorisé")
	}
	canonical, err := filepath.EvalSymlinks(clean)
	if errors.Is(err, os.ErrNotExist) {
		// A dangling symlink is never a file creation target.
		if _, e := os.Lstat(clean); e == nil {
			return nil, "", "", errors.New("lien invalide")
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(clean))
		if e != nil {
			return nil, "", "", errors.New("dossier parent inaccessible")
		}
		canonical = filepath.Join(parent, filepath.Base(clean))
		err = nil
	}
	if err != nil {
		return nil, "", "", errors.New("chemin inaccessible")
	}
	for _, root := range roots {
		if acpInside(root.Name(), canonical) {
			rel, _ := filepath.Rel(root.Name(), canonical)
			return root, rel, canonical, nil
		}
	}
	return nil, "", "", errors.New("lien hors du dossier autorisé")
}
func acpReadFile(root *os.Root, rel string) (string, error) {
	f, err := acpOpenRead(root, rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("fichier régulier requis")
	}
	data, err := io.ReadAll(io.LimitReader(f, acpMaxFile+1))
	if err != nil || len(data) > acpMaxFile {
		return "", errors.New("fichier trop long")
	}
	return string(data), nil
}
func (p *acpBinding) readFile(path string, line, limit *int) (any, error) {
	p.fsMu.Lock()
	defer p.fsMu.Unlock()
	root, rel, _, err := acpScopedPath(p.roots, path)
	if err != nil {
		return nil, err
	}
	content, err := acpReadFile(root, rel)
	if err != nil {
		return nil, errors.New("lecture refusée")
	}
	if line != nil || limit != nil {
		start := 0
		if line != nil {
			if *line < 0 {
				return nil, errors.New("ligne invalide")
			}
			start = max(0, *line-1)
		}
		lines := strings.SplitAfter(content, "\n")
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if limit != nil {
			if *limit < 0 {
				return nil, errors.New("limite invalide")
			}
			if *limit < end-start {
				end = start + *limit
			}
		}
		content = strings.Join(lines[start:end], "")
	}
	return map[string]any{"content": content}, nil
}
func (p *acpBinding) writeFile(path, content string) (any, error) {
	if len(content) > acpMaxFile {
		return nil, errors.New("fichier trop long")
	}
	p.fsMu.Lock()
	defer p.fsMu.Unlock()
	root, rel, canonical, err := acpScopedPath(p.roots, path)
	if err != nil {
		return nil, err
	}
	before, err := acpReadFile(root, rel)
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return nil, errors.New("écriture refusée")
	}
	p.mu.Lock()
	if p.state.FileBaselines == nil {
		p.state.FileBaselines = map[string]string{}
	}
	baseline, observed := p.state.FileBaselines[canonical]
	if !observed {
		baseline = before
	}
	bytes := len(baseline)
	for key, value := range p.state.FileBaselines {
		if key != canonical {
			bytes += len(value)
		}
	}
	p.mu.Unlock()
	if bytes > acpMaxBaselines {
		return nil, errors.New("limite de suivi des fichiers atteinte")
	}
	// Replace atomically through a new regular file. A failed write cannot
	// truncate the original, and an attacker cannot substitute a FIFO/device.
	mode := os.FileMode(0600)
	if info, err := root.Stat(rel); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("fichier régulier requis")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("écriture refusée")
	}
	temporary := filepath.Join(filepath.Dir(rel), ".loom-acp-"+newSessionID())
	f, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, errors.New("écriture refusée")
	}
	defer root.Remove(temporary)
	_, err = f.WriteString(content)
	if err == nil {
		err = f.Chmod(mode)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(temporary, rel)
	}
	if err != nil {
		return nil, errors.New("écriture échouée")
	}

	add, del := acpLineCounts(before, content)
	p.mu.Lock()
	p.state.FileBaselines[canonical] = baseline
	change := ACPChangedFile{Path: canonical, Op: "edit", Add: add, Del: del, At: time.Now().UnixMilli()}
	if created {
		change.Op = "create"
	}
	found := false
	for i, old := range p.state.Files {
		if old.Path == canonical {
			p.state.Files[i] = change
			found = true
			break
		}
	}
	if !found {
		p.state.Files = append(p.state.Files, change)
	}
	files := append([]ACPChangedFile{}, p.state.Files...)
	p.mu.Unlock()
	p.publish(DiscussionEvent{"type": "files", "files": files})
	return map[string]any{}, nil
}
