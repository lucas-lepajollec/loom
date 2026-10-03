// loom — single-binary LLM server manager + web UI for llama.cpp deployments.
// Point d'entrée réel : Main(), appelé par cmd/loom (les métadonnées Windows
// .syso/go:generate vivent là-bas, dans le dossier du package main).
package loom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Version = "0.2.0"

// Main est le vrai main() du binaire (cmd/loom ne fait que l'appeler).
func Main() {
	// Privileged updater never loads local dotenv, user data or harness state.
	if len(os.Args) > 1 && os.Args[1] == "system-update" {
		mustExit(cmdSystemUpdate(os.Args[2:]))
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "node" {
		if err := cmdNode(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "[err]", err)
			os.Exit(1)
		}
		return
	}

	// Rattache la console du terminal parent si on est lancé depuis un shell
	// (Windows : binaire GUI). Retourne false au double-clic (aucune console) →
	// on bascule alors sur l'expérience « application ». Hors Windows : toujours
	// true. À faire AVANT toute écriture.
	haveConsole := setupConsole()

	// Charge la configuration locale de dev (.env.local, .env) si présente.
	loadDotEnv()

	// Nettoie un éventuel binaire .old laissé par une mise à jour Windows.
	cleanupOldBinary()

	// ACP bridge for Antigravity, launched by Loom as a harness process.
	if len(os.Args) > 1 && os.Args[1] == "agy-acp" {
		runAgyACP(os.Stdin, os.Stdout)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "loom-fake-acp" {
		if os.Getenv("LOOM_DEV_FAKE_ACP") != "1" {
			return
		}
		runFakeACP(os.Stdin, os.Stdout)
		return
	}
	// Run as root (sudo loom …): never leave the user's data owned by root.
	adoptUserLoomHome()
	workspaceSessions.installACPShutdown()
	defer workspaceSessions.shutdownACP()
	args := os.Args[1:]
	noArgs := len(args) == 0
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	// Double-clic sur le binaire (aucun argument, aucune console rattachée) → on
	// lance l'expérience « application » (UI web + navigateur + icône tray) plutôt
	// que d'afficher l'aide. Le binaire étant compilé en sous-système GUI, il n'y a
	// AUCUNE console à ce stade (donc plus de fenêtre noire). Lancé depuis un shell,
	// `loom` sans argument garde son comportement d'aide.
	if noArgs && !haveConsole {
		mustExit(cmdApp(args))
		return
	}
	switch cmd {
	case "app":
		mustExit(cmdApp(args))
	case "start", "restart":
		// Un moteur sans BIN ni MODEL démarre, meurt, et systemd le relance en
		// boucle : l'utilisateur ne voyait qu'un « activating » rassurant et un
		// /health muet. On le dit AVANT de lancer le service.
		mustExit(preflightEngine())
		mustExit(serviceAction(cmd))
	case "stop", "status", "enable", "disable":
		mustExit(serviceAction(cmd))
	case "logs":
		mustExit(serviceLogs())
	case "edit":
		mustExit(editConfig())
	case "set-api-key":
		mustExit(cmdSetAPIKey(args))
	case "set-web-key":
		mustExit(cmdSetWebKey(args))
	case "password":
		mustExit(cmdPassword(args))
	case "vram":
		mustExit(showVram())
	case "gpu":
		mustExit(cmdGPU(args))
	case "network":
		mustExit(cmdNetwork(args))
	case "switch":
		mustExit(cmdSwitch(args))
	case "chat":
		mustExit(cmdChat(args))
	case "export":
		mustExit(cmdExport(args))
	case "web":
		mustExit(cmdWeb(args))
	case "ui":
		mustExit(cmdUI(args))
	case "agent":
		mustExit(cmdAgent(args))
	case "internet":
		mustExit(cmdInternet(args))
	case "memory":
		mustExit(cmdMemory(args))
	case "serve":
		mustExit(cmdServe(args))
	case "test":
		mustExit(cmdTest(args))
	case "bench":
		mustExit(cmdBench(args))
	case "llamacpp":
		mustExit(cmdLlamacpp(args))
	case "update":
		mustExit(cmdUpdate(args))
	case "where":
		mustExit(cmdWhere(args))
	case restartArg:
		// Sous-commande interne, absente de l'aide : l'accompagnateur détaché qui
		// attend la fermeture de l'app puis la relance.
		mustExit(cmdRestartAfterUpdate(args))
	case "install":
		mustExit(cmdInstall(args))
	case "uninstall":
		mustExit(cmdUninstall(args))
	case "version", "-v", "--version":
		fmt.Println("loom", Version)
	case "help", "-h", "--help", "":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printHelp()
		os.Exit(2)
	}
}

func printHelp() {
	fmt.Printf(`loom %s — llama.cpp control plane + web UI

Usage: loom <command> [args]

Loom runs as two services: loom-engine (the model) and loom-ui
(the web interface and OpenAI /v1 proxy).

Engine (loom-engine):
  start | stop | restart        manage the service
  status | logs                 status / live logs
  enable | disable              automatic startup at boot
  edit                          edit configuration in $EDITOR
  switch [N]                    activate a preset from presets/ (interactive or by number)
  test | bench [N]              check that the AI responds / measure prefill + decode tok/s
  vram                          GPU/VRAM usage (nvidia-smi)
  gpu [index…]                  list GPUs / select which to use (gpu all = all)
  set-api-key [key]              protect llama-server (Bearer key); omitted = generate, "" = remove
  network [on|off|status]        make the OpenAI endpoint reachable from the local network
                                (HOST + firewall rule on Windows)

Engine node (Linux GPU machine):
  node init|serve|install|update engine-only API and optional user service (see docs/engine-node.md)

Interface (loom-ui):
  ui [start|stop|restart|status]  manage the interface service
  web [PORT]                    serve the interface in the foreground (default :2510)
  password [--stdin]            set/reset the access password; revokes browser sessions
  set-web-key [key]              protect the control API; omitted = generate, "" = remove

Interaction:
  chat [system-prompt]           streaming terminal chat
  export [options] [file]        export the web interface conversation
                                (Markdown by default, “-” = standard output)
                                --json  --last N  --no-reasoning
                                --no-tools  --no-results
  memory [off|ondemand|status]   persistent memory (off by default; no auto mode)
  internet [on|off|status|engine <go|crawl4ai>|url <url>|key <key>]
                                AI web access (built-in engine or Crawl4AI server)

llama.cpp backend:
  llamacpp install               clone + compile llama.cpp (CUDA/ROCm/Metal/CPU), point BIN to it
  llamacpp update                git pull + rebuild the existing backend
  llamacpp status                current commit, detected backend, lag behind origin

Installation:
  where                         show binary, database and directory locations
  install | uninstall           install / uninstall
  update [--check]               update from GitHub releases
  serve                         service entrypoint: exec llama-server (internal use)

Env:
  LOOM_HOME    data root (default: ~/.local/share/loom)
  EDITOR       editor for 'edit' (default: nano, notepad on Windows)
`, Version)
}

func mustExit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "[err]", err)
		workspaceSessions.shutdownACP()
		os.Exit(1)
	}
}

