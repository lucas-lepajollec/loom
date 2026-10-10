package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"net/url"
	"sync"
)

// AgentEvent is runtime-private display data. It never belongs in model context.
// Raw contains the provider frame, limited to 64 KiB (a preview if larger).
type AgentEvent struct {
	Usage     *AgentUsage     `json:"usage,omitempty"`
	Type      string          `json:"type"`
	Runtime   string          `json:"runtime"`
	ThreadID  string          `json:"thread_id,omitempty"`
	TurnID    string          `json:"turn_id,omitempty"`
	ItemID    string          `json:"item_id,omitempty"`
	ItemType  string          `json:"item_type,omitempty"`
	Stream    string          `json:"stream,omitempty"`
	Delta     string          `json:"delta,omitempty"`
	Replace   bool            `json:"replace,omitempty"`
	Status    string          `json:"status,omitempty"`
	Error     string          `json:"error,omitempty"`
	Message   string          `json:"message,omitempty"`
	Method    string          `json:"method,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Raw       json.RawMessage `json:"raw"`
	Request   *AgentRequest   `json:"request,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Decision  string          `json:"decision,omitempty"`
	Outcome   string          `json:"outcome,omitempty"`
}

// Scope separates newly spent tokens from retained thread/context observations.
// Pointers preserve unknown counts instead of reporting zero.
type AgentUsage struct {
	Scope         string `json:"scope"`
	Input         *int64 `json:"input,omitempty"`
	Output        *int64 `json:"output,omitempty"`
	Cached        *int64 `json:"cached,omitempty"`
	Reasoning     *int64 `json:"reasoning,omitempty"`
	Total         *int64 `json:"total,omitempty"`
	ContextWindow *int64 `json:"context_window,omitempty"`
}

