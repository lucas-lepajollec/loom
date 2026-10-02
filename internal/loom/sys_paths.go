package loom

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// « Où sont mes fichiers ? » — la question que personne ne devrait avoir à poser.
// Un binaire unique qui s'auto-installe au premier lancement rend les
// emplacements invisibles : on ne sait plus si le .exe qu'on a double-cliqué EST
// l'application ou seulement un installateur, ni où atterrissent la config, les
// modèles et les fichiers que l'agent écrit. On centralise donc la réponse ici,
// et on l'expose à la fois en ligne de commande (`loom where`) et dans l'UI.

type loomPaths struct {
	Home      string `json:"home"`      // LOOM_HOME : racine des données
	Database  string `json:"database"`  // loom.db : config, préférences, conversation, clés
	Exe       string `json:"exe"`       // binaire en cours d'exécution
	Installed string `json:"installed"` // binaire installé (peut différer de Exe)
	Workspace string `json:"workspace"` // dossier de travail du mode agent (jetable)
	Scripts   string `json:"scripts"`   // dossier des scripts de l'agent (protégé)
	Memory    string `json:"memory"`
	Presets   string `json:"presets"`
	Models    string `json:"models"`
	Backends  string `json:"backends"`
}

func currentPaths() loomPaths {
	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return loomPaths{
		Home:      LoomHome(),
		Database:  dbPath(),
		Exe:       exe,
		Installed: installedExePath(),
		Workspace: agentWorkspace(),
		Scripts:   scriptsDir(),
		Memory:    memoryDir(),
		Presets:   presetsDir(),
		Models:    modelsDir(),
		Backends:  backendsDir(),
	}
}

func cmdWhere(args []string) error {
	p := currentPaths()
	fmt.Printf("Loom locations\n\n")
	for _, row := range [][2]string{
		{"data (LOOM_HOME)", p.Home},
		{"database", p.Database},
		{"running binary", p.Exe},
		{"installed binary", p.Installed},
		{"agent workspace", p.Workspace},
		{"scripts", p.Scripts},
		{"memory", p.Memory},
		{"presets", p.Presets},
		{"models", p.Models},
		{"backends", p.Backends},
	} {
		fmt.Printf("  %-20s %s\n", row[0], row[1])
	}
	if p.Exe != p.Installed {
		fmt.Printf("\n%s you are running a copy that is NOT the installed binary.\n", dim("[info]"))
		fmt.Printf("  Updates from the application only modify the running copy.\n")
	}
	return nil
}

// handlePaths (GET /api/paths) alimente le bloc « Emplacements » de l'UI.
func handlePaths(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, 200, currentPaths())
}
