package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpExecuteTool = "loom_mcp_execute"
const maxPipelineCalls = 30

// Upstream I/O may overlap, human confirmation is presented one at a time.
type pipelineApprovalKey struct{}

type pipelineCall struct {
	Server string         `json:"server"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args,omitempty"`
}
type predicate struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}
type pipelineStep struct {
	pipelineCall
	ID     string         `json:"id"`
	Op     string         `json:"op"`
	From   string         `json:"from,omitempty"`
	Fields []string       `json:"fields,omitempty"`
	Where  *predicate     `json:"where,omitempty"`
	Calls  []pipelineCall `json:"calls,omitempty"`
	Call   *pipelineCall  `json:"call,omitempty"`
	Limit  int            `json:"limit,omitempty"`
}

var stepID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
var templateRef = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

func (c pipelineCall) valid() bool { return c.Server != "" && c.Tool != "" }

func validateProgram(steps []pipelineStep) error {
	if len(steps) == 0 || len(steps) > 64 {
		return errors.New("expected 1–64 pipeline steps")
	}
	ids := map[string]bool{}
	var calls int
	for i, s := range steps {
		if s.Op != "return" && (!stepID.MatchString(s.ID) || ids[s.ID] || s.ID == "item") {
			return fmt.Errorf("invalid or duplicate id at step %d", i+1)
		}
		if s.From != "" {
			id, _, _ := strings.Cut(s.From, ".")
			if !ids[id] {
				return fmt.Errorf("unknown source at step %d", i+1)
			}
		}
		if err := validFields(s.Fields); err != nil {
			return err
		}
		switch s.Op {
		case "call":
			if !s.pipelineCall.valid() {
				return errors.New("call requires server and tool")
			}
			calls++
		case "parallel":
			if len(s.Calls) == 0 {
				return errors.New("parallel requires calls")
			}
			for _, c := range s.Calls {
				if !c.valid() {
					return errors.New("invalid parallel call")
				}
			}
			calls += len(s.Calls)
		case "foreach":
			if s.From == "" || s.Call == nil || !s.Call.valid() || s.Limit < 0 || s.Limit > 25 {
				return errors.New("foreach requires a source, call and limit of at most 25")
			}
		case "map", "pick":
			if s.From == "" || len(s.Fields) == 0 {
				return errors.New("projection requires from and fields")
			}
		case "filter":
			if s.From == "" || s.Where == nil || s.Where.Field == "" || !slices.Contains([]string{"eq", "neq", "contains", "gt", "lt", "exists"}, s.Where.Op) {
				return errors.New("invalid filter predicate")
			}
		case "return":
			if i != len(steps)-1 || (s.From == "" && len(s.Fields) == 0) {
				return errors.New("return must be last and select a source or fields")
			}
		default:
			return fmt.Errorf("unknown operation at step %d", i+1)
		}
		ids[s.ID] = true
	}
	if calls > maxPipelineCalls {
		return errors.New("pipeline exceeds 30 tool calls")
	}
	return nil
}

type pipeline struct {
	values    map[string]any // read-only while a batch runs
	remaining int
	calls     int
	invoke    func(context.Context, mcpTarget, json.RawMessage) (*mcp.CallToolResult, error)
}

func (p *pipeline) resolve(path string, item any) (any, error) {
	root, rest, _ := strings.Cut(path, ".")
	base, ok := p.values[root]
	if root == "item" {
		base, ok = item, true
	}
	if ok {
		if v, ok := valueAt(base, rest); ok {
			return v, nil
		}
	}
	return nil, errors.New("unknown interpolation reference")
}

