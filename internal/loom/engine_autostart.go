package loom

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"
)

// Démarrage du moteur à la demande, sur un nœud moteur : une application qui
// appelle /v1 (un agent, Tradoc…) ne devrait pas avoir à allumer le moteur à
// la main. Le front llama.cpp charge ensuite le modèle demandé, comme pour
// toute requête. Un échec est retenu 30 s pour ne pas relancer en boucle.

const (
	engineAutoStartWait  = 90 * time.Second
	engineAutoStartRetry = 30 * time.Second
)

var engineAutoStart struct {
	sync.Mutex
	failedAt time.Time
	err      error
}

// engineStartHook est remplacé par les tests.
var engineStartHook = func() error {
	if err := ensureLoomEngineBind(); err != nil {
		return err
	}
	if err := preflightEngine(); err != nil {
		return err
	}
	return serviceAction("start")
}

func engineListening(u *url.URL) bool {
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureEngineForRequest démarre le moteur local s'il n'écoute pas, puis
// attend qu'il écoute. Il renvoie nil quand la requête peut partir.
func ensureEngineForRequest(ctx context.Context, u *url.URL) error {
	if engineListening(u) {
		return nil
	}
	engineAutoStart.Lock()
	defer engineAutoStart.Unlock()
	if engineListening(u) {
		return nil
	}
	if engineAutoStart.err != nil && time.Since(engineAutoStart.failedAt) < engineAutoStartRetry {
		return engineAutoStart.err
	}
	fmt.Printf("[loom node] inference request: starting the engine\n")
	if err := engineStartHook(); err != nil {
		engineAutoStart.err, engineAutoStart.failedAt = fmt.Errorf("could not start the engine: %w", err), time.Now()
		return engineAutoStart.err
	}
	deadline := time.Now().Add(engineAutoStartWait)
	for time.Now().Before(deadline) {
		if engineListening(u) {
			engineAutoStart.err = nil
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	engineAutoStart.err, engineAutoStart.failedAt = fmt.Errorf("the engine did not start within %s", engineAutoStartWait), time.Now()
	return engineAutoStart.err
}
