package loom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

const engineActivityState = "engine_native_activity"

// Le chat web et le front tournent parfois dans deux processus. Ce bail court
// protège aussi les pauses outils d'un tour natif ; un crash libère le bail.
func engineGenerationLease(ctx context.Context) func() {
	if currentEngineNode() != nil {
		return func() {}
	}
	id, err := newEngineSecret()
	if err != nil {
		return func() {}
	}
	path := dbPath()
	update := func(remove bool) {
		_ = store.Update(path, bkState, func(b *bolt.Bucket) error {
			leases := map[string]int64{}
			if raw := b.Get([]byte(engineActivityState)); len(raw) > 0 {
				if err := json.Unmarshal(raw, &leases); err != nil {
					return err
				}
			}
			now := time.Now()
			for key, until := range leases {
				if until <= now.UnixNano() {
					delete(leases, key)
				}
			}
			if remove {
				delete(leases, id)
			} else {
				leases[id] = now.Add(90 * time.Second).UnixNano()
			}
			raw, err := json.Marshal(leases)
			if err != nil {
				return err
			}
			return b.Put([]byte(engineActivityState), raw)
		})
	}
	update(false)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		defer update(true)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				update(false)
			}
		}
	}()
	return func() { close(stop); <-done }
}

func engineNativeGenerating() bool {
	if discussionGenerating() || benchBusy.Load() {
		return true
	}
	var leases map[string]int64
	b, err := getBytesErr(bkState, engineActivityState)
	if err != nil {
		return true
	}
	if len(b) > 0 && json.Unmarshal(b, &leases) != nil {
		return true
	}
	for _, until := range leases {
		if until > time.Now().UnixNano() {
			return true
		}
	}
	return false
}

func engineModelSelected() bool {
	if n := currentEngineNode(); n != nil {
		return !n.Direct || n.Model != ""
	}
	return ReadConfig()["MODEL"] != ""
}
