package loom

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

type brainConsolidationConfig struct {
	AutoCandidates bool `json:"auto_candidates"`
}

func (s *brainService) consolidationConfigLocked() (brainConsolidationConfig, error) {
	cfg := brainConsolidationConfig{AutoCandidates: true}
	if err := brainAvailable(); err != nil {
		return cfg, err
	}
	b, err := s.storage.read("consolidation.json", 4096)
	if err != nil || len(b) == 0 {
		return cfg, err
	}
	var stored struct {
		AutoCandidates *bool `json:"auto_candidates"`
	}
	if err = json.Unmarshal(b, &stored); err != nil {
		return cfg, err
	}
	if stored.AutoCandidates == nil {
		return cfg, errors.New("invalid consolidation settings")
	}
	cfg.AutoCandidates = *stored.AutoCandidates
	return cfg, nil
}
func (s *brainService) consolidationHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	s.consolidationMu.Lock()
	defer s.consolidationMu.Unlock()
	cfg, err := s.consolidationConfigLocked()
	if err == nil && r.Method == "POST" {
		var req struct {
			AutoCandidates *bool `json:"auto_candidates"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.AutoCandidates == nil {
			err = errors.New("auto_candidates must be a boolean")
		} else {
			cfg.AutoCandidates = *req.AutoCandidates
			var b []byte
			b, err = json.Marshal(cfg)
			if err == nil {
				err = s.storage.write("consolidation.json", b)
			}
		}
	}
	brainResponse(w, cfg, err)
}
func memoryCandidateScope(projectID string) string {
	if projectID != "" {
		return "project:" + projectID
	}
	return "global"
}
func (s *brainService) collectCandidates(session RuntimeSession, index int, text string) error {
	s.consolidationMu.Lock()
	defer s.consolidationMu.Unlock()
	cfg, err := s.consolidationConfigLocked()
	if err != nil || !cfg.AutoCandidates {
		return err
	}
	drafts := brain.CandidatesFromMessage(text)
	if len(drafts) == 0 {
		return nil
	}
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	_, err = store.AddCandidates(drafts, memoryCandidateScope(session.ProjectID), brain.MemoryProvenance{Kind: "discussion", DiscussionID: session.ID, MessageIndex: &index, Agent: session.RuntimeID})
	return err
}
func collectDiscussionCandidates(session RuntimeSession, index int, text string) {
	s := theBrain()
	metadata := RuntimeSession{ID: session.ID, ProjectID: session.ProjectID, RuntimeID: session.RuntimeID}
	s.candidateWrites.Add(1)
	go func() {
		defer s.candidateWrites.Done()
		if err := s.collectCandidates(metadata, index, text); err != nil {
			log.Printf("Brain candidate collection: %v", err)
		}
	}()
}
func collectNativeCandidates(archiveID, projectID string, index int, text string) {
	s := theBrain()
	s.candidateWrites.Add(1)
	go func() {
		defer s.candidateWrites.Done()
		session := RuntimeSession{ID: archiveID, ProjectID: projectID, RuntimeID: "llama.cpp"}
		for _, bound := range workspaceSessions.list() {
			if bound.NativeArchive == archiveID && bound.RuntimeID == "llama.cpp" {
				session.ID = bound.ID
				break
			}
		}
		if err := s.collectCandidates(session, index, text); err != nil {
			log.Printf("Brain candidate collection: %v", err)
		}
	}()
}
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
func (s *brainService) consolidateHTTP(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req struct {
		DiscussionID string `json:"discussion_id"`
		Consent      bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.DiscussionID) == "" {
		brainResponse(w, nil, errors.New("discussion_id is required"))
		return
	}
	if err := brainAvailable(); err != nil {
		brainResponse(w, nil, err)
		return
	}
	base, err := brainDistillDestination(req.Consent)
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	discussions, err := brainDistillDiscussions(ctx, brainDistillRequest{DiscussionID: req.DiscussionID})
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	var session RuntimeSession
	if !getStoreJSON(bkRuntimeSessions, req.DiscussionID, &session) {
		var archive convArchive
		if active := conv.snapshotForSession(); active != nil && active.ID == req.DiscussionID {
			archive = *active
		} else {
			getStoreJSON(bkChatHist, req.DiscussionID, &archive)
		}
		session.ProjectID, session.RuntimeID = archive.ProjectID, "llama.cpp"
	}
	result, err := brainDistillWithChat(ctx, base, engineAPIKey(), engineRequestModel(), discussions[0])
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	store, err := s.memoryStore()
	items := []brain.MemoryItem{}
	if err == nil {
		classes := map[string]string{"decision": "episodic", "fact": "semantic", "preference": "semantic", "todo": "working"}
		for _, item := range result {
			index := item.Source.MessageIndex
			var saved []brain.MemoryItem
			saved, err = store.AddCandidates([]brain.CandidateDraft{{Class: classes[item.Kind], Text: item.Text}}, memoryCandidateScope(session.ProjectID), brain.MemoryProvenance{Kind: "distilled", DiscussionID: req.DiscussionID, MessageIndex: &index, Agent: session.RuntimeID})
			if err != nil {
				break
			}
			items = append(items, saved...)
		}
	}
	brainResponse(w, map[string]any{"ok": true, "items": items}, err)
}
