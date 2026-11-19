package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Comptes d'abonnement des harness installés sur cette machine, lus auprès de
// chaque CLI par son moyen officiel, sans jeton ni appel privé :
//   - Claude Code : le relevé qu'il garde dans ~/.claude.json, rafraîchi au
//     besoin par « claude -p /usage » (commande locale, aucun token) ;
//   - Codex : son serveur d'application (account/rateLimits/read), qui donne
//     aussi les crédits de réinitialisation et permet d'en utiliser un ;
//   - Antigravity : « agy -p /quota » et « agy -p /credits » en JSON ;
//   - Copilot : relevé par Codeman (requête mensuelle).
// Les quotas sont ceux du compte : peu importe la machine qui les lit. Ce que
// Codeman relève complète les comptes absents d'ici.

const (
	quotaAccountsMaxAge = 3 * time.Minute
	quotaClaudeStale    = 10 * time.Minute
	quotaCLITimeout     = 30 * time.Second
)

type quotaLimit struct {
	ID      string  `json:"id"`
	Group   string  `json:"group,omitempty"`
	Window  string  `json:"window"` // 5h, 7d, month ou other (voir Label)
	Label   string  `json:"label,omitempty"`
	Used    float64 `json:"used"`
	ResetAt int64   `json:"reset_at,omitempty"`
}

type resetCredit struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	GrantedAt   int64  `json:"granted_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
}

// Réinitialisations offertes. Exact : le nombre est connu (Codex) ; sinon on
// sait seulement qu'il en reste au moins une (Claude Code). Use : « direct »
// quand Loom peut l'utiliser sur demande, « cli » quand seul le CLI le peut.
type quotaResetInfo struct {
	Available int           `json:"available"`
	Exact     bool          `json:"exact"`
	Credits   []resetCredit `json:"credits,omitempty"`
	Use       string        `json:"use"`
	CheckedAt int64         `json:"checked_at,omitempty"`
	Stale     bool          `json:"stale,omitempty"`
}

type quotaAccount struct {
	ID        string          `json:"id"`
	Plan      string          `json:"plan,omitempty"`
	Source    string          `json:"source"` // cli ou codeman
	Limits    []quotaLimit    `json:"limits"`
	Resets    *quotaResetInfo `json:"resets,omitempty"`
	Credits   *float64        `json:"credits,omitempty"` // crédits IA d'Antigravity
	CheckedAt int64           `json:"checked_at"`
}

var quotaAccountsCache struct {
	sync.Mutex
	list []quotaAccount
	at   time.Time
}
var quotaAccountsRefresh sync.Mutex

func invalidateQuotaAccounts() {
	quotaAccountsCache.Lock()
	quotaAccountsCache.at = time.Time{}
	quotaAccountsCache.Unlock()
}

// readQuotaAccounts renvoie les comptes lus il y a moins de maxAge, ou les
// relit (une seule relecture à la fois).
func readQuotaAccounts(ctx context.Context, maxAge time.Duration) []quotaAccount {
	fresh := func() ([]quotaAccount, bool) {
		quotaAccountsCache.Lock()
		defer quotaAccountsCache.Unlock()
		return quotaAccountsCache.list, !quotaAccountsCache.at.IsZero() && time.Since(quotaAccountsCache.at) < maxAge
	}
	if list, ok := fresh(); ok {
		return list
	}
	quotaAccountsRefresh.Lock()
	defer quotaAccountsRefresh.Unlock()
	if list, ok := fresh(); ok {
		return list
	}
	var (
		wg      sync.WaitGroup
		readers = []func(context.Context) *quotaAccount{readClaudeAccount, readCodexAccount, readAntigravityAccount}
		found   = make([]*quotaAccount, len(readers))
	)
	for i, read := range readers {
		wg.Add(1)
		go func() { defer wg.Done(); found[i] = read(ctx) }()
	}
	wg.Wait()
	list := []quotaAccount{}
	for _, a := range found {
		if a != nil {
			list = append(list, *a)
		}
	}
	list = mergeCodemanAccounts(list)
	quotaAccountsCache.Lock()
	quotaAccountsCache.list, quotaAccountsCache.at = list, time.Now()
	quotaAccountsCache.Unlock()
	return list
}

// Comptes relevés par Codeman mais absents d'ici (CLI sur une autre machine).
func mergeCodemanAccounts(list []quotaAccount) []quotaAccount {
	codemanQuotas.Lock()
	latest := codemanQuotas.latest
	codemanQuotas.Unlock()
	have := map[string]bool{}
	for _, a := range list {
		have[a.ID] = true
	}
	for _, src := range []struct {
		id string
		w  map[string]quotaWindow
	}{{"claude", latest.Claude}, {"codex", latest.Codex}, {"copilot", latest.Copilot}} {
		if have[src.id] || len(src.w) == 0 {
			continue
		}
		a := quotaAccount{ID: src.id, Source: "codeman", CheckedAt: latest.At}
		for _, win := range []string{"5h", "7d", "month"} {
			if q, ok := src.w[win]; ok {
				a.Limits = append(a.Limits, quotaLimit{ID: src.id + "-" + win, Window: win, Used: q.Used, ResetAt: q.ResetAt})
			}
		}
		list = append(list, a)
	}
	return list
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Max(0, math.Min(100, v))
}

func parseResetTime(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// runQuotaCLI lance une commande d'un CLI hors de tout projet et renvoie sa
// sortie standard.
func runQuotaCLI(ctx context.Context, name string, args ...string) ([]byte, error) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, quotaCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	return cmd.Output()
}

// ---- Claude Code ----

func claudeConfigFile() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type claudeConfigQuota struct {
	Usage *struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			FiveHour       *claudeUsageWindow `json:"five_hour"`
			SevenDay       *claudeUsageWindow `json:"seven_day"`
			SevenDayOpus   *claudeUsageWindow `json:"seven_day_opus"`
			SevenDaySonnet *claudeUsageWindow `json:"seven_day_sonnet"`
			Limits         []struct {
				Kind     string   `json:"kind"`
				Percent  *float64 `json:"percent"`
				ResetsAt string   `json:"resets_at"`
			} `json:"limits"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
	Reset *struct {
		Holds      bool  `json:"holdsUnusedReset"`
		CheckedAt  int64 `json:"checkedAt"`
		ValidUntil int64 `json:"validUntil"`
	} `json:"unusedLimitResetStatusCache"`
}

