package ajean

// backend_hub.go — proxy Hugging Face pour le hub de modèles GGUF.
// L'UI ne parle pas à huggingface.co : on filtre, on groupe les tranches, et
// on attache une estimation VRAM à partir du matériel local (même source que
// /api/vram). Les drapeaux llama.cpp ne sont pas inventés ici ; on ne sert
// que des liens .gguf déjà exposés par le dépôt.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hubAPIBase = "https://huggingface.co/api/models"
var hubWebBase = "https://huggingface.co"

var hubClient = &http.Client{Timeout: 12 * time.Second}

const hubReadmeBytes = 200 << 10
const hubReadmeChars = 120000

var (
	hubRepoRe   = regexp.MustCompile(`^[A-Za-z0-9][-A-Za-z0-9_.]{0,95}(/[A-Za-z0-9][-A-Za-z0-9_.]{0,95})?$`)
	hubParamsRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*([bBmM])(?:[-_\s.]|$)`)
	hubQuantRe  = regexp.MustCompile(`(?i)(?:^|[-_.])((?:UD-)?(?:IQ|Q|TQ|F|BF)\d{1,2}(?:[._][A-Z0-9]+)*)(?:[-_.]|$)`)
)

type hfSibling struct {
	Rfilename string `json:"rfilename"`
	Size      int64  `json:"size"`
}

type hubFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Quant  string `json:"quant"`
	URL    string `json:"url"`
	Vision bool   `json:"vision,omitempty"`
	Parts  int    `json:"parts,omitempty"`
}

func hubValidRepo(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") {
		return false
	}
	return hubRepoRe.MatchString(id)
}

func hubParseParamsB(s string) float64 {
	matches := hubParamsRe.FindAllStringSubmatch(s, -1)
	var best float64
	for _, m := range matches {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil || v <= 0 {
			continue
		}
		if strings.EqualFold(m[2], "m") {
			v = v / 1000
		}
		if v >= 2000 {
			continue
		}
		if v > best {
			best = v
		}
	}
	return best
}

func hubParseQuant(name string) string {
	base := name
	if i := strings.LastIndex(strings.ToLower(name), ".gguf"); i >= 0 {
		base = name[:i]
	}
	m := hubQuantRe.FindStringSubmatch(base)
	if len(m) < 2 {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(m[1], ".", "_"))
}

func hubIsMmproj(name string) bool {
	return strings.Contains(strings.ToLower(name), "mmproj")
}

// hubNeededMB approxime le poids chargé : GGUF (déjà quantifié) + ~12 % de
// mapping + cache KV grossier (16 Mo · B-params · k-ctx) + 384 Mo runtime.
// C'est le même ordre de grandeur que LM Studio / Unsloth, pas une mesure
// llama.cpp.
func hubNeededMB(fileBytes int64, paramsB float64, ctx int) float64 {
	if ctx <= 0 {
		ctx = 4096
	}
	if fileBytes < 0 {
		fileBytes = 0
	}
	weights := float64(fileBytes) / (1024 * 1024)
	kv := paramsB * (float64(ctx) / 1024.0) * 16.0
	return weights*1.12 + kv + 384
}

func hubVerdict(needed, vramTotal, vramFree, ramTotal float64) string {
	_ = vramFree // l'occupation actuelle ne compte pas : un chargement remplace le modèle en VRAM
	if vramTotal > 0 && needed <= vramTotal*0.90 {
		return "fits"
	}
	if vramTotal > 0 && needed <= vramTotal {
		return "tight"
	}
	if needed > 0 && ramTotal > 0 && needed <= ramTotal {
		return "offload"
	}
	if vramTotal <= 0 && ramTotal <= 0 {
		return "unknown"
	}
	return "no"
}

func hubVRAMTotals() (total, used, ram int) {
	for _, g := range liveGPUs() {
		if t, ok := asInt(g["total"]); ok {
			total += t
		}
		if u, ok := asInt(g["used"]); ok {
			used += u
		}
	}
	_, ram = ramUsageMB()
	return total, used, ram
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}

func hubGET(raw string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "loom/"+Version)
	req.Header.Set("Accept", "application/json")
	return hubClient.Do(req)
}

var (
	hubFrontmatterRe = regexp.MustCompile(`(?s)^---\s*\n.*?\n---\s*\n?`)
	hubMdLinkRe      = regexp.MustCompile(`\]\(([^)]+)\)`)
	hubHtmlSrcRe     = regexp.MustCompile(`(?i)((?:src|href)=)(["'])([^"']+)(["'])`)
	hubChromeHeadRe  = regexp.MustCompile(`(?i)^#{1,6}\s+(details|model card(?: for .*)?|dataset card(?: for .*)?|card for .*)$`)
)

func hubResolveReadmeURL(src, base string) string {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "#") {
		return src
	}
	low := strings.ToLower(src)
	if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") {
		return ""
	}
	if strings.Contains(src, "://") || strings.HasPrefix(low, "data:") || strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") {
		return src
	}
	if strings.HasPrefix(src, "//") {
		return "https:" + src
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimPrefix(strings.TrimPrefix(src, "./"), "/")
}

