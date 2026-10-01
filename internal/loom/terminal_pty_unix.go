//go:build !windows

package loom

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

const ptySupported = true

type unixPTY struct {
	f   *os.File
	cmd *exec.Cmd
}

func startPTY(argv []string, dir string, env []string) (termProcess, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		return nil, err
	}
	return &unixPTY{f: f, cmd: cmd}, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixPTY) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > 1000 || rows > 500 {
		return errors.New("taille invalide")
	}
	return pty.Setsize(p.f, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close ends the whole session (the shell is a session leader: SIGHUP its
// process group, then kill if it lingers).
func (p *unixPTY) Close() error {
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		_ = p.cmd.Process.Kill()
	}
	return p.f.Close()
}

func (p *unixPTY) Wait() (int, error) {
	err := p.cmd.Wait()
	_ = p.f.Close()
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode(), err
	}
	return -1, err
}