func loadClaudeConfigQuota() (claudeConfigQuota, bool) {
	var cfg claudeConfigQuota
	f, err := os.Open(claudeConfigFile())
	if err != nil {
		return cfg, false
	}
	defer f.Close()
	return cfg, json.NewDecoder(io.LimitReader(f, 32<<20)).Decode(&cfg) == nil
}

// Fenêtres connues de Claude : session (5 h) et semaine, tous modèles ou par
// modèle.
var claudeLimitKinds = map[string][2]string{
	"session": {"5h", ""}, "weekly_all": {"7d", ""}, "weekly_opus": {"7d", "Opus"}, "weekly_sonnet": {"7d", "Sonnet"},
}

func readClaudeAccount(ctx context.Context) *quotaAccount {
	cfg, ok := loadClaudeConfigQuota()
	if _, err := exec.LookPath("claude"); err == nil {
		// Relevé trop ancien : /usage le rafraîchit (commande locale, sans
		// modèle ni session enregistrée).
		if !ok || cfg.Usage == nil || time.Since(time.UnixMilli(cfg.Usage.FetchedAtMs)) > quotaClaudeStale {
			if _, err := runQuotaCLI(ctx, "claude", "-p", "/usage", "--no-session-persistence", "--output-format", "json"); err == nil {
				cfg, ok = loadClaudeConfigQuota()
			}
		}
	}
	if !ok || (cfg.Usage == nil && cfg.Reset == nil) {
		return nil
	}
	a := &quotaAccount{ID: "claude", Source: "cli", Limits: []quotaLimit{}, CheckedAt: time.Now().UnixMilli()}
	if u := cfg.Usage; u != nil {
		a.CheckedAt = u.FetchedAtMs
		for _, l := range u.Utilization.Limits {
			if l.Percent == nil {
				continue
			}
			win := [2]string{"other", l.Kind}
			if k, ok := claudeLimitKinds[l.Kind]; ok {
				win = k
			}
			a.Limits = append(a.Limits, quotaLimit{ID: "claude-" + l.Kind, Window: win[0], Label: win[1], Used: clampPercent(*l.Percent), ResetAt: parseResetTime(l.ResetsAt)})
		}
		if len(a.Limits) == 0 {
			for _, w := range []struct {
				kind string
				w    *claudeUsageWindow
			}{{"session", u.Utilization.FiveHour}, {"weekly_all", u.Utilization.SevenDay}, {"weekly_opus", u.Utilization.SevenDayOpus}, {"weekly_sonnet", u.Utilization.SevenDaySonnet}} {
				if w.w == nil || w.w.Utilization == nil {
					continue
				}
				k := claudeLimitKinds[w.kind]
				a.Limits = append(a.Limits, quotaLimit{ID: "claude-" + w.kind, Window: k[0], Label: k[1], Used: clampPercent(*w.w.Utilization), ResetAt: parseResetTime(w.w.ResetsAt)})
			}
		}
	}
	if r := cfg.Reset; r != nil {
		info := &quotaResetInfo{Use: "cli", CheckedAt: r.CheckedAt}
		if r.Holds {
			info.Available = 1
			info.Stale = r.ValidUntil > 0 && r.ValidUntil < time.Now().UnixMilli()
		}
		a.Resets = info
	}
	return a
}

