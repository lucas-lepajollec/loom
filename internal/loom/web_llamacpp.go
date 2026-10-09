// web_llamacpp.go — pilotage du backend llama.cpp depuis l'UI web :
// statut (commit, retard, binaire, plan de build), vérification des mises à
// jour (git fetch), et jobs asynchrones install / update / rebuild dont la
// progression et les logs sont pollés par le client.
//
//	GET  /api/llamacpp           → statut complet (dépôt, commit, binaire, plan)
//	POST /api/llamacpp/check     → git fetch + retard sur origin (réseau)
//	POST /api/llamacpp/install   → job d'installation {force?}
//	POST /api/llamacpp/update    → job de mise à jour {clean?}
//	GET  /api/llamacpp/job?from= → progression du job (phase, lignes, erreur)
package loom

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

// lcJob est LE job llama.cpp en cours (un seul à la fois). Les lignes de log
// sont indexées de façon absolue (base = index de Lines[0]) pour que le client
// puisse poller « à partir de N » même quand on tronque le début.
type lcJob struct {
	Action    string // "install" | "update"
	Phase     string
	Compiled  int // fichiers compilés (progression du build)
	Running   bool
	Err       string
	StartedAt int64
	EndedAt   int64
	OldCommit string
	NewCommit string
	lines     []string
	base      int
}

var (
	lcMu  sync.Mutex
	lcCur *lcJob
)

const lcMaxLines = 4000

// lcAppend ajoute une ligne au log du job courant et met à jour la phase / le
// compteur de fichiers compilés à partir du contenu (mêmes heuristiques que le
// spinner du CLI). Appelée par le sink de build (emitBuildLine).
func lcAppend(line string) {
	lcMu.Lock()
	defer lcMu.Unlock()
	if lcCur == nil {
		return
	}
	lcCur.lines = append(lcCur.lines, line)
	if len(lcCur.lines) > lcMaxLines {
		drop := len(lcCur.lines) - lcMaxLines
		lcCur.lines = append([]string(nil), lcCur.lines[drop:]...)
		lcCur.base += drop
	}
	if f := compiledFile(line); f != "" {
		lcCur.Compiled++
		lcCur.Phase = fmt.Sprintf("building… %d files", lcCur.Compiled)
	} else if p := phaseLabel(line); p != "" {
		lcCur.Phase = p
	}
	lcSaveLine(line)
	lcSave(false)
}

// lcPhase pose une phase explicite (étapes hors build : clone, fetch, service…)
// et la trace aussi dans le log.
func lcPhase(phase string) {
	lcMu.Lock()
	if lcCur != nil {
		lcCur.Phase = phase
		lcCur.lines = append(lcCur.lines, "▶ "+phase)
		lcSaveLine("▶ " + phase)
		lcSave(true)
	}
	lcMu.Unlock()
}

// startLcJob démarre un job (install ou update) si aucun n'est en cours.
func startLcJob(action string, run func()) error {
	return startLcJobContext(context.Background(), action, run)
}
func startLcJobContext(ctx context.Context, action string, run func()) error {
	subject := "node.install"
	if action == "update" || action == "prebuilt" && resolvedEngineBin() == prebuiltServerBin() && isFile(prebuiltServerBin()) {
		subject = "node.update"
	}
	if err := workspaceSessions.authorizePolicy(ctx, policy.Input{Subject: subject, MachineID: "local", Fallback: policy.Allow}, false); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lcMu.Lock()
	defer lcMu.Unlock()
	if lcCur != nil && lcCur.Running {
		return fmt.Errorf("a %s job is already in progress", lcCur.Action)
	}
	lcCur = &lcJob{Action: action, Running: true, Phase: "starting…", StartedAt: time.Now().Unix()}
	lcResetLog()
	lcSave(true)
	setBuildSink(lcAppend)
	go func() {
		defer func() {
			setBuildSink(nil)
			lcMu.Lock()
			lcCur.Running = false
			lcCur.EndedAt = time.Now().Unix()
			lcSave(true)
			lcMu.Unlock()
		}()
		run()
	}()
	return nil
}

func lcFail(err error) {
	lcMu.Lock()
	if lcCur != nil {
		lcCur.Err = err.Error()
		lcCur.Phase = "failed"
		lcCur.lines = append(lcCur.lines, "✗ "+err.Error())
		lcSaveLine("✗ " + err.Error())
		lcSave(true)
	}
	lcMu.Unlock()
}

