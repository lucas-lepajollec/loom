package loom

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"
)

// Historical names delegate to the leaf; Loom retains application state.

type Tool = tools.Tool

type ToolFunction = tools.ToolFunction

func memSearchTool() Tool { return tools.MemSearchTool() }

func memReadTool() Tool { return tools.MemReadTool() }

func memAddTool() Tool { return tools.MemAddTool() }

func memEditTool() Tool { return tools.MemEditTool() }

type MemPage = tools.MemPage

type MemHit = tools.MemHit

type MemMode = tools.MemMode

const (
	MemOff      = tools.MemOff
	MemOnDemand = tools.MemOnDemand
	MemAlways   = tools.MemAlways
)

var errAlreadyApplied = tools.ErrAlreadyApplied

func memFileName(name string) (string, error) { return tools.MemFileName(name) }

func titleOf(content string) string { return tools.TitleOf(content) }

func uniqueTerms(q string) []string { return tools.UniqueTerms(q) }

func snippetAround(content string, terms []string) string { return tools.SnippetAround(content, terms) }

func MemList() []MemPage { return nativeMemory().MemList() }

func MemSearch(query string, limit int) []MemHit { return nativeMemory().MemSearch(query, limit) }

func MemRead(name string, offset, limit int) (string, error) {
	return nativeMemory().MemRead(name, offset, limit)
}

func MemAdd(name, content string) error { return nativeMemory().MemAdd(name, content) }

func MemEdit(name, oldText, newText string) error {
	return nativeMemory().MemEdit(name, oldText, newText)
}

func MemContent(name string) string { return nativeMemory().MemContent(name) }

func MemSave(name, old, content string) error { return nativeMemory().MemSave(name, old, content) }

func MemDelete(name string) error { return nativeMemory().MemDelete(name) }

func safeMemPath(name string) (string, error) { return nativeMemory().SafePath(name) }

type nativeMemoryStore struct{}

func (nativeMemoryStore) Directory() string                        { return memoryDir() }
func (nativeMemoryStore) PageText(name string) (string, bool)      { return memPageText(name) }
func (nativeMemoryStore) ReadPage(name string) ([]byte, error)     { return memReadPage(name) }
func (nativeMemoryStore) WriteFile(name string, data []byte) error { return writeMemFile(name, data) }
func (nativeMemoryStore) LockedError() error                       { return errMemLocked }
func nativeMemory() tools.Memory                                   { return tools.Memory{Store: nativeMemoryStore{}} }

func normalizeCrawlURL(raw string) string { return tools.NormalizeCrawlURL(raw) }

func normalizeLines(raw string) []string { return tools.NormalizeLines(raw) }

func extractOutline(lines []string) string { return tools.ExtractOutline(lines) }

func formatLines(lines []string, startLine int) string { return tools.FormatLines(lines, startLine) }

func formatBytes(n int) string { return tools.FormatBytes(n) }

func decodeHTMLEntities(s string) string { return tools.DecodeHTMLEntities(s) }

func decodeUddg(raw string) string { return tools.DecodeUddg(raw) }

type crwlOptions = tools.CrawlOptions

type crwlResult tools.CrawlResult

func (r crwlResult) markdown(raw bool) string { return tools.CrawlResult(r).MarkdownText(raw) }

type searchResult = tools.SearchResult

const autoDismissJS = tools.AutoDismissJS
const goFetchTimeout = tools.GoFetchTimeout

func runCrwl(target string, opts crwlOptions) (string, error) {
	return tools.RunCrwl(nativeCrawlSource{}, http.DefaultClient, target, opts)
}

type nativeCrawlSource struct{}

func (nativeCrawlSource) CrawlURL() string { return crawl4aiURL() }
func (nativeCrawlSource) CrawlKey() string { return crawl4aiKey() }

func duckduckgoSearch(query string, limit int) ([]searchResult, error) {
	return tools.DuckduckgoSearch(nativeWebSource{}, query, limit)
}

func goFetch(ctx context.Context, target string) (body []byte, contentType, finalURL string, err error) {
	return tools.GoFetch(goHTTPClient, ctx, target)
}

func goParseHTML(body []byte, contentType string) (*html.Node, error) {
	return tools.GoParseHTML(body, contentType)
}

func goFetchMarkdown(target string) (string, error) {
	return tools.GoFetchMarkdown(goHTTPClient, target)
}

func goArticleMarkdown(doc *html.Node, base *url.URL) (md, title string) {
	return tools.GoArticleMarkdown(doc, base)
}

func goConvert(node *html.Node, baseURL string) (string, error) {
	return tools.GoConvert(node, baseURL)
}