func (p *pipeline) interpolate(value, item any, space *int) (any, error) {
	switch x := value.(type) {
	case string:
		matches := templateRef.FindAllStringSubmatchIndex(x, -1)
		if len(matches) == 0 {
			return x, nil
		}
		var b strings.Builder
		last := 0
		for _, m := range matches {
			v, err := p.resolve(x[m[2]:m[3]], item)
			if err != nil {
				return nil, err
			}
			if m[0] == 0 && m[1] == len(x) {
				return v, nil
			}
			var text string
			switch v := v.(type) {
			case string:
				text = v
			case bool, json.Number:
				text = fmt.Sprint(v)
			default:
				return nil, errors.New("embedded interpolation requires a scalar")
			}
			*space -= len(text)
			if *space < 0 || len(text)+b.Len()+m[0]-last > intermediateLimit {
				return nil, sizeError()
			}
			b.WriteString(x[last:m[0]])
			b.WriteString(text)
			last = m[1]
		}
		b.WriteString(x[last:])
		return b.String(), nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			next, err := p.interpolate(v, item, space)
			if err != nil {
				return nil, err
			}
			out[k] = next
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			next, err := p.interpolate(v, item, space)
			if err != nil {
				return nil, err
			}
			out[i] = next
		}
		return out, nil
	default:
		return value, nil
	}
}

func (p *pipeline) call(ctx context.Context, c pipelineCall, item any) (*mcp.CallToolResult, error) {
	space := p.remaining
	args, err := p.interpolate(c.Args, item, &space)
	if err != nil {
		return nil, err
	}
	if _, err = jsonSize(args, p.remaining); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	result, err := p.invoke(ctx, mcpTarget{c.Server, c.Tool}, raw)
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, errors.New("upstream tool reported an error")
	}
	return result, nil
}

