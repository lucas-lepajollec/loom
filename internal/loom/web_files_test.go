package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func filesCall(t *testing.T, h http.HandlerFunc, method, target, body string) (int, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func secondBrainFixture(t *testing.T) string {
	t.Helper()
	testHome(t)
	dir := t.TempDir()
	if err := SetConfigKey(secondBrainKey, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "projets"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(dir, "projets", "plan.md")
	if err := os.WriteFile(plan, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(plan, old, old)
	return dir
}

// Notes are read, saved and listed; a save over a newer file is refused.
func TestFilesReadWriteAndConflict(t *testing.T) {
	dir := secondBrainFixture(t)
	code, read := filesCall(t, handleFilesRead, "GET", "/api/files/read?path=projets/plan.md", "")
	if code != 200 || read["content"] != "# Plan\n" {
		t.Fatalf("read %d %v", code, read)
	}
	mod := int64(read["modified"].(float64))
	body, _ := json.Marshal(map[string]any{"path": "projets/plan.md", "content": "# Plan\n\n- [ ] faire\n", "modified": mod})
	if code, out := filesCall(t, handleFilesWrite, "POST", "/api/files/write", string(body)); code != 200 {
		t.Fatalf("write %d %v", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "projets", "plan.md")); string(b) != "# Plan\n\n- [ ] faire\n" {
		t.Fatalf("content %q", b)
	}
	// The old modification time no longer matches: someone else wrote since.
	if code, out := filesCall(t, handleFilesWrite, "POST", "/api/files/write", string(body)); code != 409 || out["conflict"] != true {
		t.Fatalf("stale write accepted: %d %v", code, out)
	}
	create, _ := json.Marshal(map[string]any{"path": "inbox/idee.md", "content": "x", "create": true})
	if code, _ := filesCall(t, handleFilesWrite, "POST", "/api/files/write", string(create)); code != 200 {
		t.Fatal("create failed")
	}
	if code, _ := filesCall(t, handleFilesWrite, "POST", "/api/files/write", string(create)); code != 409 {
		t.Fatal("create over an existing file accepted")
	}
	code, tree := filesCall(t, handleFilesTree, "GET", "/api/files/tree", "")
	raw, _ := json.Marshal(tree["tree"])
	if code != 200 || !strings.Contains(string(raw), `"path":"inbox/idee.md"`) || !strings.Contains(string(raw), `"path":"projets"`) {
		t.Fatalf("tree %s", raw)
	}
}

// Paths never leave the folder: parent segments, absolute paths, hidden
// folders and symlinks pointing outside are refused.
func TestFilesStayInsideTheFolder(t *testing.T) {
	dir := secondBrainFixture(t)
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600)
	for _, p := range []string{"../secret.txt", "/etc/passwd", ".trash/x", "projets/../../x"} {
		if code, _ := filesCall(t, handleFilesRead, "GET", "/api/files/read?path="+p, ""); code == 200 {
			t.Fatalf("read %q allowed", p)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
			t.Fatal(err)
		}
		if code, out := filesCall(t, handleFilesRead, "GET", "/api/files/read?path=escape/secret.txt", ""); code == 200 {
			t.Fatalf("symlink escape: %v", out)
		}
		body, _ := json.Marshal(map[string]any{"path": "escape/new.md", "content": "x", "create": true})
		filesCall(t, handleFilesWrite, "POST", "/api/files/write", string(body))
		if _, err := os.Stat(filepath.Join(outside, "new.md")); err == nil {
			t.Fatal("write followed a symlink outside the folder")
		}
	}
}

// Rename and delete; delete keeps the file in the folder's trash.
func TestFilesRenameAndTrash(t *testing.T) {
	dir := secondBrainFixture(t)
	if code, out := filesCall(t, handleFilesOp, "POST", "/api/files/op", `{"op":"rename","path":"projets/plan.md","to":"archives/plan.md"}`); code != 200 {
		t.Fatalf("rename %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "archives", "plan.md")); err != nil {
		t.Fatal(err)
	}
	if code, _ := filesCall(t, handleFilesOp, "POST", "/api/files/op", `{"op":"delete","path":"archives/plan.md"}`); code != 200 {
		t.Fatal("delete failed")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ".trash"))
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "plan.md") {
		t.Fatalf("trash %v", entries)
	}
	if code, _ := filesCall(t, handleFilesOp, "POST", "/api/files/op", `{"op":"mkdir","path":"journal"}`); code != 200 {
		t.Fatal("mkdir failed")
	}
}
