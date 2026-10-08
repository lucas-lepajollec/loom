package loom

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/lucas-lepajollec/loom/internal/loom/engine/llamacpp"
)

// backend_llama_owned.go — llama-server enfant de `loom serve`.
//
// Le process `serve` reste vivant : il tient le front /v1 (HOST:PORT) et peut
// relancer le GGUF sans tuer l'écouteur. systemd / directStart supervisent
// encore `serve` ; si llama-server meurt tout seul, `serve` sort.

// Loom prepares the command and keeps application cleanup outside the engine.
func ownedLlamaLaunch(bin string, args []string) llamacpp.Launch {
	noteEngineLaunch(args)
	cmd := exec.Command(bin, args[1:]...)
	cmd.Args[0] = bin
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Dir, cmd.Env = LoomHome(), os.Environ()
	return llamacpp.Launch{
		Command: cmd, Ports: llamacpp.Ports{Public: LLMPort(), Backend: llamaBackendPort()}, Log: os.Stderr,
		UnexpectedExit: func() {
			_ = SetConfigKey("MODEL", "")
			_ = putStr(bkState, "active_preset", "")
			_ = putStr(bkState, routerStateActive, "")
			_ = putStr(bkState, routerStateCurrent, "")
			clearOAIRuntime()
		},
	}
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
	if err := llamaOwner.Restart(func() llamacpp.Launch { return ownedLlamaLaunch(args[0], args) }); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[loom serve] llama-server restarted (internal port %d)\n", llamaBackendPort())
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