func goCloneDoc(doc *html.Node) (*html.Node, error) { return tools.GoCloneDoc(doc) }

func baseString(u *url.URL) string { return tools.BaseString(u) }

func goSearch(query string, limit int) ([]searchResult, error) {
	return tools.GoSearch(goHTTPClient, query, limit)
}

func goHasClass(n *html.Node, class string) bool { return tools.GoHasClass(n, class) }

func goAttr(n *html.Node, name string) string { return tools.GoAttr(n, name) }

func goFindByClass(root *html.Node, class string) []*html.Node {
	return tools.GoFindByClass(root, class)
}

func goFirstByClass(root *html.Node, class string) *html.Node {
	return tools.GoFirstByClass(root, class)
}

func goText(n *html.Node) string { return tools.GoText(n) }

type cacheEntry = tools.Page

type fetchOptions = tools.FetchOptions

func cacheKeyFor(u string, opts fetchOptions) string { return tools.CacheKeyFor(u, opts) }

func webSearchTool() Tool { return tools.WebSearchTool() }

func webOpenTool() Tool { return tools.WebOpenTool(nativeWebSource{}) }

func webReadTool() Tool { return tools.WebReadTool() }

func webGrepTool() Tool { return tools.WebGrepTool() }

func capWebOutput(s string) string { return tools.CapWebOutput(s) }

func toolWebSearch(args map[string]any) string { return tools.ToolWebSearch(nativeWebSource{}, args) }

func toolWebOpen(args map[string]any) string { return tools.ToolWebOpen(nativeWebSource{}, args) }

func toolWebRead(args map[string]any) string { return tools.ToolWebRead(nativeWebSource{}, args) }

func toolWebGrep(args map[string]any) string { return tools.ToolWebGrep(nativeWebSource{}, args) }

type nativeWebSource struct{}

func (nativeWebSource) Engine() string { return webEngine() }
func (nativeWebSource) Search(query string, limit int) ([]tools.SearchResult, error) {
	return goSearch(query, limit)
}
func (nativeWebSource) Crawl(target string, opts tools.CrawlOptions) (string, error) {
	return runCrwl(target, opts)
}
func (nativeWebSource) SearchPages(query string, limit int) ([]tools.SearchResult, error) {
	return configuredWebSearch(context.Background(), query, limit)
}
func (nativeWebSource) GetPage(url string, opts tools.FetchOptions) (*tools.Page, error) {
	return getPage(url, opts)
}
func (nativeWebSource) FindCached(url string) *tools.Page { return findCached(url) }

func tailRunes(s string, n int) string { return tools.TailRunes(s, n) }

func mcpSanitize(s string) string { return tools.MCPSanitize(s) }

func mcpExposedName(server, tool string) string { return tools.MCPExposedName(server, tool) }

func mcpNormalizeSchema(schema any) map[string]any { return tools.MCPNormalizeSchema(schema) }

func mcpArgLabel(args map[string]any) string { return tools.MCPArgLabel(args) }

func isMCPTool(name string) bool { return tools.IsMCPTool(name) }

func flattenMCPContent(res *mcpsdk.CallToolResult) string { return tools.FlattenMCPContent(res) }

// Native subprocess setup remains application/platform owned.
func mcpConnect(ctx context.Context, name string, cfg MCPServerConfig) (*mcpsdk.ClientSession, error) {
	var cmd *exec.Cmd
	if cfg.Transport() == "stdio" {
		cmd = hideCmd(exec.Command(cfg.Command, cfg.Args...))
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	return tools.ConnectMCP(ctx, name, cfg, Version, cmd, &http.Client{})
}

// The pool instance is owned by Loom; session/protocol logic has no globals.
type mcpSession = tools.MCPSession
type mcpManager struct{ *tools.MCPManager }
type MCPServerStatus = tools.MCPServerStatus

func (m *mcpManager) ensure(name string, cfg MCPServerConfig) *mcpSession { return m.Ensure(name, cfg) }
func mcpInvalidate(name string)                                           { mcpMgr.Invalidate(name) }
func mcpCloseAll()                                                        { mcpMgr.CloseAll() }
func mcpEnsureAll(servers map[string]MCPServerConfig)                     { mcpMgr.EnsureAll(servers) }
func mcpTools() []Tool                                                    { return mcpMgr.Tools() }
func mcpCall(name string, args map[string]any) string                     { return mcpMgr.Call(name, args) }
func mcpPromptLine() string                                               { return mcpMgr.PromptLine() }
func MCPStatus() ([]MCPServerStatus, error)                               { return mcpMgr.Status() }
