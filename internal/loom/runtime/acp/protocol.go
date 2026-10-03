package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ACP v1 is NDJSON JSON-RPC, with requests in both directions. The reader
// stays available while client requests (notably permission) await the user.
const MaxFrame = 4 << 20
const maxInboundRequests = 32

var ErrClosed = errors.New("ACP agent disconnected")

type Frame struct {
	Replied func()          `json:"-"`
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return "ACP request rejected by the agent" } // upstream errors can contain secrets

type Client struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stdin    io.WriteCloser
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan Frame
	next     atomic.Uint64
	done     chan struct{}
	requests chan struct{}
	once     sync.Once
	Handler  func(*Frame) (any, error)
	Notify   func(Frame)
}

// NewClient prepares a transport for a resolved command. Install handlers before Start.
func NewClient(cmd *exec.Cmd) (*Client, error) {
	ProcessGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrClosed
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, ErrClosed
	}
	cmd.Stderr = io.Discard // never expose MCP env, auth diagnostics or agent stderr
	c := &Client{cmd: cmd, stdout: out, stdin: in, pending: map[string]chan Frame{}, done: make(chan struct{}), requests: make(chan struct{}, maxInboundRequests)}
	// Start/reader happen after handlers are installed by Start().
	c.Notify = func(Frame) {}
	c.Handler = func(*Frame) (any, error) { return nil, errors.New("unsupported ACP method") }
	return c, nil
}
func (c *Client) Start() error {
	if err := c.cmd.Start(); err != nil {
		c.Close()
		return errors.New("could not launch the ACP agent")
	}
	go c.read(c.stdout)
	go func() { _ = c.cmd.Wait(); c.Close() }()
	return nil
}
func (c *Client) Close() {
	c.once.Do(func() { close(c.done); KillProcessGroup(c.cmd); _ = c.stdin.Close(); _ = c.stdout.Close() })
}
func (c *Client) Write(v any) error {
	b, err := json.Marshal(v)
	if err != nil || len(b) > MaxFrame {
		return errors.New("ACP message too long")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	_, err = c.stdin.Write(append(b, '\n'))
	if err != nil {
		return ErrClosed
	}
	return nil
}
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	id, _ := json.Marshal(c.next.Add(1))
	ch := make(chan Frame, 1)
	c.mu.Lock()
	c.pending[string(id)] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, string(id)); c.mu.Unlock() }()
	if err := c.Write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	case f := <-ch:
		if f.Error != nil {
			return f.Error
		}
		if result != nil && json.Unmarshal(f.Result, result) != nil {
			return errors.New("invalid ACP response")
		}
		return nil
	}
}
func (c *Client) Notification(method string, params any) error {
	return c.Write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (c *Client) read(out io.Reader) {
	defer c.Close()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), MaxFrame)
	for sc.Scan() {
		var f Frame
		if json.Unmarshal(sc.Bytes(), &f) != nil || f.JSONRPC != "2.0" {
			return
		}
		if f.Method == "" {
			c.mu.Lock()
			ch := c.pending[string(f.ID)]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- f:
				default:
				}
			}
		} else if len(f.ID) == 0 {
			c.Notify(f) // ordered updates, including those immediately before prompt completion
		} else {
			// Pending permissions cannot block ordered updates, but an agent must
			// not create unlimited blocked handlers. Disconnect on overload.
			select {
			case c.requests <- struct{}{}:
			default:
				return
			}
			go func(f Frame) {
				defer func() { <-c.requests }()
				defer func() {
					if f.Replied != nil {
						f.Replied()
					}
				}()
				result, err := c.Handler(&f)
				reply := map[string]any{"jsonrpc": "2.0", "id": f.ID}
				if err != nil {
					code := -32602
					var rpcError *RPCError
					if errors.As(err, &rpcError) {
						code = rpcError.Code
					}
					reply["error"] = &RPCError{Code: code, Message: "client request rejected"}
				} else {
					reply["result"] = result
				}
				_ = c.Write(reply)
			}(f)
		}
	}
}

// Done closes when the owned transport disconnects.
func (c *Client) Done() <-chan struct{} { return c.done }
