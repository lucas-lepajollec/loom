package loom

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type runtimeRun struct {
	session     RuntimeSession
	cancel      context.CancelFunc
	finalStatus string
	finalError  string
	acpBytes    int
	acpError    string
}
type runtimeSessions struct {
	shutdownOnce sync.Once
	acpMu        sync.Mutex
	acp          map[string]*acpBinding
	nativeMu     sync.Mutex
	mu           sync.Mutex
	keys         map[string]string
	balances     *providerBalanceCache
	runs         map[string]*runtimeRun
	subscribers  map[string]map[*discussionSubscriber]bool
}

func newRuntimeSessions() *runtimeSessions {
	return &runtimeSessions{acp: map[string]*acpBinding{}, keys: map[string]string{}, balances: newProviderBalanceCache(nil), runs: map[string]*runtimeRun{}, subscribers: map[string]map[*discussionSubscriber]bool{}}
}

var workspaceSessions = newRuntimeSessions()

func (m *runtimeSessions) providers() []CloudProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []CloudProvider{}
	for id := range allKV(bkProviders) {
		var p CloudProvider
		if getStoreJSON(bkProviders, id, &p) {
			p.Ready = m.keys[id] != ""
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *runtimeSessions) saveProvider(p CloudProvider, key string) (CloudProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.Name = strings.TrimSpace(p.Name)
	p.Model = strings.TrimSpace(p.Model)
	if p.UsageMode != "" && p.UsageMode != "none" {
		return p, errors.New("mode de décompte des tokens invalide")
	}
	if len(p.Models) > 32 {
		return p, errors.New("32 modèles maximum par provider")
	}
	models := []string{}
	seen := map[string]bool{}
	for _, model := range append([]string{p.Model}, p.Models...) {
		model = strings.TrimSpace(model)
		if len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") {
			return p, errors.New("identifiant de modèle trop long")
		}
		if model != "" && !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	p.Models = models
	if len(models) > 32 {
		return p, errors.New("32 modèles maximum par provider")
	}
	if p.Name == "" || len(p.Name) > 100 || p.Model == "" || len(p.Model) > 200 {
		return p, errors.New("nom et modèle requis (100 et 200 octets maximum)")
	}
	endpoint, err := validateCloudEndpoint(p.Endpoint)
	if err != nil {
		return p, err
	}
	p.Endpoint = endpoint
	if strings.ContainsAny(key, "\r\n") || len(key) > 4096 {
		return p, errors.New("clé invalide")
	}
	if p.ID == "" {
		p.ID = newSessionID()
	} else {
		var old CloudProvider
		if !getStoreJSON(bkProviders, p.ID, &old) {
			return p, errors.New("provider introuvable")
		}
		if old.Endpoint != p.Endpoint {
			return p, errors.New("créez une nouvelle connexion pour changer la destination")
		}
	}
	p.Ready = false
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		return p, errors.New("connexion non enregistrée : stockage indisponible ou verrouillé")
	}
	if strings.TrimSpace(key) != "" {
		m.keys[p.ID] = strings.TrimSpace(key)
	}
	p.Ready = m.keys[p.ID] != ""
	if err := rememberProviderKey(p.ID, m.keys[p.ID], p.Remember); err != nil {
		p.Remember = false
		_ = putStoreJSON(bkProviders, p.ID, p)
		return p, err
	}
	return p, nil
}

// disconnect forgets the key in memory and in the keychain.
func (m *runtimeSessions) disconnect(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, id)
	_ = rememberProviderKey(id, "", false)
	var p CloudProvider
	if getStoreJSON(bkProviders, id, &p) && p.Remember {
		p.Remember = false
		_ = putStoreJSON(bkProviders, id, p)
	}
}

func (m *runtimeSessions) create(projectID, providerID string, consent bool) (RuntimeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s RuntimeSession
	if providerID != "" && !consent {
		return s, errors.New("confirmez l’envoi des messages et du contexte sélectionné vers ce provider")
	}
	var p CloudProvider
	if providerID != "" && !getStoreJSON(bkProviders, providerID, &p) {
		return s, errors.New("provider introuvable")
	}
	if providerID != "" && m.keys[p.ID] == "" {
		return s, errors.New("reconnectez ce provider dans Modèles → Providers")
	}
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("projet introuvable")
		}
	}
	now := time.Now().UnixMilli()
	s = RuntimeSession{ID: newSessionID(), ProjectID: projectID, RuntimeID: "openai-compatible", ProviderID: p.ID, ProviderName: p.Name, Endpoint: p.Endpoint, Model: p.Model, Title: "Nouvelle discussion cloud", CreatedAt: now, UpdatedAt: now, Status: "idle", Messages: []Message{}}
	s.Title = "Nouvelle discussion"
	if providerID == "" {
		s.RuntimeID = "llama.cpp"
		s.ProviderName = "llama.cpp"
		s.Model = ReadConfig()["MODEL"]
	}
	return s, putStoreJSON(bkRuntimeSessions, s.ID, s)
}

