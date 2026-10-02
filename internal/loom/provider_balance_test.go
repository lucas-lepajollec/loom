package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type balanceFixtureTransport func(*http.Request) (*http.Response, error)

func (f balanceFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// In-process httptest fixtures exercise the HTTP wire without depending on
// permission to bind loopback sockets in a restricted sandbox.
func balanceFixtureClient(h http.HandlerFunc) *http.Client {
	return &http.Client{Transport: balanceFixtureTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h(w, r)
		return w.Result(), nil
	})}
}
func balanceValue(t *testing.T, v *float64, want float64) {
	t.Helper()
	if v == nil || *v != want {
		t.Fatalf("amount %v, want %v", v, want)
	}
}
func TestProviderBalanceOfficialFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, path, body, currency string
		balance                              float64
		granted                              *float64
	}{
		{"DeepSeek", "https://api.deepseek.com/v1", "/user/balance", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`, "CNY", 110, balanceTestPtr(10)},
		{"Kimi global", "https://api.moonshot.ai/v1", "/v1/users/me/balance", `{"code":0,"status":true,"scode":"0x0","data":{"available_balance":49.58894,"voucher_balance":46.58893,"cash_balance":3.00001}}`, "USD", 49.58894, balanceTestPtr(46.58893)},
		{"Kimi China", "https://api.moonshot.cn/v1", "/v1/users/me/balance", `{"code":0,"status":true,"data":{"available_balance":0,"voucher_balance":0,"cash_balance":0}}`, "CNY", 0, balanceTestPtr(0)},
		{"SiliconFlow global", "https://api.siliconflow.com/v1", "/v1/user/info", `{"code":20000,"status":true,"data":{"id":"private-account-id","email":"private@example.invalid","balance":"0.88","chargeBalance":"88.00","totalBalance":"88.88"}}`, "", 88.88, nil},
		{"SiliconFlow China", "https://api.siliconflow.cn/v1", "/v1/user/info", `{"code":20000,"status":true,"data":{"totalBalance":"0"}}`, "", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.ContentLength != 0 {
					t.Error("unexpected balance request")
				}
				io.WriteString(w, tc.body)
			})
			p := CloudProvider{ID: "fixture", Name: tc.name, Endpoint: tc.endpoint}
			q := readProviderBalance(context.Background(), p, "synthetic-secret", client)
			if calls != 1 || !q.Supported || q.Error != "" || q.FetchedAt == 0 || q.Source == "" || q.ProviderID != p.ID || q.Name != p.Name {
				t.Fatalf("%+v calls=%d", q, calls)
			}
			balanceValue(t, q.Balance, tc.balance)
			if tc.granted == nil {
				if q.Granted != nil {
					t.Fatal("invented grant")
				}
			} else {
				balanceValue(t, q.Granted, *tc.granted)
			}
			if tc.currency == "" {
				if q.Currency != nil {
					t.Fatal("invented currency")
				}
			} else if q.Currency == nil || *q.Currency != tc.currency {
				t.Fatal("currency")
			}
			raw, _ := json.Marshal(q)
			for _, field := range []string{"used", "limit", "limit_remaining", "free_tier", "day", "week", "month"} {
				if !strings.Contains(string(raw), `"`+field+`":null`) {
					t.Fatalf("missing nullable %s: %s", field, raw)
				}
			}
			for _, private := range []string{"synthetic-secret", "private-account-id", "private@example.invalid", "chargeBalance", "topped_up_balance"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("private upstream data returned")
				}
			}
		})
	}
}
func balanceTestPtr(v float64) *float64 { return &v }
func TestProviderBalanceOpenRouter(t *testing.T) {
	for _, status := range []int{200, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			client := balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer router-secret" {
					t.Error("request")
				}
				switch r.URL.Path {
				case "/api/v1/key":
					io.WriteString(w, `{"data":{"label":"router-secret","limit":100,"limit_remaining":74.5,"usage":25.5,"usage_daily":1,"usage_weekly":2,"usage_monthly":3,"is_free_tier":false}}`)
				case "/api/v1/credits":
					w.WriteHeader(status)
					if status == 200 {
						io.WriteString(w, `{"data":{"total_credits":100.5,"total_usage":30.75}}`)
					} else {
						io.WriteString(w, `{"error":{"message":"router-secret: management key required"}}`)
					}
				default:
					t.Error("undocumented endpoint")
				}
			})
			q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://openrouter.ai/api/v1"}, "router-secret", client)
			if !q.Supported || calls.Load() != 2 || q.Currency == nil || *q.Currency != "USD" || q.FreeTier == nil || *q.FreeTier || q.Granted != nil {
				t.Fatalf("%+v", q)
			}
			balanceValue(t, q.Limit, 100)
			balanceValue(t, q.LimitRemaining, 74.5)
			balanceValue(t, q.PeriodUsage.Day, 1)
			balanceValue(t, q.PeriodUsage.Week, 2)
			balanceValue(t, q.PeriodUsage.Month, 3)
			if status == 200 {
				balanceValue(t, q.Balance, 69.75)
				balanceValue(t, q.Used, 30.75)
				if q.Error != "" {
					t.Fatal(q.Error)
				}
			} else {
				if q.Balance != nil || !strings.Contains(q.Error, "HTTP 403") {
					t.Fatalf("%+v", q)
				}
				balanceValue(t, q.Used, 25.5)
			}
			raw, _ := json.Marshal(q)
			if strings.Contains(string(raw), "router-secret") {
				t.Fatal("key disclosed")
			}
		})
	}
}
func TestProviderBalanceUnsupportedHosts(t *testing.T) {
	client := balanceFixtureClient(func(http.ResponseWriter, *http.Request) { t.Error("unsupported provider contacted") })
	for _, host := range []string{"api.mistral.ai", "api.openai.com", "api.anthropic.com", "api.groq.com", "generativelanguage.googleapis.com", "openrouter.ai.evil.invalid", "api.deepseek.com.evil.invalid", "proxy.invalid"} {
		q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://" + host + "/v1"}, "secret", client)
		if q.Supported || q.Error == "" || q.Balance != nil || q.Currency != nil {
			t.Fatalf("%s: %+v", host, q)
		}
	}
	for _, endpoint := range []string{"http://openrouter.ai/api/v1", "https://user:pass@api.deepseek.com", "https://api.deepseek.com:8443/v1", "https://api.deepseek.com/v1?key=secret"} {
		q := readProviderBalance(context.Background(), CloudProvider{Endpoint: endpoint}, "secret", client)
		if q.Supported {
			t.Fatal("unsafe destination accepted")
		}
	}
}
func TestProviderBalanceErrorsSanitizedAndUnknown(t *testing.T) {
	for _, body := range []string{
		`{"code":0,"status":false,"message":"synthetic-secret","data":{"available_balance":12}}`,
		`{"code":401,"data":{"available_balance":12}}`,
		`{"error":{"message":"synthetic-secret"}}`,
		`{"code":0,"data":{"available_balance":"synthetic-secret"}}`,
		`not-json synthetic-secret`,
	} {
		q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.moonshot.ai/v1"}, "synthetic-secret", balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		if q.Error == "" || strings.Contains(q.Error, "synthetic-secret") || q.Balance != nil {
			t.Fatalf("%+v", q)
		}
	}
	for _, body := range []string{`{"is_available":true,"balance_infos":[]}`, `{"balance_infos":[{"currency":"secret","total_balance":"1"}]}`, `{"balance_infos":[{"currency":"USD","total_balance":"1"},{"currency":"CNY","total_balance":"2"}]}`, `{"balance_infos":[{"currency":"USD","total_balance":"NaN"}]}`} {
		q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		if q.Error == "" || q.Balance != nil || q.Currency != nil {
			t.Fatalf("invalid balance %+v", q)
		}
	}
	q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.moonshot.ai/v1"}, "secret", balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":{}}`) }))
	if q.Error != "" || q.Balance != nil || q.Granted != nil {
		t.Fatal("missing became zero")
	}
	for _, status := range []int{401, 429, 500} {
		q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); io.WriteString(w, "secret") }))
		if q.Error == "" || strings.Contains(q.Error, "secret") {
			t.Fatal("unsanitized HTTP error")
		}
	}
	client := &http.Client{Transport: balanceFixtureTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("transport leaked secret") })}
	q = readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", client)
	if q.Error == "" || strings.Contains(q.Error, "secret") {
		t.Fatal("unsanitized transport error")
	}
}
func TestProviderBalanceRedirectBoundAndDeadline(t *testing.T) {
	calls := 0
	client := balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://other.invalid/secret")
		w.WriteHeader(302)
	})
	q := readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", client)
	if calls != 1 || !strings.Contains(q.Error, "302") {
		t.Fatal("redirect followed")
	}
	q = readProviderBalance(context.Background(), CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", providerBalanceMaxBody+1))
	}))
	if q.Error == "" {
		t.Fatal("oversize accepted")
	}
	client = &http.Client{Transport: balanceFixtureTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > providerBalanceTimeout {
			t.Error("missing ten second budget")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q = readProviderBalance(ctx, CloudProvider{Endpoint: "https://api.deepseek.com"}, "secret", client)
	if !strings.Contains(q.Error, "10-second") {
		t.Fatal("deadline not reported")
	}
}
func TestProviderBalanceCache(t *testing.T) {
	testHome(t)
	var calls atomic.Int32
	client := balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"balance_infos":[{"currency":"USD","total_balance":"0"}]}`)
	})
	c := newProviderBalanceCache(client)
	p := CloudProvider{ID: "provider", Name: "Name", Endpoint: "https://api.deepseek.com/v1"}
	for i := 0; i < 2; i++ {
		balanceValue(t, c.read(context.Background(), p, "key", false).Balance, 0)
	}
	if calls.Load() != 1 {
		t.Fatal("cache missed")
	}
	p.Name = "Updated"
	if c.read(context.Background(), p, "key", false).Name != "Updated" {
		t.Fatal("stale name")
	}
	c.read(context.Background(), p, "key", true)
	if calls.Load() != 2 {
		t.Fatal("refresh cached")
	}
	c.mu.Lock()
	entry := c.items[p.ID]
	entry.at = time.Now().Add(-providerBalanceTTL)
	c.items[p.ID] = entry
	c.mu.Unlock()
	c.read(context.Background(), p, "key", false)
	if calls.Load() != 3 {
		t.Fatal("expired cache reused")
	}
	c.read(context.Background(), p, "new-key", false)
	if calls.Load() != 4 {
		t.Fatal("key rotation reused cache")
	}
	p.Endpoint = "https://api.deepseek.com"
	c.read(context.Background(), p, "new-key", false)
	if calls.Load() != 5 {
		t.Fatal("destination changed reused cache")
	}
	// Failures have the same TTL and do not repeatedly hit an account.
	c = newProviderBalanceCache(balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	c.read(context.Background(), p, "key", false)
	c.read(context.Background(), p, "key", false)
	if calls.Load() != 6 {
		t.Fatal("error was not cached")
	}
}
func TestProviderBalanceConcurrentCache(t *testing.T) {
	testHome(t)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	c := newProviderBalanceCache(balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		io.WriteString(w, `{"balance_infos":[{"currency":"USD","total_balance":"1"}]}`)
	}))
	p := CloudProvider{ID: "fixture", Endpoint: "https://api.deepseek.com"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); c.read(context.Background(), p, "key", false) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.read(ctx, p, "key", true).Error != "reading interrupted" {
		t.Error("cache wait ignored cancellation")
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.read(context.Background(), p, "key", false) }()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate account reads", calls.Load())
	}
}

func isolateBalanceSessions(t *testing.T, client *http.Client) {
	t.Helper()
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	workspaceSessions.balances.client = client
	t.Cleanup(func() { workspaceSessions = old })
}
func saveBalanceProvider(t *testing.T, id, endpoint, key string) {
	t.Helper()
	if err := putStoreJSON(bkProviders, id, CloudProvider{ID: id, Name: id, Endpoint: endpoint, Model: "fixture"}); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.keys[id] = key
}
func TestProviderBalanceAPI(t *testing.T) {
	var calls atomic.Int32
	// Both supported providers must enter their HTTP read before either finishes.
	both := make(chan struct{})
	var once sync.Once
	client := balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			once.Do(func() { close(both) })
		}
		select {
		case <-both:
		case <-time.After(time.Second):
			t.Error("provider reads are sequential")
		}
		if r.URL.Host == "api.deepseek.com" {
			io.WriteString(w, `{"balance_infos":[{"currency":"USD","total_balance":"2"}]}`)
		} else {
			io.WriteString(w, `{"code":0,"status":true,"data":{"available_balance":3}}`)
		}
	})
	isolateBalanceSessions(t, client)
	saveBalanceProvider(t, "a", "https://api.deepseek.com/v1", "a-secret")
	saveBalanceProvider(t, "b", "https://api.moonshot.ai/v1", "b-secret")
	saveBalanceProvider(t, "c", "https://api.openai.com/v1", "c-secret")
	saveBalanceProvider(t, "disconnected", "https://api.deepseek.com/v1", "")
	if err := storeWebKey("control-secret"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.Header.Set("Authorization", "Bearer control-secret")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/usage/providers", "/api/usage/providers/refresh"} {
		method := "GET"
		if strings.HasSuffix(path, "refresh") {
			method = "POST"
		}
		if w := request(method, path, `{"provider_id":"a"}`, false); w.Code != 401 {
			t.Fatal("unprotected route", w.Code)
		}
	}
	w := request("GET", "/api/usage/providers", "", true)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		OK        bool              `json:"ok"`
		Providers []ProviderBalance `json:"providers"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || !response.OK || len(response.Providers) != 3 || calls.Load() != 2 {
		t.Fatal("connected results", w.Body.String())
	}
	if response.Providers[2].Supported || response.Providers[2].Error == "" {
		t.Fatal("unsupported capability")
	}
	for _, secret := range []string{"a-secret", "b-secret", "c-secret", "control-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("key returned")
		}
	}
	request("GET", "/api/usage/providers", "", true)
	if calls.Load() != 2 {
		t.Fatal("GET bypassed cache")
	}
	w = request("POST", "/api/usage/providers/refresh", `{"provider_id":"a"}`, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"provider_id":"a"`) || calls.Load() != 3 {
		t.Fatal("refresh", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"provider_id":"missing"}`, 404}, {`{"provider_id":"disconnected"}`, 409}, {`{"provider_id":"a","key":"secret"}`, 400}, {`{}`, 404}} {
		if w := request("POST", "/api/usage/providers/refresh", tc.body, true); w.Code != tc.status {
			t.Fatal("validation", w.Code)
		}
	}
	if w := request("POST", "/api/usage/providers", "", true); w.Code != 405 {
		t.Fatal("method")
	}
	if w := request("GET", "/api/usage/providers/refresh", "", true); w.Code != 405 {
		t.Fatal("method")
	}
}
func TestProviderBalanceVaultLock(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "during"}[during], func(t *testing.T) {
			var calls atomic.Int32
			isolateBalanceSessions(t, balanceFixtureClient(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if during {
					if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
						t.Error(err)
					}
				}
				io.WriteString(w, `{"balance_infos":[{"currency":"USD","total_balance":"99"}]}`)
			}))
			clearMemDEK()
			t.Cleanup(clearMemDEK)
			saveBalanceProvider(t, "a", "https://api.deepseek.com", "secret")
			if !during {
				if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			handleProviderBalances(w, httptest.NewRequest("GET", "/api/usage/providers", nil))
			if w.Code != 423 || strings.Contains(w.Body.String(), "99") || strings.Contains(w.Body.String(), "secret") {
				t.Fatal("locked disclosure", w.Code, w.Body.String())
			}
			if !during && calls.Load() != 0 {
				t.Fatal("locked account contacted")
			}
			if len(workspaceSessions.balances.items) != 0 {
				t.Fatal("locked account cached")
			}
			r := httptest.NewRequest("POST", "/api/usage/providers/refresh", strings.NewReader(`{"provider_id":"a"}`))
			r.Header.Set("Content-Type", "application/json")
			w = httptest.NewRecorder()
			handleProviderBalanceRefresh(w, r)
			if w.Code != 423 {
				t.Fatal("locked refresh")
			}
		})
	}
}
