// Package agentstdio supervises Loom-owned JSONL processes. It supports Codex's
// bidirectional JSON-RPC and Pi's correlated command responses without a shell.
package agentstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

const MaxFrame = 4 << 20

var ErrClosed = errors.New("agent protocol disconnected")

type Frame struct {
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
	Type    string          `json:"type,omitempty"`
	Command string          `json:"command,omitempty"`
	Success bool            `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Raw     json.RawMessage `json:"-"`
}
type Client struct {
	stderr    diagnosticBuffer
	cmd       *exec.Cmd
	in        io.WriteCloser
	out       io.ReadCloser
	mu        sync.Mutex
	writeMu   sync.Mutex
	pending   map[string]chan Frame
	next      atomic.Uint64
	done      chan struct{}
	once      sync.Once
	terminal  error
	requests  chan struct{}
	requestWG sync.WaitGroup
	Notify    func(Frame)
	Handle    func(Frame) (any, error)
	Prepare   func(Frame) func() (any, error)
}

func New(cmd *exec.Cmd) (*Client, error) {
	acp.ProcessGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, out: out, pending: map[string]chan Frame{}, done: make(chan struct{}), requests: make(chan struct{}, 32), Notify: func(Frame) {}, Handle: func(Frame) (any, error) { return nil, errors.New("unsupported client request") }}
	cmd.Stderr = &c.stderr
	return c, nil
}
func (c *Client) Start() error {
	if err := c.cmd.Start(); err != nil {
		c.fail(err)
		return err
	}
	go c.read()
	// read drains stdout before Wait, including final events on process exit.
	return nil
}
func (c *Client) Done() <-chan struct{} { return c.done }
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return c.terminal
	}
	return ErrClosed
}
func (c *Client) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		if errors.Is(err, ErrClosed) {
			if detail := c.stderr.String(c.cmd.Env); detail != "" {
				err = fmt.Errorf("%w: %s", ErrClosed, detail)
			}
		}
		c.terminal = err
		c.mu.Unlock()
		close(c.done)
		acp.KillProcessGroup(c.cmd)
		_ = c.in.Close()
		_ = c.out.Close()
	})
}
func (c *Client) Close() { c.fail(ErrClosed) }
func (c *Client) Write(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > MaxFrame {
		return errors.New("agent frame too large")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	_, err = c.in.Write(append(b, '\n'))
	return err
}
func (c *Client) Call(ctx context.Context, method string, params any, pi bool, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := fmt.Sprintf("loom-%d", c.next.Add(1))
	key, _ := json.Marshal(id)
	ch := make(chan Frame, 1)
	c.mu.Lock()
	c.pending[string(key)] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, string(key)); c.mu.Unlock() }()
	req := map[string]any{"id": id, "method": method, "params": params}
	if pi {
		req = map[string]any{"id": id, "type": method}
		if params != nil {
			b, _ := json.Marshal(params)
			var p map[string]any
			if json.Unmarshal(b, &p) != nil {
				return errors.New("invalid Pi command")
			}
			for k, v := range p {
				req[k] = v
			}
		}
	}
	if err := c.Write(req); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	case f := <-ch:
		raw := f.Result
		if pi {
			if !f.Success {
				return providerError(f.Error)
			}
			raw = f.Data
		} else if len(f.Error) > 0 && string(f.Error) != "null" {
			return providerError(f.Error)
		}
		if frame, ok := result.(*Frame); ok {
			*frame = f
			return nil
		}
		if result != nil && len(raw) > 0 && json.Unmarshal(raw, result) != nil {
			return errors.New("invalid " + method + " response")
		}
		return nil
	}
}
func providerError(raw json.RawMessage) error {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return errors.New(s)
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return errors.New(e.Message)
	}
	return fmt.Errorf("agent rejected request: %s", raw)
}
func (c *Client) read() {
	defer func() { c.fail(ErrClosed); _ = c.cmd.Wait() }()
	sc := bufio.NewScanner(c.out)
	sc.Buffer(make([]byte, 64<<10), MaxFrame)
	for sc.Scan() {
		var f Frame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			c.fail(fmt.Errorf("invalid agent JSON: %w", err))
			return
		}
		f.Raw = append(json.RawMessage(nil), sc.Bytes()...)
		if f.Method == "" && (f.Type == "" || f.Type == "response") {
			c.mu.Lock()
			ch := c.pending[string(f.ID)]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- f:
				default:
				}
			} else {
				c.Notify(f)
			}
		} else if f.Method != "" && f.Type == "" && len(f.ID) > 0 {
			select {
			case c.requests <- struct{}{}:
			default:
				c.fail(errors.New("too many agent requests"))
				return
			}
			c.requestWG.Add(1)
			var prepared func() (any, error)
			if c.Prepare != nil {
				prepared = c.Prepare(f)
			}
			go func(f Frame) {
				defer c.requestWG.Done()
				defer func() { <-c.requests }()
				var value any
				var err error
				if prepared != nil {
					value, err = prepared()
				} else {
					value, err = c.Handle(f)
				}
				reply := map[string]any{"id": f.ID}
				if err != nil {
					reply["error"] = map[string]any{"code": -32601, "message": err.Error()}
				} else {
					reply["result"] = value
				}
				_ = c.Write(reply)
			}(f)
		} else {
			c.Notify(f)
		}
	}
	if err := sc.Err(); err != nil {
		c.fail(err)
	}
}

// Diagnostic text stays private to the runtime. Bound it and remove launch
// credentials before surfacing process failures; never write it to a log file.
// Diagnostics is a bounded, credential-redacted stderr buffer for owned native processes.
type Diagnostics = diagnosticBuffer

type diagnosticBuffer struct {
	mu   sync.Mutex
	text string
}

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > 16<<10 {
		b.text = b.text[len(b.text)-(16<<10):]
	}
	return len(p), nil
}

var diagnosticSecrets = regexp.MustCompile(`(?i)(bearer[ \t]+|(?:api[_-]?key|token|password|secret)["']?[ \t]*[=:][ \t]*["']?)[^\s"']+`)

func (b *diagnosticBuffer) String(env []string) string {
	b.mu.Lock()
	text := b.text
	b.mu.Unlock()
	if env == nil {
		env = os.Environ()
	}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if ok && len(value) >= 4 && (strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET")) {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	text = diagnosticSecrets.ReplaceAllString(text, "${1}[redacted]")
	return strings.TrimSpace(text)
}
