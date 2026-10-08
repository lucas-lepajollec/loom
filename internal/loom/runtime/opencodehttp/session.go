package opencodehttp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type TurnConfig struct{ SessionID, Workdir, Model string }
type Session struct {
	Client *Client
	Broker *agent.RequestBroker
	Emit   agent.EventSink[agent.AgentEvent]
}

func (s *Session) Turn(ctx context.Context, cfg TurnConfig, prompt string, bound func(string)) (err error) {
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sid := cfg.SessionID
	var response struct {
		ID string `json:"id"`
	}
	if sid == "" {
		if err = s.Client.Call(ctx, "POST", "/session", cfg.Workdir, map[string]any{}, &response); err != nil {
			return err
		}
		sid = response.ID
		if sid == "" {
			return errors.New("OpenCode session ID missing")
		}
	} else {
		if err = s.Client.Call(ctx, "GET", "/session/"+url.PathEscape(sid), cfg.Workdir, nil, &response); err != nil {
			return err
		}
		if response.ID != sid {
			return errors.New("OpenCode resumed session ID mismatch")
		}
	}
	bound(sid)
	sub, err := s.Client.request(ctx, "GET", "/event", cfg.Workdir, nil)
	if err != nil {
		return err
	}
	defer sub.Body.Close()
	if !strings.HasPrefix(sub.Header.Get("Content-Type"), "text/event-stream") {
		return errors.New("OpenCode event endpoint is not SSE")
	}
	seed := make([]byte, 16)
	if _, err = rand.Read(seed); err != nil {
		return err
	}
	userID := "msg_" + hex.EncodeToString(seed)
	m := NewMapper(sid, userID)
	emit := func(e agent.AgentEvent) error {
		if !s.Emit(e) {
			return context.Canceled
		}
		return nil
	}
	if err = emit(agent.AgentEvent{Type: "turn.started", Runtime: "opencode", ThreadID: sid, TurnID: userID, Raw: agent.JSON(map[string]any{"sessionID": sid, "messageID": userID})}); err != nil {
		return err
	}
	var wg sync.WaitGroup
	replies := make(chan error, 32)
	defer func() { cancel(); s.Broker.Cancel(); wg.Wait() }()
	stopped := make(chan struct{})
	abortDone := make(chan struct{})
	go func() {
		defer close(abortDone)
		select {
		case <-parentCtx.Done():
		case <-stopped:
			if parentCtx.Err() == nil {
				return
			}
		}
		abort, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = s.Client.Call(abort, "POST", "/session/"+url.PathEscape(sid)+"/abort", cfg.Workdir, nil, nil)
	}()
	defer func() {
		close(stopped)
		cancel()
		s.Broker.Cancel()
		wg.Wait()
		<-abortDone
		if err != nil && parentCtx.Err() == nil {
			abort, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.Client.Call(abort, "POST", "/session/"+url.PathEscape(sid)+"/abort", cfg.Workdir, nil, nil)
			stop()
		}
		status := "completed"
		if err != nil {
			status = "failed"
			if parentCtx.Err() != nil || errors.Is(err, context.Canceled) {
				status = "cancelled"
			}
		}
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		_ = emit(agent.AgentEvent{Type: "turn.completed", Runtime: "opencode", ThreadID: sid, TurnID: userID, Status: status, Error: detail, Raw: agent.JSON(map[string]any{"error": detail})})
	}()
	body := map[string]any{"messageID": userID, "parts": []map[string]any{{"type": "text", "text": prompt}}}
	if cfg.Model != "" && cfg.Model != "default" {
		provider, model, ok := strings.Cut(cfg.Model, "/")
		if !ok || provider == "" || model == "" {
			return errors.New("OpenCode model must be provider/model")
		}
		body["model"] = map[string]any{"providerID": provider, "modelID": model}
	}
	// Subscribe before submitting. Do not retry a prompt after any HTTP failure.
	if err = s.Client.Call(ctx, "POST", "/session/"+url.PathEscape(sid)+"/prompt_async", cfg.Workdir, body, nil); err != nil {
		return err
	}
	scanner := bufio.NewScanner(sub.Body)
	scanner.Buffer(make([]byte, 64<<10), MaxFrame)
	var data strings.Builder
	dispatch := func(raw []byte) (bool, error) {
		events, e := m.Events(raw)
		if e != nil {
			return false, e
		}
		for _, event := range events {
			event.TurnID = userID
			if event.Type == "turn.completed" {
				return true, nil
			}
			if event.Type == "error" {
				_ = emit(event)
				return false, errors.New(event.Error)
			}
			if event.Request != nil {
				wait, e := s.Broker.Open(ctx, event)
				if e != nil {
					return false, e
				}
				r := *event.Request
				wg.Add(1)
				go func() {
					defer wg.Done()
					answer, e := wait(ctx)
					if e == nil && ctx.Err() == nil {
						e = s.reply(ctx, cfg.Workdir, sid, r, answer)
					}
					if e != nil && ctx.Err() == nil {
						select {
						case replies <- e:
						default:
						}
						cancel()
					}
				}()
			} else if e = emit(event); e != nil {
				return false, e
			}
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				done, e := dispatch([]byte(strings.TrimSuffix(data.String(), "\n")))
				data.Reset()
				if e != nil {
					return e
				}
				if done {
					return nil
				}
			}
		} else if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			if data.Len()+len(value) > MaxFrame {
				return errors.New("OpenCode SSE frame too large")
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
		select {
		case e := <-replies:
			return e
		default:
		}
	}
	select {
	case e := <-replies:
		return e
	default:
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	return errors.New("OpenCode event stream disconnected before completion")
}
func (s *Session) reply(ctx context.Context, dir, sid string, r agent.AgentRequest, a agent.RequestAnswer) error {
	id := url.PathEscape(strings.TrimPrefix(r.ID, "opencode:"))
	if r.Kind == "approval" {
		reply := "reject"
		switch a.Decision {
		case "allow_once":
			reply = "once"
		case "allow_always":
			reply = "always"
		}
		path := "/permission/" + id + "/reply"
		body := map[string]any{"reply": reply}
		if r.Method == "permission.updated" {
			path = "/session/" + url.PathEscape(sid) + "/permissions/" + id
			body = map[string]any{"response": reply}
		}
		return s.Client.Call(ctx, "POST", path, dir, body, nil)
	}
	if a.Decision == "cancel" {
		return s.Client.Call(ctx, "POST", "/question/"+id+"/reject", dir, nil, nil)
	}
	answers := [][]string{}
	for _, q := range r.Questions {
		values := a.Answers[q.ID]
		if values == nil {
			values = []string{}
		}
		answers = append(answers, values)
	}
	if len(answers) == 0 {
		return fmt.Errorf("OpenCode question has no answers")
	}
	return s.Client.Call(ctx, "POST", "/question/"+id+"/reply", dir, map[string]any{"answers": answers}, nil)
}

func (c *Client) List(ctx context.Context, dir string) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	err := c.Call(ctx, "GET", "/session", dir, nil, &rows)
	return rows, err
}
