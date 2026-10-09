package loom

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type runtimeRun struct {
	session     RuntimeSession
	cancel      context.CancelFunc
	finalStatus string
	finalError  string
	acpBytes    int
	acpError    string
	providerKey string
	toolPending bool
}
type runtimeSessions struct {
	policyConsents policyConsents
	policyWorkers  sync.WaitGroup
	events         *events.Bus
	health         domainHealth
	notify         *notificationService
	shutdownOnce   sync.Once
	acpMu          sync.Mutex
	acp            map[string]*acpBinding
	requests       map[string]*agent.RequestBroker
	providerMu     sync.Mutex
	nativeMu       sync.Mutex
	mu             sync.Mutex
	keys           map[string]string
	balances       *providerBalanceCache
	preparing      map[string]bool
	runs           map[string]*runtimeRun
	subscribers    map[string]map[*discussionSubscriber]bool
}

func newRuntimeSessions() *runtimeSessions {
	return &runtimeSessions{events: events.New(512), preparing: map[string]bool{}, acp: map[string]*acpBinding{}, keys: map[string]string{}, balances: newProviderBalanceCache(nil), runs: map[string]*runtimeRun{}, subscribers: map[string]map[*discussionSubscriber]bool{}}
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
	m.providerMu.Lock()
	defer m.providerMu.Unlock()
	p.Name = strings.TrimSpace(p.Name)
	p.Model = strings.TrimSpace(p.Model)
	if p.UsageMode != "" && p.UsageMode != "none" {
		return p, errors.New("invalid token counting mode")
	}
	if len(p.Models) > 32 {
		return p, errors.New("maximum 32 models per provider")
	}
	models := []string{}
	seen := map[string]bool{}
	for _, model := range append([]string{p.Model}, p.Models...) {
		model = strings.TrimSpace(model)
		if len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") {
			return p, errors.New("model ID too long")
		}
		if model != "" && !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	p.Models = models
	if len(models) > 32 {
		return p, errors.New("maximum 32 models per provider")
	}
	if p.Name == "" || len(p.Name) > 100 || p.Model == "" || len(p.Model) > 200 {
		return p, errors.New("name and model required (maximum 100 and 200 bytes)")
	}
	endpoint, err := validateCloudEndpoint(p.Endpoint)
	if err != nil {
		return p, err
	}
	p.Endpoint = endpoint
	if strings.ContainsAny(key, "\r\n") || len(key) > 4096 {
		return p, errors.New("invalid key")
	}
	if p.ID == "" {
		p.ID = newSessionID()
	} else {
		var old CloudProvider
		if !getStoreJSON(bkProviders, p.ID, &old) {
			return p, errors.New("provider not found")
		}
		p.ContextWindows = old.ContextWindows
		if old.Endpoint != p.Endpoint {
			return p, errors.New("create a new connection to change the destination")
		}
	}
	p.Ready = false
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		return p, errors.New("connection not saved: storage unavailable or locked")
	}
	m.mu.Lock()
	if strings.TrimSpace(key) != "" {
		m.keys[p.ID] = strings.TrimSpace(key)
	}
	credential := m.keys[p.ID]
	p.Ready = credential != ""
	m.mu.Unlock()
	if err := rememberProviderKey(p.ID, credential, p.Remember); err != nil {
		p.Remember = false
		_ = putStoreJSON(bkProviders, p.ID, p)
		return p, err
	}
	return p, nil
}

// disconnect forgets the key in memory and in the keychain.
func (m *runtimeSessions) disconnect(id string) error {
	m.providerMu.Lock()
	defer m.providerMu.Unlock()
	if err := rememberProviderKey(id, "", false); err != nil {
		return errors.New("stored credential could not be removed")
	}
	m.mu.Lock()
	delete(m.keys, id)
	m.mu.Unlock()
	var p CloudProvider
	if getStoreJSON(bkProviders, id, &p) && p.Remember {
		p.Remember = false
		return putStoreJSON(bkProviders, id, p)
	}
	return nil
}

