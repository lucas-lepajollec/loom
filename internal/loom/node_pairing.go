package loom

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

const (
	nodeHandshake    = 1
	nodePairingKey   = "node_pairing"
	pairCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	pairCodeLifetime = 10 * time.Minute
)

type nodeDescriptor struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Role      string   `json:"role"`
	Modules   []string `json:"modules"`
	Handshake int      `json:"handshake"`
}

type pairedMain struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt int64  `json:"paired_at"`
}

type nodePairRate struct {
	Failures int   `json:"failures"`
	Expires  int64 `json:"expires"`
}

type nodePairState struct {
	Hash     []byte                  `json:"hash,omitempty"`
	Expires  int64                   `json:"expires,omitempty"`
	Attempts int                     `json:"attempts,omitempty"`
	Started  bool                    `json:"started"`
	Main     *pairedMain             `json:"main,omitempty"`
	Remotes  map[string]nodePairRate `json:"remotes,omitempty"`
}

// The store opens/closes bbolt for each operation. One transaction serializes
// both HTTP consumption and code replacement by a separate running CLI.
func updateNodePairState(fn func(*nodePairState) error) error {
	return store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		var s nodePairState
		if raw := b.Get([]byte(nodePairingKey)); len(raw) != 0 {
			if err := json.Unmarshal(raw, &s); err != nil {
				return err
			}
		}
		if err := fn(&s); err != nil {
			return err
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return b.Put([]byte(nodePairingKey), raw)
	})
}

func loomMachineID() (string, error) {
	if raw, err := getBytesErr(bkState, "loom_machine_id"); err != nil {
		return "", err
	} else if len(raw) != 0 {
		return string(raw), nil
	}
	var id string
	err := store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		id = string(b.Get([]byte("loom_machine_id")))
		if id != "" {
			return nil
		}
		var err error
		id, err = newNodeToken()
		if err != nil {
			return err
		}
		return b.Put([]byte("loom_machine_id"), []byte(id))
	})
	return id, err
}

func describeNode() (nodeDescriptor, error) {
	id, err := loomMachineID()
	name, _ := os.Hostname()
	return nodeDescriptor{ID: id, Name: name, Version: Version, Role: "engine-node", Modules: nodeModules(), Handshake: nodeHandshake}, err
}

func checkNodeHandshake(major int) error {
	// No handshake field is the legacy token-based protocol. Keep that fallback.
	if major != 0 && major != nodeHandshake {
		return fmt.Errorf("node handshake mismatch: major %d is unsupported (expected %d); update Loom and the node", major, nodeHandshake)
	}
	return nil
}

func normalizePairCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) == 9 && code[4] == '-' {
		code = code[:4] + code[5:]
	}
	return code
}

func generatePairCode() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	var code [8]byte
	for i, v := range random {
		code[i] = pairCodeAlphabet[int(v)&31]
	}
	return string(code[:4]) + "-" + string(code[4:]), nil
}

// firstStart issues/logs a code only once, even after expiry or service restart.
// Explicit pair always replaces the code and keeps per-address rate limits.
func issueNodePairCode(firstStart bool, now time.Time) (string, error) {
	if _, err := readNodeToken(); err != nil {
		return "", err
	}
	code, err := generatePairCode()
	if err != nil {
		return "", err
	}
	err = updateNodePairState(func(s *nodePairState) error {
		if firstStart && (s.Started || s.Main != nil) {
			code = ""
			return nil
		}
		hash := sha256.Sum256([]byte(normalizePairCode(code)))
		s.Hash, s.Expires, s.Attempts, s.Started = hash[:], now.Add(pairCodeLifetime).UnixMilli(), 0, true
		return nil
	})
	return code, err
}

func printNodePairCode(code string) {
	if code != "" {
		fmt.Printf("[loom node] Pairing code: %s (valid 10 minutes, single use)\n", code)
	}
}

type nodePairRequest struct {
	Code string `json:"code"`
	Main struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"main"`
	Force bool `json:"force,omitempty"`
}

type nodePairExchange struct {
	MachineToken string         `json:"machine_token"`
	InferenceKey string         `json:"inference_key"`
	Node         nodeDescriptor `json:"node"`
}

func validPairLabel(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 128 && !strings.ContainsAny(s, "\r\n\x00")
}

func handleNodePair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if r.Header.Get("Origin") != "" {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "node accepts machine requests only"})
		return
	}
	var req nodePairRequest
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !validPairLabel(req.Main.ID) || !validPairLabel(req.Main.Name) || !validPairLabel(req.Main.Version) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "main id, name and version required (maximum 128 bytes each)"})
		return
	}
	token, err := readNodeToken()
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": "node credential unavailable"})
		return
	}
	key := readAPIKey()
	node, err := describeNode()
	if err != nil || key == "" {
		sendJSON(w, 503, map[string]any{"ok": false, "error": "node state unavailable"})
		return
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	now := time.Now()
	status, errorCode, message := 200, "", ""
	var main *pairedMain
	err = updateNodePairState(func(s *nodePairState) error {
		if s.Remotes == nil {
			s.Remotes = map[string]nodePairRate{}
		}
		for addr, rate := range s.Remotes {
			if now.UnixMilli() >= rate.Expires {
				delete(s.Remotes, addr)
			}
		}
		rate := s.Remotes[remote]
		if rate.Failures >= 5 || (rate.Failures == 0 && len(s.Remotes) >= 256) {
			status, errorCode, message = 429, "rate_limited", "too many pairing attempts; wait 10 minutes"
			return nil
		}
		hash := sha256.Sum256([]byte(normalizePairCode(req.Code)))
		matches := subtle.ConstantTimeCompare(hash[:], s.Hash) == 1
		if !matches || len(s.Hash) != sha256.Size || now.UnixMilli() >= s.Expires {
			s.Attempts++
			if s.Attempts >= 5 || now.UnixMilli() >= s.Expires {
				s.Hash, s.Expires = nil, 0
			}
			if rate.Failures == 0 {
				rate.Expires = now.Add(pairCodeLifetime).UnixMilli()
			}
			rate.Failures++
			s.Remotes[remote] = rate
			status, errorCode, message = 401, "invalid_code", "invalid or expired pairing code; run loom node pair for a fresh code"
			return nil
		}
		if s.Main != nil && s.Main.ID != req.Main.ID && !req.Force {
			main = s.Main
			status, errorCode, message = 409, "already_paired", "node already paired to "+s.Main.Name+"; use force with a valid fresh code to re-pair"
			return nil
		}
		s.Main = &pairedMain{ID: req.Main.ID, Name: req.Main.Name, PairedAt: now.UnixMilli()}
		s.Hash, s.Expires, s.Attempts = nil, 0, 0
		delete(s.Remotes, remote)
		return nil
	})
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": "node pairing state could not be saved"})
		return
	}
	if status != 200 {
		body := map[string]any{"ok": false, "error": message, "error_code": errorCode}
		if main != nil {
			body["main"] = main
		}
		if status == 429 {
			w.Header().Set("Retry-After", "600")
		}
		sendJSON(w, status, body)
		return
	}
	sendJSON(w, 200, nodePairExchange{MachineToken: token, InferenceKey: key, Node: node})
}

func nodeIsPaired() bool {
	raw, err := getBytesErr(bkState, nodePairingKey)
	var s nodePairState
	// Fail closed for discovery: unreadable pairing state never advertises free.
	return err != nil || (len(raw) != 0 && (json.Unmarshal(raw, &s) != nil || s.Main != nil))
}
