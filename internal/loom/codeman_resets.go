package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Réinitialisations de quota offertes par les abonnements (crédits « reset »).
// Codex les liste par son serveur d'application officiel (JSON-RPC,
// account/rateLimits/read) ; Claude Code ne garde en cache qu'un indicateur
// « une réinitialisation inutilisée existe ». Lecture seule : Loom ne les
// utilise jamais, l'utilisateur les déclenche dans le CLI. Les CLI tournent là
// où tourne Codeman : rien n'est lu quand Codeman est sur une autre machine.

const quotaResetsEvery = 10 * time.Minute

type resetCredit struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	GrantedAt   int64  `json:"granted_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
}

type quotaResets struct {
	CheckedAt int64 `json:"checked_at"`
	Codex     *struct {
		Available int           `json:"available"`
		Credits   []resetCredit `json:"credits"`
	} `json:"codex,omitempty"`
	Claude *struct {
		Available bool  `json:"available"`
		CheckedAt int64 `json:"checked_at,omitempty"`
	} `json:"claude,omitempty"`
}

var quotaResetsCache struct {
	sync.Mutex
	value quotaResets
	at    time.Time
}

// codemanIsLocal: les CLI de Codeman sont sur cette machine.
func codemanIsLocal() bool {
	cfg := loadCodemanConfig()
	if cfg.URL == "" {
		return false
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
}

func readQuotaResets(ctx context.Context) quotaResets {
	quotaResetsCache.Lock()
	defer quotaResetsCache.Unlock()
	if time.Since(quotaResetsCache.at) < quotaResetsEvery {
		return quotaResetsCache.value
	}
	out := quotaResets{CheckedAt: time.Now().UnixMilli()}
	if codemanIsLocal() {
		out.Codex = readCodexResetCredits(ctx)
		out.Claude = readClaudeResetHint()
	}
	quotaResetsCache.value, quotaResetsCache.at = out, time.Now()
	return out
}

// Codex : une requête au serveur d'application, lancé pour l'occasion.
func readCodexResetCredits(parent context.Context) *struct {
	Available int           `json:"available"`
	Credits   []resetCredit `json:"credits"`
} {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "app-server")
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if cmd.Start() != nil {
		return nil
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	enc := json.NewEncoder(stdin)
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "loom", "version": Version}}})
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var reply struct {
		ID     int `json:"id"`
		Result struct {
			Credits struct {
				AvailableCount int `json:"availableCount"`
				Credits        []struct {
					ResetType   string `json:"resetType"`
					Status      string `json:"status"`
					Title       string `json:"title"`
					Description string `json:"description"`
					GrantedAt   int64  `json:"grantedAt"`
					ExpiresAt   int64  `json:"expiresAt"`
				} `json:"credits"`
			} `json:"rateLimitResetCredits"`
		} `json:"result"`
		Error any `json:"error"`
	}
	for sc.Scan() {
		var m struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.ID {
		case 1:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "account/rateLimits/read", "params": map[string]any{}})
		case 2:
			if json.Unmarshal(sc.Bytes(), &reply) != nil || reply.Error != nil {
				return nil
			}
			out := &struct {
				Available int           `json:"available"`
				Credits   []resetCredit `json:"credits"`
			}{Available: reply.Result.Credits.AvailableCount, Credits: []resetCredit{}}
			for _, c := range reply.Result.Credits.Credits {
				if c.Status != "available" {
					continue
				}
				out.Credits = append(out.Credits, resetCredit{Type: c.ResetType, Title: c.Title, Description: c.Description, GrantedAt: c.GrantedAt * 1000, ExpiresAt: c.ExpiresAt * 1000})
			}
			return out
		}
	}
	return nil
}

// Claude Code : son cache « réinitialisation inutilisée » (valeur et date de
// vérification seulement ; le détail n'est donné que par /limit-reset).
func readClaudeResetHint() *struct {
	Available bool  `json:"available"`
	CheckedAt int64 `json:"checked_at,omitempty"`
} {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		path = filepath.Join(dir, ".claude.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var cfg struct {
		Cache *struct {
			Holds      bool  `json:"holdsUnusedReset"`
			CheckedAt  int64 `json:"checkedAt"`
			ValidUntil int64 `json:"validUntil"`
		} `json:"unusedLimitResetStatusCache"`
	}
	if json.NewDecoder(io.LimitReader(f, 32<<20)).Decode(&cfg) != nil || cfg.Cache == nil {
		return nil
	}
	return &struct {
		Available bool  `json:"available"`
		CheckedAt int64 `json:"checked_at,omitempty"`
	}{Available: cfg.Cache.Holds, CheckedAt: cfg.Cache.CheckedAt}
}
