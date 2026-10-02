package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// webMaxOutput borne ce qu'UN appel d'outil web injecte dans le contexte, comme
// toolMaxOutput (8000) pour le shell et mcpMaxOutput (12000) pour MCP. Sans ce
// plafond, un `web_read(limit=500)` sur une page dense pouvait pousser 25 000
// caractères d'un coup : la fenêtre partait en fumée en pleine recherche, ce qui
// déclenchait des compactages en cascade au milieu du raisonnement.
const webMaxOutput = 8000

type WebSource interface {
	Engine() string
	SearchPages(string, int) ([]SearchResult, error)
	GetPage(string, FetchOptions) (*Page, error)
	FindCached(string) *Page
}

func WebSearchTool() Tool {
	return Tool{Type: "function", Function: ToolFunction{
		Name:        "web_search",
		Description: "Search the web and return ranked results.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Query"},
				"limit": map[string]any{"type": "integer", "description": "Default 8, max 20"},
			},
			"required": []string{"query"},
		},
	}}
}

// WebOpenTool : le schéma DÉPEND du moteur web actif.
//
// Le moteur intégré n'a pas de DOM vivant : Actions / wait_for / dismiss_popups
// seraient acceptés puis ignorés en silence. Les déclarer quand même reviendrait
// à mentir au modèle — il croirait pouvoir déplier une section ou fermer un
// bandeau, constaterait que rien ne change, et réessaierait en boucle (le failure
// mode classique, cf. le garde-fou anti-boucle de chat_agent.go). On ne déclare
// donc que ce que le moteur sait réellement faire, et on annonce la limite du JS
// dans la description pour que le modèle change de source au lieu d'insister.
func WebOpenTool(source WebSource) Tool {
	desc := "Fetch a URL and return its metadata and outline, not the content; call this before web_read or web_grep."
	// La limite JavaScript vit dans la description du paramètre URL (pas dans la
	// phrase de l'outil) : c'est un avertissement de comportement, pas un exemple,
	// et il évite que le modèle réessaie en boucle une page rendue côté client.
	urlDesc := "Full URL"
	if source.Engine() == EngineGo {
		urlDesc += " (this engine reads served HTML only, no JavaScript: a client-rendered page comes back empty — switch source instead of retrying)"
	}
	props := map[string]any{
		"url":     map[string]any{"type": "string", "description": urlDesc},
		"refresh": map[string]any{"type": "boolean", "description": "Bypass the 10-minute cache (default false)"},
	}
	if source.Engine() != EngineGo {
		props["actions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "JS snippets to run on the page before extraction (expand sections, click 'show more', etc.)"}
		props["dismiss_popups"] = map[string]any{"type": "boolean", "description": "Auto-close cookie/overlay banners (default true)"}
		props["wait_for"] = map[string]any{"type": "string", "description": "CSS selector or JS expression to wait for after the actions"}
	}
	return Tool{Type: "function", Function: ToolFunction{
		Name:        "web_open",
		Description: desc,
		Parameters: map[string]any{
			"type":       "object",
			"properties": props,
			"required":   []string{"url"},
		},
	}}
}

func WebReadTool() Tool {
	return Tool{Type: "function", Function: ToolFunction{
		Name:        "web_read",
		Description: "Read a range of lines from a URL already opened with web_open.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":    map[string]any{"type": "string", "description": "URL opened with web_open"},
				"offset": map[string]any{"type": "integer", "description": "Start line (default 1)"},
				"limit":  map[string]any{"type": "integer", "description": "Default 80, max 500"},
			},
			"required": []string{"url"},
		},
	}}
}

func WebGrepTool() Tool {
	return Tool{Type: "function", Function: ToolFunction{
		Name:        "web_grep",
		Description: "Find lines matching a regex in a URL already opened with web_open.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":         map[string]any{"type": "string", "description": "URL opened with web_open"},
				"pattern":     map[string]any{"type": "string", "description": "Regex (case-insensitive)"},
				"context":     map[string]any{"type": "integer", "description": "Context lines (default 2)"},
				"max_matches": map[string]any{"type": "integer", "description": "Cap (default 30)"},
			},
			"required": []string{"url", "pattern"},
		},
	}}
}

// CapWebOutput tronque en gardant le DÉBUT (contrairement au shell, où c'est la
// fin qui porte l'info) et dit au modèle comment lire la suite proprement.
func CapWebOutput(s string) string {
	if r := []rune(s); len(r) > webMaxOutput {
		return string(r[:webMaxOutput]) +
			"\n…[truncated: response too long. Read in chunks with web_read(offset, limit) or target specific content with web_grep.]"
	}
	return s
}

