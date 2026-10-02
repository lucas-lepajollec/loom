package llamacpp

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A test-binary child gives the owner a real process without sockets, models or
// a shell. Interrupt handling remains the platform's native exec behavior.
func TestSupervisorChild(t *testing.T) {
	switch os.Getenv("LOOM_SUPERVISOR_TEST_CHILD") {
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "exit":
		os.Exit(3)
	}
}

func childLaunch(mode string, exited func()) func() Launch {
	return func() Launch {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSupervisorChild$")
		cmd.Env = append(os.Environ(), "LOOM_SUPERVISOR_TEST_CHILD="+mode)
		return Launch{Command: cmd, Ports: Ports{Public: 8081, Backend: 18081}, Log: io.Discard, UnexpectedExit: exited}
	}
}

func TestSupervisorRequestedStopAndRestart(t *testing.T) {
	s, other := NewSupervisor(), NewSupervisor()
	exited := make(chan struct{}, 2)
	s.Init()
	t.Cleanup(s.Shutdown)
	if err := s.Start(childLaunch("wait", func() { exited <- struct{}{} })); err != nil {
		t.Fatal(err)
	}
	if !s.Managed() || !s.Running() || other.Managed() || other.Running() {
		t.Fatal("process ownership leaked")
	}
	if got := s.RunningPorts(); got != (Ports{8081, 18081}) {
		t.Fatalf("ports: %+v", got)
	}
	old := s.cmd
	if err := s.Restart(func() Launch {
		if s.cmd != nil {
			t.Error("replacement prepared before stop")
		}
		return childLaunch("wait", func() { exited <- struct{}{} })()
	}); err != nil {
		t.Fatal(err)
	}
	if old.ProcessState == nil {
		t.Fatal("old child not reaped")
	}
	s.Shutdown()
	if s.Running() || s.Managed() || s.LastError() != "" {
		t.Fatal("requested stop reported as crash")
	}
	select {
	case <-exited:
		t.Fatal("requested stop ran application cleanup")
	default:
	}
}

func TestSupervisorUnexpectedExit(t *testing.T) {
	s := NewSupervisor()
	exited := make(chan struct{})
	if err := s.Start(childLaunch("exit", func() { close(exited) })); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("exit cleanup missing")
	}
	if s.Running() || !strings.Contains(s.LastError(), "insufficient VRAM") {
		t.Fatalf("unexpected exit: %q", s.LastError())
	}
	if err := s.WaitRouterUp(time.Second, func() bool { return false }); err == nil || !strings.Contains(err.Error(), s.LastError()) {
		t.Fatalf("readiness: %v", err)
	}
	if err := s.WaitRouterUp(time.Second, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	s.SetLastError("retained")
	err := s.Start(func() Launch {
		return Launch{Command: exec.Command(filepath.Join(t.TempDir(), "missing")), Log: io.Discard}
	})
	if err == nil || !strings.HasPrefix(err.Error(), "llama-server : ") || s.LastError() != "retained" {
		t.Fatalf("start failure: %v, %q", err, s.LastError())
	}
}

func TestSupervisorCachesAreIndependent(t *testing.T) {
	a, b := NewSupervisor(), NewSupervisor()
	probes := 0
	probe := func() bool { probes++; return true }
	if !a.RouterModeCached(probe) || !a.RouterModeCached(probe) || probes != 1 {
		t.Fatal("props cache lost")
	}
	if b.RouterModeCached(func() bool { return false }) {
		t.Fatal("props cache shared")
	}
	a.InvalidateRouterMode()
	if a.RouterModeCached(func() bool { return false }) {
		t.Fatal("props invalidation lost")
	}
	bin := filepath.Join(t.TempDir(), "server")
	if err := os.WriteFile(bin, []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	read := func() string { reads++; return "--parallel N  number of server slots\n" }
	a.HelpText(bin, read)
	a.HelpText(bin, read)
	if reads != 1 || len(a.HelpFlags()) != 1 || b.HelpFlags() != nil {
		t.Fatal("help cache ownership")
	}
	flags := a.HelpFlags()
	flags[0].ID = "changed"
	if a.HelpFlags()[0].ID == "changed" {
		t.Fatal("help snapshot not copied")
	}
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	changed := st.ModTime().Add(time.Second)
	if err := os.Chtimes(bin, changed, changed); err != nil {
		t.Fatal(err)
	}
	a.HelpText(bin, read)
	if reads != 2 {
		t.Fatal("help mtime invalidation lost")
	}
	a.HelpText("other", func() string { reads++; return "" })
	a.HelpText("other", func() string { reads++; return "" })
	if reads != 4 {
		t.Fatal("empty help must be retried")
	}

	model := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(model, []byte("invalid GGUF"), 0600); err != nil {
		t.Fatal(err)
	}
	weights := 0
	weight := func(string) int64 { weights++; return 42 }
	if got := a.LoadGGUFMeta(model, weight); got.Path != model || got.FileBytes != 42 {
		t.Fatalf("metadata: %+v", got)
	}
	a.LoadGGUFMeta(model, weight)
	b.LoadGGUFMeta(model, weight)
	if weights != 2 {
		t.Fatal("model cache ownership")
	}
	if err := os.WriteFile(model, []byte("changed GGUF size"), 0600); err != nil {
		t.Fatal(err)
	}
	a.LoadGGUFMeta(model, weight)
	if weights != 3 {
		t.Fatal("model size invalidation lost")
	}
	if got := a.LoadGGUFMeta("missing", weight); got.Path != "" || got.FileBytes != 0 {
		t.Fatal("missing metadata not unknown")
	}

	a.NoteServerSlots([]Slot{{ID: 0, IDTask: 7, IsProcessing: true, NDecoded: 12, PromptN: 40}})
	recent, _ := a.NoteServerSlots([]Slot{{ID: 0, IDTask: 7}})
	if len(recent) != 1 || recent[0].Tokens != 12 || recent[0].Prompt != 40 {
		t.Fatalf("completion: %+v", recent)
	}
	if a.ServerStats(0, 0)["completed"] != 1 || b.ServerStats(0, 0)["completed"] != 0 {
		t.Fatal("slot cache ownership")
	}
	a.NoteServerSlots([]Slot{{ID: 0, IDTask: 7}})
	if a.ServerStats(0, 0)["completed"] != 1 {
		t.Fatal("completion recorded twice")
	}
	a.ResetServerWatch()
	if a.ServerStats(0, 0)["completed"] != 0 {
		t.Fatal("watch reset lost")
	}
}
