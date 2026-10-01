package loom

import (
	"errors"
	"fmt"
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

type ACPChangedFile struct {
	Path string `json:"path"`
	Op   string `json:"op"`
	Add  int    `json:"add"`
	Del  int    `json:"del"`
	At   int64  `json:"at"`
}

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
	f, err := root.Open(rel)
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
			if *line < 1 {
				return nil, errors.New("ligne invalide")
			}
			start = *line - 1
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
	// Open without truncation first: inspect the opened descriptor before writing.
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, errors.New("écriture refusée")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("fichier régulier requis")
	}
	if err = f.Truncate(0); err == nil {
		_, err = f.WriteString(content)
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
			if old.Op == "create" {
				change.Op = "create"
			}
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
func acpLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type acpDiffLine struct {
	kind byte
	text string
}

// Line LCS on the changed middle; equal prefixes/suffixes are retained. A
// bounded fallback is still a valid diff for very large unrelated rewrites.
func acpLineDiff(before, after string) []acpDiffLine {
	a, b := acpLines(before), acpLines(after)
	out := []acpDiffLine{}
	first := 0
	for first < len(a) && first < len(b) && a[first] == b[first] {
		out = append(out, acpDiffLine{' ', a[first]})
		first++
	}
	last := 0
	for last < len(a)-first && last < len(b)-first && a[len(a)-1-last] == b[len(b)-1-last] {
		last++
	}
	x, y := a[first:len(a)-last], b[first:len(b)-last]
	if len(x) > 0 && len(y) > 0 && len(x) <= 1000000/len(y) {
		width := len(y) + 1
		dp := make([]int, (len(x)+1)*width)
		for i := len(x) - 1; i >= 0; i-- {
			for j := len(y) - 1; j >= 0; j-- {
				if x[i] == y[j] {
					dp[i*width+j] = dp[(i+1)*width+j+1] + 1
				} else {
					dp[i*width+j] = max(dp[(i+1)*width+j], dp[i*width+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(x) || j < len(y) {
			if i < len(x) && j < len(y) && x[i] == y[j] {
				out = append(out, acpDiffLine{' ', x[i]})
				i++
				j++
			} else if i < len(x) && (j == len(y) || dp[(i+1)*width+j] >= dp[i*width+j+1]) {
				out = append(out, acpDiffLine{'-', x[i]})
				i++
			} else {
				out = append(out, acpDiffLine{'+', y[j]})
				j++
			}
		}
	} else {
		for _, line := range x {
			out = append(out, acpDiffLine{'-', line})
		}
		for _, line := range y {
			out = append(out, acpDiffLine{'+', line})
		}
	}
	for _, line := range a[len(a)-last:] {
		out = append(out, acpDiffLine{' ', line})
	}
	return out
}
func acpLineCounts(before, after string) (add, del int) {
	for _, line := range acpLineDiff(before, after) {
		if line.kind == '+' {
			add++
		}
		if line.kind == '-' {
			del++
		}
	}
	return
}
func acpUnifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", path, path, min(1, len(acpLines(before))), len(acpLines(before)), min(1, len(acpLines(after))), len(acpLines(after)))
	for _, line := range acpLineDiff(before, after) {
		out.WriteByte(line.kind)
		out.WriteString(line.text)
		if !strings.HasSuffix(line.text, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
	return out.String()
}
