package llamacpp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Supervisor owns one llama-server child and its observation caches. Each
// instance is independent; callers supply launch environment and exit policy.
// Do not copy a Supervisor after first use.
type Supervisor struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	done        chan struct{}
	generation  int
	managed     bool
	lastError   string
	ports       Ports
	routerMu    sync.Mutex
	switchMu    sync.Mutex
	helpMu      sync.Mutex
	help        llamaHelpCache
	modelMu     sync.Mutex
	models      map[string]ggufMetaCacheEnt
	propsMu     sync.Mutex
	propsSeen   time.Time
	propsRouter bool
	slotsMu     sync.Mutex
	slots       map[int]slotWatch
	recent      []Recent
	completed   int
	genTok      int
	promptTok   int
	tokSSum     float64
	sessionAt   time.Time
}

// Ports records the public front and internal engine ports of the owned child.
type Ports struct{ Public, Backend int }

// Launch is prepared by Loom, including its OS environment and application
// cleanup. Log is the same writer used for child output and exit diagnostics.
type Launch struct {
	Command        *exec.Cmd
	Ports          Ports
	Log            io.Writer
	UnexpectedExit func()
}

func NewSupervisor() *Supervisor {
	return &Supervisor{models: map[string]ggufMetaCacheEnt{}, slots: map[int]slotWatch{}, sessionAt: time.Now()}
}

func (s *Supervisor) SetLastError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = msg
}
func (s *Supervisor) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}
func (s *Supervisor) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managed = true
}
func (s *Supervisor) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managed = false
	s.stopLocked()
}
func (s *Supervisor) Managed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managed
}
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil && s.cmd.Process != nil
}
func (s *Supervisor) RunningPorts() Ports {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ports
}

// Start prepares the command under the owner lock, as did the original launcher.
func (s *Supervisor) Start(prepare func() Launch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(prepare())
}
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// Restart prepares the replacement only after the old child has stopped.
func (s *Supervisor) Restart(prepare func() Launch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return s.startLocked(prepare())
}

// RouterLock serializes INI publication and model loads for this owner.
func (s *Supervisor) RouterLock() sync.Locker { return &s.routerMu }

// SwitchLock preserves serialization of public model-selection requests.
func (s *Supervisor) SwitchLock() sync.Locker { return &s.switchMu }

func (s *Supervisor) startLocked(launch Launch) error {
	cmd := launch.Command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("llama-server : %w", err)
	}
	s.cmd = cmd
	s.ports = launch.Ports
	s.generation++
	gen := s.generation
	done := make(chan struct{})
	s.done = done
	s.lastError = ""
	go func() {
		err := cmd.Wait()
		close(done)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.generation != gen {
			return
		}
		s.cmd = nil
		if err != nil {
			msg := fmt.Sprintf("The model stopped (%v) — insufficient VRAM or crash", err)
			s.lastError = msg
			fmt.Fprintf(launch.Log, "[loom serve] %s\n", msg)
		} else {
			s.lastError = "The model stopped unexpectedly"
			fmt.Fprintf(launch.Log, "[loom serve] llama-server stopped unexpectedly\n")
		}
		if launch.UnexpectedExit != nil {
			go launch.UnexpectedExit()
		}
	}()
	return nil
}

func (s *Supervisor) stopLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	done := s.done
	s.generation++
	_ = s.cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		_ = s.cmd.Process.Kill()
		if done != nil {
			<-done
		}
	}
	s.cmd = nil
	s.done = nil
}

// WaitRouterUp preserves readiness polling and checks the owned child's state.
func (s *Supervisor) WaitRouterUp(budget time.Duration, reachable func() bool) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if reachable() {
			return nil
		}
		if !s.Running() {
			return fmt.Errorf("llama-server stopped: %s", s.LastError())
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out")
}

func BackendPortFor(public int) int {
	if public <= 0 {
		public = 8081
	}
	cand := public + 10000
	if cand > 65535 {
		cand = public - 10000
	}
	if cand < 1 || cand == public {
		if public != 18081 {
			return 18081
		}
		return 18082
	}
	return cand
}
