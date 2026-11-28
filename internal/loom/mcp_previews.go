package loom

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A suggestion is inert metadata. Resolving files and granting listeners are
// separate, authenticated browser actions.
type previewItem struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"Session ID, inferred from a scoped token when omitted"`
	Kind      string `json:"kind" jsonschema:"port or file"`
	Target    string `json:"target,omitempty" jsonschema:"Saved machine ID, defaults to local"`
	Port      int    `json:"port,omitempty" jsonschema:"Loopback HTTP port, 1024 through 65535 (port previews only)"`
	Path      string `json:"path,omitempty" jsonschema:"File path or application URL path beginning with /"`
	Label     string `json:"label,omitempty" jsonschema:"Optional short display name"`
}

type previewRegistry struct {
	sync.Mutex
	items map[string][]previewItem
}

var sessionPreviews = previewRegistry{items: make(map[string][]previewItem)}
var previewIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_:.@-]{1,160}$`)

func (s *previewRegistry) list(id string) []previewItem {
	s.Lock()
	defer s.Unlock()
	return append([]previewItem{}, s.items[id]...)
}

func (s *previewRegistry) update(item previewItem, remove bool) (previewItem, error) {
	if item.Target == "" {
		item.Target = "local"
	}
	if !previewIdentifier.MatchString(item.SessionID) || !previewIdentifier.MatchString(item.Target) ||
		len(item.Label) > 160 || len(item.Path) > 2048 || strings.IndexFunc(item.Label+item.Path, unicode.IsControl) >= 0 {
		return item, errors.New("invalid preview session or metadata")
	}
	switch item.Kind {
	case "port":
		if item.Port < 1024 || item.Port > 65535 {
			return item, errors.New("invalid development port")
		}
		path, err := previewPath(item.Path)
		if err != nil {
			return item, err
		}
		item.Path = path
	case "file":
		rel, err := cleanRel(strings.TrimPrefix(item.Path, "/"))
		if err != nil || rel == "." || item.Port != 0 {
			return item, errors.New("invalid file path")
		}
	default:
		return item, errors.New("expected a port or file")
	}
	s.Lock()
	defer s.Unlock()
	items := s.items[item.SessionID]
	i := slices.IndexFunc(items, func(other previewItem) bool {
		return item.Kind == other.Kind && item.Target == other.Target && item.Port == other.Port && item.Path == other.Path
	})
	switch {
	case remove && i >= 0:
		items = slices.Delete(items, i, i+1)
	case remove:
		return item, nil
	case i >= 0:
		items[i] = item
	case len(items) >= 48 || (len(items) == 0 && len(s.items) >= 512):
		return item, errors.New("session preview limit reached")
	default:
		items = append(items, item)
	}
	if len(items) == 0 {
		delete(s.items, item.SessionID)
	} else {
		s.items[item.SessionID] = items
	}
	orchHub.publish("preview", map[string]string{"session_id": item.SessionID})
	return item, nil
}

func handleSessionPreviews(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("session_id")
		if !previewIdentifier.MatchString(id) {
			filesError(w, errors.New("invalid session id"))
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "previews": sessionPreviews.list(id)})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		previewItem
		Action string `json:"action"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Action != "" && req.Action != "add" && req.Action != "remove" {
		filesError(w, errors.New("invalid action"))
		return
	}
	item, err := sessionPreviews.update(req.previewItem, req.Action == "remove")
	if err != nil {
		filesError(w, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "preview": item})
}

func registerPreviewTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "loom_preview_open", Description: "Suggest a file path or a development port (1024–65535) in the session Preview tab. Use kind file or port and a saved machine ID or local. A scoped token supplies session_id. The user chooses what to open.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, args previewItem) (*mcp.CallToolResult, previewItem, error) {
			if scope, ok := sessionScopeFromContext(ctx); ok {
				if args.SessionID != "" && args.SessionID != scope.SessionID {
					return nil, previewItem{}, errors.New("preview belongs to another session")
				}
				args.SessionID = scope.SessionID
			}
			item, err := sessionPreviews.update(args, false)
			return nil, item, err
		})
}
