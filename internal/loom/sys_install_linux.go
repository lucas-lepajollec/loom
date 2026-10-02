//go:build linux

package loom

import (
	"fmt"
	"os"
	"os/exec"
)

// sys_install_linux.go — la part systemd de l'installation. Le parcours commun
// (dossiers, lien du binaire, /etc/default, chown) vit dans sys_install_unix.go.

// sudoersTemplate autorise l'utilisateur à piloter UNE unité sans mot de passe.
// Posé pour les deux : l'interface, qui tourne sans privilèges, doit pouvoir
// redémarrer le moteur (bascule de preset) comme elle-même (nouveau jeton).
const sudoersTemplate = `# Allows %[1]s to control unit %[2]s without a password (created by loom install).
%[1]s ALL=(root) NOPASSWD: /bin/systemctl start %[2]s, /bin/systemctl stop %[2]s, /bin/systemctl restart %[2]s, /bin/systemctl enable %[2]s, /bin/systemctl disable %[2]s
`

// engineUnitTemplate — le moteur : exec llama-server, supervisé directement par
// systemd. Champs : User, WorkingDirectory, ExecStart.
const engineUnitTemplate = `[Unit]
Description=Loom — llama.cpp engine
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
Environment=LOOM_HOME=%s
EnvironmentFile=-/etc/default/loom
ExecStart=%s
Restart=on-failure
RestartSec=3

# Priorité CPU : on remonte le process pour qu'il ne soit pas dépriorisé face
# aux tâches de fond (sampling/orchestration côté CPU pèsent sur le débit même
# en inference GPU). Nice négatif + scheduling normal réactif.
Nice=-10
CPUSchedulingPolicy=other

[Install]
WantedBy=multi-user.target
`

// uiUnitTemplate — l'interface : UI web locale, tunnel du relais et endpoint
// OpenAI, servis par un SEUL process (donc une seule conversation).
// Champs : unité du moteur (dépendance), User, WorkingDirectory, ExecStart.
const uiUnitTemplate = `[Unit]
Description=Loom — web interface
After=network-online.target %s.service
Wants=network-online.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
Environment=LOOM_HOME=%s
EnvironmentFile=-/etc/default/loom
ExecStart=%s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

func installServices(targetUser, loomHome string) error {
	svc, exe := serviceName(), installedExePath()
	units := map[string]string{
		svc:        fmt.Sprintf(engineUnitTemplate, targetUser, loomHome, loomHome, exe+" serve"),
		uiUnitName: fmt.Sprintf(uiUnitTemplate, svc, targetUser, loomHome, loomHome, exe+" web"),
	}
	for name, body := range units {
		path := "/etc/systemd/system/" + name + ".service"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Printf("  %s %s\n", green("✓"), path)

		sudoers := fmt.Sprintf(sudoersTemplate, targetUser, name)
		sudoersPath := "/etc/sudoers.d/" + name
		if err := os.WriteFile(sudoersPath, []byte(sudoers), 0o440); err != nil {
			return err
		}
		fmt.Printf("  %s %s\n", green("✓"), sudoersPath)
	}
	if err := installSystemUpdater(targetUser); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// uninstallServices arrête, désactive et efface les deux unités. L'interface
// passe AVANT le moteur : elle sait le redémarrer, l'ordre inverse pourrait le
// relancer sous nos pieds.
func uninstallServices() {
	svc := serviceName()
	for _, name := range []string{uiUnitName, svc} {
		_ = exec.Command("systemctl", "stop", name).Run()
		_ = exec.Command("systemctl", "disable", name).Run()
	}
	for _, p := range []string{
		"/etc/systemd/system/" + svc + ".service",
		"/etc/systemd/system/" + uiUnitName + ".service",
		"/etc/sudoers.d/" + svc,
		"/etc/sudoers.d/" + uiUnitName,
		systemUpdateRule, systemUpdateHelper,
	} {
		if err := os.Remove(p); err == nil {
			fmt.Printf("  %s %s\n", green("✓"), p)
		}
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
}
