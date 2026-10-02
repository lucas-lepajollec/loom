//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// sys_firewall_windows.go — règles de pare-feu entrantes pour le moteur.
//
// Sur une installation Windows fraîche, même avec HOST=0.0.0.0, le pare-feu
// bloque tout ce qui arrive du réseau : l'utilisateur voyait « ça écoute
// partout » et se prenait quand même un délai d'attente depuis son autre
// machine. Loom pose donc la règle lui-même, quand il en a le droit.
//
// `loom install` ne réclame PAS les droits administrateur (voir
// sys_install_windows.go) : netsh échouera donc souvent. On ne fait pas semblant
// que ça a marché — firewallState relit la règle, et firewallManualHint donne la
// commande exacte à coller dans un terminal administrateur.

// firewallRuleName identifie nos règles. Une par port : changer PORT dans la
// configuration ne doit pas laisser une règle ouverte sur l'ancien.
func firewallRuleName(port int) string {
	return "Loom moteur (port " + strconv.Itoa(port) + ")"
}

// netsh exécute netsh sans faire clignoter de console (hideCmd) et renvoie sa
// sortie combinée.
func netsh(inert bool, args ...string) (string, error) {
	if inert {
		return "", fmt.Errorf("firewall not managed")
	}
	out, err := HideCmd(exec.Command("netsh", args...)).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// firewallOpen autorise le port en entrée (TCP), pour les profils privé et
// domaine seulement : ouvrir un modèle non authentifié sur un réseau public
// (café, hôtel) n'est pas un défaut qu'on pose au nom de l'utilisateur.
func FirewallOpen(port int, inert bool) error {
	_ = FirewallClose(port, inert) // idempotence : pas d'empilement de règles homonymes
	out, err := netsh(inert, "advfirewall", "firewall", "add", "rule",
		"name="+firewallRuleName(port), "dir=in", "action=allow",
		"protocol=TCP", "localport="+strconv.Itoa(port), "profile=private,domain")
	if err != nil {
		return fmt.Errorf("firewall rule rejected (administrator permissions required): %s", out)
	}
	return nil
}

// firewallClose retire la règle. Absente = rien à faire, et surtout pas une
// erreur.
func FirewallClose(port int, inert bool) error {
	_, _ = netsh(inert, "advfirewall", "firewall", "delete", "rule", "name="+firewallRuleName(port))
	return nil
}

// firewallState relit l'état RÉEL de la règle plutôt que de croire au succès
// supposé d'une commande passée.
func FirewallState(port int, inert bool) string {
	out, err := netsh(inert, "advfirewall", "firewall", "show", "rule", "name="+firewallRuleName(port))
	if err != nil || strings.TrimSpace(out) == "" {
		return "ferme"
	}
	// netsh répond « Aucune règle ne correspond aux critères » (localisé) avec un
	// code de sortie non nul : le err ci-dessus suffit. Ici la règle existe.
	return "ouvert"
}

// firewallManualHint : la commande à coller dans un terminal ADMINISTRATEUR
// quand Loom n'a pas pu poser la règle lui-même.
func FirewallManualHint(port int) string {
	return fmt.Sprintf("the Windows firewall is still blocking port %d. Open an ADMINISTRATOR terminal and run:\n"+
		`  netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=private,domain`,
		port, firewallRuleName(port), port)
}