func hubPrepareReadme(md, base string) string {
	body := strings.TrimSpace(hubFrontmatterRe.ReplaceAllString(md, ""))
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if hubChromeHeadRe.MatchString(strings.TrimSpace(line)) {
			continue
		}
		lines = append(lines, line)
	}
	body = strings.Join(lines, "\n")
	if base != "" {
		body = hubMdLinkRe.ReplaceAllStringFunc(body, func(s string) string {
			m := hubMdLinkRe.FindStringSubmatch(s)
			return "](" + hubResolveReadmeURL(m[1], base) + ")"
		})
		body = hubHtmlSrcRe.ReplaceAllStringFunc(body, func(s string) string {
			m := hubHtmlSrcRe.FindStringSubmatch(s)
			return m[1] + m[2] + hubResolveReadmeURL(m[3], base) + m[2]
		})
	}
	body = strings.TrimSpace(body)
	if len([]rune(body)) > hubReadmeChars {
		r := []rune(body)
		body = strings.TrimSpace(string(r[:hubReadmeChars])) + "\n\n---\n\nCarte tronquée. Le README complet est sur Hugging Face."
	}
	return body
}

// hubFetchReadme suit Unsloth (studio/.../hf-readme.ts) : /resolve/{branch}/README.md,
// pas /raw — les miroirs ne servent pas toujours cette route.
func hubFetchReadme(id string) (md, branch string) {
	for _, b := range []string{"main", "master"} {
		u := strings.TrimRight(hubWebBase, "/") + "/" + id + "/resolve/" + b + "/README.md"
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "loom/"+Version)
		req.Header.Set("Accept", "text/plain, text/markdown, */*")
		resp, err := hubClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, hubReadmeBytes+1))
		resp.Body.Close()
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) > hubReadmeBytes {
			data = data[:hubReadmeBytes]
		}
		return string(data), b
	}
	return "", ""
}

func hubGroupGGUF(siblings []hfSibling, repo string) (weights, vision []hubFile) {
	byName := map[string]hfSibling{}
	for _, s := range siblings {
		byName[s.Rfilename] = s
	}
	seen := map[string]bool{}
	for _, s := range siblings {
		name := path.Base(s.Rfilename)
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		if isFollowerShard(name) {
			continue
		}
		key := s.Rfilename
		if seen[key] {
			continue
		}
		seen[key] = true
		size := s.Size
		parts := 0
		if _, total, ok := shardInfo(name); ok {
			parts = total
			size = 0
			for _, n := range shardFamily(name) {
				full := strings.Replace(s.Rfilename, name, n, 1)
				if sib, ok := byName[full]; ok {
					size += sib.Size
				}
			}
		}
		f := hubFile{
			Name:   name,
			Path:   s.Rfilename,
			Size:   size,
			Quant:  hubParseQuant(name),
			URL:    "https://huggingface.co/" + repo + "/resolve/main/" + strings.TrimPrefix(s.Rfilename, "/"),
			Vision: hubIsMmproj(name),
			Parts:  parts,
		}
		if f.Vision {
			vision = append(vision, f)
		} else {
			weights = append(weights, f)
		}
	}
	return weights, vision
}

func handleHubSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSON(w, 405, map[string]any{"ok": false, "error": "GET only"})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	if author != "" && !hubValidRepo(author) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "auteur invalide"})
		return
	}
	sort := r.URL.Query().Get("sort")
	switch sort {
	case "likes", "lastModified":
	default:
		sort = "downloads"
	}
	limit := 24
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 40 {
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = n
	}

	u, err := url.Parse(hubAPIBase)
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	v := u.Query()
	v.Set("filter", "gguf")
	v.Set("sort", sort)
	v.Set("direction", "-1")
	v.Set("limit", strconv.Itoa(limit))
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if q != "" {
		v.Set("search", q)
	}
	if author != "" {
		v.Set("author", author)
	}
	pipeline := strings.TrimSpace(r.URL.Query().Get("pipeline"))
	if pipeline != "" {
		v.Set("pipeline_tag", pipeline)
	}
	u.RawQuery = v.Encode()

	resp, err := hubGET(u.String())
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "Hugging Face injoignable : " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		sendJSON(w, 502, map[string]any{"ok": false, "error": fmt.Sprintf("Hugging Face HTTP %d", resp.StatusCode)})
		return
	}
	var raw []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "réponse Hugging Face illisible"})
		return
	}
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		id, _ := m["id"].(string)
		if id == "" {
			id, _ = m["modelId"].(string)
		}
		if !hubValidRepo(id) {
			continue
		}
		author, _ := m["author"].(string)
		if author == "" {
			if i := strings.IndexByte(id, '/'); i > 0 {
				author = id[:i]
			}
		}
		modified := m["lastModified"]
		if modified == nil {
			modified = m["updatedAt"]
		}
		item := map[string]any{
			"id":        id,
			"author":    author,
			"downloads": m["downloads"],
			"likes":     m["likes"],
			"modified":  modified,
			"pipeline":  m["pipeline_tag"],
			"tags":      m["tags"],
			"params_b":  hubParseParamsB(id),
			"gated":     m["gated"],
			"private":   m["private"],
		}
		out = append(out, item)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": out})
}

func handleHubModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSON(w, 405, map[string]any{"ok": false, "error": "GET only"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !hubValidRepo(id) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "identifiant de dépôt invalide"})
		return
	}
	rawURL := strings.TrimRight(hubAPIBase, "/") + "/" + id + "?blobs=true"
	resp, err := hubGET(rawURL)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "Hugging Face injoignable : " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "dépôt introuvable ou restreint"})
		return
	}
	if resp.StatusCode != 200 {
		sendJSON(w, 502, map[string]any{"ok": false, "error": fmt.Sprintf("Hugging Face HTTP %d", resp.StatusCode)})
		return
	}
	var raw struct {
		ID           string         `json:"id"`
		Author       string         `json:"author"`
		Downloads    int            `json:"downloads"`
		Likes        int            `json:"likes"`
		LastModified string         `json:"lastModified"`
		PipelineTag  string         `json:"pipeline_tag"`
		Tags         []string       `json:"tags"`
		Siblings     []hfSibling    `json:"siblings"`
		CardData     map[string]any `json:"cardData"`
		Gated        any            `json:"gated"`
		Private      bool           `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "réponse Hugging Face illisible"})
		return
	}
	if raw.ID == "" {
		raw.ID = id
	}
	if raw.Author == "" {
		if i := strings.IndexByte(raw.ID, '/'); i > 0 {
			raw.Author = raw.ID[:i]
		}
	}
	files, vision := hubGroupGGUF(raw.Siblings, raw.ID)
	paramsB := hubParseParamsB(raw.ID)
	for _, f := range files {
		if p := hubParseParamsB(f.Name); p > paramsB {
			paramsB = p
		}
	}
	vramTotal, vramUsed, ramTotal := hubVRAMTotals()
	license := ""
	if raw.CardData != nil {
		if s, ok := raw.CardData["license"].(string); ok {
			license = s
		}
	}
	readme := ""
	if rawMD, branch := hubFetchReadme(raw.ID); rawMD != "" {
		base := strings.TrimRight(hubWebBase, "/") + "/" + raw.ID + "/resolve/" + branch + "/"
		readme = hubPrepareReadme(rawMD, base)
	}
	sendJSON(w, 200, map[string]any{
		"ok":            true,
		"id":            raw.ID,
		"author":        raw.Author,
		"downloads":     raw.Downloads,
		"likes":         raw.Likes,
		"modified":      raw.LastModified,
		"pipeline":      raw.PipelineTag,
		"tags":          raw.Tags,
		"license":       license,
		"gated":         raw.Gated,
		"private":       raw.Private,
		"params_b":      paramsB,
		"files":         files,
		"vision":        vision,
		"readme":        readme,
		"vram_total_mb": vramTotal,
		"vram_used_mb":  vramUsed,
		"ram_total_mb":  ramTotal,
	})
}

func hubSafeAvatarURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "huggingface.co" || host == "cdn-avatars.huggingface.co" || strings.HasSuffix(host, ".huggingface.co") {
		return true
	}
	if base, err := url.Parse(hubWebBase); err == nil && base.Hostname() != "" && strings.EqualFold(base.Hostname(), host) {
		return true
	}
	return false
}

func hubResolveAvatar(author string) string {
	for _, kind := range []string{"organizations", "users"} {
		raw := strings.TrimRight(hubWebBase, "/") + "/api/" + kind + "/" + url.PathEscape(author) + "/avatar"
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "loom/"+Version)
		req.Header.Set("Accept", "application/json")
		resp, err := hubClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			continue
		}
		var payload struct {
			AvatarURL string `json:"avatarUrl"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&payload)
		resp.Body.Close()
		if err != nil || payload.AvatarURL == "" || !hubSafeAvatarURL(payload.AvatarURL) {
			continue
		}
		return payload.AvatarURL
	}
	return ""
}

func handleHubAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSON(w, 405, map[string]any{"ok": false, "error": "GET only"})
		return
	}
	author := strings.TrimSpace(r.URL.Query().Get("a"))
	if !hubValidRepo(author) || strings.Contains(author, "/") {
		http.NotFound(w, r)
		return
	}
	imgURL := hubResolveAvatar(author)
	if imgURL == "" {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequest(http.MethodGet, imgURL, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req.Header.Set("User-Agent", "loom/"+Version)
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/*;q=0.8")
	resp, err := hubClient.Do(req)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "avatar injoignable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		http.NotFound(w, r)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || strings.Contains(ct, "json") || strings.HasPrefix(ct, "text/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 2<<20))
}