func ToolWebSearch(source WebSource, args map[string]any) string {
	query, _ := args["query"].(string)
	limit := 8
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	results, err := source.SearchPages(query, limit)
	if err != nil {
		return "❌ Search failed: " + err.Error()
	}
	if len(results) == 0 {
		return fmt.Sprintf("No results for “%s”", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Search: %s\n%d DuckDuckGo result(s)\n\n", query, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return strings.TrimRight(b.String(), "\n")
}

func ToolWebOpen(source WebSource, args map[string]any) string {
	u, _ := args["url"].(string)
	opts := FetchOptions{DismissPopups: true}
	if v, ok := args["refresh"].(bool); ok {
		opts.Force = v
	}
	if v, ok := args["dismiss_popups"].(bool); ok {
		opts.DismissPopups = v
	}
	if v, ok := args["wait_for"].(string); ok {
		opts.WaitFor = v
	}
	if arr, ok := args["actions"].([]any); ok {
		for _, a := range arr {
			if s, ok := a.(string); ok {
				opts.Actions = append(opts.Actions, s)
			}
		}
	}
	entry, err := source.GetPage(u, opts)
	if err != nil {
		return "❌ " + err.Error()
	}
	total := len(entry.Lines)
	chars := total
	for _, l := range entry.Lines {
		chars += len(l)
	}
	return fmt.Sprintf("# Opened: %s\nTotal: %d lines, %s (%d characters)\nCached for 10 min. Use web_read or web_grep to read.\n\n## Outline (heading line numbers)\n```\n%s\n```",
		entry.URL, total, FormatBytes(chars), chars, ExtractOutline(entry.Lines))
}

func ToolWebRead(source WebSource, args map[string]any) string {
	u, _ := args["url"].(string)
	entry := source.FindCached(u)
	if entry == nil {
		return fmt.Sprintf("❌ Page missing from cache. Call web_open(\"%s\") first.", u)
	}
	total := len(entry.Lines)
	offset := 1
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	if offset < 1 {
		offset = 1
	}
	limit := 80
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	start := offset - 1
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	slice := entry.Lines[start:end]
	remaining := total - end
	tail := " (end of page)"
	if remaining > 0 {
		tail = fmt.Sprintf(" (%d more below)", remaining)
	}
	return fmt.Sprintf("# %s\nLines %d–%d of %d%s\n\n```\n%s\n```",
		entry.URL, offset, end, total, tail, FormatLines(slice, offset))
}

func ToolWebGrep(source WebSource, args map[string]any) string {
	u, _ := args["url"].(string)
	pattern, _ := args["pattern"].(string)
	entry := source.FindCached(u)
	if entry == nil {
		return fmt.Sprintf("❌ Page missing from cache. Call web_open(\"%s\") first.", u)
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return "❌ Invalid regex: " + err.Error()
	}
	ctx := 2
	if v, ok := args["context"].(float64); ok {
		ctx = int(v)
	}
	if ctx < 0 {
		ctx = 0
	}
	maxMatches := 30
	if v, ok := args["max_matches"].(float64); ok {
		maxMatches = int(v)
	}
	if maxMatches < 1 {
		maxMatches = 1
	}
	lines := entry.Lines
	var matchIdx []int
	for i := 0; i < len(lines) && len(matchIdx) < maxMatches; i++ {
		if re.MatchString(lines[i]) {
			matchIdx = append(matchIdx, i)
		}
	}
	if len(matchIdx) == 0 {
		return fmt.Sprintf("# %s\nNo matches for /%s/i", entry.URL, pattern)
	}
	// Fusionne les fenêtres de contexte qui se chevauchent.
	type rng struct{ s, e int }
	var ranges []rng
	for _, i := range matchIdx {
		s := i - ctx
		if s < 0 {
			s = 0
		}
		e := i + ctx
		if e > len(lines)-1 {
			e = len(lines) - 1
		}
		if n := len(ranges); n > 0 && s <= ranges[n-1].e+1 {
			if e > ranges[n-1].e {
				ranges[n-1].e = e
			}
		} else {
			ranges = append(ranges, rng{s, e})
		}
	}
	var blocks []string
	for _, r := range ranges {
		blocks = append(blocks, "```\n"+FormatLines(lines[r.s:r.e+1], r.s+1)+"\n```")
	}
	capped := ""
	if len(matchIdx) == maxMatches {
		capped = fmt.Sprintf(" (capped at %d)", maxMatches)
	}
	return fmt.Sprintf("# %s\n%d match(es) for /%s/i%s\n\n%s",
		entry.URL, len(matchIdx), pattern, capped, strings.Join(blocks, "\n\n---\n\n"))
}
