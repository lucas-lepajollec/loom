package loom

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

type memoryStatus struct {
	Running        bool                    `json:"running"`
	LastRun        int64                   `json:"last_run"`
	LastError      string                  `json:"last_error"`
	LastOperations []brain.MemoryOperation `json:"last_operations"`
	DiscussionID   string                  `json:"discussion_id,omitempty"`
}
type memoryConsolidation struct {
	mu     sync.Mutex
	status memoryStatus
	cancel context.CancelFunc
	left   map[string]bool
	viewed string
}
type memoryCheckpoint struct {
	Bytes  int    `json:"bytes"`
	Prefix string `json:"prefix"`
}

const bkMemoryConsolidation = "brain_memory_consolidation"

func brainDistillDestination(consent bool) (string, error) {
	base := engineBase()
	u, err := url.Parse(base)
	if err != nil {
		return "", errors.New("invalid chat engine destination")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" && !consent {
		return "", errors.New("consent required to send discussion text to the linked engine")
	}
	return base, nil
}
func discussionGenerating() bool {
	workspaceSessions.mu.Lock()
	busy := len(workspaceSessions.runs) > 0 || len(workspaceSessions.preparing) > 0
	workspaceSessions.mu.Unlock()
	conv.mu.Lock()
	busy = busy || conv.Generating
	conv.mu.Unlock()
	return busy
}

