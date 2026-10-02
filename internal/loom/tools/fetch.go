package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Un vrai User-Agent : beaucoup de sites servent une page dégradée (ou un 403)
// aux clients qui s'annoncent comme des robots.
const goFetchUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

const (
	GoFetchTimeout  = 30 * time.Second
	goFetchMaxBytes = 8 << 20 // 8 Mo : au-delà ce n'est plus une page à lire
)

// consentCookies : cookies qui marquent le bandeau de consentement comme DÉJÀ
// TRAITÉ, afin d'atteindre la page derrière le mur.
//
// ⚠ On signale un REFUS, jamais une acceptation : ces valeurs disent « pas de
// suivi, bandeau fermé ». Le but est de lire la page, pas d'ouvrir des traceurs
// au nom de l'utilisateur. Les en-têtes DNT et Sec-GPC (refus de vente/partage,
// juridiquement reconnu en Californie et respecté par une partie des sites)
// vont dans le même sens.
//
// Envoyés à tout le monde : un cookie inconnu d'un site est simplement ignoré.
var consentCookies = []string{
	"gdpr=0",
	"gdpr_consent=0",
	"cookieconsent_status=deny",
	"OptanonAlertBoxClosed=2024-01-01T00:00:00.000Z",
	"SOCS=CAESHAgBEhJnd3NfMjAyNDAxMDEtMF9SQzEaAmZyIAEaBgiA_LyaBg", // Google : « refuser tout »
}

// Seuils du diagnostic « rendu JavaScript » (voir goFetchMarkdown).
const (
	jsShellTextMax = 200   // texte extrait en dessous duquel il n'y a rien à lire
	jsShellHTMLMin = 15000 // HTML au-dessus duquel « rien à lire » devient suspect
)

// GoFetch effectue le GET et renvoie le corps (plafonné), le Content-Type et
// l'URL finale après redirections.
func GoFetch(client HTTPDoer, ctx context.Context, target string) (body []byte, contentType, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", goFetchUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.7")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9,en;q=0.8")
	// Signaux de refus du pistage (voir consentCookies).
	req.Header.Set("DNT", "1")
	req.Header.Set("Sec-GPC", "1")
	for _, c := range consentCookies {
		req.Header.Add("Cookie", c)
	}
	// Pas d'Accept-Encoding manuel : le transport gère gzip tout seul.

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, goFetchMaxBytes))
	if err != nil {
		return nil, "", "", fmt.Errorf("reading interrupted: %v", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "", "", fmt.Errorf("HTTP %d on %s", resp.StatusCode, target)
	}
	return b, resp.Header.Get("Content-Type"), resp.Request.URL.String(), nil
}

// GoParseHTML décode le corps selon le charset annoncé (ou détecté) puis le
// parse en arbre DOM.
func GoParseHTML(body []byte, contentType string) (*html.Node, error) {
	r, err := charset.NewReader(strings.NewReader(string(body)), contentType)
	if err != nil {
		// Charset inconnu : on tente en l'état plutôt que d'abandonner.
		r = strings.NewReader(string(body))
	}
	return html.Parse(r)
}

// GoFetchMarkdown est l'équivalent pur Go de runCrwl : elle récupère une URL et
// en renvoie le contenu en Markdown.
//
// Les contenus non-HTML (texte brut, Markdown, JSON…) sont renvoyés tels quels :
// c'est le cas des README GitHub, vers lesquels NormalizeCrawlURL redirige.
func GoFetchMarkdown(client HTTPDoer, target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GoFetchTimeout)
	defer cancel()

	body, contentType, finalURL, err := GoFetch(client, ctx, target)
	if err != nil {
		return "", err
	}

	ct := strings.ToLower(contentType)
	isHTML := strings.Contains(ct, "html") || strings.Contains(ct, "xml")
	if ct == "" {
		// Certains serveurs n'annoncent rien : on devine sur le contenu.
		isHTML = strings.Contains(strings.ToLower(string(body[:min(len(body), 1024)])), "<html")
	}
	if !isHTML {
		if strings.Contains(ct, "text/") || strings.Contains(ct, "json") || ct == "" {
			return string(body), nil
		}
		return "", fmt.Errorf("unreadable content type: %s", contentType)
	}

	doc, err := GoParseHTML(body, contentType)
	if err != nil {
		return "", fmt.Errorf("unreadable HTML: %v", err)
	}

	base, _ := url.Parse(finalURL)
	md, title := GoArticleMarkdown(doc, base)
	if strings.TrimSpace(md) == "" {
		// Readability n'a rien trouvé d'« article » (page d'accueil, listing,
		// appli…). On convertit la page entière plutôt que de renvoyer vide.
		md, err = GoConvert(doc, finalURL)
		if err != nil {
			return "", err
		}
	}
	if title != "" && !strings.HasPrefix(strings.TrimSpace(md), "# ") {
		md = "# " + title + "\n\n" + md
	}

	// Diagnostic du rendu client. Beaucoup de HTML (scripts, bundles, templates)
	// mais presque aucun texte extractible = coquille de SPA. Le rapport compte
	// plus que la taille absolue : une vraie petite page (example.com : ~1 Ko de
	// HTML, ~170 caractères de texte) n'est PAS suspecte, alors qu'une page de
	// 300 Ko qui ne rend que 80 caractères l'est clairement.
	//
	// Sans ce message le modèle relançait la même URL en boucle (cf. le garde-fou
	// anti-boucle de chat_agent.go) : on lui dit explicitement de changer de source.
	if len(strings.TrimSpace(md)) < jsShellTextMax && len(body) > jsShellHTMLMin {
		return "", fmt.Errorf("page retrieved (%s of HTML) but only %d text characters: "+
			"it is most likely rendered using JavaScript, which the built-in web engine does not execute. "+
			"Do NOT retry this URL — find another source (official documentation, project repository, "+
			"an article about it)", FormatBytes(len(body)), len(strings.TrimSpace(md)))
	}
	return md, nil
}