type AgentRequest struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"` // approval, user_input, elicitation
	Method       string          `json:"method"`
	ItemID       string          `json:"item_id,omitempty"`
	ApprovalKind string          `json:"approval_kind,omitempty"` // command, file, tool, plan
	Options      []RequestOption `json:"options,omitempty"`
	Questions    []InputQuestion `json:"questions,omitempty"`
	Message      string          `json:"message,omitempty"`
	Schema       json.RawMessage `json:"schema,omitempty"`
	URL          string          `json:"url,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}
type RequestOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
type InputQuestion struct {
	ID          string          `json:"id"`
	Header      string          `json:"header,omitempty"`
	Question    string          `json:"question"`
	Options     []RequestOption `json:"options,omitempty"`
	FreeText    bool            `json:"free_text"`
	MultiSelect bool            `json:"multi_select,omitempty"`
	Optional    bool            `json:"optional,omitempty"`
	Secret      bool            `json:"secret,omitempty"`
}
type RequestAnswer struct {
	Decision string              `json:"decision,omitempty"`
	Answers  map[string][]string `json:"answers,omitempty"`
	Content  json.RawMessage     `json:"content,omitempty"`
}

func BoundedJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	if len(raw) <= 64<<10 && json.Valid(raw) {
		return append(json.RawMessage(nil), raw...)
	}
	n := min(len(raw), 8<<10)
	b, _ := json.Marshal(map[string]any{"truncated": true, "bytes": len(raw), "preview": string(raw[:n])})
	return b
}
func JSON(value any) json.RawMessage { b, _ := json.Marshal(value); return BoundedJSON(b) }

// RequestBroker keeps live response channels separate from durable request data.
// Publishing under this lock orders opened/resolved even when a turn ends early.
type RequestBroker struct {
	// Decide is installed before publication. It authorizes only requests the
	// upstream actually exposes; unknown requests retain explicit interaction.
	Decide func(context.Context, AgentEvent) (RequestAnswer, bool, error)
	// AuthorizeAnswer rechecks application policy immediately before delivering
	// an explicit answer. It must not call back into this broker.
	AuthorizeAnswer func(context.Context, AgentRequest, RequestAnswer) (RequestAnswer, error)
	mu              sync.Mutex
	pending         map[string]*pendingRequest
	closed          bool
	emit            EventSink[AgentEvent]
	runtime         string
}
type pendingRequest struct {
	request AgentRequest
	event   AgentEvent
	answer  chan RequestAnswer
	ctx     context.Context
}

func NewRequestBroker(name string, emit EventSink[AgentEvent]) *RequestBroker {
	return &RequestBroker{runtime: name, emit: emit, pending: map[string]*pendingRequest{}}
}
func (b *RequestBroker) Ask(ctx context.Context, e AgentEvent) (RequestAnswer, error) {
	wait, err := b.Open(ctx, e)
	if err != nil {
		return RequestAnswer{}, err
	}
	return wait(ctx)
}

// Open records the request synchronously in protocol order; only waiting for
// its answer runs asynchronously, so turn completion cannot overtake opening.
func (b *RequestBroker) Open(ctx context.Context, e AgentEvent) (func(context.Context) (RequestAnswer, error), error) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed || ctx.Err() != nil {
		return nil, context.Canceled
	}
	if b.Decide != nil && e.Request != nil {
		answer, automatic, err := b.Decide(ctx, e)
		if err != nil {
			return nil, err
		}
		if automatic {
			if err := ValidateAnswer(*e.Request, answer); err != nil {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return func(context.Context) (RequestAnswer, error) { return answer, nil }, nil
		}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, context.Canceled
	}
	if e.Request == nil || e.Request.ID == "" {
		b.mu.Unlock()
		return nil, errors.New("request ID required")
	}
	id := e.Request.ID
	if b.pending[id] != nil || len(b.pending) >= 32 {
		b.mu.Unlock()
		return nil, errors.New("duplicate request or too many pending requests")
	}
	p := &pendingRequest{request: *e.Request, event: e, answer: make(chan RequestAnswer, 1), ctx: ctx}
	b.pending[id] = p
	if !b.emit(e) {
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, context.Canceled
	}
	b.mu.Unlock()
	return func(ctx context.Context) (RequestAnswer, error) {
		select {
		case a := <-p.answer:
			if ctx.Err() != nil {
				return RequestAnswer{Decision: "cancel"}, nil
			}
			return a, nil
		case <-ctx.Done():
			b.resolve(id, RequestAnswer{Decision: "cancel"}, false)
			return RequestAnswer{Decision: "cancel"}, nil
		}
	}, nil
}
func (b *RequestBroker) Resolve(id string, a RequestAnswer) error { return b.resolve(id, a, true) }
func (b *RequestBroker) resolve(id string, a RequestAnswer, validate bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending[id]
	if p == nil {
		return errors.New("request missing or already resolved")
	}
	cancelled := validate && (b.closed || p.ctx.Err() != nil)
	if cancelled {
		a = RequestAnswer{Decision: "cancel"}
		validate = false
	}
	if validate {
		if err := ValidateAnswer(p.request, a); err != nil {
			return err
		}
		if b.AuthorizeAnswer != nil {
			var err error
			a, err = b.AuthorizeAnswer(p.ctx, p.request, a)
			if err != nil {
				return err
			}
			if err = ValidateAnswer(p.request, a); err != nil {
				return err
			}
		}
	}
	delete(b.pending, id)
	outcome := "answered"
	switch a.Decision {
	case "cancel":
		outcome = "cancelled"
	case "deny", "decline":
		outcome = "declined"
	case "allow_once", "allow_always", "accept":
		outcome = "accepted"
	}
	e := p.event
	e.Type = "request.resolved"
	e.Request = nil
	e.RequestID = id
	e.Outcome = outcome
	e.Decision = a.Decision
	e.Payload = nil
	b.emit(e)
	p.answer <- a
	if cancelled {
		return errors.New("request cancelled")
	}
	return nil
}
func (b *RequestBroker) Cancel() {
	b.mu.Lock()
	b.closed = true
	ids := make([]string, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, id := range ids {
		_ = b.resolve(id, RequestAnswer{Decision: "cancel"}, false)
	}
}
func ValidateAnswer(r AgentRequest, a RequestAnswer) error {
	if a.Decision == "cancel" {
		return nil
	}
	switch r.Kind {
	case "approval":
		for _, o := range r.Options {
			if o.ID == a.Decision {
				return nil
			}
		}
		return errors.New("invalid approval decision")
	case "user_input":
		if a.Decision != "" {
			return errors.New("user input requires answers")
		}
		if len(a.Answers) != len(r.Questions) {
			return errors.New("answer every question")
		}
		for _, q := range r.Questions {
			answers, present := a.Answers[q.ID]
			if !present {
				return errors.New("answer every question")
			}
			if len(answers) == 0 && !q.Optional {
				return errors.New("answer every question")
			}
			if !q.MultiSelect && len(answers) > 1 {
				picks := 0
				for _, v := range answers {
					for _, o := range q.Options {
						if o.ID == v || o.Label == v {
							picks++
							break
						}
					}
				}
				if picks > 1 {
					return errors.New("question allows one selection")
				}
			}
			seen := map[string]bool{}
			for _, v := range answers {
				if seen[v] {
					return errors.New("duplicate question answer")
				}
				seen[v] = true
				valid := q.FreeText
				for _, o := range q.Options {
					if o.ID == v || o.Label == v {
						valid = true
					}
				}
				if !valid || len(v) > 64<<10 {
					return errors.New("invalid question answer")
				}
			}
		}
	case "elicitation":
		if a.Decision != "accept" && a.Decision != "decline" {
			return errors.New("elicitation decision: accept, decline or cancel")
		}
		if a.Decision == "accept" && len(r.Schema) > 0 && (!json.Valid(a.Content) || string(a.Content) == "null") {
			return errors.New("form content required")
		}
		if a.Decision == "accept" && len(r.Schema) > 0 {
			var schema jsonschema.Schema
			if err := json.Unmarshal(r.Schema, &schema); err != nil {
				return errors.New("invalid elicitation schema")
			}
			resolved, err := schema.Resolve(&jsonschema.ResolveOptions{Loader: func(*url.URL) (*jsonschema.Schema, error) {
				return nil, errors.New("external schema references are unsupported")
			}})
			if err != nil {
				return errors.New("unsupported elicitation schema")
			}
			var content any
			_ = json.Unmarshal(a.Content, &content)
			if err := resolved.Validate(content); err != nil {
				return errors.New("elicitation content does not match schema")
			}
		}

	default:
		return errors.New("unsupported request kind")
	}
	return nil
}

type CapabilityObservation struct {
	Check string `json:"check"`
	At    int64  `json:"at"`
	Count uint64 `json:"count"`
}

// Evidence levels are independent. Historical success survives a current failure.
type CapabilityEvidence struct {
	Discovered       *CapabilityObservation `json:"discovered,omitempty"`
	ProtocolVerified *CapabilityObservation `json:"protocol_verified,omitempty"`
	ObservedWorking  *CapabilityObservation `json:"observed_working,omitempty"`
	Health           string                 `json:"health"`
}

type CompatibilityRecord struct {
	Fingerprint         string                        `json:"fingerprint,omitempty"`
	Evidence            map[string]CapabilityEvidence `json:"evidence,omitempty"`
	CapabilityChecks    []capability.Probe            `json:"capability_checks,omitempty"`
	TestedVersionSource string                        `json:"tested_version_source,omitempty"`
	Runtime             string                        `json:"runtime"`
	Executable          string                        `json:"executable"`
	Version             string                        `json:"version"`
	Protocol            string                        `json:"protocol"`
	AdapterVersion      string                        `json:"adapter_version"`
	AdapterPackage      string                        `json:"adapter_package,omitempty"`
	AgentVersion        string                        `json:"agent_version,omitempty"`
	TestedVersions      []string                      `json:"tested_versions,omitempty"`
	TestedVersion       string                        `json:"tested_version"`
	Capabilities        []string                      `json:"capabilities"`
	Warning             string                        `json:"warning,omitempty"`
}

// NewCompatibilityRecord constructs all native and ACP records. Version lists
// remain provenance; they never establish per-capability session success.
func NewCompatibilityRecord(r CompatibilityRecord) *CompatibilityRecord {
	r.AgentVersion = r.Version
	if r.TestedVersion == "" && len(r.TestedVersions) > 0 {
		r.TestedVersion = r.TestedVersions[len(r.TestedVersions)-1]
	}
	return &r
}
