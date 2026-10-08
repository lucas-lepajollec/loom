package loom

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

var errBrainVectorCache = errors.New("invalid vector cache")

type brainEmbedModel struct {
	ID             string `json:"id"`
	Repo           string `json:"repo"`
	File           string `json:"file"`
	QueryPrefix    string `json:"-"`
	DocumentPrefix string `json:"-"`
	Pooling        string `json:"-"`
}

var brainEmbedModels = []brainEmbedModel{
	{"nomic-v2", "nomic-ai/nomic-embed-text-v2-moe-GGUF", "nomic-embed-text-v2-moe.Q8_0.gguf", "search_query: ", "search_document: ", "mean"},
	{"nomic", "nomic-ai/nomic-embed-text-v1.5-GGUF", "nomic-embed-text-v1.5.Q8_0.gguf", "search_query: ", "search_document: ", "mean"},
	{"bge-small", "CompendiumLabs/bge-small-en-v1.5-gguf", "bge-small-en-v1.5-q8_0.gguf", "Represent this sentence for searching relevant passages: ", "", "cls"},
}

func brainEmbeddingModel(id string) (brainEmbedModel, error) {
	for _, m := range brainEmbedModels {
		if m.ID == id {
			return m, nil
		}
	}
	return brainEmbedModel{}, errors.New("unknown embedding model")
}

