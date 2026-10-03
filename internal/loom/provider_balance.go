package loom

// Provider account observations are independent of model execution and native
// harness quotas. Only documented read-only endpoints on exact official hosts
// receive the connection's own session key.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const providerBalanceTTL = 30 * time.Second
const providerBalanceTimeout = 10 * time.Second
const providerBalanceMaxBody = 1 << 20

type ProviderPeriodUsage struct {
	Day   *float64 `json:"day"`
	Week  *float64 `json:"week"`
	Month *float64 `json:"month"`
}
type ProviderBalance struct {
	ProviderID     string              `json:"provider_id"`
	Name           string              `json:"name"`
	Currency       *string             `json:"currency"`
	Balance        *float64            `json:"balance"`
	Granted        *float64            `json:"granted"`
	Used           *float64            `json:"used"`
	Limit          *float64            `json:"limit"`
	LimitRemaining *float64            `json:"limit_remaining"`
	PeriodUsage    ProviderPeriodUsage `json:"period_usage"`
	FreeTier       *bool               `json:"free_tier"`
	Source         string              `json:"source"`
	FetchedAt      int64               `json:"fetched_at"`
	Error          string              `json:"error"`
	Supported      bool                `json:"supported"`
}

// A fingerprint invalidates observations on key rotation without retaining a
// credential in the cache. Cache ownership follows the workspace session.
type providerBalanceIdentity struct {
	ID, Endpoint string
	Credential   [32]byte
}
type providerBalanceEntry struct {
	identity providerBalanceIdentity
	result   ProviderBalance
	at       time.Time
}
type providerBalanceFlight struct {
	done   chan struct{}
	result ProviderBalance
}
type providerBalanceCache struct {
	mu      sync.Mutex
	items   map[string]providerBalanceEntry
	flights map[providerBalanceIdentity]*providerBalanceFlight
	client  *http.Client
}

func newProviderBalanceCache(client *http.Client) *providerBalanceCache {
	return &providerBalanceCache{items: map[string]providerBalanceEntry{}, flights: map[providerBalanceIdentity]*providerBalanceFlight{}, client: client}
}
func providerBalanceVaultOpen() bool { return !memEncActive() || memUnlocked() }
func (c *providerBalanceCache) read(ctx context.Context, p CloudProvider, key string, refresh bool) ProviderBalance {
	identity := providerBalanceIdentity{p.ID, p.Endpoint, sha256.Sum256([]byte(key))}
	c.mu.Lock()
	if entry, ok := c.items[p.ID]; ok && entry.identity == identity && !refresh && time.Since(entry.at) < providerBalanceTTL {
		c.mu.Unlock()
		q := entry.result
		q.Name = p.Name
		return q
	}
	if flight := c.flights[identity]; flight != nil {
		c.mu.Unlock()
		select {
		case <-flight.done:
			q := flight.result
			q.Name = p.Name
			return q
		case <-ctx.Done():
			q, _, _ := providerBalanceDescriptor(p)
			q.Error = "reading interrupted"
			return q
		}
	}
	flight := &providerBalanceFlight{done: make(chan struct{})}
	c.flights[identity] = flight
	c.mu.Unlock()
	q := readProviderBalance(ctx, p, key, c.client)
	c.mu.Lock()
	// A vault relocked during I/O must not admit private account data.
	if providerBalanceVaultOpen() {
		c.items[p.ID] = providerBalanceEntry{identity, q, time.Now()}
	}
	flight.result = q
	delete(c.flights, identity)
	close(flight.done)
	c.mu.Unlock()
	return q
}

func providerBalanceDescriptor(p CloudProvider) (ProviderBalance, string, string) {
	q := ProviderBalance{ProviderID: p.ID, Name: p.Name, FetchedAt: time.Now().Unix()}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		q.Error = "public balance API unavailable for this destination"
		return q, "", ""
	}
	host := strings.ToLower(u.Hostname())
	kind, currency := "", ""
	switch host {
	case "openrouter.ai":
		kind, currency = "openrouter", "USD"
	case "api.deepseek.com":
		kind = "deepseek"
	case "api.moonshot.ai":
		kind, currency = "moonshot", "USD"
	case "api.moonshot.cn":
		kind, currency = "moonshot", "CNY"
	case "api.siliconflow.com", "api.siliconflow.cn":
		kind = "siliconflow"
	default:
		q.Error = "public balance API unavailable with a normal API key"
		return q, "", ""
	}
	q.Supported = true
	if currency != "" {
		q.Currency = &currency
	}
	return q, kind, "https://" + host
}

