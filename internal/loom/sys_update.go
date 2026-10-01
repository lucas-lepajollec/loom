package loom

import (
	"fmt"
	"golang.org/x/mod/semver"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// updateInfo décrit l'état de mise à jour (partagé CLI + UI web).
type updateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`
}

// checkForUpdate interroge GitHub et compare à la version courante. Réutilisé
// par `loom update` (CLI) et par l'endpoint web /api/update.
func checkForUpdate() (updateInfo, error) {
	info := updateInfo{Current: Version}
	rel, err := fetchLatestRelease()
	if err != nil {
		return info, err
	}
	latest := ensureV(rel.TagName)
	if !semver.IsValid(latest) {
		return info, fmt.Errorf("tag de release inattendu : %q", rel.TagName)
	}
	info.Latest = strings.TrimPrefix(latest, "v")
	info.URL = rel.HTMLURL
	info.Available = semver.Compare(latest, ensureV(Version)) > 0
	return info, nil
}

// applyUpdate télécharge et installe le binaire de la dernière release pour
// l'OS/arch courant. Renvoie la nouvelle version. Ne redémarre AUCUN service
// (voir printRestartHint / le message renvoyé à l'UI).
func applyUpdate() (string, error) {
	rel, err := fetchLatestRelease()
	if err != nil {
		return "", fmt.Errorf("impossible de contacter GitHub : %w", err)
	}
	latest := ensureV(rel.TagName)
	if !semver.IsValid(latest) {
		return "", fmt.Errorf("tag de release inattendu : %q", rel.TagName)
	}
	if semver.Compare(latest, ensureV(Version)) <= 0 {
		return Version, fmt.Errorf("déjà à jour (%s)", Version)
	}

	want, url, size := pickAsset(rel)
	if url == "" {
		return "", fmt.Errorf("aucun binaire %s dans la release %s (os/arch %s/%s)",
			updateAssetName(), latest, runtime.GOOS, runtime.GOARCH)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// Contrôle des droits AVANT de télécharger : sans ça, l'échec survient sur le
	// os.Create du fichier temporaire et l'utilisateur reçoit un « open
	// /usr/local/bin/.loom-update.tmp: permission denied » incompréhensible,
	// notamment depuis le bouton de l'UI web (loom web lancé sans sudo).
	if err := checkUpdateWritable(exe); err != nil {
		return "", err
	}
	tmp := filepath.Join(filepath.Dir(exe), ".loom-update.tmp")

	if err := downloadTo(url, tmp); err != nil {
		os.Remove(tmp)
		if os.IsPermission(err) {
			return "", updatePermissionError(exe)
		}
		return "", fmt.Errorf("téléchargement : %w", err)
	}
	if err := verifyChecksum(rel, want, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode()
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if got := fileSize(tmp); size > 0 && got != size {
		os.Remove(tmp)
		return "", fmt.Errorf("taille inattendue (%d o reçus, %d attendus) — mise à jour annulée", got, size)
	}
	if err := replaceBinary(exe, tmp); err != nil {
		os.Remove(tmp)
		if os.IsPermission(err) {
			return "", updatePermissionError(exe)
		}
		return "", err
	}
	return strings.TrimPrefix(latest, "v"), nil
}

func cmdUpdate(args []string) error {
	checkOnly := false
	for _, a := range args {
		if a == "--check" || a == "-check" || a == "check" {
			checkOnly = true
		}
	}
	fmt.Println("recherche de la dernière version…")
	info, err := checkForUpdate()
	if err != nil {
		return fmt.Errorf("impossible de contacter GitHub : %w", err)
	}
	if !info.Available {
		fmt.Printf("loom est déjà à jour (%s).\n", Version)
		return nil
	}
	fmt.Printf("nouvelle version disponible : %s  (actuelle : %s)\n", info.Latest, Version)
	fmt.Printf("  %s\n", info.URL)
	if checkOnly {
		fmt.Println("lance 'loom update' pour l'installer.")
		return nil
	}
	fmt.Printf("téléchargement de %s…\n", updateAssetName())
	newVer, err := applyUpdate()
	if err != nil {
		return err
	}
	fmt.Printf("✓ loom mis à jour en %s\n", newVer)
	printRestartHint()
	return nil
}

// handleUpdateCheck (GET /api/update) : renvoie l'état de mise à jour pour l'UI.
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info, err := checkForUpdate()
	if err != nil {
		sendJSON(w, 200, map[string]any{"current": Version, "available": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, info)
}

// handleUpdateApply (POST /api/update/apply) : télécharge et installe la dernière
// version. Sur un serveur Linux/systemd, relance ensuite AUTOMATIQUEMENT le service
// d'UI (loom-ui : il sert l'UI, le chat et le tunnel) pour appliquer la MAJ sans
// avoir à SSH — voir restartAfterUpdate. Renvoie la version + le message de statut.
func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	newVer, err := applyUpdate()
	if err != nil {
		// 500 : un client script peut tester le statut HTTP ; l'UI, elle, lit le JSON.
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	restarting, msg := restartAfterUpdate()
	if !restarting {
		msg = restartHintText()
	}
	sendJSON(w, 200, map[string]any{"ok": true, "version": newVer, "restart": msg, "restarting": restarting})
}

// restartAfterUpdate relance le service d'UI (uiServiceName, « loom-ui » : il sert
// l'interface, le chat et le tunnel) juste après une MAJ déclenchée depuis l'UI, pour
// éviter le SSH manuel. Conditions : Linux/systemd + service actif. On NE touche PAS à
// loom-engine (llama-server) pour ne pas recharger le modèle (long et inattendu depuis
// un bouton « mettre à jour »). Le redémarrage est DIFFÉRÉ (la réponse HTTP doit partir
// d'abord) et lancé en --no-block : systemd enregistre le job puis nous arrête/relance ;
// la clé E2E (.e2e_key) survit au restart, donc pas de ré-appairage et l'UI se reconnecte
// toute seule (boucle de reconnexion du flux). Renvoie (déclenché, message). Non-serveur
// (poste client, Windows) → (false, "").
func restartAfterUpdate() (bool, string) {
	// Windows : pas de superviseur pour nous relancer, on delegue a un
	// accompagnateur detache (voir sys_restart_windows.go). C'est aussi ce qui
	// cree la fenetre ou plus rien ne tient le dossier de donnees, donc ou la
	// migration peut aboutir.
	if runtime.GOOS == "windows" {
		return scheduleAppRestart()
	}
	if runtime.GOOS != "linux" || !uiServiceActive() {
		return false, ""
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // laisser la réponse HTTP atteindre le client
		// Le service tourne souvent en User=nathan (pas root) : systemctl brut
		// échouerait alors (polkit). On repli sur « sudo -n systemctl » comme
		// uiServiceCtl, les sudoers /etc/sudoers.d/loom-ui autorisant le restart.
		//
		// SURTOUT PAS de --no-block : la règle sudoers (« systemctl restart <unit> »)
		// matche les arguments À L'IDENTIQUE, donc « --no-block restart <unit> » n'est
		// PAS couvert → sudo exige un mot de passe → échec silencieux, le service ne
		// redémarrait jamais et l'utilisateur restait sur l'ancienne version tout en
		// voyant « installé ✓ ». Le restart bloquant est fiable : systemd tue tout le
		// cgroup (ce process ET le client systemctl) d'un coup, le job de restart est
		// déjà enregistré côté PID 1 et aboutit. C'est la MÊME invocation que le switch
		// de preset et « loom ui restart », qui eux marchaient déjà.
		bin, args := "systemctl", []string{"restart", uiServiceName()}
		if os.Geteuid() != 0 {
			bin, args = "sudo", append([]string{"-n", "systemctl"}, args...)
		}
		_ = exec.Command(bin, args...).Run()
	}()
	return true, "Service " + uiServiceName() + " redémarré automatiquement — la page va se reconnecter seule (le modèle n'est pas rechargé)."
}

func restartHintText() string {
	if runtime.GOOS == "windows" {
		return "Redémarre Loom (quitte puis relance) pour appliquer la mise à jour."
	}
	return "Redémarre pour appliquer : sudo systemctl restart " + uiServiceName() +
		" (ajoute " + serviceName() + " si la mise à jour touche le moteur)."
}

func printRestartHint() { fmt.Println(restartHintText()) }
