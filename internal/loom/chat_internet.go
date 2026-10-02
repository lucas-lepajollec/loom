package loom

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Accès internet de l'IA — port Go de l'extension pi ~/.pi/agent/extensions/web.ts.
//
// loom parle à un serveur Crawl4AI (Chrome headless, endpoint /crawl) dont l'URL
// est configurée dans config.env (CRAWL4AI_URL). Quand le mode agent ET l'accès
// internet sont actifs ET que le serveur répond, l'IA dispose de 4 outils :
//
//   - web_search : recherche DuckDuckGo (via crawl), liste {title, url, snippet}.
//   - web_open   : récupère une URL (cache 10 min), renvoie SEULEMENT les métadonnées
//                  (nb de lignes, taille, plan des titres) — pas le contenu.
//   - web_read   : lit une plage de lignes d'une URL déjà ouverte (offset + limit).
//   - web_grep   : recherche regex dans une URL déjà ouverte, lignes + contexte.
//
// Workflow attendu : web_open(url) → web_read/web_grep. Même logique que pi.

// ─── configuration & état ───────────────────────────────────────────────────

// crawl4aiURL renvoie l'URL du serveur Crawl4AI (config.env CRAWL4AI_URL), sans
// slash final. Vide si non configuré.
func crawl4aiURL() string {
	u := strings.TrimSpace(ReadConfig()["CRAWL4AI_URL"])
	return strings.TrimRight(u, "/")
}

// crawl4aiKey renvoie la clé d'accès au serveur Crawl4AI, vide si le serveur
// est ouvert. Envoyée en « Authorization: Bearer … », ce qu'attend Crawl4AI
// quand l'authentification est activée (CRAWL4AI_API_TOKEN côté serveur).
//
// Elle est rangée hors de la configuration : celle-ci est ce qu'on copie-colle
// pour demander de l'aide, et une clé en clair dedans finit par fuiter. Même
// modèle que la clé de complétion.
func crawl4aiKey() string { return getStr(bkState, "crawl_key") }

// writeCrawlKey enregistre (clé non vide) ou efface (clé vide) la clé Crawl4AI.
func writeCrawlKey(key string) error {
	_ = SetConfigKey("CRAWL4AI_KEY", "") // ne laisse rien traîner dans la config
	return putStr(bkState, "crawl_key", key)
}

// crawlAuth pose l'en-tête d'authentification sur une requête vers Crawl4AI.
// Sans clé configurée, la requête part telle quelle.
func crawlAuth(req *http.Request) {
	if k := crawl4aiKey(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
}

// internetEnabled : accès internet actif = interrupteur armé ET moteur web
// utilisable. Avec le moteur intégré (engineGo) il n'y a rien à configurer ;
// avec Crawl4AI il faut en plus une URL de serveur.
func internetEnabled() bool {
	if webEngine() == engineCrawl && crawl4aiURL() == "" {
		return false
	}
	return getBool(bkState, "internet")
}

func setInternetEnabled(on bool) error { return putBool(bkState, "internet", on) }

// crawlReachable teste que le serveur Crawl4AI répond, avec un cache court (~30 s)
// pour ne pas ralentir chaque tour de chat. Un serveur configuré mais injoignable
// => les outils web ne sont pas proposés (« actif ET fonctionnel »).
var (
	reachMu   sync.Mutex
	reachOK   bool
	reachURL  string
	reachWhen time.Time
)

const reachTTL = 30 * time.Second

func crawlReachable() bool {
	if webEngine() == engineGo {
		// Moteur intégré : rien à joindre, il tourne dans ce process. On ne
		// teste pas la connectivité internet ici — un ping à chaque tour de
		// chat coûterait plus cher que l'échec de la requête réelle.
		return true
	}
	base := crawl4aiURL()
	if base == "" {
		return false
	}
	reachMu.Lock()
	defer reachMu.Unlock()
	if base == reachURL && time.Since(reachWhen) < reachTTL {
		return reachOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok := false
	// Crawl4AI expose /health ; on tolère aussi une simple réponse HTTP sur la racine.
	for _, path := range []string{"/health", "/"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			continue
		}
		crawlAuth(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 500 {
			ok = true
			break
		}
	}
	reachOK, reachURL, reachWhen = ok, base, time.Now()
	return ok
}

// ─── URL & normalisation ────────────────────────────────────────────────────

// ─── cache mémoire (TTL 10 min) ─────────────────────────────────────────────

var (
	pageCacheMu sync.Mutex
	pageCache   = map[string]*cacheEntry{}
)

const pageCacheTTL = 10 * time.Minute

// findCached : entrée de cache la plus récente pour une URL (toutes options).
func findCached(rawURL string) *cacheEntry {
	u := normalizeCrawlURL(rawURL)
	pageCacheMu.Lock()
	defer pageCacheMu.Unlock()
	var best *cacheEntry
	for k, v := range pageCache {
		if k == u || strings.HasPrefix(k, u+"#") {
			if time.Since(v.FetchedAt) > pageCacheTTL {
				continue
			}
			if best == nil || v.FetchedAt.After(best.FetchedAt) {
				best = v
			}
		}
	}
	return best
}

func getPage(rawURL string, opts fetchOptions) (*cacheEntry, error) {
	u := normalizeCrawlURL(rawURL)
	key := cacheKeyFor(u, opts)
	pageCacheMu.Lock()
	cached := pageCache[key]
	pageCacheMu.Unlock()
	if !opts.Force && cached != nil && time.Since(cached.FetchedAt) < pageCacheTTL {
		return cached, nil
	}

	jsCode := []string{}
	if opts.DismissPopups {
		jsCode = append(jsCode, autoDismissJS)
	}
	jsCode = append(jsCode, opts.Actions...)
	pageTimeout := 0
	if opts.WaitFor != "" || len(opts.Actions) > 0 {
		pageTimeout = 45000
	}
	var md string
	var err error
	if webEngine() == engineGo {
		// Moteur intégré : pas de DOM vivant, donc jsCode / waitFor / actions
		// sont sans objet (voir web_fetch_go.go).
		md, err = goFetchMarkdown(u)
	} else {
		md, err = runCrwl(u, crwlOptions{JSCode: jsCode, WaitFor: opts.WaitFor, PageTimeoutMS: pageTimeout})
	}
	if err != nil {
		return nil, err
	}
	// Le diagnostic « probablement du JavaScript » est posé par goFetchMarkdown,
	// qui seul connaît la taille du HTML brut. Ici on n'attrape que le cas trivial.
	if strings.TrimSpace(md) == "" {
		return nil, fmt.Errorf("page vide")
	}
	entry := &cacheEntry{URL: u, Lines: normalizeLines(md), FetchedAt: time.Now()}
	pageCacheMu.Lock()
	pageCache[key] = entry
	pageCacheMu.Unlock()
	return entry, nil
}