func readProviderBalance(ctx context.Context, p CloudProvider, key string, client *http.Client) ProviderBalance {
	q, kind, origin := providerBalanceDescriptor(p)
	if !q.Supported {
		return q
	}
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n") {
		q.Error = "reconnect this provider: session API key unavailable or invalid"
		return q
	}
	ctx, cancel := context.WithTimeout(ctx, providerBalanceTimeout)
	defer cancel()
	if client == nil {
		client = &http.Client{Timeout: providerBalanceTimeout}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	switch kind {
	case "openrouter":
		q.Source = "OpenRouter · /api/v1/key (key usage)"
		var keyData struct {
			Data *struct {
				Limit     *float64 `json:"limit"`
				Remaining *float64 `json:"limit_remaining"`
				Usage     *float64 `json:"usage"`
				Day       *float64 `json:"usage_daily"`
				Week      *float64 `json:"usage_weekly"`
				Month     *float64 `json:"usage_monthly"`
				FreeTier  *bool    `json:"is_free_tier"`
			} `json:"data"`
		}
		var credits struct {
			Data *struct {
				Credits *float64 `json:"total_credits"`
				Usage   *float64 `json:"total_usage"`
			} `json:"data"`
		}
		var keyErr, creditsErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			keyErr = providerBalanceJSON(ctx, &safeClient, origin+"/api/v1/key", key, &keyData)
		}()
		go func() {
			defer wg.Done()
			creditsErr = providerBalanceJSON(ctx, &safeClient, origin+"/api/v1/credits", key, &credits)
		}()
		wg.Wait()
		if keyErr == nil && keyData.Data == nil {
			keyErr = errors.New("unrecognized balance format")
		}
		if creditsErr == nil && credits.Data == nil {
			creditsErr = errors.New("unrecognized balance format")
		}
		if keyErr == nil {
			d := keyData.Data
			q.Limit, q.LimitRemaining, q.Used = d.Limit, d.Remaining, d.Usage
			q.PeriodUsage = ProviderPeriodUsage{d.Day, d.Week, d.Month}
			q.FreeTier = d.FreeTier
		} else {
			q.Error = "/api/v1/key : " + keyErr.Error()
		}
		if creditsErr == nil {
			q.Source += " · /api/v1/credits (account balance)"
			d := credits.Data
			if d.Usage != nil {
				q.Used = d.Usage
				q.Source += " · used: account"
			}
			if d.Credits != nil && d.Usage != nil {
				balance := *d.Credits - *d.Usage
				if !math.IsInf(balance, 0) && !math.IsNaN(balance) {
					q.Balance = &balance
				}
			}
		} else {
			q.Error = strings.TrimSpace(q.Error + " /api/v1/credits : " + creditsErr.Error())
		}
	case "deepseek":
		q.Source = "DeepSeek · /user/balance"
		var data struct {
			Infos []struct {
				Currency string        `json:"currency"`
				Total    balanceNumber `json:"total_balance"`
				Granted  balanceNumber `json:"granted_balance"`
			} `json:"balance_infos"`
		}
		err := providerBalanceJSON(ctx, &safeClient, origin+"/user/balance", key, &data)
		if err != nil {
			q.Error = err.Error()
			break
		}
		// Never sum different currencies or silently choose one account balance.
		if len(data.Infos) != 1 {
			q.Error = "single balance unavailable: no currency or multiple currencies reported"
			break
		}
		d := data.Infos[0]
		if d.Currency != "USD" && d.Currency != "CNY" {
			q.Error = "unrecognized balance currency"
			break
		}
		q.Currency, q.Balance, q.Granted = &d.Currency, d.Total.value, d.Granted.value
	case "moonshot":
		q.Source = "Moonshot/Kimi · /v1/users/me/balance"
		var data struct {
			Code   *int  `json:"code"`
			Status *bool `json:"status"`
			Data   *struct {
				Available *float64 `json:"available_balance"`
				Voucher   *float64 `json:"voucher_balance"`
			} `json:"data"`
		}
		err := providerBalanceJSON(ctx, &safeClient, origin+"/v1/users/me/balance", key, &data)
		if err != nil {
			q.Error = err.Error()
			break
		}
		if data.Data == nil || data.Code == nil || *data.Code != 0 || (data.Status != nil && !*data.Status) {
			q.Error = "balance rejected or unrecognized format"
			break
		}
		q.Balance, q.Granted = data.Data.Available, data.Data.Voucher
	case "siliconflow":
		q.Source = "SiliconFlow · /v1/user/info (totalBalance)"
		var data struct {
			Code   *int  `json:"code"`
			Status *bool `json:"status"`
			Data   *struct {
				Total balanceNumber `json:"totalBalance"`
			} `json:"data"`
		}
		err := providerBalanceJSON(ctx, &safeClient, origin+"/v1/user/info", key, &data)
		if err != nil {
			q.Error = err.Error()
			break
		}
		if data.Data == nil || data.Code == nil || *data.Code != 20000 || (data.Status != nil && !*data.Status) {
			q.Error = "balance rejected or unrecognized format"
			break
		}
		// The documented schema gives no currency or meaning for promotional credit.
		q.Balance = data.Data.Total.value
	}
	if ctx.Err() != nil {
		q.Error = "reading interrupted (10-second limit)"
	}
	q.FetchedAt = time.Now().Unix()
	return q
}

