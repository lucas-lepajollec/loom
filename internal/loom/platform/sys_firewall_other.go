//go:build !windows

package platform

// Linux et macOS n'ont pas de pare-feu entrant activé par défaut, et quand il y
// en a un (ufw, firewalld, pf) c'est une affaire d'administration système qu'un
// gestionnaire de modèles n'a pas à trancher dans le dos de l'utilisateur. On
// n'en pilote donc aucun : « inconnu » dit exactement ça, et l'interface
// n'affiche pas d'avertissement de pare-feu là où il n'y a rien à avertir.

func FirewallOpen(int, bool) error  { return nil }
func FirewallClose(int, bool) error { return nil }

func FirewallState(int, bool) string { return "inconnu" }

func FirewallManualHint(int) string { return "" }
