package loom

import (
	"fmt"
	"strings"
)

// La mémoire de loom = des fichiers Markdown plats sous memory/<nom>.md.
// L'IA y range ce qu'elle veut retenir entre les sessions (préférences,
// décisions, procédures, infos projet). Quatre outils : mem_search, mem_read,
// mem_add, mem_edit.

// memMode lit MEM_MODE. Défaut et « always » = off : la mémoire auto
// (chercher/écrire tout seul) est hors produit.
func memMode() MemMode {
	switch strings.ToLower(strings.TrimSpace(ReadConfig()["MEM_MODE"])) {
	case "ondemand", "on-demand", "demand":
		return MemOnDemand
	default:
		return MemOff
	}
}

// setMemMode persiste le mode mémoire dans config.env.
func setMemMode(m MemMode) error {
	return SetConfigKey("MEM_MODE", string(m))
}

// cmdMemory : loom memory [off|ondemand|always|status]
func cmdMemory(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	label := map[MemMode]string{
		MemOff:      "désactivée (l'IA n'a aucun accès mémoire)",
		MemOnDemand: "sur demande (outils dispo, utilisés seulement si tu le demandes)",
		MemAlways:   "auto (l'IA cherche et sauve d'elle-même)",
	}
	switch sub {
	case "off":
		if err := setMemMode(MemOff); err != nil {
			return err
		}
	case "ondemand", "on-demand", "demand", "manual", "manuel":
		if err := setMemMode(MemOnDemand); err != nil {
			return err
		}
	case "always", "auto":
		if err := setMemMode(MemOff); err != nil {
			return err
		}
		fmt.Printf("%s mémoire auto retirée — mode off\n", yellow("[info]"))
	case "", "status":
		m := memMode()
		fmt.Printf("%s  mode: %s — %s\n", cyan("Mémoire"), bold(string(m)), label[m])
		pages := MemList()
		fmt.Printf("  %d page(s) sous %s\n", len(pages), memoryDir())
		return nil
	default:
		return fmt.Errorf("usage: loom memory [off|ondemand|status]")
	}
	m := memMode()
	fmt.Printf("%s mémoire : %s — %s\n", green("[ok]"), bold(string(m)), label[m])
	return nil
}
