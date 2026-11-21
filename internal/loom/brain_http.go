package loom

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Each mux owns its Brain service; no new application global is introduced.
type brainService struct {
	mu             sync.Mutex
	storage        brainStorage
	engine         *brain.Engine
	semantic       *brainSemantic
	itemsMu        sync.Mutex
	items          *brain.MarkdownStore
	itemsDir       string
	itemsEncrypted bool
	consolidation  memoryConsolidation
	transcriptMu   sync.Mutex
	transcriptTail chan struct{}
	writeRefresh   sync.WaitGroup
	leaveJobs      sync.WaitGroup
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
		e, err := brain.New(brain.Options{Storage: s.storage, ExcludedDirectories: s.brainIndexExclusions, Conversations: s.discussionDocuments, Memory: s.memoryDocuments, Distilled: s.distilledDocuments, Available: brainAvailable})
		if err != nil {
			return nil, err
		}
		s.engine = e
	}
	return s.engine, nil
}
func (s *brainService) run(ctx context.Context) {
	defer func() {
		s.cancelMemoryConsolidation()
		s.leaveJobs.Wait()
		transcriptJobs.Wait()
		s.writeRefresh.Wait()
	}()
	consolidationDone := make(chan struct{})
	go func() { defer close(consolidationDone); s.memoryConsolidationLoop(ctx) }()
	defer func() { <-consolidationDone }()
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
	api := webAPI(mux)
	api("/api/brain/memory/consolidate", s.memoryConsolidateHTTP)
	api("/api/brain/memory/status", s.memoryStatusHTTP)
	api("/api/brain/memory/settings", s.memorySettingsHTTP)
	api("/api/brain/semantic", s.semanticHTTP)
	server := brain.MCPServer(s)
	server.AddReceivingMiddleware(gatewayPolicyMiddleware(nil))
	registerMCPTransport(mux, "/mcp/brain", func(*http.Request) *mcp.Server { return server })
	registerMCPTransport(mux, "/mcp/loom", func(*http.Request) *mcp.Server { return gatewayServer(s) })
	if ctx != nil {
		go s.run(ctx)
	}
}