// ---- Codex ----

type codexRPC struct {
	enc  *json.Encoder
	sc   *bufio.Scanner
	next int
}

func (c *codexRPC) call(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := c.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for c.sc.Scan() {
		var m struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(c.sc.Bytes(), &m) != nil || m.ID == nil || *m.ID != id {
			continue
		}
		if m.Error != nil {
			return nil, errors.New(m.Error.Message)
		}
		return m.Result, nil
	}
	if err := c.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

// withCodexAppServer lance le serveur d'application de Codex le temps de fn.
func withCodexAppServer(parent context.Context, fn func(*codexRPC) error) error {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, quotaCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "app-server")
	cmd.Dir = os.TempDir()
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	rpc := &codexRPC{enc: json.NewEncoder(stdin), sc: sc}
	if _, err := rpc.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "loom", "version": Version}}); err != nil {
		return err
	}
	if err := rpc.enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	return fn(rpc)
}

type codexWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	Minutes     int64   `json:"windowDurationMins"`
	ResetsAt    int64   `json:"resetsAt"`
}

func codexLimit(w *codexWindow) (quotaLimit, bool) {
	if w == nil {
		return quotaLimit{}, false
	}
	l := quotaLimit{Used: clampPercent(w.UsedPercent), ResetAt: w.ResetsAt * 1000}
	switch {
	case w.Minutes == 300:
		l.Window = "5h"
	case w.Minutes == 7*24*60:
		l.Window = "7d"
	case w.Minutes >= 60:
		l.Window, l.Label = "other", fmt.Sprintf("%dh", w.Minutes/60)
	default:
		l.Window, l.Label = "other", fmt.Sprintf("%dmin", w.Minutes)
	}
	l.ID = "codex-" + l.Window + l.Label
	return l, true
}