// String amounts are documented by DeepSeek and SiliconFlow. Null/missing
// values stay unknown; malformed or non-finite numbers never become zero.
type balanceNumber struct{ value *float64 }

func (n *balanceNumber) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.value = nil
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) != nil {
		return errors.New("unrecognized amount")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return errors.New("unrecognized amount")
	}
	n.value = &v
	return nil
}

// Return only fixed diagnostics and HTTP status, never upstream bodies, URLs,
// transport errors or JSON decoder messages (all can echo credentials).
func providerBalanceJSON(ctx context.Context, client *http.Client, endpoint, key string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid balance destination")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("balance API unreachable or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("balance rejected (HTTP %d)", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, providerBalanceMaxBody+1))
	if err != nil || len(b) > providerBalanceMaxBody {
		return errors.New("balance response too large or interrupted")
	}
	if json.Unmarshal(b, dst) != nil {
		return errors.New("unrecognized balance format")
	}
	return nil
}

func (m *runtimeSessions) providerBalanceKey(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !providerBalanceVaultOpen() {
		return ""
	}
	return m.keys[id]
}
func handleProviderBalances(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	providers := workspaceSessions.providers()
	connected := []CloudProvider{}
	for _, p := range providers {
		if p.Ready {
			connected = append(connected, p)
		}
	}
	rows := make([]ProviderBalance, len(connected))
	var wg sync.WaitGroup
	for i, p := range connected {
		key := workspaceSessions.providerBalanceKey(p.ID)
		wg.Add(1)
		go func(i int, p CloudProvider, key string) {
			defer wg.Done()
			rows[i] = workspaceSessions.balances.read(r.Context(), p, key, false)
		}(i, p, key)
	}
	wg.Wait()
	if !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "providers": rows})
}
func handleProviderBalanceRefresh(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		ProviderID string `json:"provider_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	for _, p := range workspaceSessions.providers() {
		if p.ID != req.ProviderID {
			continue
		}
		key := workspaceSessions.providerBalanceKey(p.ID)
		if key == "" {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "reconnect this provider: session API key unavailable"})
			return
		}
		q := workspaceSessions.balances.read(r.Context(), p, key, true)
		if !usageVaultAccess(w) {
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "provider": q})
		return
	}
	sendJSON(w, 404, map[string]any{"ok": false, "error": "provider not found"})
}
