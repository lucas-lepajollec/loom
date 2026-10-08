//go:build !windows

package loom

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

const ptySupported = true

type unixPTY struct {
	f         *os.File
	cmd       *exec.Cmd
	closeOnce sync.Once
	closeErr  error
}

func startPTY(argv []string, dir string, env []string) (termProcess, error) {
	return startPTYSize(argv, dir, env, 100, 30)
}

func startPTYSize(argv []string, dir string, env []string, cols, rows uint16) (termProcess, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &unixPTY{f: f, cmd: cmd}, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixPTY) Resize(cols, rows uint16) error {
	if !validTerminalSize(cols, rows) {
		return errors.New("invalid size")
	}
	return pty.Setsize(p.f, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close ends the whole session (the shell is a session leader: SIGHUP its
// process group, then SIGKILL the group so descendants cannot linger).
func (p *unixPTY) Close() error {
	p.closeOnce.Do(func() {
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		p.closeErr = p.f.Close()
	})
	return p.closeErr
}

func (p *unixPTY) Wait() (int, error) {
	err := p.cmd.Wait()
	p.closeOnce.Do(func() { p.closeErr = p.f.Close() })
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode(), err
	}
	return -1, err
}
