//go:build windows

package loom

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ptySupported = conPTYAvailable()

func conPTYAvailable() bool {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	for _, name := range []string{"CreatePseudoConsole", "ResizePseudoConsole", "ClosePseudoConsole"} {
		if kernel.NewProc(name).Find() != nil {
			return false
		}
	}
	return true
}

type windowsPTY struct {
	input, output *os.File
	mu            sync.Mutex // serializes resize and native resource cleanup
	console, job  windows.Handle
	done          chan struct{}
	code          int
	waitErr       error
	closeOnce     sync.Once
	closeErr      error
}

func startPTY(argv []string, dir string, env []string) (_ termProcess, err error) {
	if !ptySupported {
		return nil, errors.New("les terminaux ne sont pas encore disponibles sous Windows : ConPTY nécessite Windows 10 1809 ou ultérieur")
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("commande invalide")
	}
	exe, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, err
	}
	app, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	args := append([]string{exe}, argv[1:]...)
	line, err := windows.UTF16PtrFromString(windowsTerminalCommandLine(args, windows.ComposeCommandLine))
	if err != nil {
		return nil, err
	}
	var cwd *uint16
	if dir != "" {
		cwd, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			return nil, err
		}
	}
	block, err := windowsTerminalEnvironment(append(os.Environ(), env...))
	if err != nil {
		return nil, err
	}

	// ConPTY requires synchronous pipes. Neither end is inheritable; the child
	// receives its console through the process attribute instead.
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err = windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, err
	}
	defer windows.CloseHandle(inRead)
	if err = windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inWrite)
		return nil, err
	}
	defer windows.CloseHandle(outWrite)
	p := &windowsPTY{input: os.NewFile(uintptr(inWrite), "conpty-input"), output: os.NewFile(uintptr(outRead), "conpty-output"), done: make(chan struct{}), code: -1}
	defer func() {
		if err != nil {
			_ = p.Close()
		}
	}()
	if err = windows.CreatePseudoConsole(windows.Coord{X: 100, Y: 30}, inRead, outWrite, 0, &p.console); err != nil {
		return nil, err
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	// This attribute takes the opaque HPCON value itself, not its address.
	// Reinterpret its pointer-sized storage without a uintptr-to-pointer cast.
	consoleValue := *(*unsafe.Pointer)(unsafe.Pointer(&p.console))
	if err = attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, consoleValue, unsafe.Sizeof(p.console)); err != nil {
		return nil, err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	p.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(p.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	var pi windows.ProcessInformation
	// Assign the suspended process before any shell code can spawn a child.
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err = windows.CreateProcess(app, line, nil, nil, false, flags, &block[0], cwd, &si.StartupInfo, &pi); err != nil {
		return nil, err
	}
	defer windows.CloseHandle(pi.Thread)
	defer func() {
		if err != nil {
			_ = windows.TerminateProcess(pi.Process, 1)
			_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
			_ = windows.CloseHandle(pi.Process)
		}
	}()
	if err = windows.AssignProcessToJobObject(p.job, pi.Process); err != nil {
		return nil, err
	}
	if _, err = windows.ResumeThread(pi.Thread); err != nil {
		return nil, err
	}
	// Wait independently of output reads: closing ConPTY on shell exit breaks
	// the output pipe so Terminal.pump can reach Wait. Keep output open here to
	// drain the final frame during ClosePseudoConsole.
	go func() {
		_, p.waitErr = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		if p.waitErr == nil {
			var code uint32
			p.waitErr = windows.GetExitCodeProcess(pi.Process, &code)
			if p.waitErr == nil {
				p.code = int(code)
				if code != 0 {
					p.waitErr = fmt.Errorf("processus terminé avec le code %d", code)
				}
			}
		}
		_ = windows.CloseHandle(pi.Process)
		p.release()
		close(p.done)
	}()
	return p, nil
}

func (p *windowsPTY) Read(b []byte) (int, error)  { return p.output.Read(b) }
func (p *windowsPTY) Write(b []byte) (int, error) { return p.input.Write(b) }
func (p *windowsPTY) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > 1000 || rows > 500 {
		return errors.New("taille invalide")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.console == 0 {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *windowsPTY) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.job != 0 {
		_ = windows.CloseHandle(p.job) // kills all descendants, including detached children
		p.job = 0
	}
	if p.console != 0 {
		windows.ClosePseudoConsole(p.console)
		p.console = 0
	}
}

func (p *windowsPTY) Close() error {
	p.closeOnce.Do(func() {
		// Close output first so native teardown cannot deadlock emitting a final
		// frame when the caller has stopped reading (or startup failed).
		p.closeErr = p.output.Close()
		_ = p.input.Close()
		p.release()
	})
	return p.closeErr
}

func (p *windowsPTY) Wait() (int, error) {
	<-p.done
	_ = p.Close()
	return p.code, p.waitErr
}
