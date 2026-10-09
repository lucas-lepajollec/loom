package opencodehttp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type fixtureRow struct {
	Frame  json.RawMessage      `json:"frame"`
	Events []string             `json:"events"`
	Text   string               `json:"text"`
	Error  string               `json:"error"`
	Answer *agent.RequestAnswer `json:"answer"`
	Reply  *struct {
		Path string `json:"path"`
		Body any    `json:"body"`
	} `json:"reply"`
}

func fixture(t *testing.T, name string) []fixtureRow {
	t.Helper()
	f, err := os.Open(filepath.Join("../../testdata/agents/opencode", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := []fixtureRow{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var row fixtureRow
		if err = json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err = scan.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}
func TestOpenCodeFixtureMapper(t *testing.T) {
	for _, name := range []string{"normal", "error", "legacy-permission", "question-cancel"} {
		t.Run(name, func(t *testing.T) {
			m := NewMapper("ses_fixture", "USER_ID")
			for _, r := range fixture(t, name) {
				events, err := m.Events(r.Frame)
				if err != nil {
					t.Fatal(err)
				}
				got := []string{}
				for _, e := range events {
					got = append(got, e.Type)
					if r.Text != "" && e.Delta != r.Text {
						t.Fatalf("text=%q want=%q", e.Delta, r.Text)
					}
					if r.Error != "" && e.Error != r.Error {
						t.Fatal(e)
					}
					if len(e.Raw) == 0 {
						t.Fatal("raw missing")
					}
				}
				if !reflect.DeepEqual(got, r.Events) {
					t.Fatalf("events=%v want=%v frame=%s", got, r.Events, r.Frame)
				}
			}
		})
	}
}

// A socket-enabled run uses a real httptest server. Restricted sandboxes replay
// the very same HTTP handlers through streaming pipes and httptest writers.
type handlerTransport struct{ handler http.Handler }
type pipeWriter struct {
	header http.Header
	pipe   *io.PipeWriter
	ready  chan int
	once   sync.Once
}

func (w *pipeWriter) Header() http.Header         { return w.header }
func (w *pipeWriter) WriteHeader(code int)        { w.once.Do(func() { w.ready <- code }) }
func (w *pipeWriter) Write(b []byte) (int, error) { w.WriteHeader(200); return w.pipe.Write(b) }
func (w *pipeWriter) Flush()                      { w.WriteHeader(200) }
func (tr handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/event" {
		rec := httptest.NewRecorder()
		tr.handler.ServeHTTP(rec, req)
		return rec.Result(), nil
	}
	reader, writer := io.Pipe()
	w := &pipeWriter{header: http.Header{}, pipe: writer, ready: make(chan int, 1)}
	go func() { tr.handler.ServeHTTP(w, req); w.WriteHeader(200); _ = writer.Close() }()
	select {
	case code := <-w.ready:
		go func() { <-req.Context().Done(); _ = reader.CloseWithError(req.Context().Err()) }()
		return &http.Response{StatusCode: code, Header: w.header.Clone(), Body: reader, Request: req}, nil
	case <-req.Context().Done():
		_ = reader.Close()
		return nil, req.Context().Err()
	}
}
func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Logf("sandbox socket failure: %v; replaying through httptest handlers without sockets", err)
		c := New("http://127.0.0.1:1", "loom", "fixture-secret")
		c.HTTP.Transport = handlerTransport{h}
		return c
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: h}}
	server.Start()
	t.Cleanup(server.Close)
	return New(server.URL, "loom", "fixture-secret")
}
func TestOpenCodeHTTPFixtureTurn(t *testing.T) {
	for _, name := range []string{"normal", "error", "legacy-permission", "question-cancel", "resume", "cancel"} {
		t.Run(name, func(t *testing.T) {
			rows := fixture(t, "normal")
			if name == "error" || name == "legacy-permission" || name == "question-cancel" {
				rows = fixture(t, name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			submitted := make(chan string, 1)
			answered := make(chan bool, 32)
			aborted := make(chan bool, 1)
			sentReplies := []string{}
			var replyMu sync.Mutex
			creates, resumes, prompts := 0, 0, 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "loom" || pass != "fixture-secret" {
					w.WriteHeader(401)
					return
				}
				if r.URL.Path != "/global/health" && r.URL.Query().Get("directory") != "/fixture" {
					t.Error("directory missing")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/session":
					creates++
					fmt.Fprint(w, `{"id":"ses_fixture"}`)
				case "/session/ses_fixture":
					resumes++
					fmt.Fprint(w, `{"id":"ses_fixture"}`)
				case "/session/ses_fixture/prompt_async":
					prompts++
					var body struct {
						MessageID string `json:"messageID"`
						Parts     []struct {
							Text string `json:"text"`
						} `json:"parts"`
						Model map[string]string `json:"model"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body.Parts) != 1 || body.Parts[0].Text != "Fixture prompt" || body.Model["modelID"] != "fixture-model" {
						t.Errorf("prompt=%+v", body)
					}
					submitted <- body.MessageID
					w.WriteHeader(204)
				case "/session/ses_fixture/abort":
					select {
					case aborted <- true:
					default:
					}
					fmt.Fprint(w, "true")
				case "/event":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
					w.(http.Flusher).Flush()
					var userID string
					select {
					case userID = <-submitted:
					case <-r.Context().Done():
						return
					}
					if name == "cancel" {
						cancel()
						<-r.Context().Done()
						return
					}
					for _, row := range rows {
						raw := strings.ReplaceAll(string(row.Frame), "USER_ID", userID)
						fmt.Fprintf(w, "data: %s\n\n", raw)
						w.(http.Flusher).Flush()
						if row.Answer != nil {
							select {
							case <-answered:
							case <-r.Context().Done():
								return
							}
						}
					}
					if name == "legacy-permission" || name == "question-cancel" {
						fmt.Fprint(w, "data: {\"type\":\"session.idle\",\"properties\":{\"sessionID\":\"ses_fixture\"}}\n\n")
						w.(http.Flusher).Flush()
					}
					<-r.Context().Done()
				default:
					var body any
					if r.Body != nil {
						_ = json.NewDecoder(r.Body).Decode(&body)
					}
					matched := false
					for _, row := range rows {
						if row.Reply != nil && row.Reply.Path == r.URL.Path {
							matched = true
							if !reflect.DeepEqual(body, row.Reply.Body) {
								t.Errorf("reply=%v want=%v", body, row.Reply.Body)
							}
						}
					}
					if !matched {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					replyMu.Lock()
					sentReplies = append(sentReplies, r.URL.Path)
					replyMu.Unlock()
					fmt.Fprint(w, "true")
					answered <- true
				}
			})
			c := testClient(t, handler)
			events := []agent.AgentEvent{}
			var eventsMu sync.Mutex
			var broker *agent.RequestBroker
			sink := func(e agent.AgentEvent) bool {
				eventsMu.Lock()
				events = append(events, e)
				eventsMu.Unlock()
				if e.Request != nil {
					for _, row := range rows {
						if row.Answer != nil {
							var f Frame
							_ = json.Unmarshal(row.Frame, &f)
							var p struct {
								ID string `json:"id"`
							}
							_ = json.Unmarshal(f.Properties, &p)
							if e.Request.ID == "opencode:"+p.ID {
								a := *row.Answer
								go func() {
									if err := broker.Resolve(e.Request.ID, a); err != nil {
										t.Error(err)
									}
								}()
							}
						}
					}
				}
				return true
			}
			broker = agent.NewRequestBroker("opencode", sink)
			s := &Session{Client: c, Broker: broker, Emit: sink}
			sid := ""
			if name == "resume" {
				sid = "ses_fixture"
			}
			err := s.Turn(ctx, TurnConfig{SessionID: sid, Workdir: "/fixture", Model: "fixture/fixture-model"}, "Fixture prompt", func(id string) {
				if id != "ses_fixture" {
					t.Error(id)
				}
			})
			if name == "error" {
				if err == nil || err.Error() != "Provider overloaded: try later." {
					t.Fatal(err)
				}
			} else if name == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				select {
				case <-aborted:
				case <-time.After(5 * time.Second): // generous under -race on shared CI runners
					t.Fatal("abort not sent")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if prompts != 1 || (name == "resume" && (creates != 0 || resumes != 1)) || (name != "resume" && creates != 1) {
				t.Fatalf("creates=%d resumes=%d prompts=%d", creates, resumes, prompts)
			}
			eventsMu.Lock()
			defer eventsMu.Unlock()
			if len(events) < 2 || events[0].Type != "turn.started" || events[len(events)-1].Type != "turn.completed" {
				t.Fatal(events)
			}
			count := 0
			answer := ""
			for _, e := range events {
				if e.Type == "turn.completed" {
					count++
				}
				if e.Stream == "assistant_text" {
					answer += e.Delta
				}
			}
			if count != 1 {
				t.Fatal("duplicate completion", events)
			}
			if name == "normal" || name == "resume" {
				if answer != "Hello world" {
					t.Fatal(answer)
				}
			}
		})
	}
}
func TestOpenCodeProviderCatalogAndVerbatimErrors(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/provider" {
			fmt.Fprint(w, `{"all":[{"id":"fixture","models":{"test":{"name":"Fixture"}}},{"id":"disconnected","models":{"hidden":{"name":"Hidden"}}}],"connected":["fixture"]}`)
		} else {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"name":"UnknownError","data":{"message":"session open in another OpenCode window"}}`)
		}
	})
	c := testClient(t, handler)
	models, err := c.Models(context.Background(), "")
	if err != nil || len(models) != 1 || !strings.Contains(string(models[0]), `"model":"fixture/test"`) {
		t.Fatal(models, err)
	}
	err = c.Call(context.Background(), "POST", "/failure", "", nil, nil)
	if err == nil || err.Error() != "session open in another OpenCode window" {
		t.Fatal(err)
	}
}
