package platform

import (
	"path/filepath"
	"runtime"
)

// installedExePath est l'emplacement canonique du binaire après installation.
// Il diffère par plateforme : sous Unix les installateurs posent /usr/local/bin/loom
// (c'est ce que référencent les unités systemd et le plist launchd) ; sous Windows,
// faute d'équivalent, on utilise LOOM_HOME\bin ajouté au PATH utilisateur.
func InstalledExePath(binDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(binDir, "loom.exe")
	}
	return "/usr/local/bin/loom"
}
