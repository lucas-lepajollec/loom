package opencodehttp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

// Server owns exactly the child it launches. Callers share one lazy instance;
// cancellation aborts a session, never the process serving other discussions.
type Server struct {
	mu        sync.Mutex
	client    *Client
	cmd       *exec.Cmd
	done      chan struct{}
	newClient func(string, string, string) *Client
	envSig    string
	// Busy reports turns in flight: a changed configuration waits for them
	// instead of killing the shared server under a running answer.
	Busy func() bool
}

func (s *Server) Client(ctx context.Context, argv []string, env []string) (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		select {
		case <-s.done:
			s.client = nil
			s.cmd = nil
		default:
			if envSignature(env) == s.envSig || (s.Busy != nil && s.Busy()) {
				return s.client, nil
			}
			acp.KillProcessGroup(s.cmd)
			<-s.done
			s.client, s.cmd = nil, nil
		}
	}
	if len(argv) == 0 {
		return nil, errors.New("OpenCode executable required")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], append(argv[1:], "serve", "--hostname", "127.0.0.1", "--port", "0", "--mdns=false")...)
	// Native credentials/config stay CLI-owned. The caller adds Loom's models
	// and their keys only when the user enabled Loom models for OpenCode.
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "OPENCODE_SERVER_USERNAME=loom", "OPENCODE_SERVER_PASSWORD="+hex.EncodeToString(secret))
	acp.ProcessGroup(cmd)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			line := scanner.Text()
			if i := strings.Index(line, "opencode server listening on "); i >= 0 {
				select {
				case address <- strings.TrimSpace(line[i+len("opencode server listening on "):]):
				default:
				}
			}
		}
	}()
	fail := func(err error) (*Client, error) { acp.KillProcessGroup(cmd); _ = stdout.Close(); return nil, err }
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	var base string
	select {
	case base = <-address:
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-done:
		return fail(errors.New("OpenCode server exited during startup"))
	case <-timeout.C:
		return fail(errors.New("OpenCode server startup timed out"))
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" {
		return fail(errors.New("invalid OpenCode loopback address"))
	}
	factory := s.newClient
	if factory == nil {
		factory = New
	}
	c := factory(base, "loom", hex.EncodeToString(secret))
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err = c.Health(check); err != nil {
		return fail(err)
	}
	// Verify the server really requires auth before sharing any context.
	unauth := factory(base, "", "")
	req, err := http.NewRequestWithContext(check, "GET", base+"/global/health", nil)
	if err != nil {
		return fail(err)
	}
	resp, err := unauth.HTTP.Do(req)
	if err != nil {
		return fail(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		return fail(errors.New("OpenCode server did not enforce authentication"))
	}
	s.client, s.cmd, s.done, s.envSig = c, cmd, done, envSignature(env)
	return c, nil
}

func envSignature(env []string) string {
	sum := sha256.Sum256([]byte(strings.Join(env, "\x00")))
	return hex.EncodeToString(sum[:])
}
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		acp.KillProcessGroup(s.cmd)
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	}
	s.cmd = nil
	s.client = nil
}
