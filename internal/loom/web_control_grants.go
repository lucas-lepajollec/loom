package loom

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func controlGrantContext(parent context.Context, grant controlGrant) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !grant.valid() {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func revocableControlStream(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grant, err := controlOwner(r)
		if err != nil || !grant.valid() {
			webAuthUnavailable(w)
			return
		}
		ctx, cancel := controlGrantContext(r.Context(), grant)
		watchDone := make(chan struct{})
		defer func() {
			cancel()
			<-watchDone
		}()
		// Cancellation also interrupts a slow client's blocked network write.
		go func() {
			defer close(watchDone)
			<-ctx.Done()
			if !grant.valid() {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now())
			}
		}()
		next(w, r.WithContext(ctx))
	}
}

// Transient grants bind upgraded connections and tickets to the authorizing
// credential generation and browser session, without retaining any secret.
type controlGrant struct {
	Expires  time.Time
	Owner    string
	Password string
	Key      string
}

func controlOwner(r *http.Request) (controlGrant, error) {
	p, credential, err := readWebPassword()
	if err != nil {
		return controlGrant{}, errors.New("authentication unavailable")
	}
	key, err := webKeyHashErr()
	if err != nil {
		return controlGrant{}, errors.New("authentication unavailable")
	}
	owner := ""
	valid, err := webSessionValid(r, credential)
	if err != nil {
		return controlGrant{}, errors.New("authentication unavailable")
	}
	if valid {
		owner = sessionRecordKey(r)
	} else if !isE2EAuthed(r) && ((p != nil || key != "") && !(key != "" && checkBearer(r, key))) {
		// The request may have passed the route guard immediately before logout
		// or rotation. Never promote its stale authorization into a fresh grant.
		return controlGrant{}, errors.New("authentication changed; sign in again")
	}
	return controlGrant{Expires: time.Now().Add(8 * time.Hour), Owner: owner, Password: hashWebKey(string(credential)), Key: key}, nil
}
func (g controlGrant) valid() bool {
	if time.Now().After(g.Expires) || (memEncActive() && !memUnlocked()) {
		return false
	}
	_, credential, err := readWebPassword()
	if err != nil || hashWebKey(string(credential)) != g.Password {
		return false
	}
	key, err := webKeyHashErr()
	if err != nil || key != g.Key {
		return false
	}
	if g.Owner != "" {
		var s webSession
		if !getStoreJSON(bkState, g.Owner, &s) || s.Expires <= time.Now().Unix() || s.Credential != hashWebKey(string(credential)) {
			return false
		}
	}
	return true
}
