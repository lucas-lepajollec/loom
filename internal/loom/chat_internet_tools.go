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
			return fmt.Errorf("configure the URL first: loom internet url <url>  (or switch to the built-in engine: loom internet engine go)")
		}
		if err := setInternetEnabled(true); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " internet access enabled — the AI has web_search/web_open/web_read/web_grep (if agent mode is active)")
	case "off":
		if err := setInternetEnabled(false); err != nil {
			return err
		}
		fmt.Println(green("[ok]") + " internet access disabled")
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
		fmt.Printf("%s Crawl4AI server: %s\n", green("[ok]"), bold(u))
	case "key":
		if len(args) < 2 {
			return fmt.Errorf("usage: loom internet key <key>  (empty to remove it: loom internet key \"\")")
		}
		k := strings.TrimSpace(args[1])
		if err := writeCrawlKey(k); err != nil {
			return err
		}
		reachMu.Lock()
		reachURL = ""
		reachMu.Unlock()
		if k == "" {
			fmt.Println(green("[ok]") + " Crawl4AI key removed")
		} else {
			fmt.Println(green("[ok]") + " Crawl4AI key saved")
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
			fmt.Println(green("[ok]") + " built-in engine — no installation required (no JavaScript rendering)")
		} else {
			fmt.Println(green("[ok]") + " Crawl4AI engine — configure the server: loom internet url <url>")
		}
	case "", "status", "list":
		state := dim("off")
		if internetEnabled() {
			state = green("on")
		}
		fmt.Printf("%s  state: %s\n", cyan("Internet access"), state)
		if webEngine() == engineGo {
			fmt.Printf("  engine:  %s (no installation, no JavaScript rendering)\n", bold("built-in"))
			fmt.Printf("  tools: web_search, web_open, web_read, web_grep\n")
			return nil
		}
		fmt.Printf("  moteur  : %s\n", bold("crawl4ai"))
		u := crawl4aiURL()
		if u == "" {
			fmt.Printf("  server: %s — configure: loom internet url <url>\n", dim("(not configured)"))
			return nil
		}
		reach := red("unreachable")
		if crawlReachable() {
			reach = green("reachable")
		}
		fmt.Printf("  server: %s (%s)\n", bold(u), reach)
		fmt.Printf("  tools: web_search, web_open, web_read, web_grep\n")
	default:
		return fmt.Errorf("usage: loom internet [on|off|status|engine <go|crawl4ai>|url <url>|key <key>]")
	}
	return nil
}