type brainSemanticConfig struct {
	AutoIndex        bool     `json:"auto_index"`
	Enabled          bool     `json:"enabled"`
	Model            string   `json:"model"`
	ProviderID       string   `json:"provider_id,omitempty"`
	ProviderEndpoint string   `json:"provider_endpoint,omitempty"`
	Consent          bool     `json:"consent"`
	Sources          []string `json:"sources,omitempty"`
	Personal         bool     `json:"personal,omitempty"`
}
type brainSemanticRequest struct {
	AutoIndex  *bool    `json:"auto_index,omitempty"`
	Action     string   `json:"action"`
	Model      string   `json:"model,omitempty"`
	ProviderID string   `json:"provider_id,omitempty"`
	Consent    bool     `json:"consent,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	Personal   bool     `json:"personal,omitempty"`
}
type brainSemanticState struct {
	brainSemanticConfig
	Models        []brainEmbedModel `json:"models"`
	ModelPresent  bool              `json:"model_present"`
	ServerRunning bool              `json:"server_running"`
	Indexing      bool              `json:"indexing"`
	Downloading   bool              `json:"downloading"`
	DownloadDone  int64             `json:"download_done,omitempty"`
	DownloadTotal int64             `json:"download_total,omitempty"`
	Embedded      int               `json:"chunks_embedded"`
	Total         int               `json:"chunks_total"`
	Error         string            `json:"error,omitempty"`
}
type brainSemantic struct {
	mu        sync.Mutex
	storage   brainStorage
	config    brainSemanticConfig
	vectors   brain.Vectors
	cmd       *exec.Cmd
	done      chan struct{}
	base      string
	idle      *time.Timer
	cancel    context.CancelFunc
	indexing  bool
	lastError string
	dl        *dlState // embedding model download in flight
}

func newBrainSemantic(storage brainStorage) (*brainSemantic, error) {
	m := &brainSemantic{storage: storage, config: brainSemanticConfig{Model: "nomic"}, vectors: brain.Vectors{Values: map[string][]float32{}}}
	b, err := storage.read("semantic.json", 128<<10)
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err = json.Unmarshal(b, &m.config); err != nil {
			return nil, err
		}
	}
	if m.config.ProviderID == "" {
		if _, err = brainEmbeddingModel(m.config.Model); err != nil {
			return nil, err
		}
	} else if !m.config.Consent || m.config.ProviderEndpoint == "" {
		return nil, errors.New("cloud embeddings require consent")
	}
	b, err = storage.read("vectors.bin", 256<<20)
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err = m.loadVectorLog(b); err != nil {
			if !errors.Is(err, errBrainVectorCache) {
				return nil, err
			}
			m.lastError = "invalid vector cache; start indexing to rebuild"
			m.vectors = brain.Vectors{Values: map[string][]float32{}}
		}
	}

	if m.vectors.Identity != m.identityLocked() {
		m.vectors = brain.Vectors{Identity: m.identityLocked(), Values: map[string][]float32{}}
	}
	return m, nil
}
func (m *brainSemantic) identityLocked() string {
	return m.config.ProviderID + "|" + m.config.ProviderEndpoint + "|" + m.config.Model
}

// Each checkpoint is a length-framed, separately encrypted vector record.
// Appending batches avoids rewriting the full index for every two chunks.
// A truncated last record after a crash is ignored; the next index request
// compacts the valid prefix atomically before appending resumed batches.
func (m *brainSemantic) loadVectorLog(data []byte) error {
	for len(data) > 0 {
		if len(data) < 4 {
			m.lastError = "interrupted vector checkpoint; indexing can resume"
			break
		}
		n := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if n <= 0 || n > 256<<20 {
			return errBrainVectorCache
		}
		if n > len(data) {
			m.lastError = "interrupted vector checkpoint; indexing can resume"
			break
		}
		raw, err := decodeMemContent(data[:n])
		if err != nil {
			return err
		}
		data = data[n:]
		var record brain.Vectors
		if err = record.UnmarshalBinary(raw); err != nil {
			return fmt.Errorf("%w: %v", errBrainVectorCache, err)
		}
		if m.vectors.Identity != "" && m.vectors.Identity != record.Identity {
			return errBrainVectorCache
		}
		m.vectors.Identity = record.Identity
		dim := 0
		for _, vec := range m.vectors.Values {
			dim = len(vec)
			break
		}
		for id, vec := range record.Values {
			if dim != 0 && dim != len(vec) {
				return errBrainVectorCache
			}
			dim = len(vec)
			m.vectors.Values[id] = vec
		}
		if len(m.vectors.Values) > brain.MaxChunks {
			return errBrainVectorCache
		}
	}
	return nil
}
func brainVectorRecord(v brain.Vectors) ([]byte, error) {
	raw, err := v.MarshalBinary()
	if err != nil {
		return nil, err
	}
	raw, err = encodeMemContent(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) > 256<<20 {
		return nil, errors.New("vector checkpoint exceeds 256 MiB")
	}
	out := make([]byte, 4, len(raw)+4)
	binary.LittleEndian.PutUint32(out, uint32(len(raw)))
	out = append(out, raw...)
	return out, nil
}
func (m *brainSemantic) saveVectorsLocked() error {
	if err := brainAvailable(); err != nil {
		return err
	}
	b, err := brainVectorRecord(m.vectors)
	if err != nil {
		return err
	}
	return m.storage.write("vectors.bin", b)
}
func (m *brainSemantic) checkpointLocked(values map[string][]float32) error {
	if err := brainAvailable(); err != nil {
		return err
	}
	dim := 0
	for _, v := range m.vectors.Values {
		dim = len(v)
		break
	}
	for _, v := range values {
		if dim != 0 && dim != len(v) {
			return errors.New("embedding model changed vector dimensions; select it again to rebuild")
		}
		dim = len(v)
	}
	b, err := brainVectorRecord(brain.Vectors{Identity: m.vectors.Identity, Values: values})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.storage.dir, "vectors.bin"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if info.Size()+int64(len(b)) > 256<<20 {
		f.Close()
		return errors.New("vector store exceeds 256 MiB")
	}
	_, writeErr := f.Write(b)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	for id, v := range values {
		m.vectors.Values[id] = v
	}
	return nil
}
func (m *brainSemantic) runningLocked() bool {
	if m.cmd == nil {
		return false
	}
	select {
	case <-m.done:
		m.cmd = nil
		m.base = ""
		return false
	default:
		return true
	}
}
func (m *brainSemantic) stopLocked() {
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	if m.cmd != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
		m.base = ""
	}
}
func (m *brainSemantic) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	m.stopLocked()
}
func (m *brainSemantic) configCopy() brainSemanticConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config
	c.Sources = append([]string{}, c.Sources...)
	return c
}
func (m *brainSemantic) state(chunks []brain.Chunk) brainSemanticState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := brainSemanticState{brainSemanticConfig: m.config, Models: brainEmbedModels, ServerRunning: m.runningLocked(), Indexing: m.indexing, Total: len(chunks), Error: m.lastError}
	for _, c := range chunks {
		if _, ok := m.vectors.Values[c.ID]; ok {
			out.Embedded++
		}
	}
	if model, err := brainEmbeddingModel(m.config.Model); err == nil && m.config.ProviderID == "" {
		info, err := os.Stat(filepath.Join(m.storage.dir, "embed", model.File))
		out.ModelPresent = err == nil && info.Mode().IsRegular() && info.Size() > 0 && m.dl == nil
	}
	if m.dl != nil {
		dlMu.Lock()
		out.Downloading, out.DownloadDone, out.DownloadTotal = true, m.dl.Done, m.dl.Total
		dlMu.Unlock()
	}
	return out
}
func brainConnectedProvider(id, endpoint string) (CloudProvider, string, error) {
	workspaceSessions.mu.Lock()
	defer workspaceSessions.mu.Unlock()
	var p CloudProvider
	if !getStoreJSON(bkProviders, id, &p) || workspaceSessions.keys[id] == "" || (endpoint != "" && p.Endpoint != endpoint) {
		return p, "", errors.New("embedding provider is disconnected or destination changed")
	}
	return p, workspaceSessions.keys[id], nil
}
func (m *brainSemantic) configure(req brainSemanticRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.indexing && req.Action != "disable" && req.Action != "auto" {
		return errors.New("indexing in progress; disable before changing settings")
	}
	next := m.config
	switch req.Action {
	case "enable":
		next.AutoIndex = false
		next.Enabled = true
		// Selecting a source is explicit. Every cloud selection requires fresh consent.
		if req.ProviderID != "" {
			if !req.Consent {
				return errors.New("consent required: note contents will leave this machine")
			}
			p, _, err := brainConnectedProvider(req.ProviderID, "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(req.Model) == "" || len(req.Model) > 200 || strings.ContainsAny(req.Model, "\r\n\x00") {
				return errors.New("embedding model required")
			}
			next.ProviderID = p.ID
			next.ProviderEndpoint = p.Endpoint
			next.Model = req.Model
			next.Consent = true
		} else if req.Model != "" || next.ProviderID == "" {
			model := req.Model
			if model == "" {
				model = "nomic"
			}
			if _, err := brainEmbeddingModel(model); err != nil {
				return err
			}
			next.Model = model
			next.ProviderID = ""
			next.ProviderEndpoint = ""
			next.Consent = false
		}
		next.Sources = append([]string{}, req.Sources...)
		next.Personal = req.Personal
	case "auto":
		if req.AutoIndex == nil || !next.Enabled {
			return errors.New("enable semantic indexing and specify auto_index")
		}
		if next.ProviderID != "" && !next.Consent {
			return errors.New("cloud embeddings require consent")
		}
		next.AutoIndex = *req.AutoIndex
		if next.AutoIndex && len(next.Sources) == 0 {
			// Freeze today's default scope. Linking a new source later must not
			// silently expand an automatic cloud transmission authorization.
			for id, kind := range brainSourceKinds() {
				if kind != "personal" {
					next.Sources = append(next.Sources, id)
				}
			}
			sort.Strings(next.Sources)
			if len(next.Sources) == 0 {
				return errors.New("select accessible sources before automatic indexing")
			}
		}
	case "disable":
		next.AutoIndex = false
		next.Enabled = false
	default:
		return errors.New("action must be enable, disable, auto, download or index")
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = m.storage.write("semantic.json", b); err != nil {
		return err
	}
	if req.Action == "disable" && m.cancel != nil {
		m.cancel()
	}
	if req.Action != "auto" {
		m.stopLocked()
	}
	m.config = next
	m.lastError = ""
	if m.vectors.Identity != m.identityLocked() {
		m.vectors = brain.Vectors{Identity: m.identityLocked(), Values: map[string][]float32{}}
		return m.saveVectorsLocked()
	}
	return nil
}

// download fetches the local embedding model in the background; progress is
// reported by state() and the lock is never held while bytes flow.
func (m *brainSemantic) download() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled || m.config.ProviderID != "" {
		return errors.New("enable a local embedding model first")
	}
	if m.dl != nil {
		return nil
	}
	model, err := brainEmbeddingModel(m.config.Model)
	if err != nil {
		return err
	}
	dir := filepath.Join(m.storage.dir, "embed")
	dest := filepath.Join(dir, model.File)
	if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	st := &dlState{}
	m.dl = st
	m.lastError = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		runDownloadSet(ctx, st, []string{"https://huggingface.co/" + model.Repo + "/resolve/main/" + model.File}, []string{dest})
		dlMu.Lock()
		errText := st.Err
		dlMu.Unlock()
		m.mu.Lock()
		m.dl = nil
		if errText != "" {
			m.lastError = errText
		}
		m.mu.Unlock()
	}()
	return nil
}
func (m *brainSemantic) startLocked(ctx context.Context) error {
	if m.runningLocked() {
		return nil
	}
	model, err := brainEmbeddingModel(m.config.Model)
	if err != nil {
		return err
	}
	path := filepath.Join(m.storage.dir, "embed", model.File)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("embedding model missing; request download first")
	}
	bin := resolvedEngineBin()
	if bin == "" {
		return errors.New("llama-server is not configured")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.New("cannot allocate embedding port")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(bin, "--model", path, "--alias", m.config.Model, "--embedding", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--ctx-size", "2048", "-ngl", "0", "--batch-size", "2048", "--ubatch-size", "2048", "--pooling", model.Pooling)
	// Do not inherit provider credentials into the local model process.
	cmd.Env = brainEmbeddingEnv()
	if err = cmd.Start(); err != nil {
		return errors.New("cannot start embedding llama-server")
	}
	m.cmd = cmd
	m.done = make(chan struct{})
	done := m.done
	go func() { _ = cmd.Wait(); close(done) }()
	m.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	readyCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(readyCtx, "GET", m.base+"/health", nil)
		resp, e := brainModelClient.Do(req)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-done:
			m.stopLocked()
			return errors.New("embedding llama-server exited before ready")
		case <-readyCtx.Done():
			m.stopLocked()
			return errors.New("embedding llama-server readiness timed out")
		case <-ticker.C:
		}
	}
}
func brainEmbeddingEnv() []string {
	// The dynamic loader and Windows runtime need the normal system paths, not
	// the application/provider environment. This is portable across all targets.
	out := []string{}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "TMPDIR", "HOME", "USERPROFILE", "LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH"} {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}

var brainModelClient = &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func brainModelPOST(ctx context.Context, url, key string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return errors.New("invalid model destination")
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	loomInferenceHeaders(req, "background")
	resp, err := brainModelClient.Do(req)
	if err != nil {
		return errors.New("model request failed or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == http.StatusServiceUnavailable {
			var response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&response) == nil && response.Error.Code == "model_busy" {
				return errEngineBusy
			}
		}
		return fmt.Errorf("model request refused (HTTP %d)", resp.StatusCode)
	}
	b, err = io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(b) > 4<<20 {
		return errors.New("model response exceeds limit or interrupted")
	}
	if json.Unmarshal(b, output) != nil {
		return errors.New("invalid model response")
	}
	return nil
}
func (m *brainSemantic) embed(ctx context.Context, texts []string, query bool) (result [][]float32, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() {
		if err != nil {
			m.lastError = err.Error()
		} else {
			m.lastError = ""
		}
	}()
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	if !m.config.Enabled {
		return nil, errors.New("semantic index disabled")
	}
	base, key := m.base, ""
	inputs := append([]string{}, texts...)
	if m.config.ProviderID != "" {
		if !m.config.Consent {
			return nil, errors.New("cloud embeddings require consent")
		}
		p, k, err := brainConnectedProvider(m.config.ProviderID, m.config.ProviderEndpoint)
		if err != nil {
			return nil, err
		}
		base = p.Endpoint
		key = k
	} else {
		if err := m.startLocked(ctx); err != nil {
			m.lastError = err.Error()
			return nil, err
		}
		base = m.base + "/v1"
		model, _ := brainEmbeddingModel(m.config.Model)
		prefix := model.DocumentPrefix
		if query {
			prefix = model.QueryPrefix
		}
		for i := range inputs {
			inputs[i] = prefix + inputs[i]
		}
		if m.idle != nil {
			m.idle.Stop()
		}
		defer func() {
			m.idle = time.AfterFunc(10*time.Minute, func() { m.mu.Lock(); defer m.mu.Unlock(); m.stopLocked() })
		}()
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	err = brainModelPOST(ctx, strings.TrimRight(base, "/")+"/embeddings", key, map[string]any{"model": m.config.Model, "input": inputs, "encoding_format": "float"}, &out)
	if err != nil {
		m.lastError = err.Error()
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, errors.New("embedding count mismatch")
	}
	vectors := make([][]float32, len(texts))
	dim := 0
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vectors) || vectors[d.Index] != nil || !brain.ValidVector(d.Embedding) || (dim != 0 && dim != len(d.Embedding)) {
			return nil, errors.New("invalid embedding vector")
		}
		dim = len(d.Embedding)
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}
func (s *brainService) semanticManager() (*brainSemantic, error) {
	if _, err := s.get(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.semantic == nil {
		m, err := newBrainSemantic(s.storage)
		if err != nil {
			return nil, err
		}
		s.semantic = m
	}
	return s.semantic, nil
}
func (s *brainService) startSemanticIndex(automatic ...bool) error {
	e, err := s.get()
	if err != nil {
		return err
	}
	m, err := s.semanticManager()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.config.Enabled {
		return errors.New("enable semantic indexing first")
	}
	if len(automatic) > 0 && automatic[0] && !m.config.AutoIndex {
		return errors.New("automatic indexing disabled")
	}
	if m.indexing {
		return errors.New("semantic indexing already running")
	}
	cfg := m.config
	if _, err = e.Chunks(brain.SearchRequest{Sources: cfg.Sources, Personal: cfg.Personal}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	m.cancel = cancel
	m.indexing = true
	go func() {
		defer cancel()
		err := func() error {
			if err := e.Refresh(ctx); err != nil {
				return err
			}
			chunks, err := e.Chunks(brain.SearchRequest{Sources: cfg.Sources, Personal: cfg.Personal})
			if err != nil {
				return err
			}
			m.mu.Lock()
			m.vectors.Prune(chunks)
			err = m.saveVectorsLocked()
			pending := []brain.Chunk{}
			for _, c := range chunks {
				if _, ok := m.vectors.Values[c.ID]; !ok {
					pending = append(pending, c)
				}
			}
			m.mu.Unlock()
			if err != nil {
				return err
			}
			// One request at a time, two chunks per batch. Each completed batch is a
			// durable checkpoint; another explicit index request resumes missing IDs.
			for i := 0; i < len(pending); i += 2 {
				if err := ctx.Err(); err != nil {
					return err
				}
				batch := pending[i:min(i+2, len(pending))]
				texts := []string{}
				for _, c := range batch {
					texts = append(texts, c.Text)
				}
				vecs, err := m.embed(ctx, texts, false)
				if err != nil {
					return err
				}
				if err := brainAvailable(); err != nil {
					return err
				}
				m.mu.Lock()
				values := map[string][]float32{}
				for j, c := range batch {
					values[c.ID] = vecs[j]
				}
				err = m.checkpointLocked(values)
				m.mu.Unlock()
				if err != nil {
					return err
				}
			}
			return nil
		}()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.indexing = false
		m.cancel = nil
		if err != nil {
			m.lastError = err.Error()
		} else {
			m.lastError = ""
		}
	}()
	return nil
}
func (s *brainService) semanticHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	m, err := s.semanticManager()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	if r.Method == "POST" {
		var req brainSemanticRequest
		if !workspaceDecode(w, r, &req) {
			return
		}
		switch req.Action {
		case "index":
			err = s.startSemanticIndex()
		case "download":
			err = m.download()
		case "disable", "auto":
			err = m.configure(req)
		default:
			e, getErr := s.get()
			if getErr != nil {
				brainResponse(w, nil, getErr)
				return
			}
			_, err = e.Chunks(brain.SearchRequest{Sources: req.Sources, Personal: req.Personal})
			if err == nil {
				err = m.configure(req)
			}
		}
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
	}
	e, getErr := s.get()
	if getErr != nil {
		brainResponse(w, nil, getErr)
		return
	}
	cfg := m.configCopy()
	chunks, err := e.Chunks(brain.SearchRequest{Sources: cfg.Sources, Personal: cfg.Personal})
	brainResponse(w, m.state(chunks), err)
}

// Reuse the checkpointed indexer. An automatic cycle only starts when the
// operator opted in and chunks are missing or obsolete. No implicit download.
func (s *brainService) refreshSemanticIfSelected() {
	m, err := s.semanticManager()
	if err != nil {
		return
	}
	cfg := m.configCopy()
	if !cfg.Enabled || !cfg.AutoIndex {
		return
	}
	e, err := s.get()
	if err != nil {
		return
	}
	chunks, err := e.Chunks(brain.SearchRequest{Sources: cfg.Sources, Personal: cfg.Personal})
	if err != nil {
		return
	}
	m.mu.Lock()
	needed := len(m.vectors.Values) != len(chunks)
	for _, c := range chunks {
		if _, ok := m.vectors.Values[c.ID]; !ok {
			needed = true
			break
		}
	}
	ready := !m.indexing
	if cfg.ProviderID == "" {
		model, modelErr := brainEmbeddingModel(cfg.Model)
		if modelErr != nil {
			ready = false
		} else {
			info, statErr := os.Stat(filepath.Join(m.storage.dir, "embed", model.File))
			ready = ready && statErr == nil && info.Mode().IsRegular()
		}
	}
	m.mu.Unlock()
	if needed && ready {
		_ = s.startSemanticIndex(true)
	}
}
