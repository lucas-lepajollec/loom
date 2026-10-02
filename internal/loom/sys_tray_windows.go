//go:build windows

package loom

// sys_tray_windows.go — icône de Loom dans la zone de notification Windows (system
// tray). Quand on lance l'app (double-clic ou `loom app`), l'UI web s'ouvre dans
// le navigateur ET une petite icône apparaît près de l'horloge : elle montre que
// Loom tourne et offre un menu « Ouvrir Loom » / « Quitter ».
//
// getlantern/systray est pur Go sur Windows (Win32 via x/sys, aucun CGO) — le
// binaire reste statique. L'équivalent macOS vit dans sys_tray_darwin.go (Cocoa,
// donc CGO) ; les builds Linux ne touchent jamais systray.

import (
	"os"

	"github.com/getlantern/systray"
)

// runTray ouvre l'UI dans le navigateur puis fait tourner l'icône de la zone de
// notification. Bloque jusqu'à ce que l'utilisateur choisisse « Quitter ».
func runTray(url string) {
	systray.Run(func() {
		systray.SetIcon(trayIcon())
		systray.SetTitle("Loom")
		systray.SetTooltip("Loom — your local AI")
		mOpen := systray.AddMenuItem("Open Loom", "Open the interface")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit Loom and unload the model")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					_ = openBrowser(url)
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, func() {
		// « Quitter » doit tout arrêter, moteur compris.
		//
		// Le moteur tourne dans un process DÉTACHÉ (voir svcStart) : il survit
		// délibérément à la fermeture de l'app pour que le modèle reste chargé
		// entre deux ouvertures de l'interface. Mais quand on quitte pour de
		// bon, il n'a plus rien pour le piloter et garde des dizaines de Go de
		// RAM, sans la moindre fenêtre pour l'expliquer. svcStop tue l'arbre
		// (taskkill /T), donc llama-server avec lui.
		//
		// Windows seulement : ici l'app EST le propriétaire du moteur. Sous
		// Linux et macOS c'est systemd ou launchd, et fermer une interface n'a
		// pas à arrêter un service système.
		_ = svcStop(false)
		workspaceSessions.shutdownACP()
		os.Exit(0)
	})
}

// trayIcon : l'icône de marque en .ico, deux tailles pour rester nette selon
// l'échelle d'affichage. Le rendu vit dans sys_brand_icon.go, partagé avec
// l'icône du .exe.
func trayIcon() []byte { return BrandICO(16, trayIconSize) }
