package loom

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

const (
	engineConnectionsState = "engine_connections"
	engineConnKeyAAD       = "loom/engine-conn/api-key"
)

type engineConnection struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Kind    string `json:"kind"`
	Model   string `json:"model"`
	Version string `json:"version"`
	KeySet  bool   `json:"key_set"`
	Active  bool   `json:"active"`
}

type storedEngineConnection struct {
	engineConnection
	SealedKey string `json:"sealed_key,omitempty"`
}

func connectionID(base string) string {
	hash := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%x", hash[:16])
}

func connectionFromNode(n *engineNode, name string) (storedEngineConnection, error) {
	key := ""
	if n.APIKey != "" {
		var err error
		key, err = sealInlineSecret(engineConnKeyAAD, n.APIKey)
		if err != nil {
			return storedEngineConnection{}, err
		}
	}
	return storedEngineConnection{
		engineConnection: engineConnection{ID: connectionID(n.V1), Name: name, URL: n.V1, Kind: n.Kind, Model: n.Model, Version: n.Version, KeySet: key != ""},
		SealedKey:        key,
	}, nil
}

func decodeEngineConnections(raw []byte) ([]storedEngineConnection, error) {
	out := []storedEngineConnection{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Called under engineNodeMu so profile changes and active selection stay consistent.
func engineConnectionsLocked() ([]storedEngineConnection, error) {
	raw, err := getBytesErr(bkState, engineConnectionsState)
	if err != nil {
		return nil, err
	}
	return decodeEngineConnections(raw)
}

func engineConnectionSnapshot() ([]engineConnection, *engineNode, error) {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	n := currentEngineNodeLocked()
	saved, err := engineConnectionsLocked()
	if err != nil {
		return nil, nil, err
	}
	out := make([]engineConnection, 0, len(saved)+1)
	for _, p := range saved {
		p.Active = n != nil && n.Direct && n.Managed == "" && connectionID(n.V1) == p.ID
		if p.Active {
			p.Model, p.Kind, p.Version, p.KeySet = n.Model, n.Kind, n.Version, n.APIKey != ""
		}
		out = append(out, p.engineConnection)
	}
	// A direct link from an older Loom remains selectable and is persisted on
	// the next selection. Reading the registry requires no database writes.
	if n != nil && n.Direct && n.Managed == "" && !slices.ContainsFunc(out, func(p engineConnection) bool { return p.Active }) {
		out = append(out, engineConnection{ID: connectionID(n.V1), Name: n.Hostname, URL: n.V1, Kind: n.Kind, Model: n.Model,
			Version: n.Version, KeySet: n.APIKey != "", Active: true})
	}
	slices.SortFunc(out, func(a, b engineConnection) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, n, nil
}

// An active selection and its registry update commit together. Managed
// child processes only live as long as Loom and use an in-memory selection.
func writeEngineSelectionLocked(n *engineNode, activate bool, profile *storedEngineConnection, remove string) error {
	var active []byte
	if activate && n != nil && n.Managed == "" {
		sealed, err := sealEngineNode(n)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(sealed)
		if err != nil {
			return err
		}
		active, err = encodeMemContent(raw)
		if err != nil {
			return err
		}
	}
	previous := currentEngineNodeLocked()
	var retained *storedEngineConnection
	if activate && previous != nil && previous.Direct && previous.Managed == "" && connectionID(previous.V1) != remove &&
		(profile == nil || profile.ID != connectionID(previous.V1)) {
		saved, err := engineConnectionsLocked()
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(saved, func(p storedEngineConnection) bool { return p.ID == connectionID(previous.V1) }) {
			p, err := connectionFromNode(previous, previous.Hostname)
			if err != nil {
				return err
			}
			retained = &p
		}
	}
	err := store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		if profile != nil || remove != "" || retained != nil {
			key := []byte(engineConnectionsState)
			saved, err := decodeEngineConnections(b.Get(key))
			if err != nil {
				return err
			}
			if retained != nil {
				saved = append(saved, *retained)
			}
			if profile != nil {
				i := slices.IndexFunc(saved, func(item storedEngineConnection) bool { return item.ID == profile.ID })
				p := *profile
				if i >= 0 {
					if p.Name == "" {
						p.Name = saved[i].Name
					}
					saved[i] = p
				} else {
					if p.Name == "" && n != nil {
						p.Name = n.Hostname
					}
					saved = append(saved, p)
				}
			}
			if remove != "" {
				saved = slices.DeleteFunc(saved, func(p storedEngineConnection) bool { return p.ID == remove })
			}
			data, err := json.Marshal(saved)
			if err != nil {
				return err
			}
			if err := b.Put(key, data); err != nil {
				return err
			}
		}
		if activate {
			key := []byte(engineNodeState)
			if active == nil {
				return b.Delete(key)
			}
			return b.Put(key, active)
		}
		return nil
	})
	if err == nil && activate {
		cacheEngineNodeLocked(n)
	}
	return err
}

func setEngineNodeIfCurrent(previous, next *engineNode) error {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	if currentEngineNodeLocked() != previous {
		return errors.New("active engine changed")
	}
	return setEngineNodeLocked(next)
}

func findEngineConnection(id string) (storedEngineConnection, error) {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	saved, err := engineConnectionsLocked()
	if err != nil {
		return storedEngineConnection{}, err
	}
	for _, p := range saved {
		if p.ID == id {
			return p, nil
		}
	}
	if n := currentEngineNodeLocked(); n != nil && n.Direct && n.Managed == "" && connectionID(n.V1) == id {
		return connectionFromNode(n, n.Hostname)
	}
	return storedEngineConnection{}, errEngineConnectionNotFound
}

var errEngineConnectionNotFound = errors.New("engine connection not found")

func deleteEngineConnection(id string) error {
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	n := currentEngineNodeLocked()
	active := n != nil && n.Direct && n.Managed == "" && connectionID(n.V1) == id
	saved, err := engineConnectionsLocked()
	if err != nil {
		return err
	}
	if !active && !slices.ContainsFunc(saved, func(p storedEngineConnection) bool { return p.ID == id }) {
		return errEngineConnectionNotFound
	}
	return writeEngineSelectionLocked(nil, active, nil, id)
}

func activateEngineConnection(ctx context.Context, id, model string) error {
	p, err := findEngineConnection(id)
	if err != nil {
		return err
	}
	key := ""
	if p.SealedKey != "" {
		key, _, err = openInlineSecret(engineConnKeyAAD, p.SealedKey)
		if err != nil {
			return err
		}
	}
	if model == "" {
		model = p.Model
	}
	n, err := prepareDirectEngine(ctx, p.URL, key, model)
	if err != nil {
		return err
	}
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	// Reject updates/deletes that happened while the network probe was running.
	saved, err := engineConnectionsLocked()
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(saved, func(item storedEngineConnection) bool { return item.ID == id }); i >= 0 {
		if saved[i] != p {
			return errors.New("engine connection changed during activation")
		}
	} else if active := currentEngineNodeLocked(); active == nil || !active.Direct || active.Managed != "" || connectionID(active.V1) != id || active.APIKey != key {
		return errEngineConnectionNotFound
	}
	p.Model, p.Kind, p.Version = n.Model, n.Kind, n.Version
	return writeEngineSelectionLocked(n, true, &p, "")
}

func handleEngineConnections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, err error) { sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()}) }
	if r.Method == http.MethodPost {
		var req struct {
			Action   string `json:"action"`
			ID       string `json:"id"`
			Name     string `json:"name"`
			URL      string `json:"url"`
			Key      string `json:"key"`
			Model    string `json:"model"`
			Activate bool   `json:"activate"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		switch req.Action {
		case "probe":
			probeEngineAddress(w, ctx, req.URL, req.Key)
			return
		case "save":
			n, err := prepareDirectEngine(ctx, req.URL, req.Key, req.Model)
			if err != nil {
				fail(400, err)
				return
			}
			p, err := connectionFromNode(n, strings.TrimSpace(req.Name))
			if err == nil {
				engineNodeMu.Lock()
				err = writeEngineSelectionLocked(n, req.Activate, &p, "")
				engineNodeMu.Unlock()
			}
			if err != nil {
				fail(500, err)
				return
			}
		case "activate":
			if err := activateEngineConnection(ctx, req.ID, req.Model); err != nil {
				code := http.StatusBadRequest
				if errors.Is(err, errEngineConnectionNotFound) {
					code = http.StatusNotFound
				}
				fail(code, err)
				return
			}
		case "unlink":
			n := currentEngineNode()
			if n != nil && n.Direct && !stopManagedDirect(n) {
				if err := setEngineNodeIfCurrent(n, nil); err != nil {
					fail(409, err)
					return
				}
			}
		case "delete":
			if err := deleteEngineConnection(req.ID); err != nil {
				code := http.StatusInternalServerError
				if errors.Is(err, errEngineConnectionNotFound) {
					code = http.StatusNotFound
				}
				fail(code, err)
				return
			}
		default:
			fail(400, errors.New("unknown action"))
			return
		}
	} else if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	connections, active, err := engineConnectionSnapshot()
	if err != nil {
		fail(500, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "connections": connections, "active_engine": catalogActiveEngine(active)})
}
