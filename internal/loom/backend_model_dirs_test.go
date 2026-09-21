package loom

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Un modèle posé hors de models/ (disque externe) doit être utilisable dès
// que son dossier est déclaré — et refusé tant qu'il ne l'est pas.
func TestResolveModelPathExtraDir(t *testing.T) {
	testHome(t)
	ext := t.TempDir()
	t.Setenv("LOOM_MODEL_DIRS", "")

	extModel := filepath.Join(ext, "gros.gguf")
	if err := os.WriteFile(extModel, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveModelPath(extModel); err == nil {
		t.Fatal("un dossier non déclaré devrait être refusé")
	}

	if err := saveExtraModelDirs([]string{ext}); err != nil {
		t.Fatal(err)
	}
	got, err := resolveModelPath(extModel)
	if err != nil {
		t.Fatalf("chemin absolu déclaré refusé : %v", err)
	}
	if got != filepath.Clean(extModel) {
		t.Fatalf("chemin = %s, attendu %s", got, extModel)
	}
	// Le simple nom de fichier doit aussi être retrouvé dans le dossier ajouté.
	if got, err := resolveModelPath("gros.gguf"); err != nil || got != extModel {
		t.Fatalf("nom seul = %q (%v), attendu %s", got, err, extModel)
	}
}

// models/ garde la priorité et les non-.gguf restent refusés.
func TestResolveModelPathHomeFirst(t *testing.T) {
	testHome(t)
	ext := t.TempDir()
	t.Setenv("LOOM_MODEL_DIRS", ext)
	if err := os.MkdirAll(modelsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{modelsDir(), ext} {
		if err := os.WriteFile(filepath.Join(d, "m.gguf"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := resolveModelPath("m.gguf")
	if err != nil || got != filepath.Join(modelsDir(), "m.gguf") {
		t.Fatalf("got %q (%v), attendu le fichier de models/", got, err)
	}
	if _, err := resolveModelPath("/etc/passwd"); err == nil {
		t.Fatal("un fichier non-.gguf devrait être refusé")
	}
}

func TestModelsDirNearBinPrefersSiblingModels(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "llama.cpp", "build", "bin", "llama-server")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	vocab := filepath.Join(root, "llama.cpp", "models")
	real := filepath.Join(root, "models")
	if err := os.MkdirAll(vocab, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vocab, "ggml-vocab-gemma.gguf"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "Gemma-Q4.gguf"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := modelsDirNearBin(bin)
	if got != real {
		t.Fatalf("near bin = %q, attendu %s", got, real)
	}
}

func TestProbedModelDirsFindsIAModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOOM_HOME", "")
	t.Setenv("LOOM_HOME", "")
	t.Setenv("LOOM_PROBE_MODELS", "")
	dir := filepath.Join(home, "IA", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gemma.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := probedModelDirs()
	if len(got) != 1 || got[0] != dir {
		t.Fatalf("probe = %v, attendu [%s]", got, dir)
	}
}

func TestProbeSkippedWhenHomeIsolated(t *testing.T) {
	testHome(t)
	if shouldProbeUserModelDirs() {
		t.Fatal("un LOOM_HOME de test ne doit pas sonder le $HOME réel")
	}
}

// La ligne MODEL= garde son chemin complet, mais la quantization se déduit
// toujours du nom de fichier.
func TestPresetModelKeepsPath(t *testing.T) {
	content := "MODEL=\"/mnt/ext/Qwen3-30B-Q5_K_M.gguf\"\nCTX=8192\n"
	if got := modelFromPresetContent(content); got != "/mnt/ext/Qwen3-30B-Q5_K_M.gguf" {
		t.Fatalf("MODEL = %q, chemin tronqué", got)
	}
	if got := detectQuant(content); got != "Q5_K_M" {
		t.Fatalf("quant = %q, attendu Q5_K_M", got)
	}
}

func TestGgufIsMmproj(t *testing.T) {
	if !ggufIsMmproj("mmproj-F16.gguf") {
		t.Fatal("mmproj-F16.gguf")
	}
	if !ggufIsMmproj("Qwen-mmproj-BF16.gguf") {
		t.Fatal("infixe mmproj")
	}
	if ggufIsMmproj("Qwen3.5-9B-Q4_K_M.gguf") {
		t.Fatal("poids marqué comme projecteur")
	}
}

func TestListGGUFFilesWalksSubdirs(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "org", "gemma")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build", "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(nested, "Gemma-Q4.gguf")
	if err := os.WriteFile(want, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ggml-vocab-gemma.gguf"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "junk", "hidden.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := listGGUFFiles(root)
	if len(got) != 1 || got[0].Path != want {
		t.Fatalf("list = %+v, attendu uniquement %s", got, want)
	}
	if modelDirCount(root) != 1 {
		t.Fatalf("count = %d, attendu 1", modelDirCount(root))
	}
}

func TestPreferredDownloadDirFallsBack(t *testing.T) {
	testHome(t)
	extra := t.TempDir()
	t.Setenv("LOOM_MODEL_DIRS", "")
	if err := saveExtraModelDirs([]string{extra}); err != nil {
		t.Fatal(err)
	}
	if err := savePreferredDownloadDir(extra); err != nil {
		t.Fatal(err)
	}
	if got := preferredDownloadDir(); got != extra {
		t.Fatalf("pref = %q, attendu %s", got, extra)
	}
	if err := saveExtraModelDirs(nil); err != nil {
		t.Fatal(err)
	}
	forgetPreferredIfGone()
	if got := preferredDownloadDir(); got != modelsDir() {
		t.Fatalf("après retrait = %q, attendu models/", got)
	}
}

func TestLlamaCppRootFromBin(t *testing.T) {
	root := t.TempDir()
	cpp := filepath.Join(root, "llama.cpp")
	bin := filepath.Join(cpp, "build", "bin", "llama-server")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := llamaCppRoot(bin)
	if got != cpp {
		t.Fatalf("root = %q, attendu %s", got, cpp)
	}
	if llamaCppRoot(filepath.Join(root, "elsewhere", "llama-server")) != "" {
		t.Fatal("un binaire hors llama.cpp ne doit pas inventer une racine")
	}
}

func TestModelDirsIncludesNestedLlamaCppModels(t *testing.T) {
	testHome(t)
	root := t.TempDir()
	cpp := filepath.Join(root, "llama.cpp")
	bin := filepath.Join(cpp, "build", "bin", "llama-server")
	nested := filepath.Join(cpp, "models", "gemma")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(nested, "Gemma-Q4.gguf")
	if err := os.WriteFile(model, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(map[string]string{"BIN": bin}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range extraModelDirs() {
		if d == cpp {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("extra dirs = %v, attendu la racine llama.cpp", extraModelDirs())
	}
	got, err := resolveModelPath("Gemma-Q4.gguf")
	if err != nil || got != model {
		t.Fatalf("resolve = %q (%v), attendu %s", got, err, model)
	}
}

// Les snapshots Hugging Face sont des liens vers blobs/ : la taille affichée
// doit être celle du fichier réel, pas celle du lien (souvent ~76 octets).
func TestListGGUFFollowsSymlinkSize(t *testing.T) {
	root := t.TempDir()
	blobs := filepath.Join(root, "blobs")
	snaps := filepath.Join(root, "snapshots", "abc")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snaps, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("G"), 4096)
	blob := filepath.Join(blobs, "sha256-deadbeef")
	if err := os.WriteFile(blob, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(snaps, "Qwen-long-name.gguf")
	if err := os.Symlink(filepath.Join("..", "..", "blobs", "sha256-deadbeef"), link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobs, "orphan.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := listGGUFFiles(root)
	if len(got) != 1 || got[0].Path != link {
		t.Fatalf("list = %+v, attendu uniquement %s", got, link)
	}
	if got[0].Size != int64(len(payload)) {
		t.Fatalf("size = %d, attendu %d (lien suivi)", got[0].Size, len(payload))
	}
}