func readCodexAccount(ctx context.Context) *quotaAccount {
	var res struct {
		RateLimits *struct {
			PlanType  string       `json:"planType"`
			Primary   *codexWindow `json:"primary"`
			Secondary *codexWindow `json:"secondary"`
		} `json:"rateLimits"`
		Credits *struct {
			AvailableCount int `json:"availableCount"`
			Credits        []struct {
				ID          string `json:"id"`
				ResetType   string `json:"resetType"`
				Status      string `json:"status"`
				Title       string `json:"title"`
				Description string `json:"description"`
				GrantedAt   int64  `json:"grantedAt"`
				ExpiresAt   int64  `json:"expiresAt"`
			} `json:"credits"`
		} `json:"rateLimitResetCredits"`
	}
	err := withCodexAppServer(ctx, func(rpc *codexRPC) error {
		raw, err := rpc.call("account/rateLimits/read", map[string]any{})
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &res)
	})
	if err != nil || (res.RateLimits == nil && res.Credits == nil) {
		return nil
	}
	a := &quotaAccount{ID: "codex", Source: "cli", Limits: []quotaLimit{}, CheckedAt: time.Now().UnixMilli()}
	if rl := res.RateLimits; rl != nil {
		a.Plan = rl.PlanType
		for _, w := range []*codexWindow{rl.Primary, rl.Secondary} {
			if l, ok := codexLimit(w); ok {
				a.Limits = append(a.Limits, l)
			}
		}
	}
	if c := res.Credits; c != nil {
		info := &quotaResetInfo{Available: c.AvailableCount, Exact: true, Use: "direct", Credits: []resetCredit{}, CheckedAt: a.CheckedAt}
		for _, cr := range c.Credits {
			if cr.Status != "available" {
				continue
			}
			info.Credits = append(info.Credits, resetCredit{ID: cr.ID, Type: cr.ResetType, Title: cr.Title, Description: cr.Description, GrantedAt: cr.GrantedAt * 1000, ExpiresAt: cr.ExpiresAt * 1000})
		}
		a.Resets = info
	}
	return a
}

// consumeCodexReset utilise un crédit de réinitialisation Codex (le premier
// disponible quand creditID est vide). Uniquement sur demande explicite.
func consumeCodexReset(ctx context.Context, creditID string) (string, error) {
	params := map[string]any{"idempotencyKey": "loom-" + randomID(16)}
	if creditID != "" {
		params["creditId"] = creditID
	}
	var out struct {
		Outcome string `json:"outcome"`
	}
	err := withCodexAppServer(ctx, func(rpc *codexRPC) error {
		raw, err := rpc.call("account/rateLimitResetCredit/consume", params)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &out)
	})
	return out.Outcome, err
}

// ---- Antigravity ----

func readAntigravityAccount(ctx context.Context) *quotaAccount {
	raw, err := runQuotaCLI(ctx, "agy", "-p", "/quota", "--output-format", "json")
	if err != nil {
		return nil
	}
	var q struct {
		Command struct {
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						ID        string   `json:"id"`
						Name      string   `json:"name"`
						Window    string   `json:"window"`
						Remaining *float64 `json:"remaining_fraction"`
						ResetTime string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(raw, &q) != nil || len(q.Command.Data.Groups) == 0 {
		return nil
	}
	a := &quotaAccount{ID: "antigravity", Source: "cli", Limits: []quotaLimit{}, CheckedAt: time.Now().UnixMilli()}
	for _, g := range q.Command.Data.Groups {
		for _, b := range g.Buckets {
			if b.Remaining == nil {
				continue
			}
			l := quotaLimit{ID: b.ID, Group: g.Name, Used: clampPercent((1 - *b.Remaining) * 100), ResetAt: parseResetTime(b.ResetTime)}
			switch b.Window {
			case "5h":
				l.Window = "5h"
			case "weekly":
				l.Window = "7d"
			default:
				l.Window, l.Label = "other", b.Name
			}
			a.Limits = append(a.Limits, l)
		}
	}
	if raw, err := runQuotaCLI(ctx, "agy", "-p", "/credits", "--output-format", "json"); err == nil {
		var c struct {
			Command struct {
				Data struct {
					Remaining *float64 `json:"remaining_credits"`
				} `json:"data"`
			} `json:"command"`
		}
		if json.Unmarshal(raw, &c) == nil {
			a.Credits = c.Command.Data.Remaining
		}
	}
	return a
}

// ---- HTTP ----

// handleQuotaReset utilise une réinitialisation, à la demande de la personne.
// Seul Codex le permet hors de son CLI ; Claude Code l'offre dans /limit-reset.
func handleQuotaReset(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Account  string `json:"account"`
		CreditID string `json:"credit_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Account != "codex" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "only Codex resets can be used from Loom"})
		return
	}
	outcome, err := consumeCodexReset(r.Context(), req.CreditID)
	invalidateQuotaAccounts()
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "outcome": outcome})
}
