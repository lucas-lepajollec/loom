package loom

import (
	"context"
	"fmt"
	"golang.org/x/mod/semver"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"time"
)

// updateInfo décrit l'état de mise à jour (partagé CLI + UI web).
type updateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	URL         string `json:"url"`
	CanApply    bool   `json:"can_apply"`
	ApplyReason string `json:"apply_reason,omitempty"`
}

// checkForUpdate interroge GitHub et compare à la version courante. Réutilisé
// par `loom update` (CLI) et par l'endpoint web /api/update.
func checkForUpdate() (updateInfo, error) {
	can, reason := updateCapability()
	info := updateInfo{Current: Version, CanApply: can, ApplyReason: reason}
	rel, err := fetchLatestRelease()
	if err != nil {
		return info, err
	}
	latest := ensureV(rel.TagName)
	if !semver.IsValid(latest) {
		return info, fmt.Errorf("unexpected release tag: %q", rel.TagName)
	}
	info.Latest = strings.TrimPrefix(latest, "v")
	info.URL = rel.HTMLURL
	info.Available = semver.Compare(latest, ensureV(Version)) > 0
	return info, nil
}

// applyUpdate télécharge et installe le binaire de la dernière release pour
// l'OS/arch courant. Renvoie la nouvelle version. Ne redémarre AUCUN service
// (voir printRestartHint / le message renvoyé à l'UI).
var loomUpdateMu sync.Mutex
var loomInstalledUpdate string // guarded by loomUpdateMu, until this process restarts

func applyUpdate() (string, error) { return applyUpdateVersion("") }

func applyUpdateVersion(expected string) (string, error) {
	if !loomUpdateMu.TryLock() {
		return "", fmt.Errorf("an update is already running")
	}
	defer loomUpdateMu.Unlock()
	if loomInstalledUpdate != "" {
		return "", fmt.Errorf("version %s is installed; restart Loom before updating again", loomInstalledUpdate)
	}
	exe, err := updateExecutable()
	if err != nil {
		return "", err
	}
	if err := checkUpdateWritable(exe); err != nil {
		if !isEngineWorker() && canUseSystemUpdater(exe) {
			version, err := runSystemUpdater(expected)
			if err == nil {
				loomInstalledUpdate = version
			}
			return version, err
		}
		return "", err
	}
	rel, err := fetchLatestRelease()
	if err != nil {
		return "", fmt.Errorf("could not contact GitHub: %w", err)
	}
	if expected != "" && strings.TrimPrefix(ensureV(rel.TagName), "v") != expected {
		return "", fmt.Errorf("release changed; check again before updating")
	}
	version, err := installReleaseUpdate(rel, exe)
	if err == nil {
		loomInstalledUpdate = version
	}
	return version, err
}

func updateExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	return exe, nil
}

func updateCapability() (bool, string) {
	exe, err := updateExecutable()
	if err != nil {
		return false, err.Error()
	}
	if err := checkUpdateWritable(exe); err != nil {
		if !isEngineWorker() && canUseSystemUpdater(exe) {
			return true, ""
		}
		if isEngineWorker() {
			return false, "Node updates need a user-owned binary in a writable directory; reinstall with install.sh --node or use a separate user-owned copy."
		}
		return false, "System installation needs one-time administrator setup: sudo loom install. Portable installations need a writable binary directory."
	}
	return true, ""
}

// The caller chooses the executable locally, never from an HTTP request.
// The privileged path passes only the fixed, root-owned installed binary.
func installReleaseUpdate(rel *ghRelease, exe string) (string, error) {
	latest := ensureV(rel.TagName)
	if !semver.IsValid(latest) {
		return "", fmt.Errorf("unexpected release tag: %q", rel.TagName)
	}
	if semver.Compare(latest, ensureV(Version)) <= 0 {
		return Version, fmt.Errorf("already up to date (%s)", Version)
	}
	want, url, size := pickAsset(rel)
	if url == "" {
		return "", fmt.Errorf("no %s binary in release %s (os/arch %s/%s)", updateAssetName(), latest, runtime.GOOS, runtime.GOARCH)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(exe), ".loom-update-*")
	if err != nil {
		return "", err
	}
	tmp := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	defer os.Remove(tmp)
	if err := downloadTo(url, tmp); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if err := verifyChecksum(rel, want, tmp); err != nil {
		return "", err
	}
	if got := fileSize(tmp); size > 0 && got != size {
		return "", fmt.Errorf("unexpected size (%d bytes received, %d expected) — update cancelled", got, size)
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm() & 0o755
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return "", err
	}
	if isEngineWorker() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		capabilities, err := exec.CommandContext(ctx, tmp, "node", "capabilities").Output()
		if err != nil || strings.TrimSpace(string(capabilities)) != "engine-node-v1" {
			return "", fmt.Errorf("release does not support this engine node; update cancelled")
		}
	}
	// A separate copy keeps the running inode intact and preserves rollback.
	if err := backupUpdateBinary(exe); err != nil {
		return "", fmt.Errorf("backup before update: %w", err)
	}
	if err := replaceBinary(exe, tmp); err != nil {
		return "", err
	}
	return strings.TrimPrefix(latest, "v"), nil
}