func (m *runtimeSessions) getLocked(id string) (RuntimeSession, bool) {
	// Reading the stored record also checks vault access before exposing a live
	// in-memory session, so locking memory doesn't leave an API read backdoor.
	var s RuntimeSession
	if !getStoreJSON(bkRuntimeSessions, id, &s) {
		return s, false
	}
	if run := m.runs[id]; run != nil {
		return cloneRuntimeSession(run.session), true
	}
	if s.Status == "running" {
		s.Status = "interrupted"
		s.Error = "Loom a redémarré pendant la réponse. Aucun renvoi automatique."
	}
	return s, true
}
func (m *runtimeSessions) get(id string) (RuntimeSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(id)
}
func (m *runtimeSessions) list() []RuntimeSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []RuntimeSession{}
	for id := range allKV(bkRuntimeSessions) {
		if s, ok := m.getLocked(id); ok {
			s.MessageCount = len(s.Messages)
			s.Messages = nil
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

func (m *runtimeSessions) start(id, requestID, text string, expectedRevision ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 24000 || len(requestID) < 8 || len(requestID) > 100 {
		return errors.New("message requis (24000 octets maximum) et identifiant de requête valide")
	}
	s, ok := m.getLocked(id)
	if !ok {
		return errors.New("discussion introuvable ou verrouillée")
	}
	for _, seen := range s.RequestIDs {
		if seen == requestID {
			return nil
		}
	}
	if s.LastRequestID == requestID {
		return nil
	}
	if harnessLifecycle.updatingRuntime(s.RuntimeID) {
		return errors.New("une mise à jour automatique du harness est en cours ; attendez sa fin")
	}
	if m.runs[id] != nil || m.nativeRunning(s) {
		return errors.New("une réponse est déjà en cours")
	}
	if s.RuntimeID == "llama.cpp" && s.NativeArchive != "" {
		return errors.New("ce fil utilise le chat local natif ; envoyez depuis la discussion")
	}
	if len(m.runs) >= 4 {
		return errors.New("quatre réponses sont déjà en cours ; attendez leur fin")
	}
	prepared := prepareDiscussion(s, text)
	if len(expectedRevision) > 0 && (expectedRevision[0] == "" || expectedRevision[0] != prepared.Context.Revision) {
		return errors.New("le modèle, le fil ou son contexte a changé ; vérifiez le panneau Contexte puis renvoyez votre message")
	}
	if prepared.Problem != "" {
		return errors.New(prepared.Problem)
	}
	key := m.keys[s.ProviderID]
	if s.RuntimeID == "openai-compatible" && key == "" {
		return errors.New("clé absente : reconnectez le provider dans Modèles → Providers")
	}
	var adapter RuntimeAdapter
	if s.RuntimeID == "llama.cpp" {
		if s.Model == "" || !sameModelPath(s.Model, ReadConfig()["MODEL"]) {
			return errors.New("chargez le modèle sélectionné depuis le panneau Modèle avant d’envoyer")
		}
		if !healthCheck() {
			return errors.New("le modèle local n’est pas prêt ; chargez-le depuis le panneau Modèle")
		}
		for _, other := range m.runs {
			if other.session.RuntimeID == "llama.cpp" {
				return errors.New("le moteur local répond déjà dans une autre discussion")
			}
		}
		adapter = localChatRuntime()
	} else if s.RuntimeID == "openai-compatible" {
		var provider CloudProvider
		getStoreJSON(bkProviders, s.ProviderID, &provider)
		provider.ID, provider.Endpoint, provider.Model = s.ProviderID, s.Endpoint, s.Model
		adapter = cloudRuntimeAdapter{provider: provider, key: key}
	} else if registered, exists := registeredRuntimes.lookup(s.RuntimeID); exists && hasRuntimeCapability(registered.Descriptor(), "chat") {
		if acp, ok := registered.(*acpAdapter); ok {
			if !acp.agent.available() {
				return errors.New("CLI ou lanceur ACP indisponible")
			}
			check := acpDirectory
			if acp.agent.Remote {
				check = remoteWorkdir
			}
			if s.Workdir == "" {
				dir, extra := projectFolders(s.ProjectID, acp.agent)
				if dir == "" && acp.agent.Remote {
					dir = acp.agent.RemoteHome
				}
				s.Workdir = dir
				if len(s.AdditionalDirs) == 0 {
					s.AdditionalDirs = extra
				}
			}
			if _, err := check(s.Workdir); err != nil {
				return err
			}
			adapter = &acpAdapter{agent: acp.agent, sessions: m, session: cloneRuntimeSession(s)}
		} else {
			adapter = registered
		}
	} else {
		return errors.New("cet adapter ne permet pas encore d’exécuter une discussion")
	}

	messages := append(append([]Message{}, s.Messages...), Message{Role: "user", Content: text})
	if len(s.Messages) == 0 && !s.CustomTitle {
		s.Title = discussionTitle(text)
	}
	s.Messages = append(messages, Message{Role: "assistant", Content: ""})
	s.Turns = append(s.Turns, discussionTurnRecord(s, prepared, len(s.Messages)-1))
	s.Turns[len(s.Turns)-1].ReasoningEffort = s.ReasoningEffort
	s.Turns[len(s.Turns)-1].StartedAt = time.Now().UnixMilli()
	s.Status = "running"
	s.Error = ""
	s.Usage = nil
	s.LastRequestID = requestID
	s.RequestIDs = append(s.RequestIDs, requestID)
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		return errors.New("enregistrement impossible : stockage indisponible ou verrouillé")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	if _, ok := adapter.(*acpAdapter); ok {
		cancel()
		ctx, cancel = context.WithCancel(context.Background())
	}
	run := &runtimeRun{session: s, cancel: cancel}
	m.runs[id] = run
	m.publishLocked(id, DiscussionEvent{"type": "turn_start", "text": text, "portable_text": true, "provenance": s.Turns[len(s.Turns)-1], "session": cloneRuntimeSession(s), "context": discussionContext(s)})
	go m.generate(ctx, run, adapter, prepared.Messages)
	return nil
}

func (m *runtimeSessions) generate(ctx context.Context, run *runtimeRun, adapter RuntimeAdapter, messages []Message) {
	defer run.cancel()
	_, err := adapter.Run(ctx, RuntimeTurn{Messages: messages, Temperature: 0.7}, func(event StreamEvent) bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.runs[run.session.ID] != run {
			return false
		}
		if ctx.Err() != nil && event.ACPEvent != nil && event.ACPEvent["type"] != "approval_resolved" {
			return false
		}
		if ctx.Err() != nil && event.ACPEvent == nil && event.ACPState == nil {
			return false
		}
		if event.ACPState != nil {
			run.session.ACPState = cloneACPState(*event.ACPState)
			turn := &run.session.Turns[len(run.session.Turns)-1]
			turn.NativeSessionID = run.session.NativeSessionID
			for _, option := range run.session.AvailableConfigOptions {
				if option["category"] == "model" {
					if model, ok := option["currentValue"].(string); ok && len(model) <= 200 {
						turn.Model = model
					}
				}
			}
		}
		if event.ACPEvent != nil {
			e := event.ACPEvent
			encoded, _ := json.Marshal(e)
			run.acpBytes += len(encoded)
			if (run.acpBytes > 64<<20 || len(run.session.Turns[len(run.session.Turns)-1].ACPEvents) >= 16384) && e["type"] != "approval_resolved" {
				run.acpError = "Journal ACP trop volumineux ; tour arrêté, événements déjà reçus conservés."
				run.cancel()
				return false
			}
			turn := &run.session.Turns[len(run.session.Turns)-1]
			turn.ACPEvents = append(turn.ACPEvents, e)
			if e["type"] == "text_delta" {
				event.Content, _ = e["text"].(string)
			}
		}
		if event.Content != "" {
			i := len(run.session.Messages) - 1
			previous, _ := run.session.Messages[i].Content.(string)
			run.session.Messages[i].Content = previous + event.Content
		}
		if event.Usage != nil {
			u := *event.Usage
			run.session.Usage = &u
			run.session.Turns[len(run.session.Turns)-1].Usage = &u
		}
		turn := &run.session.Turns[len(run.session.Turns)-1]
		// Codex emits a readable summary, not a reconstructed hidden trace. Keep
		// it in display metadata only; never inject it into the portable prompt.
		if run.session.RuntimeID == "codex" && event.Reasoning != "" && len(turn.ReasoningSummary)+len(event.Reasoning) <= 32<<10 {
			turn.ReasoningSummary += event.Reasoning
		}
		if event.Stats != nil {
			stats := *event.Stats
			turn.Stats = &stats
		}
		if event.DurationSeconds > 0 {
			turn.DurationSeconds = event.DurationSeconds
		}
		if event.NativeSessionID != "" && len(event.NativeSessionID) <= 200 {
			turn.NativeSessionID = event.NativeSessionID
		}
		if event.HarnessEvent != nil {
			updated := false
			for i := range turn.Events {
				if turn.Events[i].Index == event.HarnessEvent.Index {
					turn.Events[i] = *event.HarnessEvent
					updated = true
					break
				}
			}
			if !updated && len(turn.Events) < 128 {
				turn.Events = append(turn.Events, *event.HarnessEvent)
			}
		}
		if event.ACPEvent != nil || event.ACPState != nil {
			if err := putStoreJSON(bkRuntimeSessions, run.session.ID, run.session); err != nil {
				run.cancel()
				return false
			}
		}
		snapshot := cloneRuntimeSession(run.session)
		m.publishLocked(run.session.ID, runtimeDiscussionEvents(event, snapshot.Turns[len(snapshot.Turns)-1])...)
		return true
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &run.session
	if run.acpError != "" {
		err = errors.New(run.acpError)
	}
	s.Status = "complete"
	s.UpdatedAt = time.Now().UnixMilli()
	// Runtimes that do not report a duration get Loom's wall-clock measure.
	if n := len(s.Turns); n > 0 && s.Turns[n-1].DurationSeconds == 0 && s.Turns[n-1].StartedAt > 0 {
		s.Turns[n-1].DurationSeconds = float64(s.UpdatedAt-s.Turns[n-1].StartedAt) / 1000
	}
	if err != nil {
		s.Status = "error"
		s.Error = err.Error()
		if errors.Is(err, context.Canceled) {
			s.Status = "cancelled"
			s.Error = "Réponse arrêtée. Le texte partiel est conservé."
		}
	}
	// Keep a failed-to-persist result in memory for recovery, instead of silently
	// losing text or reporting that it was saved. A later retry can persist it.
	if putStoreJSON(bkRuntimeSessions, s.ID, *s) != nil {
		run.finalStatus, run.finalError = s.Status, s.Error
		s.Status = "unsaved"
		s.Error = "Réponse non enregistrée : déverrouillez le stockage puis réessayez."
		m.finishDiscussionLocked(*s)
		return
	}
	m.finishDiscussionLocked(*s)
	delete(m.runs, s.ID)
}

func (m *runtimeSessions) stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run := m.runs[id]; run != nil {
		if run.session.Status == "unsaved" {
			recovered := run.session
			recovered.Status, recovered.Error = run.finalStatus, run.finalError
			if err := putStoreJSON(bkRuntimeSessions, id, recovered); err != nil {
				return err
			}
			m.publishLocked(id, DiscussionEvent{"session": cloneRuntimeSession(recovered), "context": discussionContext(recovered)})
			delete(m.runs, id)
		} else {
			run.cancel()
		}
	}
	return nil
}

func (m *runtimeSessions) remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[id] != nil {
		return errors.New("arrêtez la réponse avant de supprimer la discussion")
	}
	s, ok := m.getLocked(id)
	if !ok {
		return errors.New("discussion introuvable")
	}
	if m.nativeRunning(s) {
		return errors.New("arrêtez la réponse locale avant de supprimer la discussion")
	}
	m.closeACP(id)
	return putBytes(bkRuntimeSessions, id, nil)
}

func (m *runtimeSessions) finishDiscussionLocked(s RuntimeSession) {
	snapshot := cloneRuntimeSession(s)
	if s.Error != "" {
		m.publishLocked(s.ID, DiscussionEvent{"type": "error", "error": s.Error})
	}
	m.publishLocked(s.ID, DiscussionEvent{"type": "turn_done", "provenance": snapshot.Turns[len(snapshot.Turns)-1], "metrics": discussionMetrics(&snapshot.Turns[len(snapshot.Turns)-1]), "session": snapshot, "context": discussionContext(s)})
}
