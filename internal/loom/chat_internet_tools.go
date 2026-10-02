// chat_internet_tools.go — les 4 outils web exposés au modèle (web_search,
// web_open, web_read, web_grep) + la sous-commande `loom internet`.
package loom

import (
"fmt"
"strings"
)

// ─── exécution des outils (appelée par le dispatch de llm_client.go) ───────────────

// ─── CLI : loom internet [on|off|status|engine|url|key] ────────────────────

func cmdInternet(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "on":
		// Le moteur intégré ne demande aucun réglage ; Crawl4AI exige un serveur.
		if webEngine() == engineCrawl && crawl4aiURL() == "" {
			return fmt.Errorf("configure d'abord l'URL : loom internet url <url>  (ou bascule sur le moteur intégré : loom internet engine go)")
		}
		if err := setInternetEnabled(true); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " accès internet activé — l'IA dispose de web_search/web_open/web_read/web_grep (si le mode agent est actif)")
	case "off":
		if err := setInternetEnabled(false); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " accès internet désactivé")
	case "url":
		if len(args) < 2 {
			return fmt.Errorf("usage: loom internet url <url>  (ex: http://localhost:11235)")
		}
		u := strings.TrimRight(strings.TrimSpace(args[1]), "/")
		if err := SetConfigKey("CRAWL4AI_URL", u); err != nil {
			return err
		}
		reachMu.Lock()
		reachURL = "" // invalide le cache de reachability
		reachMu.Unlock()
		fmt.Printf("%s serveur Crawl4AI : %s\n", green("[ok]"), bold(u))
	case "key":
		if len(args) < 2 {
			return fmt.Errorf("usage: loom internet key <clé>  (vide pour l'enlever : loom internet key \"\")")
		}
		k := strings.TrimSpace(args[1])
		if err := writeCrawlKey(k); err != nil {
			return err
		}
		reachMu.Lock()
		reachURL = ""
		reachMu.Unlock()
		if k == "" {
			fmt.Println(green("[ok]") + " clé Crawl4AI retirée")
		} else {
			fmt.Println(green("[ok]") + " clé Crawl4AI enregistrée")
		}
	case "engine":
		if len(args) < 2 {
			return fmt.Errorf("usage: loom internet engine <go|crawl4ai>")
		}
		e := strings.ToLower(strings.TrimSpace(args[1]))
		if err := setWebEngine(e); err != nil {
			return err
		}
		if e == engineGo {
			fmt.Println(green("[ok]") + " moteur intégré — aucune installation requise (pas de rendu JavaScript)")
		} else {
			fmt.Println(green("[ok]") + " moteur Crawl4AI — configure le serveur : loom internet url <url>")
		}
	case "", "status", "list":
		state := dim("off")
		if internetEnabled() {
			state = green("on")
		}
		fmt.Printf("%s  état: %s\n", cyan("Accès internet"), state)
		if webEngine() == engineGo {
			fmt.Printf("  moteur  : %s (aucune installation, pas de rendu JavaScript)\n", bold("intégré"))
			fmt.Printf("  outils  : web_search, web_open, web_read, web_grep\n")
			return nil
		}
		fmt.Printf("  moteur  : %s\n", bold("crawl4ai"))
		u := crawl4aiURL()
		if u == "" {
			fmt.Printf("  serveur : %s — configure : loom internet url <url>\n", dim("(non configuré)"))
			return nil
		}
		reach := red("injoignable")
		if crawlReachable() {
			reach = green("joignable")
		}
		fmt.Printf("  serveur : %s (%s)\n", bold(u), reach)
		fmt.Printf("  outils  : web_search, web_open, web_read, web_grep\n")
	default:
		return fmt.Errorf("usage: loom internet [on|off|status|engine <go|crawl4ai>|url <url>|key <clé>]")
	}
	return nil
}
