package loom

import (
	"context"
	"errors"
	"fmt"
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
	mu              sync.Mutex
	storage         brainStorage
	engine          *brain.Engine
	semantic        *brainSemantic
	itemsMu         sync.Mutex
	items           *brain.MemoryStore
	itemsDir        string
	itemsBase       string
	itemsEncrypted  bool
	distillMu       sync.Mutex
	consolidationMu sync.Mutex
	continuity      brainContinuity
	candidateWrites sync.WaitGroup
	writeRefresh    sync.WaitGroup
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
		e, err := brain.New(brain.Options{Storage: s.storage, ExcludedDirectories: s.skillsIndexExclusions, Conversations: brainConversations, Memory: brainMemory, Distilled: s.distilledDocuments, Available: brainAvailable})
		if err != nil {
			return nil, err
		}
		s.engine = e
	}
	return s.engine, nil
}
func (s *brainService) run(ctx context.Context) {
	continuityDone := make(chan struct{})
	go func() { defer close(continuityDone); s.continuityLoop(ctx) }()
	defer func() { <-continuityDone }()
	defer func() {
		s.mu.Lock()
		m := s.semantic
		s.mu.Unlock()
		if m != nil {
			m.close()
		}
	}()
	refresh := func() {
		if e, err := s.get(); err == nil {
			_ = e.Refresh(ctx)
			s.refreshSemanticIfSelected()
			syncPortableMCP()
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
	return s.SearchContext(context.Background(), r)
}
func (s *brainService) SearchContext(ctx context.Context, r brain.SearchRequest) ([]brain.Hit, error) {
	var scopeErr error
	r.Sources, r.PathPrefixes, scopeErr = scopedBrainProject(r.ProjectID, r.Sources, r.PathPrefixes)
	if scopeErr != nil {
		return nil, scopeErr
	}
	e, err := s.get()
	if err != nil {
		return nil, err
	}
	lexical, err := e.Search(r)
	if err != nil || strings.TrimSpace(r.Query) == "" {
		return lexical, err
	}
	m, err := s.semanticManager()
	if err != nil {
		return lexical, nil
	}
	cfg := m.configCopy()
	if !cfg.Enabled {
		return lexical, nil
	}
	chunks, chunkErr := e.Chunks(r)
	if chunkErr != nil {
		return nil, chunkErr
	}
	m.mu.Lock()
	ready := false
	for _, c := range chunks {
		if _, ok := m.vectors.Values[c.ID]; ok {
			ready = true
			break
		}
	}
	m.mu.Unlock()
	if !ready {
		return lexical, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	query, err := m.embed(ctx, []string{r.Query}, true)
	if err != nil {
		return lexical, brainAvailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled || m.config.Model != cfg.Model || m.config.ProviderID != cfg.ProviderID || m.config.ProviderEndpoint != cfg.ProviderEndpoint {
		return lexical, brainAvailable()
	}
	return e.HybridSearch(r, query[0], m.vectors.Values)
}
func (s *brainService) Pack(r brain.PackRequest) (brain.Pack, error) {
	return s.PackContext(context.Background(), r)
}
func (s *brainService) PackContext(ctx context.Context, r brain.PackRequest) (brain.Pack, error) {
	var scopeErr error
	r.Sources, r.PathPrefixes, scopeErr = scopedBrainProject(r.ProjectID, r.Sources, r.PathPrefixes)
	if scopeErr != nil {
		return brain.Pack{}, scopeErr
	}
	e, err := s.get()
	if err != nil {
		return brain.Pack{}, err
	}
	if r.BudgetTokens < 0 || r.BudgetTokens > 8000 {
		return brain.Pack{}, errors.New("budget_tokens must be between 1 and 8000")
	}
	hits, err := s.SearchContext(ctx, brain.SearchRequest{Query: r.Query, Sources: r.Sources, Personal: r.Personal, Limit: 100, PathPrefixes: r.PathPrefixes})
	if err != nil {
		return brain.Pack{}, err
	}
	return e.PackHits(r, hits)
}
func (s *brainService) Read(r brain.ReadRequest) (brain.Chunk, error) {
	if r.ProjectID != "" && r.Source != "" && !hasName(r.Sources, r.Source) {
		r.Sources = append(r.Sources, r.Source)
	}
	var scopeErr error
	r.Sources, r.PathPrefixes, scopeErr = scopedBrainProject(r.ProjectID, r.Sources, r.PathPrefixes)
	if scopeErr != nil {
		return brain.Chunk{}, scopeErr
	}
	e, err := s.get()
	if err != nil {
		return brain.Chunk{}, err
	}
	return e.Read(r)
}
func (s *brainService) WriteSecondBrain(r brain.WriteRequest) error {
	return secondBrainWriteSource(r.Source, r.File, r.Content)
}
func (s *brainService) EditSecondBrain(r brain.EditRequest) error {
	return secondBrainEditSource(r.Source, r.File, r.Old, r.New)
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
			Connector  string   `json:"connector"`
			Remote     string   `json:"remote"`
			Branch     string   `json:"branch"`
			Permission string   `json:"permission"`
			Primary    bool     `json:"primary"`
			Secondary  bool     `json:"secondary"`
			Exclude    []string `json:"exclude"`
			Action     string   `json:"action"`
			ID         string   `json:"id"`
			Label      string   `json:"label"`
			Path       string   `json:"path"`
			Kind       string   `json:"kind"`
			Include    []string `json:"include,omitempty"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Action == "sync" {
			var source *brain.Source
			for _, item := range e.Sources() {
				if item.ID == req.ID && !item.ReadOnly {
					copy := item
					source = &copy
					break
				}
			}
			if source == nil {
				brainResponse(w, nil, errors.New("second brain not found"))
				return
			}
			if err = syncBrainRemote(r.Context(), *source); err == nil {
				err = e.Refresh(r.Context())
			}
			brainResponse(w, map[string]any{"ok": err == nil, "sources": e.Sources()}, err)
			return
		}
		switch req.Action {
		case "add", "":
			if req.ID == "" {
				// A stable id from the name, unique among sources.
				base := skillDirSlug(firstNonEmpty(req.Label, filepath.Base(strings.TrimSuffix(req.Remote, "/")), filepath.Base(req.Path)))
				req.ID = base
				for i := 2; ; i++ {
					taken := false
					for _, existing := range e.Sources() {
						taken = taken || existing.ID == req.ID
					}
					if !taken && req.ID != "conversations" && req.ID != "memory" && req.ID != "distilled" {
						break
					}
					req.ID = fmt.Sprintf("%s-%d", base, i)
				}
			}
			if req.Connector == "git-remote" && strings.TrimSpace(req.Remote) != "" {
				for _, existing := range e.Sources() {
					if existing.ID == req.ID && !existing.ReadOnly {
						req.Path = existing.Path
						if existing.Remote != strings.TrimSpace(req.Remote) {
							err = errors.New("unlink this second brain before changing its Git remote")
						}
						break
					}
				}
				if err == nil && req.Path == "" {
					req.Path, err = cloneBrainRemote(r.Context(), req.ID, strings.TrimSpace(req.Remote), strings.TrimSpace(req.Branch))
				}
			}
			if err == nil && req.Primary {
				req.Permission = "write"
				req.Secondary = false
			}
			if err == nil {
				err = e.Update(brain.Source{ID: req.ID, Label: req.Label, Path: req.Path, Kind: req.Kind, Include: req.Include, Connector: req.Connector, Remote: strings.TrimSpace(req.Remote), Branch: strings.TrimSpace(req.Branch), Permission: req.Permission, Primary: req.Primary, Secondary: req.Secondary, Exclude: req.Exclude})
			}
			// Retain managed checkouts on rejected configuration: reconnect may
			// have reused an existing repository containing user edits.
		case "remove":
			err = e.Remove(req.ID)
		case "relabel":
			err = e.Relabel(req.ID, req.Label)
		default:
			err = errors.New("action must be add, remove, relabel or sync")
		}
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
		if skillsHomeConfig().Mode == "brain" {
			syncSkillSinksAsync()
		}
		syncPortableMCP()
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sources": e.Sources(), "refreshing": e.Refreshing(), "refresh_seconds": 180})
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
	sendJSON(w, 200, map[string]any{"ok": true, "sources": e.Sources(), "refreshing": e.Refreshing(), "refresh_seconds": 180})
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
	hits, err := s.SearchContext(r.Context(), brain.SearchRequest{Query: r.URL.Query().Get("query"), ProjectID: r.URL.Query().Get("project_id"), Sources: brainFilter(r), Personal: r.URL.Query().Get("personal") == "true", Limit: limit})
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
	pack, err := s.PackContext(r.Context(), req)
	brainResponse(w, pack, err)
}
func (s *brainService) read(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "GET") {
		return
	}
	q := r.URL.Query()
	chunk, err := s.Read(brain.ReadRequest{ChunkID: q.Get("chunk_id"), ProjectID: q.Get("project_id"), Source: q.Get("source"), Path: q.Get("path"), Heading: q["heading"], Sources: brainFilter(r), Personal: q.Get("personal") == "true"})
	brainResponse(w, chunk, err)
}

// brainSvc is the process-wide Brain (one index per LOOM_HOME).
var (
	brainSvcMu sync.Mutex
	brainSvc   *brainService
)

func theBrain() *brainService {
	brainSvcMu.Lock()
	defer brainSvcMu.Unlock()
	dir := filepath.Join(LoomHome(), "brain")
	if brainSvc == nil || brainSvc.storage.dir != dir {
		brainSvc = newBrainService(LoomHome())
	}
	return brainSvc
}

func registerBrainRoutes(mux *http.ServeMux, ctx context.Context) {
	s := theBrain()
	for route, handler := range map[string]http.HandlerFunc{"items": s.itemsHTTP, "items/update": s.updateMemoryHTTP, "items/forget": s.forgetMemoryHTTP, "sources": s.sources, "reindex": s.reindex, "search": s.search, "pack": s.pack, "read": s.read, "semantic": s.semanticHTTP, "distill": s.distillHTTP, "consolidate": s.consolidateHTTP, "consolidation": s.consolidationHTTP, "continuity": s.continuityHTTP, "continuity/run": s.continuityRunHTTP, "continuity/status": s.continuityStatusHTTP, "distilled": s.distilledHTTP, "distilled/delete": s.deleteDistilledHTTP, "distilled/review": s.reviewDistilledHTTP} {
		protected := requireWebAuth(handler)
		mux.HandleFunc("/api/brain/"+route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			protected(w, r)
		})
	}
	server := brain.MCPServer(s)
	registerMCPTransport(mux, "/mcp/brain", func(*http.Request) *mcp.Server { return server })
	registerMCPTransport(mux, "/mcp/loom", func(*http.Request) *mcp.Server { return gatewayServer(s) })
	if ctx != nil {
		go s.run(ctx)
	}
}