// Batches reserve their entire call count before the first side effect. Results
// stay in input order; a failure cancels work waiting on approval or the pool.
func (p *pipeline) batch(ctx context.Context, calls []pipelineCall, items []any, workers int) (any, error) {
	if p.calls+len(calls) > maxPipelineCalls {
		return nil, errors.New("pipeline exceeds 30 tool calls")
	}
	p.calls += len(calls)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make([]any, len(calls))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	next, available := 0, p.remaining-2-max(0, len(calls)-1)
	for range min(workers, len(calls)) {
		wg.Go(func() {
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(calls) || ctx.Err() != nil {
					return
				}
				var item any
				if items != nil {
					item = items[i]
				}
				result, err := p.call(ctx, calls[i], item)
				mu.Lock()
				var v any
				if err == nil {
					v, err = resultValue(result, available)
				}
				if err == nil {
					var size int
					size, err = jsonSize(v, available)
					if err == nil {
						available -= size
						out[i] = v
					}
				}
				if err != nil && first == nil {
					first = err
					cancel()
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, ctx.Err()
}

func matchesPredicate(row any, w predicate) bool {
	v, ok := valueAt(row, w.Field)
	if w.Op == "exists" {
		return ok
	}
	if !ok {
		return false
	}
	switch w.Op {
	case "eq":
		return reflect.DeepEqual(v, w.Value)
	case "neq":
		return !reflect.DeepEqual(v, w.Value)
	case "contains":
		switch x := v.(type) {
		case string:
			part, ok := w.Value.(string)
			return ok && strings.Contains(x, part)
		case []any:
			return slices.ContainsFunc(x, func(a any) bool { return reflect.DeepEqual(a, w.Value) })
		}
	case "gt", "lt":
		a, ok1 := v.(json.Number)
		b, ok2 := w.Value.(json.Number)
		if ok1 && ok2 {
			if x, e := a.Int64(); e == nil {
				if y, e := b.Int64(); e == nil {
					return w.Op == "gt" && x > y || w.Op == "lt" && x < y
				}
			}
			x, e1 := a.Float64()
			y, e2 := b.Float64()
			return e1 == nil && e2 == nil && (w.Op == "gt" && x > y || w.Op == "lt" && x < y)
		}
	}
	return false
}

func (p *pipeline) step(ctx context.Context, s pipelineStep) (any, error) {
	source, found := valueAt(p.values, s.From)
	if !found {
		return nil, errors.New("source path unavailable")
	}
	switch s.Op {
	case "call":
		if p.calls >= maxPipelineCalls {
			return nil, errors.New("pipeline exceeds 30 tool calls")
		}
		p.calls++
		result, err := p.call(ctx, s.pipelineCall, nil)
		if err != nil {
			return nil, err
		}
		return resultValue(result, p.remaining)
	case "parallel":
		return p.batch(ctx, s.Calls, nil, 8)
	case "foreach", "filter":
		rows, ok := source.([]any)
		if !ok {
			return nil, errors.New("source must be an array")
		}
		if s.Op == "filter" {
			out := []any{}
			for _, row := range rows {
				if matchesPredicate(row, *s.Where) {
					out = append(out, row)
				}
			}
			return out, nil
		}
		if s.Limit > 0 {
			rows = rows[:min(s.Limit, len(rows))]
		}
		if len(rows) > 25 {
			return nil, errors.New("foreach exceeds 25 items")
		}
		calls := make([]pipelineCall, len(rows))
		for i := range calls {
			calls[i] = *s.Call
		}
		return p.batch(ctx, calls, rows, 1)
	default:
		return projectFields(source, s.Fields)
	}
}

func executePipeline(ctx context.Context, raw json.RawMessage, invoke func(context.Context, mcpTarget, json.RawMessage) (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, pipelineApprovalKey{}, make(chan struct{}, 1))
	if len(raw) > 128<<10 || !json.Valid(raw) {
		return nil, errors.New("invalid pipeline input")
	}
	var program struct {
		Steps []pipelineStep `json:"steps"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&program); err != nil {
		return nil, errors.New("invalid pipeline fields")
	}
	if err := validateProgram(program.Steps); err != nil {
		return nil, err
	}
	p := pipeline{values: map[string]any{}, remaining: intermediateLimit, invoke: invoke}
	var last any
	for i, s := range program.Steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v, err := p.step(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Op, err)
		}
		if s.Op != "return" {
			size, err := jsonSize(v, p.remaining)
			if err != nil {
				return nil, err
			}
			p.remaining -= size
			p.values[s.ID] = v
		}
		last = v
	}
	if _, err := jsonSize(last, intermediateLimit); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(last)
	if err != nil {
		return nil, err
	}
	return boundedResult(data, finalLimit, false), nil
}

var executeDefinition = func() *mcp.Tool {
	call := mcpObjectSchema([]string{"server", "tool"}, map[string]string{"server": "string", "tool": "string", "args": "object"})
	fields := map[string]any{"type": "array", "items": map[string]string{"type": "string"}}
	step := map[string]any{"type": "object", "required": []string{"op"}, "properties": map[string]any{
		"op": map[string]any{"type": "string", "enum": []string{"call", "parallel", "filter", "map", "pick", "foreach", "return"}},
		"id": map[string]string{"type": "string"}, "from": map[string]string{"type": "string"},
		"server": map[string]string{"type": "string"}, "tool": map[string]string{"type": "string"}, "args": map[string]string{"type": "object"},
		"calls": map[string]any{"type": "array", "items": call}, "call": call, "fields": fields,
		"where": map[string]any{"type": "object", "required": []string{"field", "op"}, "properties": map[string]any{"field": map[string]string{"type": "string"}, "op": map[string]any{"enum": []string{"eq", "neq", "contains", "gt", "lt", "exists"}}, "value": map[string]any{}}},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 25},
	}}
	return &mcp.Tool{Name: mcpExecuteTool, Description: "Run ordered data-flow steps in one turn. Give each step an id. call uses server/tool/args, parallel uses calls, foreach uses from/call and optional limit. Reference prior JSON with {{id.path}} or {{item.path}} in args. filter uses from/where, map and pick use from/fields. End with return from a step, or fields across step IDs. Only the selected result is returned. Up to 30 calls, 60 seconds.", InputSchema: map[string]any{"type": "object", "required": []string{"steps"}, "properties": map[string]any{"steps": map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "items": step}}}}
}()
