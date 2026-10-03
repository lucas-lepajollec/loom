package tools

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// AutoDismissJS : dismisser conservateur de bandeaux cookies/consentement, exécuté
// dans la page. N'agit que sur des éléments qui RESSEMBLENT à une UI cookie
// (position fixed/sticky ou id/class cookie/consent/gdpr). Port direct de web.ts.
const AutoDismissJS = `(() => { try {
  const TEXT = /^(accept all|accept|i accept|agree|i agree|got it|i understand|j'accepte|tout accepter|accepter|d'accord|allow all|allow|consent|continue|ok)$/i;
  const isOverlayish = (el) => {
    try {
      let cur = el;
      for (let i = 0; i < 6 && cur; i++) {
        const s = getComputedStyle(cur);
        if (s.position === 'fixed' || s.position === 'sticky') return true;
        const idcls = ((cur.id || '') + ' ' + (cur.className || '')).toLowerCase();
        if (/cookie|consent|gdpr|cmp|privacy/.test(idcls)) return true;
        cur = cur.parentElement;
      }
    } catch {}
    return false;
  };
  const candidates = document.querySelectorAll('button, [role="button"], input[type="button"], input[type="submit"]');
  let clicked = 0;
  for (const b of candidates) {
    if (clicked >= 2) break;
    const t = (b.innerText || b.value || b.getAttribute('aria-label') || '').trim();
    if (!t || !TEXT.test(t)) continue;
    if (b.offsetParent === null) continue;
    if (!isOverlayish(b)) continue;
    try { b.click(); clicked++; } catch {}
  }
  document.querySelectorAll('[id*="cookie" i],[id*="consent" i],[class*="cookie" i],[class*="consent" i],[id*="gdpr" i],[class*="gdpr" i],[id*="cmp" i],[class*="cmp" i]')
    .forEach(el => { try {
      const s = getComputedStyle(el);
      if (s.position === 'fixed' || s.position === 'sticky') el.remove();
    } catch {} });
  document.documentElement.style.overflow = 'auto';
  if (document.body) document.body.style.overflow = 'auto';
} catch {} })();`

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

var (
	wsRe        = regexp.MustCompile(`\s+`)
	numEntityRe = regexp.MustCompile(`&#(\d+);`)
	ddgHeadRe   = regexp.MustCompile(`^##\s+\[([^\]]+)\]\(([^)]+)\)\s*$`)
	ddgLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	urlishRe    = regexp.MustCompile(`^[\w.-]+\.[a-z]{2,}`)
)

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// NormalizeCrawlURL : github.com/owner/repo → README brut. Port de web.ts.
func NormalizeCrawlURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host == "github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/README.md", parts[0], parts[1])
		}
	}
	return raw
}

// NormalizeLines : \r\n → \n, trim trailing, réduit les runs de lignes vides et
// les doublons consécutifs, retire les vides en tête/queue. Port de web.ts.
func NormalizeLines(raw string) []string {
	src := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := []string{}
	prev := ""
	blankRun := 0
	trailSpace := regexp.MustCompile(`[ \t]+$`)
	for _, l := range src {
		l = trailSpace.ReplaceAllString(l, "")
		if strings.TrimSpace(l) == "" {
			blankRun++
			if blankRun > 1 {
				continue
			}
			out = append(out, "")
			prev = ""
			continue
		}
		blankRun = 0
		if l == prev {
			continue
		}
		out = append(out, l)
		prev = l
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func ExtractOutline(lines []string) string {
	var out []string
	for i, l := range lines {
		if m := headingRe.FindStringSubmatch(l); m != nil {
			indent := strings.Repeat("  ", len(m[1])-1)
			out = append(out, fmt.Sprintf("%5d | %s%s", i+1, indent, m[2]))
		}
	}
	if len(out) == 0 {
		return "(no Markdown headings found)"
	}
	return strings.Join(out, "\n")
}

func FormatLines(lines []string, startLine int) string {
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%5d | %s\n", startLine+i, l)
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.2f MB", float64(n)/1024/1024)
	}
}

func DecodeHTMLEntities(s string) string {
	s = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&#x2F;", "/", "&nbsp;", " ",
	).Replace(s)
	return numEntityRe.ReplaceAllStringFunc(s, func(m string) string {
		var n int
		fmt.Sscanf(m, "&#%d;", &n)
		if n > 0 {
			return string(rune(n))
		}
		return m
	})
}

// DecodeUddg : DDG enrobe les résultats en //duckduckgo.com/l/?uddg=URL_ENCODÉE
func DecodeUddg(raw string) string {
	s := raw
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	if q := u.Query().Get("uddg"); q != "" {
		if dec, e := url.QueryUnescape(q); e == nil {
			return dec
		}
	}
	return s
}

type CrawlOptions struct {
	JSCode        []string
	WaitFor       string
	PageTimeoutMS int
	RawMarkdown   bool // true => préfère raw_markdown (recherche) ; false => fit_markdown
}

// CrawlResult modélise un résultat Crawl4AI. Markdown peut être une string ou un
// objet {raw_markdown, fit_markdown} — on gère les deux via json.RawMessage.
type CrawlResult struct {
	Success      bool            `json:"success"`
	ErrorMessage string          `json:"error_message"`
	Markdown     json.RawMessage `json:"markdown"`
}

func (r CrawlResult) MarkdownText(preferRaw bool) string {
	if len(r.Markdown) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r.Markdown, &s) == nil {
		return s
	}
	var obj struct {
		Raw string `json:"raw_markdown"`
		Fit string `json:"fit_markdown"`
	}
	if json.Unmarshal(r.Markdown, &obj) == nil {
		if preferRaw {
			if obj.Raw != "" {
				return obj.Raw
			}
			return obj.Fit
		}
		if obj.Fit != "" {
			return obj.Fit
		}
		return obj.Raw
	}
	return ""
}

