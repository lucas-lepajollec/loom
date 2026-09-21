package ajean

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Les .gguf vivent dans $AJEAN_HOME/models, mais un modèle de 40 Go tient
// rarement sur le disque système : on autorise des dossiers supplémentaires
// (disque dur externe, second SSD, montage réseau…). La liste est persistée en
// base — PAS dans la configuration, que le changement de preset remplace — et
// peut aussi venir de $AJEAN_MODEL_DIRS.

// modelDirsListSep sépare les dossiers dans $AJEAN_MODEL_DIRS (':' sous Unix,
// ';' sous Windows, comme le PATH).
func modelDirsListSep() string {
	if runtime.GOOS == "windows" {
		return ";"
	}
	return ":"
}

func declaredModelDirs() []string {
	var raw []string
	getJSON(bkState, "model_dirs", &raw)
	return dedupDirs(raw, modelsDir())
}

// extraModelDirs renvoie les dossiers déclarés par l'utilisateur (base puis
// variable d'environnement), nettoyés et dédoublonnés, models/ exclu.
func extraModelDirs() []string {
	raw := declaredModelDirs()
	if v := os.Getenv("LOOM_MODEL_DIRS"); v != "" {
		raw = append(raw, strings.Split(v, modelDirsListSep())...)
	}
	if v := os.Getenv("AJEAN_MODEL_DIRS"); v != "" {
		raw = append(raw, strings.Split(v, modelDirsListSep())...)
	}
	if shouldProbeUserModelDirs() {
		raw = append(raw, probedModelDirs()...)
	}
	if bin := strings.TrimSpace(ReadConfig()["BIN"]); bin != "" {
		if d := llamaCppRoot(bin); d != "" {
			raw = append(raw, d)
		}
	}
	return dedupDirs(raw, modelsDir())
}

// shouldProbeUserModelDirs : un LOOM_HOME/AJEAN_HOME explicite (tests, install
// isolée) n'hérite pas du $HOME de login. En `go run` sans env, on sonde.
func shouldProbeUserModelDirs() bool {
	if os.Getenv("LOOM_PROBE_MODELS") == "0" {
		return false
	}
	if os.Getenv("LOOM_HOME") != "" || os.Getenv("AJEAN_HOME") != "" {
		return os.Getenv("LOOM_PROBE_MODELS") == "1"
	}
	return true
}

// probedModelDirs cherche des .gguf dans des emplacements habituels. Aucun
// chemin utilisateur n'est écrit en dur dans la config : on ne garde que les
// dossiers qui existent et contiennent au moins un modèle.
func probedModelDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	cands := []string{
		filepath.Join(home, "IA", "models"),
		filepath.Join(home, "models"),
		filepath.Join(home, "llm", "models"),
	}
	var out []string
	for _, d := range cands {
		if modelDirCount(d) > 0 {
			out = append(out, d)
		}
	}
	return out
}

func isVocabGGUF(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "ggml-vocab-") || strings.HasPrefix(n, "ggml-model-vocab")
}

// ggufIsMmproj : projecteur vision, pas un modèle lançable.
func ggufIsMmproj(name string) bool {
	return strings.Contains(strings.ToLower(name), "mmproj")
}

func usefulModelDirCount(dir string) int {
	return len(listGGUFFiles(dir))
}

// skipModelWalkDir ignore les dossiers qui n'abritent pas de GGUF utiles
// (build llama.cpp, caches HF blobs/) pour ne pas descendre tout l'arbre.
func skipModelWalkDir(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || strings.HasPrefix(n, ".") {
		return true
	}
	switch n {
	case "build", "cmake-build-debug", "cmake-build-release", "cmakefiles", "node_modules", "target", "__pycache__", "blobs":
		return true
	}
	return false
}

type ggufEntry struct {
	Name, Path, Root string
	Size             int64
}

