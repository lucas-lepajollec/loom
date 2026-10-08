package opencodehttp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenCodeServerProcess(t *testing.T) {
	marker := os.Getenv("LOOM_OPENCODE_SERVER_FIXTURE")
	if marker == "" {
		return
	}
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "--hostname 127.0.0.1 --port 0 --mdns=false") || len(os.Getenv("OPENCODE_SERVER_PASSWORD")) != 64 || os.Getenv("OPENCODE_SERVER_USERNAME") != "loom" {
		os.Exit(3)
	}
	f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(4)
	}
	_, _ = f.WriteString("start\n")
	_ = f.Close()
	fmt.Println("opencode server listening on http://127.0.0.1:4096")
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
	}
	os.Exit(0)
}
func TestOpenCodeServerLazySingletonAuthenticationAndClose(t *testing.T) {
	marker := t.TempDir() + "/starts"
	t.Setenv("LOOM_OPENCODE_SERVER_FIXTURE", marker)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "loom" || len(pass) != 64 {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"healthy":true,"version":"1.18.33"}`)
	})
	s := &Server{newClient: func(base, user, pass string) *Client {
		c := New(base, user, pass)
		c.HTTP.Transport = handlerTransport{handler}
		return c
	}}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	argv := []string{exe, "-test.run=^TestOpenCodeServerProcess$", "--"}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("server eagerly launched")
	}
	var wg sync.WaitGroup
	clients := make(chan *Client, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Client(ctx, argv, nil)
			if err != nil {
				t.Error(err)
				return
			}
			clients <- c
		}()
	}
	wg.Wait()
	close(clients)
	var first *Client
	for c := range clients {
		if first == nil {
			first = c
		}
		if first != c {
			t.Fatal("multiple clients/server launches")
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "start\n" {
		t.Fatal(string(data), err)
	}
	s.Close()
	if _, err = s.Client(ctx, argv, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(marker)
	if string(data) != "start\nstart\n" {
		t.Fatal(string(data))
	}
}
func TestOpenCodeServerRejectsUnauthenticatedServer(t *testing.T) {
	marker := t.TempDir() + "/starts"
	t.Setenv("LOOM_OPENCODE_SERVER_FIXTURE", marker)
	exe, _ := os.Executable()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"healthy":true,"version":"1.18.33"}`) })
	s := &Server{newClient: func(base, user, pass string) *Client {
		c := New(base, user, pass)
		c.HTTP.Transport = handlerTransport{h}
		return c
	}}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := s.Client(ctx, []string{exe, "-test.run=^TestOpenCodeServerProcess$", "--"}, nil)
	if err == nil || err.Error() != "OpenCode server did not enforce authentication" {
		t.Fatal(err)
	}
}