func lcDone(msg string) {
	lcMu.Lock()
	if lcCur != nil {
		lcCur.Phase = msg
		lcCur.lines = append(lcCur.lines, "✓ "+msg)
		lcSaveLine("✓ " + msg)
		lcSave(true)
	}
	lcMu.Unlock()
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleLlamacpp renvoie le statut du moteur lié : kind server|full, BIN,
// dépôt git s'il y en a un, sondes, et le job éventuel.
func handleLlamacpp(w http.ResponseWriter, r *http.Request) {
	cfgBin := strings.TrimSpace(ReadConfig()["BIN"])
	kind := engineKind(cfgBin)
	repo := engineRepo(cfgBin)

	out := map[string]any{
		"kind":             kind,
		"config_bin":       cfgBin,
		"linked":           kind != "",
		"repo":             repo,
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
		"default_full_dir": defaultRepoDir(),
		"probes":           probedLlamaServers(),
		"backends_dir":     filepath.Join(LoomHome(), "backends"),
	}

	compiled := ""
	if repo != "" {
		compiled = llamaServerBin(repo)
	}
	out["bin"] = compiled
	out["installed"] = repo != "" && isDir(filepath.Join(repo, ".git"))
	out["in_use"] = compiled != "" && samePath(compiled, cfgBin)
	out["can_update"] = kind != ""
	out["update_kind"] = ""
	if kind == "full" {
		out["update_kind"] = "git"
	} else if kind == "server" {
		out["update_kind"] = "prebuilt"
	}

	if repo != "" && isDir(filepath.Join(repo, ".git")) {
		out["branch"] = gitOutput(repo, "rev-parse", "--abbrev-ref", "HEAD")
		out["commit"] = gitOutput(repo, "rev-parse", "--short", "HEAD")
		out["commit_date"] = gitOutput(repo, "log", "-1", "--format=%ci")
		out["commit_msg"] = gitOutput(repo, "log", "-1", "--format=%s")
		if br, _ := out["branch"].(string); br != "" && br != "HEAD" {
			if behind := gitOutput(repo, "rev-list", "--count", "HEAD..origin/"+br); behind != "" {
				n, _ := strconv.Atoi(behind)
				out["behind"] = n
			}
		}
	}

	plan := detectBuildPlan()
	out["plan"] = map[string]any{
		"backend": plan.backend,
		"arch":    plan.cudaArch,
		"jobs":    plan.jobs,
	}
	reco := recommendedMode(plan.backend)
	out["reco"] = reco
	if why, _ := reco["why"].(string); why != "" {
		out["server_gpu_note"] = why
	}

	pbTag, _ := prebuiltVersion()
	pbBin := prebuiltServerBin()
	out["prebuilt"] = map[string]any{
		"tag":    pbTag,
		"bin":    pbBin,
		"dir":    prebuiltDir(),
		"in_use": pbBin != "" && prebuiltOwns(cfgBin),
	}
	out["job"] = lcJobSnapshot(0, false)
	sendJSON(w, 200, out)
}

// samePath compare deux chemins en neutralisant séparateurs, symlinks et casse
// (Windows).
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	norm := func(p string) string {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		p = filepath.Clean(p)
		return strings.ToLower(filepath.ToSlash(p))
	}
	return norm(a) == norm(b)
}

// handleLlamacppCheck fait un vrai git fetch puis renvoie le retard sur origin
// et le dernier commit distant. Synchrone (quelques secondes réseau).
func handleLlamacppCheck(w http.ResponseWriter, r *http.Request) {
	cfgBin := strings.TrimSpace(ReadConfig()["BIN"])
	repo := engineRepo(cfgBin)
	if repo == "" {
		repo = llamacppRepoDir()
	}
	if !isDir(filepath.Join(repo, ".git")) {
		sendJSON(w, 200, map[string]any{"ok": false, "error": "no linked llama.cpp repository — update the official binary, or add llama.cpp"})
		return
	}
	branch := gitOutput(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" || branch == "HEAD" {
		branch = "master"
	}
	if err := runStep("git fetch", repo, "git", "fetch", "origin", "--quiet"); err != nil {
		sendJSON(w, 200, map[string]any{"ok": false, "error": "git fetch failed: " + err.Error()})
		return
	}
	behind := 0
	if b := gitOutput(repo, "rev-list", "--count", "HEAD..origin/"+branch); b != "" {
		behind, _ = strconv.Atoi(b)
	}
	sendJSON(w, 200, map[string]any{
		"ok":            true,
		"behind":        behind,
		"local":         gitOutput(repo, "rev-parse", "--short", "HEAD"),
		"remote":        gitOutput(repo, "rev-parse", "--short", "origin/"+branch),
		"remote_date":   gitOutput(repo, "log", "-1", "--format=%ci", "origin/"+branch),
		"remote_msg":    gitOutput(repo, "log", "-1", "--format=%s", "origin/"+branch),
		"branch":        branch,
		"has_binary":    llamaServerBin(repo) != "",
		"needs_rebuild": behind > 0 || llamaServerBin(repo) == "",
	})
}