var memoryLoadedModel = func() string {
	if !serviceIsActive() || currentEngineNode() != nil {
		return ""
	}
	models, err := serviceResidentModels()
	if err != nil {
		return ""
	}
	current := llamaRouter().CurrentName()
	for _, model := range models {
		if model == current {
			return model
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func memoryResidentMatches(loaded, selected string) bool {
	if loaded == selected || sameModelPath(loaded, selected) {
		return true
	}
	for _, entry := range loadRouterEntries() {
		if entry.Name == loaded {
			for _, option := range entry.Options {
				if (option[0] == "m" || option[0] == "model") && sameModelPath(option[1], selected) {
					return true
				}
			}
		}
	}
	return false
}

// Inspect the existing linked route without loading a catalog model. A router
// advertises unloaded models too, so presence alone is insufficient there.
func memoryLinkedModel(n *engineNode, selected string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if n.Direct {
		var list struct {
			Data []struct {
				ID     string `json:"id"`
				Status *struct {
					Value string `json:"value"`
				} `json:"status"`
			} `json:"data"`
		}
		code, err := directGET(ctx, n.V1, "/v1/models", n.APIKey, &list)
		if err != nil || code != http.StatusOK {
			return ""
		}
		for _, item := range list.Data {
			if item.ID == selected && ((!n.Router && item.Status == nil) || (item.Status != nil && item.Status.Value == "loaded")) {
				return item.ID
			}
		}
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(n.URL, "/")+"/api/status", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var status struct {
		Active bool   `json:"active"`
		Health bool   `json:"health"`
		Model  string `json:"model"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status) == nil && status.Active && status.Health && memoryResidentMatches(status.Model, selected) {
		return "loom"
	}
	return ""
}

func memoryOwnedDestination(loaded string) (string, string, string) {
	model := loaded
	if !serviceRouterMode() {
		model = "loom"
	}
	// Bypass Loom's public proxy, whose normal role includes waking an engine.
	return llamaBackendURL().String() + "/v1/chat/completions", backendInferenceKey(""), model
}

// A provider/model pair is stored as JSON. Consent is bound to its exact
// endpoint and model, rather than a blanket permission for any provider.
type memoryModelPair struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}
type memoryModelConsent struct {
	ProviderID string `json:"provider_id"`
	Endpoint   string `json:"endpoint"`
	Model      string `json:"model"`
}

func memoryDestination(session RuntimeSession) (endpoint, key, model string, err error) {
	cfg := ReadConfig()
	choice := cfg["brain.consolidation_model"]
	if choice == "" {
		choice = "discussion"
	}
	if choice == "off" {
		return
	}
	if choice == "discussion" && loomTranscript(session) {
		model = session.Model
		if session.RuntimeID == "openai-compatible" {
			var p CloudProvider
			p, key, err = brainConnectedProvider(session.ProviderID, session.Endpoint)
			if err != nil {
				return "", "", "", nil
			}
			endpoint = strings.TrimRight(p.Endpoint, "/") + "/chat/completions"
			return
		}
		// Only the discussion's currently loaded model can be used. This path
		// never starts, swaps or loads an inference engine.
		base, e := brainDistillDestination(cfg["brain.consolidation_local_consent"] == engineBase())
		if e != nil {
			return "", "", "", nil
		}
		if n := currentEngineNode(); n != nil {
			model = memoryLinkedModel(n, model)
			if model == "" {
				return "", "", "", nil
			}
			return strings.TrimRight(base, "/") + "/v1/chat/completions", n.APIKey, model, nil
		}
		loaded := memoryLoadedModel()
		if loaded == "" || model == "" || !memoryResidentMatches(loaded, model) {
			return "", "", "", nil
		}
		endpoint, key, model = memoryOwnedDestination(loaded)
		return
	}
	if choice == "discussion" {
		choice = cfg["brain.consolidation_fallback"]
		if choice == "" || choice == "discussion" {
			return
		}
	}
	if choice == "off" {
		return
	}
	if choice == "local-loaded" {
		model = memoryLoadedModel()
		if model != "" {
			endpoint, key, model = memoryOwnedDestination(model)
		}
		return
	}
	var pair memoryModelPair
	if err = json.Unmarshal([]byte(choice), &pair); err != nil || pair.ProviderID == "" || pair.Model == "" {
		return "", "", "", errors.New("brain.consolidation_model must be discussion, local-loaded, off or a provider/model JSON pair")
	}
	var p CloudProvider
	p, key, err = brainConnectedProvider(pair.ProviderID, "")
	if err != nil {
		return "", "", "", nil
	}
	var consent memoryModelConsent
	_ = json.Unmarshal([]byte(cfg["brain.consolidation_consent"]), &consent)
	if consent.ProviderID != pair.ProviderID || consent.Endpoint != p.Endpoint || consent.Model != pair.Model {
		return "", "", "", nil
	}
	return strings.TrimRight(p.Endpoint, "/") + "/chat/completions", key, pair.Model, nil
}

var memoryModelCall = func(ctx context.Context, endpoint, key, model, input string) (string, error) {
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := brainModelPOST(ctx, endpoint, key, map[string]any{"model": model, "temperature": 0.1, "max_tokens": 8192, "stream": false, "messages": []map[string]string{{"role": "system", "content": brain.ConsolidationInstructions}, {"role": "user", "content": input}}}, &out)
	if err != nil {
		return "", err
	}
	if len(out.Choices) != 1 {
		return "", errors.New("missing consolidation response")
	}
	return out.Choices[0].Message.Content, nil
}

func consolidationPrefix(text string) string {
	hash := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", hash)
}
func boundedBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text
}
func consolidationInput(t brain.Transcript, part string, indexes brain.MemoryIndexes) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Discussion: %s; project: %s\nNEW transcript portion:\n%s\n", t.ID, t.ProjectID, part)
	type scopedMemory struct {
		scope string
		files brain.MemoryFiles
		index string
	}
	memories := []scopedMemory{{scope: "global", files: indexes.Global}}
	if indexes.Project != nil {
		memories = append(memories, scopedMemory{scope: "project", files: *indexes.Project})
	}
	// Reserve both indexes before spending the separate topic-file allowance.
	for i := range memories {
		memories[i].index, _ = brain.BoundMemoryIndex(memories[i].files.Index)
		fmt.Fprintf(&b, "\n%s MEMORY.md:\n%s", memories[i].scope, memories[i].index)
	}
	remaining := 32 << 10
	for _, memory := range memories {
		for _, file := range memory.files.Items {
			if file.Malformed || !strings.Contains(memory.index, "("+file.File+")") {
				continue
			}
			entry := "\n" + memory.scope + "/" + file.File + " (" + file.Type + ", " + file.Name + "):\n" + file.Text + "\n"
			if len(entry) > remaining {
				continue
			}
			b.WriteString(entry)
			remaining -= len(entry)
		}
	}
	return b.String()
}
func (s *brainService) runMemoryConsolidation(ctx context.Context, id string) (status memoryStatus, err error) {
	if err = brainAvailable(); err != nil {
		return
	}
	c := &s.consolidation
	c.mu.Lock()
	if c.status.Running {
		c.mu.Unlock()
		return status, errors.New("memory consolidation already running")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	c.cancel = cancel
	c.status.Running = true
	c.mu.Unlock()
	defer cancel()
	ran := false
	operations := []brain.MemoryOperation{}
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cancel = nil
		c.status.Running = false
		if ran || err != nil {
			c.status.LastRun = time.Now().UnixMilli()
			c.status.LastError = ""
			if err != nil {
				c.status.LastError = err.Error()
			}
			c.status.LastOperations = operations
			c.status.DiscussionID = id
			data, _ := json.Marshal(c.status)
			if encoded, e := encodeMemContent(data); e == nil {
				_ = s.storage.write("memory-status.json", encoded)
			}
		}
		status = c.status
	}()
	if discussionGenerating() {
		return status, nil
	}
	discussions, err := memoryDiscussions(ctx)
	if err != nil {
		return status, err
	}
	var d memoryDiscussion
	found := false
	for _, candidate := range discussions {
		if candidate.Session.ID == id {
			d = candidate
			found = true
			break
		}
	}
	if !found {
		return status, errors.New("discussion not found")
	}
	body := transcriptBody(d.Transcript)
	var checkpoint memoryCheckpoint
	if raw, e := getBytesErr(bkMemoryConsolidation, id); e != nil {
		return status, e
	} else if len(raw) > 0 {
		raw, e = decodeMemContent(raw)
		if e != nil {
			return status, e
		}
		if e = json.Unmarshal(raw, &checkpoint); e != nil {
			return status, e
		}
	}
	start := checkpoint.Bytes
	if start < 0 || start > len(body) || consolidationPrefix(body[:start]) != checkpoint.Prefix {
		start = 0
	}
	if start == len(body) {
		return status, nil
	}
	part := boundedBytes(body[start:], 24<<10)
	endpoint, key, model, err := memoryDestination(d.Session)
	if err != nil || endpoint == "" || model == "" {
		return status, err
	}
	indexes, err := s.MemoryIndex(brain.MemoryIndexRequest{ProjectID: d.Session.ProjectID})
	if err != nil {
		return status, err
	}
	if discussionGenerating() {
		return status, nil
	}
	in := sessionPolicyInput(d.Session, "memory.consolidate", policy.Allow)
	in.Endpoint, in.Model = strings.TrimSuffix(endpoint, "/chat/completions"), model
	for _, provider := range workspaceSessions.providers() {
		if strings.TrimRight(provider.Endpoint, "/") == in.Endpoint {
			in.ProviderID = provider.ID
		}
	}
	if err := workspaceSessions.authorizePolicy(ctx, in, false); err != nil {
		return status, err
	}
	ctx = withPolicySession(ctx, workspaceSessions, d.Session)
	ran = true
	reply, err := memoryModelCall(ctx, endpoint, key, model, consolidationInput(d.Transcript, part, indexes))
	if errors.Is(err, errEngineBusy) {
		ran = false
		return status, nil
	}
	if err != nil {
		return status, err
	}
	proposed, err := brain.ParseMemoryOperations(reply)
	if err != nil {
		return status, err
	}
	if err = ctx.Err(); err != nil {
		return status, err
	}
	if discussionGenerating() {
		return status, nil
	}
	current, err := memoryDiscussions(ctx)
	if err != nil {
		return status, err
	}
	unchanged := false
	for _, now := range current {
		if now.Session.ID == id {
			unchanged = reflect.DeepEqual(now, d)
		}
	}
	if !unchanged {
		return status, nil
	}
	currentIndexes, e := s.MemoryIndex(brain.MemoryIndexRequest{ProjectID: d.Session.ProjectID})
	if e != nil {
		return status, e
	}
	if !reflect.DeepEqual(currentIndexes, indexes) {
		return status, nil
	}
	store, err := s.memoryStore()
	if err != nil {
		return status, err
	}
	// Validate the complete batch against the snapshot before any write. Do not
	// accept model-invented filenames or delete a human-edited malformed file.
	scopes := make([]string, len(proposed))
	touched := map[string]bool{}
	for i, op := range proposed {
		scope := op.Scope
		if scope == "project" {
			if d.Session.ProjectID == "" {
				return status, errors.New("project operation without project")
			}
			scope = "project:" + d.Session.ProjectID
		}
		scopes[i] = scope
		target := scope + "/" + op.File
		if op.File != "" {
			if touched[target] {
				return status, errors.New("multiple operations target the same memory file")
			}
			touched[target] = true
		}
		if op.File != "" {
			old, e := store.Read(brain.MemoryRead{Scope: scope, File: op.File})
			if op.Op == "create" {
				if e == nil {
					return status, errors.New("create targets an existing file; use update")
				}
				if !errors.Is(e, os.ErrNotExist) {
					return status, e
				}
			} else if e != nil {
				return status, e
			}
			if old.Malformed {
				return status, errors.New("malformed memory cannot be changed")
			}
		}
	}
	for i, op := range proposed {
		if err = ctx.Err(); err != nil {
			return status, err
		}
		if op.Op == "delete" {
			err = store.Delete(brain.MemoryRead{Scope: scopes[i], File: op.File})
		} else {
			var saved brain.MemoryFile
			saved, err = store.Write(brain.MemoryWrite{Scope: scopes[i], File: op.File, Name: op.Name, Description: op.Description, Type: op.Type, Text: op.Text}, map[string]any{"discussion_id": id, "date": time.Now().UTC().Format(time.RFC3339)})
			op.File = saved.File
		}
		if err != nil {
			return status, err
		}
		operations = append(operations, op)
	}
	if err = putStoreJSON(bkMemoryConsolidation, id, memoryCheckpoint{start + len(part), consolidationPrefix(body[:start+len(part)])}); err != nil {
		return status, err
	}
	if len(operations) > 0 {
		s.refreshMemoryIndex()
	}
	return status, nil
}
func (s *brainService) memoryConsolidationLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.consolidatePaused(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *brainService) consolidatePaused(ctx context.Context, now time.Time) {
	if discussionGenerating() {
		return
	}
	discussions, err := memoryDiscussions(ctx)
	if err != nil {
		return
	}
	for _, d := range discussions {
		s.consolidation.mu.Lock()
		left := s.consolidation.left[d.Session.ID]
		s.consolidation.mu.Unlock()
		if !left && (d.Transcript.UpdatedAt <= 0 || now.Sub(time.UnixMilli(d.Transcript.UpdatedAt)) < 5*time.Minute) {
			continue
		}
		if _, err = s.runMemoryConsolidation(ctx, d.Session.ID); err != nil || ctx.Err() != nil {
			return
		}
		s.consolidation.mu.Lock()
		delete(s.consolidation.left, d.Session.ID)
		s.consolidation.mu.Unlock()
	}
}

// Reading a different discussion indicates that the user left the prior one.
func (s *brainService) discussionViewed(id string) {
	s.consolidation.mu.Lock()
	previous := s.consolidation.viewed
	if previous != "" && previous != id {
		if s.consolidation.left == nil {
			s.consolidation.left = map[string]bool{}
		}
		s.consolidation.left[previous] = true
	}
	s.consolidation.viewed = id
	s.consolidation.mu.Unlock()
	if previous != "" && previous != id {
		s.leaveJobs.Add(1)
		go func() {
			defer s.leaveJobs.Done()
			s.consolidatePaused(context.Background(), time.Now())
		}()
	}
}
func (s *brainService) cancelMemoryConsolidation() {
	s.consolidation.mu.Lock()
	if s.consolidation.cancel != nil {
		s.consolidation.cancel()
	}
	s.consolidation.mu.Unlock()
}
func (s *brainService) memoryConsolidateHTTP(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req struct {
		DiscussionID string `json:"discussion_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.DiscussionID) == "" {
		brainResponse(w, nil, errors.New("discussion_id required"))
		return
	}
	status, err := s.runMemoryConsolidation(r.Context(), req.DiscussionID)
	brainResponse(w, status, err)
}
func (s *brainService) memoryStatusHTTP(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "GET") {
		return
	}
	if err := brainAvailable(); err != nil {
		brainResponse(w, nil, err)
		return
	}
	s.consolidation.mu.Lock()
	status := s.consolidation.status
	if status.LastRun == 0 && !status.Running {
		if b, err := s.storage.read("memory-status.json", 1<<20); err == nil && len(b) > 0 {
			if b, err = decodeMemContent(b); err == nil {
				_ = json.Unmarshal(b, &status)
			}
		}
	}
	status.LastOperations = append([]brain.MemoryOperation{}, status.LastOperations...)
	s.consolidation.mu.Unlock()
	brainResponse(w, status, nil)
}

// GET/POST /api/brain/memory/settings: which model consolidates memory.
// model/fallback: "discussion" | "local-loaded" | "off" | {provider_id, model}.
// Choosing a provider pair is the consent to send transcripts to it.
func (s *brainService) memorySettingsHTTP(w http.ResponseWriter, r *http.Request) {
	type setting struct {
		Model    json.RawMessage `json:"model"`
		Fallback json.RawMessage `json:"fallback"`
	}
	encode := func(v string) json.RawMessage {
		if v == "" {
			v = "discussion"
		}
		if strings.HasPrefix(v, "{") {
			return json.RawMessage(v)
		}
		b, _ := json.Marshal(v)
		return b
	}
	if r.Method == http.MethodPost {
		var req setting
		if !workspaceDecode(w, r, &req) {
			return
		}
		apply := func(key string, raw json.RawMessage) error {
			if len(raw) == 0 {
				return nil
			}
			var word string
			if json.Unmarshal(raw, &word) == nil {
				if word != "discussion" && word != "local-loaded" && word != "off" {
					return errors.New("model must be discussion, local-loaded, off or a provider/model pair")
				}
				return SetConfigKey(key, word)
			}
			var pair memoryModelPair
			if err := json.Unmarshal(raw, &pair); err != nil || pair.ProviderID == "" || pair.Model == "" {
				return errors.New("provider pair needs provider_id and model")
			}
			p, _, err := brainConnectedProvider(pair.ProviderID, "")
			if err != nil {
				return err
			}
			in := policy.Input{Subject: "memory.consolidate", MachineID: "local", ProviderID: pair.ProviderID, Endpoint: p.Endpoint, Model: pair.Model, Operation: "select", Fallback: policy.Allow}
			if err := workspaceSessions.authorizePolicy(r.Context(), in, false); err != nil {
				return err
			}
			b, _ := json.Marshal(pair)
			c, _ := json.Marshal(memoryModelConsent{ProviderID: pair.ProviderID, Endpoint: p.Endpoint, Model: pair.Model})
			if err := SetConfigKey(key, string(b)); err != nil {
				return err
			}
			return SetConfigKey("brain.consolidation_consent", string(c))
		}
		if err := apply("brain.consolidation_model", req.Model); err != nil {
			brainResponse(w, nil, err)
			return
		}
		if err := apply("brain.consolidation_fallback", req.Fallback); err != nil {
			brainResponse(w, nil, err)
			return
		}
	} else if !workspaceMethod(w, r, "GET") {
		return
	}
	cfg := ReadConfig()
	brainResponse(w, setting{Model: encode(cfg["brain.consolidation_model"]), Fallback: encode(cfg["brain.consolidation_fallback"])}, nil)
}