func (m *runtimeSessions) create(projectID, providerID string, consent bool) (RuntimeSession, error) {
	return m.createContext(context.Background(), projectID, providerID, consent)
}
func (m *runtimeSessions) createContext(ctx context.Context, projectID, providerID string, consent bool) (RuntimeSession, error) {
	var s RuntimeSession
	if providerID != "" {
		var p CloudProvider
		if !getStoreJSON(bkProviders, providerID, &p) {
			return s, errors.New("provider not found")
		}
		in := policy.Input{Subject: "data.send_provider", Operation: "select", ProjectID: projectID, ProviderID: providerID, Endpoint: p.Endpoint, Model: p.Model, Fallback: policy.Confirm}
		if err := m.authorizePolicy(ctx, in, consent); err != nil {
			return s, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return s, err
	}
	var p CloudProvider
	if providerID != "" && !getStoreJSON(bkProviders, providerID, &p) {
		return s, errors.New("provider not found")
	}
	if providerID != "" && m.keys[p.ID] == "" {
		return s, errors.New("reconnect this provider in Models → Providers")
	}
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("project not found")
		}
	}
	now := time.Now().UnixMilli()
	s = RuntimeSession{ID: newSessionID(), ProjectID: projectID, RuntimeID: "openai-compatible", ProviderID: p.ID, ProviderName: p.Name, Endpoint: p.Endpoint, Model: p.Model, Title: "New cloud discussion", CreatedAt: now, UpdatedAt: now, Status: "idle", Messages: []Message{}}
	s.Title = "New conversation"
	if providerID == "" {
		s.RuntimeID = "llama.cpp"
		s.ProviderName = "llama.cpp"
		s.Model = ReadConfig()["MODEL"]
	}
	return s, putStoreJSON(bkRuntimeSessions, s.ID, s)
}

