package loom

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// backend_llama_owned.go — llama-server enfant de `loom serve`.
//
// Le process `serve` reste vivant : il tient le front /v1 (HOST:PORT) et peut
// relancer le GGUF sans tuer l'écouteur. systemd / directStart supervisent
// encore `serve` ; si llama-server meurt tout seul, `serve` sort.

var (
	ownedMu         sync.Mutex
	ownedCmd        *exec.Cmd
	ownedDone       chan struct{}
	ownedGen        int
	ownedSupervisor bool
	ownedLastError  string
)

func setLlamaLastError(msg string) {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	ownedLastError = msg
}

func getLlamaLastError() string {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	return ownedLastError
}

func initOwnedLlamaSupervisor() {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	ownedSupervisor = true
}

func shutdownOwnedLlamaSupervisor() {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	ownedSupervisor = false
	stopOwnedLlamaLocked()
}

func ownedLlamaManaged() bool {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	return ownedSupervisor
}

func ownedLlamaRunning() bool {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	return ownedCmd != nil && ownedCmd.Process != nil
}

func startOwnedLlama(bin string, args []string) error {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	return startOwnedLlamaLocked(bin, args)
}

func startOwnedLlamaLocked(bin string, args []string) error {
	cmd := exec.Command(bin, args[1:]...)
	cmd.Args[0] = bin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Dir = LoomHome()
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("llama-server : %w", err)
	}
	ownedCmd = cmd
	ownedGen++
	gen := ownedGen
	done := make(chan struct{})
	ownedDone = done
	ownedLastError = ""
	go func() {
		err := cmd.Wait()
		close(done)
		ownedMu.Lock()
		defer ownedMu.Unlock()
		if ownedGen != gen {
			return
		}
		ownedCmd = nil
		if err != nil {
			msg := fmt.Sprintf("Le modèle s'est arrêté (%v) — mémoire VRAM insuffisante ou crash", err)
			ownedLastError = msg
			fmt.Fprintf(os.Stderr, "[loom serve] %s\n", msg)
		} else {
			ownedLastError = "Le modèle s'est arrêté inopinément"
			fmt.Fprintf(os.Stderr, "[loom serve] llama-server arrêté inopinément\n")
		}
		// Nettoie MODEL dans la config pour ne pas laisser un modèle fantôme
		go func() {
			_ = SetConfigKey("MODEL", "")
			_ = putStr(bkState, "active_preset", "")
			_ = putStr(bkState, routerStateActive, "")
			_ = putStr(bkState, routerStateCurrent, "")
			clearOAIRuntime()
		}()
	}()
	return nil
}

func stopOwnedLlamaLocked() {
	if ownedCmd == nil || ownedCmd.Process == nil {
		return
	}
	done := ownedDone
	ownedGen++
	_ = ownedCmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		_ = ownedCmd.Process.Kill()
		if done != nil {
			<-done
		}
	}
	ownedCmd = nil
	ownedDone = nil
}

func stopOwnedLlama() {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	stopOwnedLlamaLocked()
}

func restartOwnedLlama() error {
	setLlamaLastError("")
	// Mode router : on change de modèle par l'API, le moteur reste vivant.
	if routerReachable() {
		return routerActivate()
	}
	args, err := buildLlamaServerArgs()
	if err != nil {
		return err
	}
	ownedMu.Lock()
	defer ownedMu.Unlock()
	stopOwnedLlamaLocked()
	if err := startOwnedLlamaLocked(args[0], args); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[loom serve] llama-server relancé (port interne %d)\n", llamaBackendPort())
	return nil
}

// restartLlamaEngine applique la configuration courante. Avec un router
// joignable, c'est un simple chargement par son API ; sinon on relance le
// process (mode historique ou moteur pas encore passé en router).
func restartLlamaEngine() error {
	if routerReachable() {
		return routerActivate()
	}
	if ownedLlamaManaged() {
		return restartOwnedLlama()
	}
	return serviceAction("restart")
}

func waitOwnedLlama() error {
	return nil
}