// listGGUFFiles parcourt root et ses sous-dossiers pour tous les .gguf
// lançables (hors vocabulaires ggml et hors shards 2..N).
func listGGUFFiles(root string) []ggufEntry {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil
	}
	var out []ggufEntry
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipModelWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") || isFollowerShard(name) || isVocabGGUF(name) {
			return nil
		}
		info, err := os.Stat(p)
		size := int64(0)
		if err == nil {
			size = info.Size()
		} else if fi, e2 := d.Info(); e2 == nil {
			size = fi.Size()
		}
		out = append(out, ggufEntry{Name: name, Path: p, Root: root, Size: size})
		return nil
	})
	return out
}

// llamaCppRoot remonte depuis llama-server jusqu'au dossier nommé llama.cpp
// (ex. …/llama.cpp/build/bin/llama-server). Vide si le binaire n'est pas
// dans un arbre llama.cpp — on ne scanne pas /usr/local au hasard.
func llamaCppRoot(bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(bin))
	for i := 0; i < 10; i++ {
		if strings.EqualFold(filepath.Base(dir), "llama.cpp") {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return filepath.Clean(dir)
			}
			return filepath.Clean(abs)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// modelsDirNearBin cherche un dossier models/ utile à proximité d'un
// llama-server (ex. …/llama.cpp/build/bin/llama-server → …/IA/models).
func modelsDirNearBin(bin string) string {
	dir := filepath.Dir(filepath.Clean(strings.TrimSpace(bin)))
	best, bestN := "", 0
	for i := 0; i < 8; i++ {
		parent := filepath.Dir(dir)
		cands := []string{filepath.Join(dir, "models"), filepath.Join(parent, "models")}
		if strings.EqualFold(filepath.Base(dir), "models") {
			cands = append(cands, dir)
		}
		for _, cand := range cands {
			if n := usefulModelDirCount(cand); n > bestN {
				best, bestN = cand, n
			}
		}
		if parent == dir {
			break
		}
		dir = parent
	}
	if bestN == 0 {
		return ""
	}
	abs, err := filepath.Abs(best)
	if err != nil {
		return best
	}
	return filepath.Clean(abs)
}

// adoptModelsDirNearBin enregistre le dossier models détecté près du BIN.
func adoptModelsDirNearBin(bin string) string {
	d := modelsDirNearBin(bin)
	if d == "" || normDir(d) == normDir(modelsDir()) {
		return d
	}
	cur := declaredModelDirs()
	for _, x := range cur {
		if normDir(x) == normDir(d) {
			return d
		}
	}
	if err := saveExtraModelDirs(append(cur, d)); err != nil {
		return ""
	}
	return d
}

// dedupDirs nettoie une liste de chemins : vides retirés, chemins absolus
// normalisés, doublons et `skip` écartés (comparaison insensible à la casse
// sous Windows, comme le système de fichiers).
func dedupDirs(list []string, skip string) []string {
	seen := map[string]bool{normDir(skip): true}
	out := []string{}
	for _, d := range list {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		k := normDir(abs)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, abs)
	}
	return out
}