// setWebSearch records the discussion's web-search switch (cloud turns).
func (m *runtimeSessions) setWebSearch(id string, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.getLocked(id)
	if !ok {
		return errors.New("discussion not found or locked")
	}
	if m.runs[id] != nil {
		return errors.New("wait for the current answer before changing web search")
	}
	if s.WebSearch != nil && *s.WebSearch == on {
		return nil
	}
	s.WebSearch = &on
	return putStoreJSON(bkRuntimeSessions, id, s)
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
		s.Error = "Loom restarted during the response. No automatic resend."
		for _, request := range s.PendingRequests {
			if len(s.Turns) > 0 {
				e := AgentEvent{Type: "request.resolved", Runtime: s.RuntimeID, RequestID: request.ID, Outcome: "cancelled", Decision: "cancel", Raw: agent.JSON(map[string]any{"reason": "Loom restarted"})}
				s.Turns[len(s.Turns)-1].ACPEvents = append(s.Turns[len(s.Turns)-1].ACPEvents, DiscussionEvent{"type": "request.resolved", "request_id": request.ID, "outcome": "cancelled", "agent_event": e})
			}
		}
		s.PendingRequests = nil
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
			if s.RuntimeID == "policy" {
				continue
			}
			s = clientSession(s)
			s.MessageCount = len(s.Messages)
			s.Messages = nil
			// Lists poll every 30 s: turn metadata only, never turn events.
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

func (m *runtimeSessions) start(id, requestID, text string, expectedRevision ...string) error {
	return m.startPrepared(id, requestID, text, prepareDiscussion, expectedRevision...)
}

// Preparation can read files, retrieve Brain passages and check a remote
// directory. It must never hold the registry lock needed by unrelated reads.
// errContextChanged: what would be sent differs from what the client prepared
// (route, history or automatic context). The client prepares again and retries.
var errContextChanged = errors.New("the model, thread or its context changed; check the Context panel then resend your message")

func (m *runtimeSessions) startPrepared(id, requestID, text string, prepare func(RuntimeSession, string) DiscussionPreview, expectedRevision ...string) error {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxMessageBytes || len(requestID) < 8 || len(requestID) > 100 {
		return errors.New("message required (maximum 112 KiB with attached files) and valid request ID")
	}
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return errors.New("discussion not found or locked")
	}
	if m.preparing[id] {
		m.mu.Unlock()
		return errors.New("discussion configuration in progress; try again")
	}
	duplicate, err := m.validateStartLocked(s, requestID)
	key := m.keys[s.ProviderID]
	m.mu.Unlock()
	if duplicate || err != nil {
		return err
	}
	original := s
	prepared := prepare(s, text)
	if len(expectedRevision) > 0 && (expectedRevision[0] == "" || expectedRevision[0] != prepared.Context.Revision) {
		return errContextChanged
	}
	if prepared.Problem != "" {
		return errors.New(prepared.Problem)
	}
	if s.RuntimeID == "openai-compatible" && key == "" {
		return errors.New("missing key: reconnect the provider in Models → Providers")
	}
	var adapter RuntimeAdapter
	if s.RuntimeID == "llama.cpp" {
		if s.Model == "" || !sameModelPath(s.Model, ReadConfig()["MODEL"]) {
			return errors.New("load the selected model from the Model panel before sending")
		}
		if !healthCheck() {
			return errors.New("the local model is not ready; load it from the Model panel")
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
				return errors.New("CLI or ACP launcher unavailable")
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
			attachPrimarySecondBrain(&s, acp.agent)
			if _, err := check(s.Workdir); err != nil {
				return err
			}
			adapter = &acpAdapter{agent: acp.agent, sessions: m, session: cloneRuntimeSession(s)}
		} else {
			adapter = registered
		}
	} else {
		return errors.New("this adapter cannot execute a discussion yet")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.getLocked(id)
	if !ok {
		return errors.New("discussion not found or locked")
	}
	if duplicate, err := m.validateStartLocked(current, requestID); duplicate || err != nil {
		return err
	}
	if !reflect.DeepEqual(original, current) || m.keys[s.ProviderID] != key {
		return errors.New("the thread or connection changed while preparing; reopen the Context panel and retry")
	}
	if s.RuntimeID == "llama.cpp" {
		for _, other := range m.runs {
			if other.session.RuntimeID == "llama.cpp" {
				return errors.New("the local engine is already responding in another discussion")
			}
		}
	}
	s.FrozenSnapshot = discussion.CloneFrozenSnapshot(prepared.Context.Snapshot)
	s.ContextExtras = prepared.Context.Extras
	messages := append(append([]Message{}, s.Messages...), Message{Role: "user", Content: text})
	if len(s.Messages) == 0 && !s.CustomTitle {
		s.Title = discussionTitle(text)
	}
	s.Messages = append(messages, Message{Role: "assistant", Content: ""})
	if s.PortableMessages != nil {
		s.PortableMessages = append(s.PortableMessages, Message{Role: "user", Content: text})
	}
	s.FrozenSnapshot = discussion.TrackFrozenPortable(s.FrozenSnapshot, s.PortableMessages)
	s.Turns = append(s.Turns, discussionTurnRecord(s, prepared, len(s.Messages)-1))
	recordTurnContext(&s.Turns[len(s.Turns)-1], prepared.Context)
	s.Turns[len(s.Turns)-1].ReasoningEffort = s.ReasoningEffort
	s.Turns[len(s.Turns)-1].StartedAt = time.Now().UnixMilli()
	s.Turns[len(s.Turns)-1].ActivityAt = s.Turns[len(s.Turns)-1].StartedAt
	s.Turns[len(s.Turns)-1].ActivityText = "Working"
	s.Status = "running"
	s.Error = ""
	s.Usage = nil
	s.LastRequestID = requestID
	s.RequestIDs = append(s.RequestIDs, requestID)
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		return errors.New("could not save: storage unavailable or locked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	if adapter.Descriptor().Kind == "harness" {
		cancel()
		ctx, cancel = context.WithCancel(context.Background())
	}
	theBrain().cancelMemoryConsolidation()
	run := &runtimeRun{session: s, cancel: cancel, providerKey: key}
	m.runs[id] = run
	m.taskEventLocked(s, events.TaskStarted, nil)
	m.publishLocked(id, DiscussionEvent{"type": "turn_start", "text": text, "portable_text": true, "provenance": s.Turns[len(s.Turns)-1], "session": cloneRuntimeSession(s), "context": turnContext(s, prepared.Context)})
	go m.generate(ctx, run, adapter, prepared.Messages, prepared.Context)
	return nil
}

// Caller holds mu. Rechecked after preparation to preserve idempotency, the
// run limit and edits/route changes from another tab.
func (m *runtimeSessions) validateStartLocked(s RuntimeSession, requestID string) (bool, error) {
	if m.preparing[s.ID] {
		return false, errors.New("discussion configuration is in progress")
	}
	for _, seen := range s.RequestIDs {
		if seen == requestID {
			return true, nil
		}
	}
	if s.LastRequestID == requestID {
		return true, nil
	}
	if harnessLifecycle.updatingRuntime(s.RuntimeID) {
		return false, errors.New("an automatic harness update is in progress; wait for it to finish")
	}
	if m.runs[s.ID] != nil || m.nativeRunning(s) {
		return false, errors.New("a response is already in progress")
	}
	if s.RuntimeID == "llama.cpp" && s.NativeArchive != "" {
		return false, errors.New("this thread uses native local chat; send from the discussion")
	}
	if len(m.runs) >= 4 {
		return false, errors.New("four responses are already running; wait for them to finish")
	}
	return false, nil
}

func turnContext(s RuntimeSession, c DiscussionContext) DiscussionContext {
	c.Revision = discussionContextRevision(s, c)
	return c
}

func (m *runtimeSessions) generate(ctx context.Context, run *runtimeRun, adapter RuntimeAdapter, messages []Message, preparedContext DiscussionContext) {
	defer run.cancel()
	ctx = withPolicySession(ctx, m, run.session)
	caps := Caps{}
	if adapter.Descriptor().Kind == "cloud" {
		caps.Internet = getBool(bkState, "internet")
		// Same rule as local turns: the Internet setting is the master switch,
		// the composer's per-discussion choice opts in.
		if run.session.WebSearch != nil {
			caps.Internet = caps.Internet && *run.session.WebSearch
		}
	}
	var err error
	if loomTranscript(run.session) && compactEnabled() {
		m.mu.Lock()
		snapshot := cloneRuntimeSession(run.session)
		m.mu.Unlock()
		window := discussionWindow(snapshot)
		used := promptTokens(messages)
		if window > 0 && float64(used) >= float64(window)*compactTriggerFrac {
			next, _, changed, compactErr := m.compactSnapshot(ctx, snapshot, run.providerKey)
			err = compactErr
			if err == nil && changed {
				theBrain().queueSessionTranscript(next)
				if err == nil {
					prepared := prepareDiscussion(next, "")
					messages, preparedContext = prepared.Messages, prepared.Context
					next.FrozenSnapshot = discussion.CloneFrozenSnapshot(prepared.Context.Snapshot)
					next.ContextExtras = prepared.Context.Extras
					m.mu.Lock()
					run.session = next
					turn := &run.session.Turns[len(run.session.Turns)-1]
					recordTurnContext(turn, prepared.Context)
					turn.ContextRevision, turn.ContextBytes, turn.InputBytes = prepared.Context.Revision, len(prepared.Context.System), prepared.TextBytes
					err = putStoreJSON(bkRuntimeSessions, next.ID, next)
					if err == nil {
						m.publishLocked(next.ID, DiscussionEvent{"type": "compacted", "session": next})
					}
					m.mu.Unlock()
				}
			}
		}
	}
	if err == nil {
		_, err = adapter.Run(ctx, RuntimeTurn{Messages: messages, Temperature: 0.7, Caps: caps}, func(event StreamEvent) bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.runs[run.session.ID] != run {
				return false
			}
			if ctx.Err() != nil && event.ACPEvent != nil && event.ACPEvent["type"] != "approval_resolved" && event.ACPEvent["type"] != "request.resolved" {
				return false
			}
			if ctx.Err() != nil && event.ACPEvent == nil && event.ACPState == nil {
				return false
			}
			if event.ACPState != nil {
				pending := run.session.PendingRequests
				run.session.ACPState = cloneACPState(*event.ACPState)
				run.session.PendingRequests = pending
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
			if event.AgentEvent != nil {
				e := event.AgentEvent
				if e.Type == "request.opened" && e.Request != nil {
					run.session.PendingRequests = append(run.session.PendingRequests, *e.Request)
				}
				if e.Type == "request.resolved" {
					next := run.session.PendingRequests[:0]
					for _, r := range run.session.PendingRequests {
						if r.ID != e.RequestID {
							next = append(next, r)
						}
					}
					run.session.PendingRequests = next
				}
			}
			if event.ACPEvent != nil {
				e := event.ACPEvent
				if e["type"] == "usage" {
					run.session.ACPUsage = acpCloneMap(e)
				}
				if e["type"] == "commands" {
					b, _ := json.Marshal(e["commands"])
					_ = json.Unmarshal(b, &run.session.Commands)
				}
				encoded, _ := json.Marshal(e)
				run.acpBytes += len(encoded)
				if (run.acpBytes > 64<<20 || len(run.session.Turns[len(run.session.Turns)-1].ACPEvents) >= 16384) && e["type"] != "approval_resolved" && e["type"] != "request.resolved" {
					run.acpError = "ACP journal too large; turn stopped, received events preserved."
					run.cancel()
					return false
				}
				turn := &run.session.Turns[len(run.session.Turns)-1]
				turn.ACPEvents = append(turn.ACPEvents, e)
				if e["type"] == "text_delta" {
					event.Content, _ = e["text"].(string)
				}
			}
			if tool := event.ToolUsed; tool != nil && !tool.Typing {
				turn := &run.session.Turns[len(run.session.Turns)-1]
				if (!tool.Done || !run.toolPending) && len(turn.ToolSummaries) < 512 {
					turn.ToolSummaries = append(turn.ToolSummaries, boundedBytes(strings.Join(strings.Fields(tool.Name), " "), 200))
				}
				run.toolPending = !tool.Done
			}
			if event.Content != "" {
				i := len(run.session.Messages) - 1
				previous, _ := run.session.Messages[i].Content.(string)
				run.session.Messages[i].Content = previous + event.Content
			}
			if event.AssistantSnapshot != nil {
				run.session.Messages[len(run.session.Messages)-1].Content = *event.AssistantSnapshot
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
			run.session.UpdatedAt = time.Now().UnixMilli()
			turn.ActivityAt = run.session.UpdatedAt
			if event.Content != "" {
				turn.ActivityText = "Writing response"
			}
			if e := event.AgentEvent; e != nil {
				switch e.Type {
				case "request.opened":
					turn.ActivityText = "Waiting for user"
				case "request.resolved":
					turn.ActivityText = "Resumed"
				case "item.started":
					if e.ItemType != "agent_message" && e.ItemType != "assistant_message" && e.ItemType != "reasoning" && e.ItemType != "user_message" && e.ItemType != "plan" {
						turn.StepsStarted++
						turn.ActivityText = "Running a step"
					}
				case "item.completed":
					if e.ItemType != "agent_message" && e.ItemType != "assistant_message" && e.ItemType != "reasoning" && e.ItemType != "user_message" && e.ItemType != "plan" {
						turn.StepsCompleted++
						turn.ActivityText = "Step finished"
					}
				}
			} else if event.ACPEvent != nil {
				switch event.ACPEvent["type"] {
				case "tool_start":
					turn.StepsStarted++
					turn.ActivityText = "Running a tool"
				case "tool_end":
					turn.StepsCompleted++
					turn.ActivityText = "Tool finished"
				}
			} else if event.ToolUsed != nil && !event.ToolUsed.Typing {
				if event.ToolUsed.Done {
					turn.StepsCompleted++
				} else {
					turn.StepsStarted++
				}
				turn.ActivityText = "Using a tool"
			}
			if event.ACPEvent != nil || event.ACPState != nil || event.AgentEvent != nil && (event.AgentEvent.Type == "request.opened" || event.AgentEvent.Type == "request.resolved") {
				if err := putStoreJSON(bkRuntimeSessions, run.session.ID, run.session); err != nil {
					run.cancel()
					return false
				}
			}
			if event.AgentEvent != nil {
				e := event.AgentEvent
				if e.Type == "request.opened" && e.Request != nil {
					m.taskEventLocked(run.session, events.TaskWaiting, e.Request)
				}
				if e.Type == "request.resolved" {
					m.taskEventLocked(run.session, events.TaskResumed, nil)
				}
			}
			m.publishLocked(run.session.ID, liveRuntimeDiscussionEvents(event, *turn)...)
			return true
		})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &run.session
	if s.PortableMessages != nil {
		s.PortableMessages = append(s.PortableMessages, s.Messages[len(s.Messages)-1])
	}
	s.FrozenSnapshot = discussion.TrackFrozenPortable(s.FrozenSnapshot, s.PortableMessages)
	if run.acpError != "" {
		err = errors.New(run.acpError)
	}
	s.PendingRequests = nil
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
			s.Error = "Response stopped. Partial text is preserved."
		}
	}
	if n := len(s.Turns); n > 0 {
		t := &s.Turns[n-1]
		t.FinishedAt, t.Outcome, t.ActivityAt, t.ActivityText = s.UpdatedAt, taskStatus(s.Status), s.UpdatedAt, taskStatus(s.Status)
	}
	// Keep a failed-to-persist result in memory for recovery, instead of silently
	// losing text or reporting that it was saved. A later retry can persist it.
	if putStoreJSON(bkRuntimeSessions, s.ID, *s) != nil {
		run.finalStatus, run.finalError = s.Status, s.Error
		s.Status = "unsaved"
		s.Error = "Response not saved: unlock storage and try again."
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
			theBrain().queueSessionTranscript(recovered)
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
	if m.preparing[id] {
		return errors.New("discussion configuration is in progress")
	}
	if m.runs[id] != nil {
		return errors.New("stop the response before deleting the discussion")
	}
	s, ok := m.getLocked(id)
	if !ok {
		return errors.New("discussion not found")
	}
	if m.nativeRunning(s) {
		return errors.New("stop the local response before deleting the discussion")
	}
	m.closeACP(id)
	return putBytes(bkRuntimeSessions, id, nil)
}

func (m *runtimeSessions) finishDiscussionLocked(s RuntimeSession) {
	kind := events.TaskCompleted
	if s.Status != "complete" {
		kind = events.TaskFailed
	}
	m.taskEventLocked(s, kind, nil)
	if s.Status != "unsaved" {
		theBrain().queueSessionTranscript(s)
	}
	snapshot := cloneRuntimeSession(s)
	if s.Error != "" {
		m.publishLocked(s.ID, DiscussionEvent{"type": "error", "error": s.Error})
	}
	m.publishLocked(s.ID, DiscussionEvent{"type": "turn_done", "provenance": snapshot.Turns[len(snapshot.Turns)-1], "metrics": discussionMetrics(&snapshot.Turns[len(snapshot.Turns)-1]), "session": snapshot, "context": discussionContext(s)})
}