// handleLlamacppInstall lance le job d'installation (clone + build + BIN).
func handleLlamacppInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Force bool   `json:"force"`
		Dir   string `json:"dir"`
	}
	if !legacyControlDecode(w, r, &req) {
		return
	}
	if err := startLcJobContext(r.Context(), "install", func() { lcRunInstall(req.Force, req.Dir) }); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleLlamacppInstallCustom lance un job d'installation d'un backend CUSTOM
// (fork llama.cpp) depuis une URL de dépôt Git : cloné dans backends/<name> et
// compilé pour la machine, SANS toucher au BIN global. Il apparaît ensuite dans
// /api/backends et se choisit par modèle (éditeur de preset → Moteur).
func handleLlamacppInstallCustom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
		Name string `json:"name"`
		Ref  string `json:"ref"`
	}
	if !legacyControlDecode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Repo) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "repository URL required"})
		return
	}
	if err := startLcJobContext(r.Context(), "custom", func() { lcRunCustomInstall(req.Repo, req.Name, req.Ref) }); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// lcRunCustomInstall : corps du job d'install d'un backend custom (sans bascule
// de BIN — c'est volontaire, il se rattache par preset).
func lcRunCustomInstall(url, name, ref string) {
	bin, err := installCustomBackend(url, name, ref, lcPhase)
	if err != nil {
		lcFail(err)
		return
	}
	lcAppend("binary compiled: " + bin)
	lcAppend("→ to use it: edit a model → Engine section → “detected backend” and select it.")
	lcDone("custom backend installed")
}

// handleLlamacppUpdate lance le job de mise à jour (pull + rebuild + restart).
// {clean:true} force une recompilation from scratch même sans nouveau commit.
func handleLlamacppUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Clean bool `json:"clean"`
	}
	if !legacyControlDecode(w, r, &req) {
		return
	}
	if err := startLcJobContext(r.Context(), "update", func() { lcRunUpdate(req.Clean) }); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handleLlamacppPrebuiltCheck interroge la dernière release officielle de
// llama.cpp et la compare à la version précompilée installée. Synchrone.
func handleLlamacppPrebuiltCheck(w http.ResponseWriter, r *http.Request) {
	tag, assets, err := fetchLlamaLatest()
	if err != nil {
		sendJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	main, _, label, _, err := pickPrebuilt(assets)
	if err != nil {
		sendJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cur, _ := prebuiltVersion()
	sendJSON(w, 200, map[string]any{
		"ok":      true,
		"latest":  tag,
		"current": cur,
		"variant": label,
		"size_mb": main.Size / 1_000_000,
		"update":  cur != tag || prebuiltServerBin() == "",
	})
}

// handleLlamacppPrebuilt lance le job de téléchargement / mise à jour des
// binaires précompilés officiels (pas de compilation).
func handleLlamacppPrebuilt(w http.ResponseWriter, r *http.Request) {
	if err := startLcJobContext(r.Context(), "prebuilt", lcRunPrebuilt); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// lcRunPrebuilt : télécharge les binaires officiels, pointe BIN dessus, en
// stoppant le service pendant le remplacement (binaire verrouillé en cours
// d'exécution) puis en le relançant.
func lcRunPrebuilt() {
	svcWasUp := serviceIsActive()
	if svcWasUp {
		lcPhase("stopping service during installation…")
		if err := serviceAction("stop"); err != nil {
			lcAppend("[warn] could not stop service: " + err.Error())
		}
	}
	bin, err := prebuiltInstall(lcAppend, lcPhase)
	if err == nil {
		if serr := SetConfigKey("BIN", bin); serr != nil {
			err = fmt.Errorf("binaries installed but failed to write BIN: %w", serr)
		} else {
			lcAppend("BIN updated")
			if models := adoptModelsDirNearBin(bin); models != "" {
				lcAppend("models : " + models)
			}
		}
	}
	if svcWasUp {
		lcPhase("restarting service…")
		if serr := serviceAction("start"); serr != nil {
			lcAppend("[warn] service restart failed: " + serr.Error())
		}
	}
	if err != nil {
		lcFail(err)
		return
	}
	tag, _ := prebuiltVersion()
	lcDone("prebuilt binaries installed (" + tag + ")")
}

// handleLlamacppUse bascule BIN entre deux versions DÉJÀ installées, sans
// rien recompiler : "fast" = binaires précompilés, "opt" = build local. Le
// service redémarre pour prendre le nouveau binaire. L'UI n'appelle ceci que
// quand la version cible existe déjà (sinon elle lance un job d'installation).
func handleLlamacppUse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
		Bin  string `json:"bin"`
	}
	if !legacyControlDecode(w, r, &req) {
		return
	}
	var bin string
	switch req.Mode {
	case "fast":
		bin = prebuiltServerBin()
	case "opt":
		bin = llamaServerBin(llamacppRepoDir())
	case "exist":
		bin = strings.TrimSpace(req.Bin)
		if !filepath.IsAbs(bin) {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "absolute path required"})
			return
		}
		if base := strings.ToLower(filepath.Base(bin)); base != "llama-server" && base != "llama-server.exe" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "this path is not a llama-server"})
			return
		}
		if !isFile(bin) {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "file not found: " + bin})
			return
		}
	default:
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown mode"})
		return
	}
	if bin == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "this version is not installed"})
		return
	}
	if err := SetConfigKey("BIN", bin); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := ensureLoomEngineBind(); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	modelsDir := adoptModelsDirNearBin(bin)
	if serviceIsActive() {
		_ = serviceAction("restart")
	}
	sendJSON(w, 200, map[string]any{"ok": true, "bin": bin, "models_dir": modelsDir})
}