// normDir met un chemin sous forme comparable (séparateurs unifiés, casse
// neutralisée sous Windows, slash final retiré).
func normDir(p string) string {
	// Clean d'abord (il réintroduit le séparateur natif), puis on unifie en '/'.
	p = strings.ReplaceAll(filepath.Clean(strings.TrimSpace(p)), "\\", "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// modelDirs renvoie tous les dossiers où chercher un .gguf : models/ d'abord
// (il reste la destination des téléchargements), puis les dossiers ajoutés.
func modelDirs() []string {
	return append([]string{modelsDir()}, extraModelDirs()...)
}

// saveExtraModelDirs enregistre la liste des dossiers supplémentaires.
func saveExtraModelDirs(dirs []string) error {
	return putJSON(bkState, "model_dirs", dedupDirs(dirs, modelsDir()))
}

// pathWithin dit si le fichier p est dans le dossier dir (ou un sous-dossier).
func pathWithin(dir, p string) bool {
	d, f := normDir(dir), normDir(p)
	return f == d || strings.HasPrefix(f, d+"/")
}

// baseName renvoie le nom de fichier d'un chemin en acceptant les DEUX
// séparateurs : un config.env écrit sous Windows (C:\models\x.gguf) peut être
// relu par un ajean Linux, où filepath.Base ne coupe pas sur '\'.
func baseName(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// resolveServeModelPath résout MODEL= pour LANCER le moteur. Différence avec
// resolveModelPath : un chemin absolu qui pointe sur un .gguf existant est
// accepté même hors des dossiers déclarés. L'allowlist protège des opérations
// destructrices pilotées depuis l'UI (suppression d'un fichier) ; l'appliquer à
// « ouvrir ce modèle en lecture » rendait la config éditée à la main
// inutilisable : MODEL=/home/moi/models/x.gguf renvoyait « dossier non
// autorisé », le moteur mourait en boucle, et le seul remède connu était
// d'aller déclarer le dossier dans l'interface web.
func resolveServeModelPath(name string) (string, error) {
	p, err := resolveModelPath(name)
	if err == nil {
		return p, nil
	}
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(name), `"`))
	if filepath.IsAbs(s) && strings.HasSuffix(strings.ToLower(s), ".gguf") {
		abs := filepath.Clean(s)
		if st, e := os.Stat(abs); e == nil && !st.IsDir() {
			return abs, nil
		}
	}
	return "", err
}

// resolveModelPath transforme une valeur MODEL= (nom de fichier OU chemin
// absolu) en chemin absolu vers un .gguf. Un chemin absolu n'est accepté que
// s'il est dans un des dossiers déclarés — sinon l'UI web deviendrait un moyen
// de lire/supprimer n'importe quel fichier de la machine. Un simple nom de
// fichier est cherché dans chaque dossier, AJEAN_HOME en premier.
func resolveModelPath(name string) (string, error) {
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(name), `"`))
	if s == "" {
		return "", fmt.Errorf("nom de modèle invalide")
	}
	if !strings.HasSuffix(strings.ToLower(s), ".gguf") {
		return "", fmt.Errorf("le modèle doit être un fichier .gguf")
	}
	if filepath.IsAbs(s) {
		abs := filepath.Clean(s)
		for _, d := range modelDirs() {
			if pathWithin(d, abs) {
				return abs, nil
			}
		}
		return "", fmt.Errorf("dossier non autorisé : %s — ajoute-le dans « Dossiers de modèles »", filepath.Dir(abs))
	}
	base := baseName(s)
	if base == "" || base == "." {
		return "", fmt.Errorf("nom de modèle invalide")
	}
	for _, d := range modelDirs() {
		p := filepath.Join(d, base)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		for _, g := range listGGUFFiles(d) {
			if strings.EqualFold(g.Name, base) {
				return g.Path, nil
			}
		}
	}
	return filepath.Join(modelsDir(), base), nil // introuvable : l'appelant décidera
}

// modelDirCount compte les .gguf lisibles d'un dossier (-1 si illisible).
func modelDirCount(dir string) int {
	if _, err := os.Stat(dir); err != nil {
		return -1
	}
	return len(listGGUFFiles(dir))
}

// diskFree renvoie l'espace libre du volume qui porte dir, ou -1 s'il est
// inconnu. Le dossier n'existe pas forcément encore (models/ au premier
// lancement) : on remonte les parents jusqu'à en trouver un qui existe.
func diskFree(dir string) int64 {
	d, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return -1
	}
	for {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return diskFreeAt(d)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return -1
		}
		d = parent
	}
}

const downloadDirKey = "download_dir"

func dirInList(abs string, dirs []string) (string, bool) {
	want := normDir(abs)
	for _, d := range dirs {
		if normDir(d) == want {
			return d, true
		}
	}
	return "", false
}