// LoomHome resolves the data directory.
// Precedence: $LOOM_HOME → /etc/default/loom → XDG / platform default.
func LoomHome() string {
	if h := os.Getenv("LOOM_HOME"); h != "" {
		return h
	}
	if h := readEtcDefault(); h != "" {
		return h
	}
	return defaultLoomHome()
}

// readEtcDefault parses /etc/default/loom for LOOM_HOME.
func readEtcDefault() string {
	b, err := os.ReadFile("/etc/default/loom")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		if k, v, ok := strings.Cut(s, "="); ok && strings.TrimSpace(k) == "LOOM_HOME" {
			return strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	return ""
}

// Arborescence de $LOOM_HOME. Elle tient en six dossiers, et rien d'autre :
// tout le reste (config, préférences, conversation, clés, drapeaux) vit dans la
// base loom.db — voir store.go.
func backendsDir() string  { return filepath.Join(LoomHome(), "backends") }
func binDir() string       { return filepath.Join(LoomHome(), "bin") }
func presetsDir() string   { return filepath.Join(LoomHome(), "presets") }
func memoryDir() string    { return filepath.Join(LoomHome(), "memory") }
func modelsDir() string    { return filepath.Join(LoomHome(), "models") }
func workspaceDir() string { return filepath.Join(LoomHome(), "workspace") }

// scriptsDir est le dossier des scripts de l'agent : à côté de memory/presets,
// HORS du workspace. Le workspace est un bac à sable jetable (clones, tests) qu'un
// nettoyage peut raser ; les scripts qu'on veut CONSERVER vivent ici, protégés par
// guardDestructive (voir chat_scripts.go) contre toute suppression récursive.
func scriptsDir() string { return filepath.Join(LoomHome(), "scripts") }

// serviceName est le nom de l'unité qui exécute llama-server.
func serviceName() string {
	if n := os.Getenv("LOOM_SERVICE"); n != "" {
		return n
	}
	return "loom-engine"
}

// Color helpers (ANSI). Disabled when stdout is not a TTY.
var colorOn = isTerminal()

func col(code, s string) string {
	if !colorOn {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
func bold(s string) string    { return col("1", s) }
func cyan(s string) string    { return col("1;36", s) }
func green(s string) string   { return col("32", s) }
func red(s string) string     { return col("31", s) }
func dim(s string) string     { return col("2", s) }
func yellow(s string) string  { return col("33", s) }
func magenta(s string) string { return col("35", s) }

// loadDotEnv charge les variables d'un fichier .env.local ou .env local en dev.
// Ces fichiers sont ignorés par git et ne quittent jamais la machine locale.
func loadDotEnv() {
	for _, f := range []string{".env.local", ".env"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for k, v := range parseEnv(string(b)) {
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}