// HTTPDoer keeps HTTP ownership and transport configuration in Loom.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type CrawlSource interface {
	CrawlURL() string
	CrawlKey() string
}

func crawlAuth(source CrawlSource, req *http.Request) {
	if k := source.CrawlKey(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
}

// runCrwl appelle POST {base}/crawl et renvoie le Markdown extrait. Port de web.ts.
func RunCrwl(source CrawlSource, client HTTPDoer, target string, opts CrawlOptions) (string, error) {
	base := source.CrawlURL()
	if base == "" {
		return "", fmt.Errorf("no Crawl4AI server configured (CRAWL4AI_URL)")
	}
	params := map[string]any{}
	if len(opts.JSCode) > 0 {
		params["js_code"] = opts.JSCode
	}
	if opts.WaitFor != "" {
		params["wait_for"] = opts.WaitFor
	}
	if opts.PageTimeoutMS > 0 {
		params["page_timeout"] = opts.PageTimeoutMS
	}
	body := map[string]any{
		"urls": []string{target},
		"browser_config": map[string]any{
			"type":   "BrowserConfig",
			"params": map[string]any{"headless": true},
		},
		"crawler_config": map[string]any{
			"type":   "CrawlerRunConfig",
			"params": params,
		},
	}
	buf, _ := json.Marshal(body)

	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/crawl", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	crawlAuth(source, req)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Crawl4AI injoignable (%s): %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return "", fmt.Errorf("Crawl4AI HTTP %d: %s", resp.StatusCode, TailRunes(string(b), 300))
	}
	var data struct {
		Results []CrawlResult `json:"results"`
	}
	// La réponse peut être soit {results:[...]}, soit un objet unique. On décode
	// d'abord la forme {results}, sinon on retombe sur un résultat unique.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if readErr != nil || len(raw) > 32<<20 {
		return "", fmt.Errorf("Crawl4AI: response unreadable or exceeds 32 MiB")
	}
	if jerr := json.Unmarshal(raw, &data); jerr != nil || len(data.Results) == 0 {
		var single CrawlResult
		if json.Unmarshal(raw, &single) == nil {
			data.Results = []CrawlResult{single}
		}
	}
	if len(data.Results) == 0 {
		return "", fmt.Errorf("Crawl4AI: empty response")
	}
	r := data.Results[0]
	if !r.Success {
		msg := r.ErrorMessage
		if msg == "" {
			msg = "crawl failed"
		}
		return "", fmt.Errorf("Crawl4AI : %s", TailRunes(msg, 300))
	}
	return r.MarkdownText(opts.RawMarkdown), nil
}

func DuckduckgoSearch(source SearchSource, query string, limit int) ([]SearchResult, error) {
	if source.Engine() == EngineGo {
		// Le moteur intégré lit la structure HTML des résultats plutôt que de
		// reparser un Markdown intermédiaire.
		return source.Search(query, limit)
	}
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	md, err := source.Crawl(searchURL, CrawlOptions{RawMarkdown: true, PageTimeoutMS: 30000})
	if err != nil {
		return nil, err
	}
	if strings.Contains(md, "anomaly-modal") || strings.Contains(md, "anomaly.js") {
		return nil, fmt.Errorf("DuckDuckGo returned an anti-bot challenge")
	}
	var results []SearchResult
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines) && len(results) < limit; i++ {
		h := ddgHeadRe.FindStringSubmatch(lines[i])
		if h == nil {
			continue
		}
		title := strings.TrimSpace(strings.ReplaceAll(DecodeHTMLEntities(h[1]), "**", ""))
		u := DecodeUddg(h[2])
		if title == "" || u == "" || strings.Contains(u, "duckduckgo.com") {
			continue
		}
		snippet := ""
		for j := i + 1; j < i+6 && j < len(lines); j++ {
			ln := strings.TrimSpace(lines[j])
			if ln == "" {
				continue
			}
			for _, lm := range ddgLinkRe.FindAllStringSubmatch(ln, -1) {
				text := strings.TrimSpace(lm[1])
				if text == "" || strings.HasPrefix(text, "!") || urlishRe.MatchString(text) {
					continue
				}
				snippet = strings.TrimSpace(strings.ReplaceAll(DecodeHTMLEntities(text), "**", ""))
				break
			}
			if snippet != "" {
				break
			}
		}
		dup := false
		for _, r := range results {
			if r.URL == u {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		results = append(results, SearchResult{Title: title, URL: u, Snippet: snippet})
	}
	return results, nil
}

const EngineGo = "go"
const EngineCrawl = "crawl4ai"

type SearchSource interface {
	Engine() string
	Search(string, int) ([]SearchResult, error)
	Crawl(string, CrawlOptions) (string, error)
}

type Page struct {
	URL       string
	Lines     []string
	FetchedAt time.Time
}

type FetchOptions struct {
	Force         bool
	Actions       []string
	DismissPopups bool
	WaitFor       string
}

func CacheKeyFor(u string, opts FetchOptions) string {
	fp, _ := json.Marshal(map[string]any{"a": opts.Actions, "d": opts.DismissPopups, "w": opts.WaitFor})
	if string(fp) == `{"a":null,"d":true,"w":""}` || string(fp) == `{"a":[],"d":true,"w":""}` {
		return u
	}
	h := md5.Sum(fp)
	return u + "#" + hex.EncodeToString(h[:])[:8]
}