// preferredDownloadDir est le dossier où atterrissent les GGUF du hub
// (et les liens collés sans destination). Vide / oublié = models/ de Loom.
func preferredDownloadDir() string {
	p := strings.TrimSpace(getStr(bkState, downloadDirKey))
	if p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			if d, ok := dirInList(abs, modelDirs()); ok {
				return d
			}
		}
	}
	return modelsDir()
}

func savePreferredDownloadDir(dir string) error {
	abs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return fmt.Errorf("chemin invalide")
	}
	d, ok := dirInList(abs, modelDirs())
	if !ok {
		return fmt.Errorf("dossier non autorisé : %s — ajoute-le dans « Dossiers de modèles »", abs)
	}
	if normDir(d) == normDir(modelsDir()) {
		return putStr(bkState, downloadDirKey, "")
	}
	return putStr(bkState, downloadDirKey, d)
}

func forgetPreferredIfGone() {
	p := strings.TrimSpace(getStr(bkState, downloadDirKey))
	if p == "" {
		return
	}
	if _, ok := dirInList(p, modelDirs()); !ok {
		_ = putStr(bkState, downloadDirKey, "")
	}
}

// resolveDownloadDir valide le dossier de destination demandé pour un
// téléchargement : vide = dossier du hub, sinon il doit être un des dossiers
// déclarés — sans quoi l'UI web permettrait d'écrire n'importe où sur la
// machine.
func resolveDownloadDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return preferredDownloadDir(), nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("chemin invalide")
	}
	if d, ok := dirInList(abs, modelDirs()); ok {
		return d, nil
	}
	return "", fmt.Errorf("dossier non autorisé : %s — ajoute-le dans « Dossiers de modèles »", abs)
}

// handleModelDirs : GET liste les dossiers de modèles (AJEAN_HOME + ajoutés),
// POST {path, action:"add"|"remove"|"download"} en ajoute, en retire un,
// ou définit le dossier des téléchargements du hub (créé s'il n'existe pas).
func handleModelDirs(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		p := strings.TrimSpace(req.Path)
		if p == "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "chemin vide"})
			return
		}
		cur := extraModelDirs()
		abs, err := filepath.Abs(p)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "chemin invalide"})
			return
		}
		switch req.Action {
		case "remove":
			kept := []string{}
			for _, d := range cur {
				if normDir(d) != normDir(abs) {
					kept = append(kept, d)
				}
			}
			cur = kept
			if err := saveExtraModelDirs(cur); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			forgetPreferredIfGone()
		case "download":
			if err := os.MkdirAll(abs, 0o755); err != nil {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "impossible de créer " + abs + " : " + err.Error()})
				return
			}
			st, err := os.Stat(abs)
			if err != nil || !st.IsDir() {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "pas un dossier : " + abs})
				return
			}
			if _, ok := dirInList(abs, modelDirs()); !ok {
				cur = append(cur, abs)
				if err := saveExtraModelDirs(cur); err != nil {
					sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
					return
				}
			}
			if err := savePreferredDownloadDir(abs); err != nil {
				sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		default:
			st, err := os.Stat(abs)
			if err != nil || !st.IsDir() {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "dossier introuvable : " + abs})
				return
			}
			cur = append(cur, abs)
			if err := saveExtraModelDirs(cur); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		sendJSON(w, 200, map[string]any{"ok": true, "download_dir": preferredDownloadDir()})
		return
	}
	dl := preferredDownloadDir()
	out := []map[string]any{{"path": modelsDir(), "home": true, "download": normDir(modelsDir()) == normDir(dl), "count": modelDirCount(modelsDir()), "exists": true, "free": diskFree(modelsDir())}}
	for _, d := range extraModelDirs() {
		n := modelDirCount(d)
		out = append(out, map[string]any{"path": d, "home": false, "download": normDir(d) == normDir(dl), "count": n, "exists": n >= 0, "free": diskFree(d)})
	}
	sendJSON(w, 200, map[string]any{"ok": true, "dirs": out, "download_dir": dl, "env": os.Getenv("AJEAN_MODEL_DIRS") != "" || os.Getenv("LOOM_MODEL_DIRS") != ""})
}