// handleLlamacppJob renvoie l'état du job courant + les lignes de log depuis
// l'offset absolu ?from=N (le client mémorise `next` et enchaîne).
func handleLlamacppJob(w http.ResponseWriter, r *http.Request) {
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	sendJSON(w, 200, lcJobSnapshot(from, true))
}

// handleLlamacppJobDismiss (POST) efface un job TERMINÉ pour que son résultat
// (surtout une erreur en rouge) cesse d'être réaffiché à chaque démarrage. Un
// job en cours n'est pas effaçable — la réponse le dit.
func handleLlamacppJobDismiss(w http.ResponseWriter, r *http.Request) {
	if lcDismiss() {
		sendJSON(w, 200, map[string]any{"ok": true})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": false, "error": "operation in progress — cannot hide"})
}

// lcJobSnapshot construit la vue JSON du job courant. withLines=false renvoie
// juste l'entête (imbriquée dans /api/llamacpp).
func lcJobSnapshot(from int, withLines bool) map[string]any {
	lcMu.Lock()
	defer lcMu.Unlock()
	if lcCur == nil {
		return map[string]any{"exists": false}
	}
	j := lcCur
	out := map[string]any{
		"exists":     true,
		"action":     j.Action,
		"running":    j.Running,
		"phase":      j.Phase,
		"compiled":   j.Compiled,
		"error":      j.Err,
		"started_at": j.StartedAt,
		"ended_at":   j.EndedAt,
		"old":        j.OldCommit,
		"new":        j.NewCommit,
	}
	if withLines {
		start := from - j.base
		if start < 0 {
			start = 0
		}
		if start > len(j.lines) {
			start = len(j.lines)
		}
		out["lines"] = append([]string(nil), j.lines[start:]...)
		out["next"] = j.base + len(j.lines)
	}
	return out
}

// ---------------------------------------------------------------------------
// Corps des jobs (miroir web de llamacppInstall / llamacppUpdate, sans stdout
// interactif : tout passe par lcPhase / le sink de build)
// ---------------------------------------------------------------------------

func lcRunInstall(force bool, dir string) {
	repo, err := resolveInstallDir(dir)
	if err != nil {
		lcFail(err)
		return
	}

	lcPhase("checking tools (git, cmake, compiler)…")
	if err := requireTools("git", "cmake"); err != nil {
		lcFail(err)
		return
	}
	if err := ensureCompiler(); err != nil {
		lcFail(err)
		return
	}
	ensureAccelerator()

	if isDir(filepath.Join(repo, ".git")) {
		if !force {
			// Dépôt déjà là : on bascule sur une mise à jour (même intention).
			lcPhase("repository already present — switching to update")
			lcRunUpdate(false)
			return
		}
		lcPhase("removing existing repository (--force)…")
		if err := os.RemoveAll(repo); err != nil {
			lcFail(err)
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		lcFail(err)
		return
	}

	lcPhase("cloning llama.cpp…")
	if err := runStep("git clone", "", "git", "clone", "--depth=1", llamacppRepoURL, repo); err != nil {
		lcFail(fmt.Errorf("git clone failed: %w", err))
		return
	}

	if !lcBuildAndSwitch(repo, true) {
		return
	}
	lcDone("installation complete")
}

func lcRunUpdate(clean bool) {
	lcPhase("checking tools (git, cmake, compiler)…")
	if err := requireTools("git", "cmake"); err != nil {
		lcFail(err)
		return
	}
	if err := ensureCompiler(); err != nil {
		lcFail(err)
		return
	}
	ensureAccelerator()

	repo := engineRepo(strings.TrimSpace(ReadConfig()["BIN"]))
	if repo == "" {
		repo = llamacppRepoDir()
	}
	if !isDir(filepath.Join(repo, ".git")) {
		lcFail(fmt.Errorf("no linked llama.cpp repository (%s) — add llama.cpp, or update the official binary", repo))
		return
	}

	oldCommit := gitOutput(repo, "rev-parse", "--short", "HEAD")
	lcMu.Lock()
	lcCur.OldCommit = oldCommit
	lcMu.Unlock()

	branch := gitOutput(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" || branch == "HEAD" {
		branch = "master"
	}

	lcPhase("git fetch origin…")
	if err := runStep("git fetch", repo, "git", "fetch", "origin", "--quiet"); err != nil {
		lcFail(fmt.Errorf("git fetch failed: %w", err))
		return
	}
	localRev := gitOutput(repo, "rev-parse", "HEAD")
	remoteRev := gitOutput(repo, "rev-parse", "origin/"+branch)
	if localRev != "" && localRev == remoteRev && !clean && llamaServerBin(repo) != "" {
		lcDone("already up to date (" + oldCommit + ") — nothing to do")
		return
	}
	if localRev != remoteRev {
		lcPhase("git pull origin/" + branch + "…")
		if err := runStep("git pull --ff-only", repo, "git", "pull", "--ff-only", "origin", branch); err != nil {
			lcFail(fmt.Errorf("git pull failed (local changes?): %w", err))
			return
		}
	}
	newCommit := gitOutput(repo, "rev-parse", "--short", "HEAD")
	lcMu.Lock()
	lcCur.NewCommit = newCommit
	lcMu.Unlock()

	// Le binaire en cours d'exécution ne peut pas être réécrit → stop du service
	// pendant le build, redémarrage après (même en échec).
	svcWasUp := serviceIsActive()
	if svcWasUp {
		lcPhase("stopping service during build…")
		if err := serviceAction("stop"); err != nil {
			lcAppend("[warn] could not stop service: " + err.Error())
		}
	}

	ok := lcBuildAndSwitch(repo, clean)
	if svcWasUp {
		lcPhase("restarting service…")
		if err := serviceAction("start"); err != nil {
			lcAppend("[warn] service restart failed: " + err.Error())
		}
	}
	if !ok {
		return
	}
	if oldCommit == newCommit {
		lcDone("recompiled (" + newCommit + ")")
	} else {
		lcDone("updated: " + oldCommit + " → " + newCommit)
	}
}

// lcBuildAndSwitch détecte le plan, compile llama-server et pointe BIN dessus.
// Renvoie false (job en échec) si une étape casse.
func lcBuildAndSwitch(repo string, clean bool) bool {
	plan := detectBuildPlan()
	lcAppend(fmt.Sprintf("build plan: backend=%s arch=%s jobs=%d", plan.backend, plan.cudaArch, plan.jobs))
	lcPhase("configuring CMake…")
	if err := buildLlamacpp(repo, plan, clean); err != nil {
		lcAppendLogTail(filepath.Join(repo, "configure.log"), filepath.Join(repo, "build.log"))
		lcFail(err)
		return false
	}
	bin := llamaServerBin(repo)
	if bin == "" {
		lcFail(fmt.Errorf("build complete but binary not found under %s", filepath.Join(repo, "build")))
		return false
	}
	lcAppend("binary compiled: " + bin)
	if err := SetConfigKey("BIN", bin); err != nil {
		lcFail(fmt.Errorf("build succeeded but failed to write BIN: %w", err))
		return false
	}
	lcAppend("BIN updated")
	if models := adoptModelsDirNearBin(bin); models != "" {
		lcAppend("models : " + models)
	}
	return true
}

// lcAppendLogTail remonte la fin des logs de build dans le job pour que
// l'erreur réelle soit visible dans l'UI sans aller chercher les fichiers.
func lcAppendLogTail(paths ...string) {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		lcAppend("--- end of " + filepath.Base(p) + " ---")
		for _, l := range lines {
			lcAppend(l)
		}
	}
}
