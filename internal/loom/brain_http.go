package loom

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Each mux owns its Brain service; no new application global is introduced.
type brainService struct {
	mu      sync.Mutex
	storage brainStorage
	engine  *brain.Engine
}

func newBrainService(home string) *brainService {
	return &brainService{storage: brainStorage{filepath.Join(home, "brain")}}
}
func (s *brainService) get() (*brain.Engine, error) {
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		e, err := brain.New(brain.Options{Storage: s.storage, Conversations: brainConversations, Memory: brainMemory, Available: brainAvailable})
		if err != nil {
			return nil, err
		}
		s.engine = e
	}
	return s.engine, nil
}
func (s *brainService) run(ctx context.Context) {
	refresh := func() {
		if e, err := s.get(); err == nil {
			_ = e.Refresh(ctx)
		}
	}
	refresh()
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
func (s *brainService) Search(r brain.SearchRequest) ([]brain.Hit, error) {
	e, err := s.get()
	if err != nil {
		return nil, err
	}
	return e.Search(r)
}
func (s *brainService) Pack(r brain.PackRequest) (brain.Pack, error) {
	e, err := s.get()
	if err != nil {
		return brain.Pack{}, err
	}
	return e.Pack(r)
}
func (s *brainService) Read(r brain.ReadRequest) (brain.Chunk, error) {
	e, err := s.get()
	if err != nil {
		return brain.Chunk{}, err
	}
	return e.Read(r)
}
func brainResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		code := 400
		if errors.Is(err, errMemLocked) {
			code = 423
		}
		sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, value)
}
func brainFilter(r *http.Request) []string {
	out := []string{}
	for _, value := range r.URL.Query()["sources"] {
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}
func (s *brainService) sources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	e, err := s.get()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	if r.Method == "POST" {
		var req struct {
			Action  string   `json:"action"`
			ID      string   `json:"id"`
			Label   string   `json:"label"`
			Path    string   `json:"path"`
			Kind    string   `json:"kind"`
			Include []string `json:"include,omitempty"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		switch req.Action {
		case "add", "":
			err = e.Update(brain.Source{ID: req.ID, Label: req.Label, Path: req.Path, Kind: req.Kind, Include: req.Include})
		case "remove":
			err = e.Remove(req.ID)
		case "relabel":
			err = e.Relabel(req.ID, req.Label)
		default:
			err = errors.New("action must be add, remove or relabel")
		}
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sources": e.Sources()})
}
func (s *brainService) reindex(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	e, err := s.get()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	err = e.Refresh(r.Context())
	// Source-specific errors retain partial results and are also visible in GET sources.
	if err != nil {
		sendJSON(w, 200, map[string]any{"ok": false, "sources": e.Sources(), "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sources": e.Sources()})
}
func (s *brainService) search(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "GET") {
		return
	}
	limit := 0
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			brainResponse(w, nil, errors.New("invalid limit"))
			return
		}
	}
	hits, err := s.Search(brain.SearchRequest{Query: r.URL.Query().Get("query"), Sources: brainFilter(r), Personal: r.URL.Query().Get("personal") == "true", Limit: limit})
	brainResponse(w, map[string]any{"hits": hits}, err)
}
func (s *brainService) pack(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req brain.PackRequest
	if !workspaceDecode(w, r, &req) {
		return
	}
	pack, err := s.Pack(req)
	brainResponse(w, pack, err)
}
func (s *brainService) read(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "GET") {
		return
	}
	q := r.URL.Query()
	chunk, err := s.Read(brain.ReadRequest{ChunkID: q.Get("chunk_id"), Source: q.Get("source"), Path: q.Get("path"), Heading: q["heading"], Sources: brainFilter(r), Personal: q.Get("personal") == "true"})
	brainResponse(w, chunk, err)
}
func registerBrainRoutes(mux *http.ServeMux, ctx context.Context) {
	s := newBrainService(LoomHome())
	for route, handler := range map[string]http.HandlerFunc{"sources": s.sources, "reindex": s.reindex, "search": s.search, "pack": s.pack, "read": s.read} {
		protected := requireWebAuth(handler)
		mux.HandleFunc("/api/brain/"+route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			protected(w, r)
		})
	}
	server := brain.MCPServer(s)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	protected := http.NewCrossOriginProtection().Handler(transport)
	authed := requireWebAuth(protected.ServeHTTP)
	mux.HandleFunc("/mcp/brain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		authed(w, r)
	})
	if ctx != nil {
		go s.run(ctx)
	}
}
