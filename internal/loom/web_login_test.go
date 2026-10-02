package loom

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func loginTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	testHome(t)
	webLoginLimit.Lock()
	webLoginLimit.at = time.Time{}
	webLoginLimit.count = 0
	clear(webLoginLimit.peers)
	webLoginLimit.Unlock()
	mux := http.NewServeMux()
	registerWebLogin(mux)
	webAPI(mux)("/api/test", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	return mux
}

func authCall(mux *http.ServeMux, method, path, body string, cookie *http.Cookie, bearer, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://loom.example"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

const testAccessPassword = "synthetic access password"

func TestPasswordMigrationAndSessions(t *testing.T) {
	mux := loginTestMux(t)
	if err := storeWebKey("legacy-automation-key"); err != nil {
		t.Fatal(err)
	}
	if err := putStr(bkState, "api_key", "inference-key"); err != nil {
		t.Fatal(err)
	}
	if err := putStr(bkState, "mem_keyvault", "untouched vault fixture"); err != nil {
		t.Fatal(err)
	}
	body := `{"password":"` + testAccessPassword + `"}`
	if w := authCall(mux, "POST", "/api/auth/password", body, nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := authCall(mux, "POST", "/api/auth/password", body, nil, "legacy-automation-key", "http://loom.example")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].MaxAge != int(webSessionLife.Seconds()) {
		t.Fatalf("cookie flags: %+v", cookies)
	}
	cookie := cookies[0]
	stored := getBytes(bkState, webPasswordKey)
	if bytes.Contains(stored, []byte(testAccessPassword)) || bytes.Contains(stored, []byte(cookie.Value)) {
		t.Fatal("plaintext credential stored")
	}
	if getStr(bkState, "api_key") != "inference-key" || getStr(bkState, "mem_keyvault") != "untouched vault fixture" || !webKeyConfigured() {
		t.Fatal("migration changed independent credentials")
	}
	for _, tc := range []struct {
		cookie *http.Cookie
		bearer string
		want   int
	}{
		{nil, "", 401}, {cookie, "", 204}, {nil, "legacy-automation-key", 204},
		{nil, testAccessPassword, 401}, {nil, "inference-key", 401},
		{&http.Cookie{Name: webSessionCookie, Value: strings.Repeat("a", 64)}, "", 401},
	} {
		if got := authCall(mux, "GET", "/api/test", "", tc.cookie, tc.bearer, "").Code; got != tc.want {
			t.Fatalf("authentication: got %d want %d", got, tc.want)
		}
	}
	// The persisted session is verified from storage on every request, including
	// a new mux/process: no browser secret or process-local session map is needed.
	other := http.NewServeMux()
	registerWebLogin(other)
	webAPI(other)("/api/test", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	if got := authCall(other, "GET", "/api/test", "", cookie, "", "").Code; got != 204 {
		t.Fatal(got)
	}
	if got := authCall(mux, "POST", "/api/test", "{}", cookie, "", "http://evil.example").Code; got != 403 {
		t.Fatal("CSRF allowed", got)
	}
	if got := authCall(mux, "POST", "/api/test", "{}", nil, "legacy-automation-key", "http://control-plane.example").Code; got != 204 {
		t.Fatal("explicit remote-engine credentials lost compatibility", got)
	}
	if got := authCall(mux, "POST", "/api/auth/login", body, nil, "", "http://evil.example").Code; got != 403 {
		t.Fatal("login CSRF allowed", got)
	}
	if got := authCall(mux, "POST", "/api/auth/logout", "{}", cookie, "", "http://evil.example").Code; got != 403 {
		t.Fatal("logout CSRF allowed", got)
	}
	if got := authCall(mux, "POST", "/api/auth/logout", "{}", cookie, "", "http://loom.example").Code; got != 200 {
		t.Fatal(got)
	}
	if got := authCall(mux, "GET", "/api/test", "", cookie, "", "").Code; got != 401 {
		t.Fatal("logout not revoked", got)
	}
}

func TestPasswordLoginChangeRecoveryAndFailClosed(t *testing.T) {
	mux := loginTestMux(t)
	if err := saveWebPassword(testAccessPassword, nil); err != nil {
		t.Fatal(err)
	}
	if err := webListenCheck("0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	st, key, err := setWebExposure(true)
	if err != nil || key != "" || !st.PasswordSet || st.KeySet {
		t.Fatalf("network created unnecessary key: %+v %v", st, err)
	}
	if got := authCall(mux, "POST", "/api/auth/login", `{"password":"wrong"}`, nil, "", "").Code; got != 401 {
		t.Fatal(got)
	}
	w := authCall(mux, "POST", "/api/auth/login", `{"password":"`+testAccessPassword+`"}`, nil, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if got := authCall(mux, "POST", "/api/auth/password", `{"password":"another synthetic password","current_password":"wrong"}`, cookie, "", "").Code; got != 401 {
		t.Fatal(got)
	}
	w = authCall(mux, "POST", "/api/auth/password", `{"password":"another synthetic password","current_password":"`+testAccessPassword+`"}`, cookie, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	newCookie := w.Result().Cookies()[0]
	if got := authCall(mux, "GET", "/api/test", "", cookie, "", "").Code; got != 401 {
		t.Fatal("old session survived rotation", got)
	}
	if got := authCall(mux, "GET", "/api/test", "", newCookie, "", "").Code; got != 204 {
		t.Fatal(got)
	}
	if err := saveWebPassword("local recovery password", nil); err != nil {
		t.Fatal(err)
	}
	if got := authCall(mux, "GET", "/api/test", "", newCookie, "", "").Code; got != 401 {
		t.Fatal("recovery did not revoke", got)
	}
	if err := putBytes(bkState, webPasswordKey, []byte(`{"version":99}`)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/test", "/api/auth/status"} {
		if got := authCall(mux, "GET", path, "", newCookie, "", "").Code; got != 503 {
			t.Fatal("corrupt auth did not fail closed", path, got)
		}
	}
}

func TestLoginLimitsExpiryAndSecureCookie(t *testing.T) {
	mux := loginTestMux(t)
	if err := saveWebPassword(testAccessPassword, nil); err != nil {
		t.Fatal(err)
	}
	_, credential, _ := readWebPassword()
	r := httptest.NewRequest("POST", "https://loom.example/api/auth/login", nil)
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	if err := issueWebSession(w, r, credential); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatal("TLS cookie not Secure")
	}
	r = httptest.NewRequest("GET", "http://loom.example/api/test", nil)
	r.AddCookie(cookie)
	expired, _ := json.Marshal(webSession{hashWebKey(string(credential)), time.Now().Add(-time.Second).Unix()})
	if err := putBytes(bkState, sessionRecordKey(r), expired); err != nil {
		t.Fatal(err)
	}
	if got := authCall(mux, "GET", "/api/test", "", cookie, "", "").Code; got != 401 {
		t.Fatal("expired session accepted", got)
	}
	t.Setenv("LOOM_COOKIE_SECURE", "1")
	if !sessionCookie(r, "synthetic", time.Now()).Secure {
		t.Fatal("explicit proxy HTTPS policy ignored")
	}
	for i := 0; i < 11; i++ {
		w = authCall(mux, "POST", "/api/auth/login", `{"password":"wrong"}`, nil, "", "")
		want := 401
		if i == 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d want %d", i, w.Code, want)
		}
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no retry hint")
	}
}

func TestPasswordValidationAndConcurrentSetup(t *testing.T) {
	loginTestMux(t)
	if err := saveWebPassword("short", []byte{}); err == nil {
		t.Fatal("short password accepted")
	}
	if err := saveWebPassword(testAccessPassword, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := saveWebPassword("other setup password", []byte{}); err == nil {
		t.Fatal("concurrent setup overwrote password")
	}
	p, _, err := readWebPassword()
	if err != nil || !p.matches(testAccessPassword) {
		t.Fatal("original password damaged")
	}
}

// Repeated device sign-ins must stay bounded; changing the password must
// revoke every stored session and a concurrently verified old credential.
func TestPasswordAllDevicesRevokedAndSessionBound(t *testing.T) {
	mux := loginTestMux(t)
	if err := saveWebPassword(testAccessPassword, nil); err != nil {
		t.Fatal(err)
	}
	_, credential, err := readWebPassword()
	if err != nil {
		t.Fatal(err)
	}
	var cookies []*http.Cookie
	for i := 0; i < 35; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://loom.example/api/auth/login", nil)
		if err := issueWebSession(w, r, credential); err != nil {
			t.Fatal(err)
		}
		cookies = append(cookies, w.Result().Cookies()[0])
	}
	active := 0
	for _, cookie := range cookies {
		if authCall(mux, "GET", "/api/test", "", cookie, "", "").Code == 204 {
			active++
		}
	}
	if active != 32 {
		t.Fatalf("active sessions: %d, want 32", active)
	}
	if err := saveWebPassword("replacement access password", credential); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		if got := authCall(mux, "GET", "/api/test", "", cookie, "", "").Code; got != 401 {
			t.Fatalf("device session survived password change: %d", got)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://loom.example/api/auth/login", nil)
	if err := issueWebSession(w, r, credential); err == nil || len(w.Result().Cookies()) != 0 {
		t.Fatal("stale credential issued a session")
	}
}
