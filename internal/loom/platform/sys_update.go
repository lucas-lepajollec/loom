package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// loom update — met à jour le binaire depuis les releases GitHub du projet.
// Périmètre minimal : compare la version, télécharge l'asset correspondant à
// l'OS/arch courant, remplace le binaire en place, puis affiche quoi redémarrer.
// AUCUN redémarrage de service automatique (choix volontaire, plus sûr).

const updateRepo = "lucas-lepajollec/loom"

type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// updateAssetName est le nom de l'asset de release pour la plateforme courante :
// loom-linux, loom-linux-arm, loom-macos, loom-macos-arm, loom-windows.exe,
// loom-windows-arm.exe. Un seul nom publié, un seul nom cherché.
func UpdateAssetName() string { return AssetNameFor(runtime.GOOS, runtime.GOARCH) }

// assetNameFor est séparée pour être vérifiable sur les six plateformes, et pas
// seulement sur celle qui exécute les tests.
func AssetNameFor(goos, goarch string) string {
	name := "loom-" + map[string]string{
		"darwin":  "macos",
		"linux":   "linux",
		"windows": "windows",
	}[goos]
	if goarch == "arm64" {
		name += "-arm"
	}
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// pickAsset trouve dans la release l'asset de cette plateforme. Renvoie son nom
// (celui qui servira à vérifier le SHA-256), son URL et sa taille.
func PickAsset(rel *Release) (name, url string, size int64) {
	want := UpdateAssetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			return a.Name, a.BrowserDownloadURL, a.Size
		}
	}
	return "", "", 0
}

// ensureV préfixe "v" si absent, pour comparer via golang.org/x/mod/semver.
func EnsureV(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	return s
}

func FetchLatestRelease() (*Release, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "loom-update")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub a répondu %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// updatePermissionError formule le seul message utile quand loom n'a pas les
// droits d'écrire son propre binaire : la commande exacte à lancer.
func UpdatePermissionError(exe string) error {
	if runtime.GOOS == "windows" {
		// Windows renvoie le MÊME code (5, accès refusé) pour « droits
		// insuffisants » et pour « fichier utilisé par un processus » : on nomme
		// les deux causes au lieu d'affirmer la mauvaise.
		return fmt.Errorf("impossible de remplacer %s : soit un autre Loom utilise ce fichier (ferme l'application et arrête le service, puis réessaie), soit les droits manquent (relance Loom en administrateur)", exe)
	}
	return fmt.Errorf("droits insuffisants pour remplacer %s (le binaire appartient à root) — lance la mise à jour en ligne de commande : sudo loom update", exe)
}

// checkUpdateWritable vérifie qu'on peut écrire dans le dossier du binaire, en
// créant réellement un fichier témoin (les bits de permission seuls ne suffisent
// pas : root, groupes, ACL, montage en lecture seule…).
func CheckUpdateWritable(exe string) error {
	dir := filepath.Dir(exe)
	probe, err := os.CreateTemp(dir, ".loom-update-probe-*")
	if err != nil {
		if os.IsPermission(err) {
			return UpdatePermissionError(exe)
		}
		return fmt.Errorf("impossible d'écrire dans %s : %w", dir, err)
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return nil
}

func DownloadTo(url, dst string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "loom-update")
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub a répondu %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cErr := io.Copy(f, resp.Body)
	if closeErr := f.Close(); cErr == nil {
		cErr = closeErr
	}
	return cErr
}

// verifyChecksum vérifie le SHA-256 du binaire téléchargé contre le fichier
// SHA256SUMS publié dans la release (format `sha256sum` : "<hex>  <nom>").
// Une release sans manifeste ne peut pas être installée automatiquement.
func VerifyChecksum(rel *Release, assetName, path string) error {
	var sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case "SHA256SUMS", "SHA256SUMS.txt", "checksums.txt":
			sumsURL = a.BrowserDownloadURL
		}
	}
	if sumsURL == "" {
		return fmt.Errorf("release sans SHA256SUMS.txt — mise à jour annulée")
	}
	req, _ := http.NewRequest("GET", sumsURL, nil)
	req.Header.Set("User-Agent", "loom-update")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("téléchargement des sommes de contrôle : %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("sommes de contrôle : GitHub a répondu %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		// sha256sum peut préfixer le nom de "*" (mode binaire).
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == assetName {
			want = strings.ToLower(f[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("SHA256SUMS présent mais sans entrée pour %q — mise à jour annulée", assetName)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("somme SHA-256 invalide (%s reçu, %s attendu) — mise à jour annulée", got, want)
	}
	return nil
}

func FileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return -1
}

// replaceBinary échange le binaire en place. Sous Unix, rename() dans le même
// dossier est atomique et fonctionne même si l'ancien binaire tourne encore
// (l'inode ouvert reste valide). Sous Windows on ne peut pas écraser un .exe en
// cours : on renomme l'ancien en .old (supprimé au prochain lancement).
func ReplaceBinary(exe, tmp string) error {
	if runtime.GOOS == "windows" {
		old, err := RenameAside(exe)
		if err != nil {
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			_ = os.Rename(old, exe) // rollback
			return err
		}
		_ = os.Remove(old) // échoue si l'exe tourne encore → nettoyé au prochain run
		return nil
	}
	return os.Rename(tmp, exe)
}

// renameAside écarte un fichier en le renommant sous un nom UNIQUE.
//
// Un nom FIXE (« .old ») est un piège sous Windows : si un processus tourne
// ENCORE depuis le .old d'une mise à jour précédente, ce fichier ne peut être ni
// supprimé ni écrasé. Le renommage échoue alors avec « Accès refusé » (errno 5),
// que os.IsPermission rapporte comme un problème de droits — d'où le message
// « relance Loom en administrateur », un conseil FAUX : aucun privilège ne
// permet de remplacer l'image d'un exécutable en cours d'exécution. Reproduit
// puis vérifié corrigé sur banc d'essai, deux processus vivants sur les deux
// noms : avec un suffixe unique le renommage passe.
// asideSeq départage deux écartements rapprochés. UnixNano seul ne suffit PAS :
// la granularité de l'horloge Windows peut rendre la même valeur à deux appels
// consécutifs, et les deux écartements se disputaient alors le même nom — le
// second renommage écrasant le premier, donc le binaire précédent perdu.
var asideSeq atomic.Uint64

func RenameAside(path string) (string, error) {
	aside := fmt.Sprintf("%s.old-%d-%d", path, time.Now().UnixNano(), asideSeq.Add(1))
	if err := os.Rename(path, aside); err != nil {
		return "", err
	}
	return aside, nil
}

// removeOldBinaries supprime les écartements laissés par les remplacements
// précédents (nom fixe hérité ET noms uniques). Best-effort : ceux dont le
// processus tourne encore résistent, on réessaiera au prochain lancement.
func RemoveOldBinaries(path string) {
	_ = os.Remove(path + ".old")
	matches, _ := filepath.Glob(path + ".old-*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// cleanupOldBinary supprime silencieusement le .old laissé par une MAJ Windows
// précédente (le fichier n'était pas supprimable tant que l'exe tournait).
func CleanupOldBinary() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	RemoveOldBinaries(exe)
	// L'alias herite laisse le meme reliquat quand il etait en cours d'execution
	// au moment ou on l'a remplace (voir replaceExe).
	RemoveOldBinaries(filepath.Join(filepath.Dir(exe), "loom.exe"))
}
