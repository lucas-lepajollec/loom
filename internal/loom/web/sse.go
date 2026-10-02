package web

import (
	"net/http"
	"sync"
	"time"
)

// SSEHeartbeat garde la réponse SSE active en écrivant un commentaire (`: ping`,
// ignoré par le parseur côté navigateur, aucun contenu donc rien à chiffrer)
// toutes les 4 s. Sans ça, un long silence (exécution d'outil en mode agent,
// gros prefill) laisse la réponse inactive et un proxy intermédiaire (Cloudflare,
// ~100 s) la coupe → le fetch navigateur échoue (« Load failed »). Retourne un
// mutex à partager avec l'émetteur (writes concurrents sur le même w) et une
// fonction d'arrêt à différer.
func SSEHeartbeat(w http.ResponseWriter, flusher http.Flusher) (*sync.Mutex, func()) {
	mu := &sync.Mutex{}
	done := make(chan struct{})
	go func() {
		// 4 s (et non 15) : borne le temps qu'un dernier bout de flux peut rester
		// coincé dans un buffer proxy (Cloudflare) faute d'octets pour le pousser.
		t := time.NewTicker(4 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				_, err := w.Write([]byte(": ping\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
				mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	return mu, func() { close(done) }
}

// SSEHeaders sets the stream headers with the route's historical cache policy.
func SSEHeaders(w http.ResponseWriter, cacheControl string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Accel-Buffering", "no")
}

// WriteSSE serializes a data frame and flush with heartbeat writes.
func WriteSSE(w http.ResponseWriter, flusher http.Flusher, mu *sync.Mutex, data []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
