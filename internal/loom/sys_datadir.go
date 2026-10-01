package loom

import (
	"os"
)

// sys_datadir.go — création du dossier de données, identique sur les trois
// plateformes. Avant, chaque installateur (Linux, macOS, Windows, plus le
// premier lancement de l'app) créait ses dossiers et écrivait son propre
// modèle de config.env : quatre copies qui divergeaient à la première
// modification. Il n'y en a plus qu'une.

// dataDirs est l'arborescence complète de $LOOM_HOME. Rien d'autre n'y est
// créé : tout le reste vit dans loom.db.
func dataDirs() []string {
	return []string{LoomHome(), backendsDir(), binDir(), presetsDir(), memoryDir(), modelsDir(), workspaceDir(), scriptsDir()}
}

// provisionDataDir crée l'arborescence et, sur une installation neuve, pose la
// configuration de départ. Idempotente : une configuration existante n'est
// jamais écrasée.
func provisionDataDir() error {
	for _, d := range dataDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if len(ReadConfig()) > 0 {
		return nil
	}
	return WriteConfig(defaultConfig())
}
