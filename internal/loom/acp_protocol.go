package loom

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
const acpMaxFrame = 4 << 20

var errACPClosed = errors.New("agent ACP déconnecté")

type acpFrame struct {
	replied func()
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}
type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *acpRPCError) Error() string { return "requête ACP refusée par l’agent" } // upstream errors can contain secrets

type acpClient struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stdin   io.WriteCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan acpFrame
	next    atomic.Uint64
	done    chan struct{}
	once    sync.Once
	handler func(*acpFrame) (any, error)
	notify  func(acpFrame)
}

func startACPClient(command string, args []string, cwd string) (*acpClient, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	acpProcessGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errACPClosed
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, errACPClosed
	}
	cmd.Stderr = io.Discard // never expose MCP env, auth diagnostics or agent stderr
	c := &acpClient{cmd: cmd, stdout: out, stdin: in, pending: map[string]chan acpFrame{}, done: make(chan struct{})}
	// Start/reader happen after handlers are installed by start().
	c.notify = func(acpFrame) {}
	c.handler = func(*acpFrame) (any, error) { return nil, errors.New("méthode ACP non prise en charge") }
	return c, nil
}
func (c *acpClient) start() error {
	if err := c.cmd.Start(); err != nil {
		c.close()
		return errors.New("impossible de lancer l’agent ACP")
	}
	go c.read(c.stdout)
	go func() { _ = c.cmd.Wait(); c.close() }()
	return nil
}
func (c *acpClient) close() {
	c.once.Do(func() { close(c.done); acpKillProcessGroup(c.cmd); _ = c.stdin.Close(); _ = c.stdout.Close() })
}
func (c *acpClient) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil || len(b) > acpMaxFrame {
		return errors.New("message ACP trop long")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return errACPClosed
	default:
	}
	_, err = c.stdin.Write(append(b, '\n'))
	if err != nil {
		return errACPClosed
	}
	return nil
}
func (c *acpClient) call(ctx context.Context, method string, params any, result any) error {
	id, _ := json.Marshal(c.next.Add(1))
	ch := make(chan acpFrame, 1)
	c.mu.Lock()
	c.pending[string(id)] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, string(id)); c.mu.Unlock() }()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errACPClosed
	case f := <-ch:
		if f.Error != nil {
			return f.Error
		}
		if result != nil && json.Unmarshal(f.Result, result) != nil {
			return errors.New("réponse ACP invalide")
		}
		return nil
	}
}
func (c *acpClient) notification(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (c *acpClient) read(out io.Reader) {
	defer c.close()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), acpMaxFrame)
	for sc.Scan() {
		var f acpFrame
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
			c.notify(f) // ordered updates, including those immediately before prompt completion
		} else {
			go func(f acpFrame) {
				defer func() {
					if f.replied != nil {
						f.replied()
					}
				}()
				result, err := c.handler(&f)
				reply := map[string]any{"jsonrpc": "2.0", "id": f.ID}
				if err != nil {
					code := -32602
					var rpcError *acpRPCError
					if errors.As(err, &rpcError) {
						code = rpcError.Code
					}
					reply["error"] = &acpRPCError{Code: code, Message: "requête client refusée"}
				} else {
					reply["result"] = result
				}
				_ = c.write(reply)
			}(f)
		}
	}
}
