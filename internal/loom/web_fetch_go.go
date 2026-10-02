package loom

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
)

// Moteur web INTÉGRÉ (pur Go) — alternative à Crawl4AI, sans rien à installer.
//
// Crawl4AI = Chromium headless piloté par Playwright : il exécute le JS de la
// page avant d'en extraire le markdown. C'est puissant mais ça demande à
// l'utilisateur de faire tourner un serveur Python/Docker — rédhibitoire pour
// un utilisateur non technique.
//
// Ce moteur-ci remplace UNIQUEMENT l'étape « URL → markdown » (runCrwl) par du
// Go pur :
//
//	http.Get → readability (port Go de Readability de Firefox, qui vire nav,
//	pubs, pieds de page) → html-to-markdown
//
// Ce qu'on perd : le rendu JavaScript. Une page 100 % React sans SSR ressort
// vide, et les options js_code / wait_for / actions n'ont plus de sens (pas de
// DOM vivant). En pratique, les sources que l'IA lit — docs, articles, blogs,
// Wikipédia, GitHub, forums — sont servies en HTML et passent très bien.
//
// Le choix se fait par la clé de config WEB_ENGINE ("go" ou "crawl4ai"). Voir
// webEngine(). Crawl4AI reste entièrement supporté.

// ─── sélection du moteur ────────────────────────────────────────────────────

const (
	engineGo    = "go"
	engineCrawl = "crawl4ai"
)

// webEngine renvoie le moteur web actif : engineGo ou engineCrawl.
//
// Par défaut on prend le moteur intégré. L'exception est l'installation qui a
// DÉJÀ une URL Crawl4AI configurée sans avoir jamais choisi de moteur : elle
// tournait sur Crawl4AI, on ne change pas son comportement dans son dos.
func webEngine() string {
	switch strings.ToLower(strings.TrimSpace(ReadConfig()["WEB_ENGINE"])) {
	case engineGo:
		return engineGo
	case engineCrawl:
		return engineCrawl
	}
	if crawl4aiURL() != "" {
		return engineCrawl
	}
	return engineGo
}

// setWebEngine enregistre le moteur web choisi et invalide le cache de
// reachability (l'état affiché dépend du moteur).
func setWebEngine(engine string) error {
	if engine != engineGo && engine != engineCrawl {
		return fmt.Errorf("unknown web engine: %q", engine)
	}
	if err := SetConfigKey("WEB_ENGINE", engine); err != nil {
		return err
	}
	reachMu.Lock()
	reachURL = ""
	reachMu.Unlock()
	return nil
}

// ─── récupération HTTP ──────────────────────────────────────────────────────

// goHTTPClient : client dédié au moteur web. Timeout global, plafond de
// redirections, et un cookie jar — indispensable pour les sites qui posent un
// cookie de session puis redirigent (murs de consentement typiques). Le jar est
// en mémoire : il disparaît à l'arrêt du process, on ne persiste rien.
// On ne touche pas à http.DefaultClient (utilisé ailleurs).
var goHTTPClient = &http.Client{
	Timeout: goFetchTimeout,
	Jar:     newCookieJar(),
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

func newCookieJar() http.CookieJar {
	j, err := cookiejar.New(nil)
	if err != nil {
		return nil // un client sans jar reste fonctionnel
	}
	return j
}