// GoArticleMarkdown applique Readability puis convertit le résultat. Renvoie du
// Markdown vide (sans erreur) si la page n'a pas de contenu d'article isolable.
func GoArticleMarkdown(doc *html.Node, base *url.URL) (md, title string) {
	// Readability mute l'arbre qu'on lui donne ; on lui passe une copie pour
	// pouvoir retomber sur le document complet en cas d'échec.
	clone, err := GoCloneDoc(doc)
	if err != nil {
		return "", ""
	}
	art, err := readability.FromDocument(clone, base)
	if err != nil || art.Node == nil {
		return "", ""
	}
	out, err := GoConvert(art.Node, BaseString(base))
	if err != nil {
		return "", ""
	}
	return out, strings.TrimSpace(art.Title())
}

// GoConvert rend un nœud HTML en Markdown, en réécrivant les liens et images
// relatifs en URLs absolues (sinon l'IA ne peut pas les rouvrir).
func GoConvert(node *html.Node, baseURL string) (string, error) {
	opts := []converter.ConvertOptionFunc{}
	if baseURL != "" {
		opts = append(opts, converter.WithDomain(baseURL))
	}
	b, err := htmltomarkdown.ConvertNode(node, opts...)
	if err != nil {
		return "", fmt.Errorf("Markdown conversion: %v", err)
	}
	return string(b), nil
}

// GoCloneDoc duplique un document HTML en le re-parsant depuis son rendu.
func GoCloneDoc(doc *html.Node) (*html.Node, error) {
	var sb strings.Builder
	if err := html.Render(&sb, doc); err != nil {
		return nil, err
	}
	return html.Parse(strings.NewReader(sb.String()))
}

func BaseString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// GoSearch interroge le point d'entrée HTML de DuckDuckGo et lit les résultats
// directement dans le DOM.
//
// Le chemin Crawl4AI passe par une conversion en Markdown avant de reparser le
// texte à coups de regex (duckduckgoSearch) ; ici on lit la structure réelle
// (.result__a, .result__snippet), ce qui est nettement plus robuste.
func GoSearch(client HTTPDoer, query string, limit int) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GoFetchTimeout)
	defer cancel()

	target := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	body, contentType, _, err := GoFetch(client, ctx, target)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(body), "anomaly-modal") || strings.Contains(string(body), "anomaly.js") {
		return nil, fmt.Errorf("DuckDuckGo returned an anti-bot challenge")
	}
	doc, err := GoParseHTML(body, contentType)
	if err != nil {
		return nil, fmt.Errorf("unreadable HTML: %v", err)
	}

	var results []SearchResult
	seen := map[string]bool{}
	for _, block := range GoFindByClass(doc, "result") {
		if len(results) >= limit {
			break
		}
		link := GoFirstByClass(block, "result__a")
		if link == nil {
			continue
		}
		href := DecodeUddg(GoAttr(link, "href"))
		title := GoText(link)
		if title == "" || href == "" || strings.Contains(href, "duckduckgo.com") || seen[href] {
			continue
		}
		snippet := ""
		if s := GoFirstByClass(block, "result__snippet"); s != nil {
			snippet = GoText(s)
		}
		seen[href] = true
		results = append(results, SearchResult{Title: title, URL: href, Snippet: snippet})
	}
	return results, nil
}

func GoHasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, f := range strings.Fields(GoAttr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

func GoAttr(n *html.Node, name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// GoFindByClass renvoie tous les nœuds portant la classe, sans descendre dans
// un nœud déjà retenu (les résultats DDG ne s'imbriquent pas).
func GoFindByClass(root *html.Node, class string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if GoHasClass(n, class) {
			out = append(out, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func GoFirstByClass(root *html.Node, class string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if GoHasClass(n, class) {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

// GoText concatène le texte d'un sous-arbre, espaces normalisés.
func GoText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(wsRe.ReplaceAllString(sb.String(), " "))
}