func backupUpdateBinary(exe string) error {
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".loom-backup-*")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return os.Rename(path, exe+".previous")
}

func cmdUpdate(args []string) error {
	checkOnly := false
	for _, a := range args {
		if a == "--check" || a == "-check" || a == "check" {
			checkOnly = true
		}
	}
	fmt.Println("looking for the latest version…")
	info, err := checkForUpdate()
	if err != nil {
		return fmt.Errorf("could not contact GitHub: %w", err)
	}
	if !info.Available {
		fmt.Printf("loom is already up to date (%s).\n", Version)
		return nil
	}
	fmt.Printf("new version available: %s  (current: %s)\n", info.Latest, Version)
	fmt.Printf("  %s\n", info.URL)
	if checkOnly {
		fmt.Println("run 'loom update' to install it.")
		return nil
	}
	fmt.Printf("downloading %s…\n", updateAssetName())
	newVer, err := applyUpdate()
	if err != nil {
		return err
	}
	fmt.Printf("✓ loom updated to %s\n", newVer)
	printRestartHint()
	return nil
}

// handleUpdateCheck (GET /api/update) : renvoie l'état de mise à jour pour l'UI.
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	info, err := checkForUpdate()
	if err != nil {
		sendJSON(w, 502, map[string]any{"current": Version, "available": false, "error": "Could not check GitHub releases: " + err.Error()})
		return
	}
	sendJSON(w, 200, info)
}

// handleUpdateApply (POST /api/update/apply) : télécharge et installe la dernière
// version. Sur un serveur Linux/systemd, relance ensuite AUTOMATIQUEMENT le service
// d'UI (loom-ui : il sert l'UI, le chat et le tunnel) pour appliquer la MAJ sans
// avoir à SSH — voir restartAfterUpdate. Renvoie la version + le message de statut.
func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	// Recheck that the release shown to the user is still available.
	info, err := checkForUpdate()
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.Version == "" || req.Version != info.Latest || !info.Available {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "Release changed or already installed; check again before updating."})
		return
	}
	newVer, err := applyUpdateVersion(req.Version)
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
// d'abord) : systemd enregistre le job puis nous arrête/relance ;
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
	if isEngineWorker() {
		if runtime.GOOS != "linux" || exec.Command("systemctl", "--user", "is-active", "--quiet", "loom-node").Run() != nil {
			return false, ""
		}
		go func() {
			time.Sleep(1500 * time.Millisecond)
			_ = exec.Command("systemctl", "--user", "restart", "loom-node").Run()
		}()
		return true, "Engine node restart scheduled; its owned engines will stop."
	}
	if runtime.GOOS != "linux" || !uiServiceActive() {
		return false, ""
	}
	if os.Geteuid() != 0 && exec.Command("sudo", "-n", "-l", "systemctl", "restart", uiServiceName()).Run() != nil {
		return false, ""
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // laisser la réponse HTTP atteindre le client
		// Le service tourne sous un utilisateur non privilégié (pas root) : systemctl brut
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
	return true, "Service " + uiServiceName() + " restart scheduled — the page will verify the new version before reloading."
}

func restartHintText() string {
	if isEngineWorker() {
		return "Restart the node: systemctl --user restart loom-node (or stop and start node serve). This stops its owned engines."
	}
	if runtime.GOOS == "windows" {
		return "Restart Loom (quit and reopen) to apply the update."
	}
	return "Restart to apply: sudo systemctl restart " + uiServiceName() +
		" (ajoute " + serviceName() + " if the update affects the engine)."
}

func printRestartHint() { fmt.Println(restartHintText()) }
