package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// A single node connection lives with the registry's process, independently of
// browser attachments. Binary input cannot be mistaken for resize JSON.
type nodeTerminalProcess struct {
	conn      *websocket.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	output    *io.PipeReader
	done      chan struct{}
	closeOnce sync.Once
	code      int
	waitErr   error
}

func startNodeTerminal(m RemoteMachine, dir, command string) (termProcess, error) {
	command = strings.TrimSpace(command)
	if strings.ContainsAny(command, "\x00") || len(command) > 2000 {
		return nil, errors.New("invalid command")
	}
	access, err := nodeMachineModuleAccess(m, "terminal")
	if err != nil {
		return nil, err
	}
	base, _, err := cleanNodeURL(access.URL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(base + "/api/node/terminal/ws")
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.RawQuery = url.Values{"cwd": {dir}, "command": {command}, "cols": {"100"}, "rows": {"30"}}.Encode()
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dialCancel()
	client := &http.Client{Transport: nodeClient.Transport, CheckRedirect: nodeClient.CheckRedirect}
	conn, resp, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + access.Token}}, HTTPClient: client})
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case 409:
				return nil, errors.New(nodeTerminalDisabled)
			case 429:
				return nil, errors.New("node terminal process limit reached")
			}
		}
		return nil, errors.New("node terminal connection failed; check machine access and directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	p := &nodeTerminalProcess{conn: conn, ctx: ctx, cancel: cancel, output: reader, done: make(chan struct{}), code: -1}
	conn.SetReadLimit(1 << 20)
	go p.pump(writer)
	return p, nil
}
func (p *nodeTerminalProcess) pump(out *io.PipeWriter) {
	defer close(p.done)
	defer out.Close()
	defer p.cancel()
	defer p.conn.CloseNow()
	for {
		typ, data, err := p.conn.Read(p.ctx)
		if err != nil {
			p.waitErr = errors.New("node terminal connection closed")
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := out.Write(data); err != nil {
				p.waitErr = err
				return
			}
			continue
		}
		var msg struct {
			Exit bool `json:"exit"`
			Code *int `json:"exit_code"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.Exit {
			if msg.Code != nil {
				p.code = *msg.Code
			}
			return
		}
	}
}
func (p *nodeTerminalProcess) Read(b []byte) (int, error) { return p.output.Read(b) }
func (p *nodeTerminalProcess) Write(b []byte) (int, error) {
	err := p.conn.Write(p.ctx, websocket.MessageBinary, b)
	if err != nil {
		return 0, errors.New("node terminal input unavailable")
	}
	return len(b), nil
}
func (p *nodeTerminalProcess) Resize(cols, rows uint16) error {
	if !validTerminalSize(cols, rows) {
		return errors.New("invalid size")
	}
	data, _ := json.Marshal(map[string]any{"resize": []uint16{cols, rows}})
	if err := p.conn.Write(p.ctx, websocket.MessageText, data); err != nil {
		return errors.New("node terminal resize unavailable")
	}
	return nil
}
func (p *nodeTerminalProcess) Close() error {
	p.closeOnce.Do(func() { p.cancel(); p.conn.CloseNow(); _ = p.output.Close() })
	return nil
}
func (p *nodeTerminalProcess) Wait() (int, error) {
	<-p.done
	return p.code, p.waitErr
}
