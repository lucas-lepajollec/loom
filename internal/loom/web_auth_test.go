package loom

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// La clé de pilotage n'est stockée QUE sous forme d'empreinte, jamais en clair,
// et la validation Bearer marche par comparaison d'empreintes.
func TestWebKeyHashOnly(t *testing.T) {
	testHome(t)
	if err := storeWebKey("ma-cle-secrete"); err != nil {
		t.Fatal(err)
	}
	if getStr(bkState, "web_key") != "" {
		t.Fatal("la clé en clair ne doit JAMAIS être stockée")
	}
	if getStr(bkState, "web_key_hash") != hashWebKey("ma-cle-secrete") {
		t.Fatal("l'empreinte devrait être stockée")
	}
	prot := requireWebAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	call := func(bearer string) int {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/status", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		prot(rr, req)
		return rr.Code
	}
	if call("ma-cle-secrete") != 200 {
		t.Fatal("la bonne clé devrait passer")
	}
	if call("mauvaise") != 401 {
		t.Fatal("une mauvaise clé devrait être refusée")
	}
	if call("") != 401 {
		t.Fatal("sans clé devrait être refusé")
	}
}

// Une requête marquée E2E-authentifiée (contexte) passe SANS clé de pilotage.
func TestE2EAuthBypassesKey(t *testing.T) {
	testHome(t)
	storeWebKey("cle")
	prot := requireWebAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	rr := httptest.NewRecorder()
	req := markE2EAuthed(httptest.NewRequest("GET", "/api/status", nil)) // pas de Bearer
	prot(rr, req)
	if rr.Code != 200 {
		t.Fatalf("une requête E2E-authentifiée devrait passer sans clé, got %d", rr.Code)
	}
	// Sans le marquage, la même requête sans Bearer est refusée (pas de forge possible).
	rr2 := httptest.NewRecorder()
	prot(rr2, httptest.NewRequest("GET", "/api/status", nil))
	if rr2.Code != 401 {
		t.Fatal("sans marquage E2E ni Bearer, doit être 401")
	}
}

// Migration : une ancienne clé en clair est convertie en empreinte puis effacée.
func TestMigrateWebKeyToHash(t *testing.T) {
	testHome(t)
	putStr(bkState, "web_key", "ancienne-cle-en-clair")
	migrateWebKeyToHash()
	if getStr(bkState, "web_key") != "" {
		t.Fatal("le clair aurait dû être effacé")
	}
	if getStr(bkState, "web_key_hash") != hashWebKey("ancienne-cle-en-clair") {
		t.Fatal("l'empreinte aurait dû être posée")
	}
}

func TestLegacyWebKeyProtectsAPIBeforeMigration(t *testing.T) {
	testHome(t)
	if err := putStr(bkState, "web_key", "ancienne-cle"); err != nil {
		t.Fatal(err)
	}
	if got := getStr(bkState, "web_key_hash"); got != "" {
		t.Fatalf("unexpected hash in legacy fixture: %q", got)
	}
	protected := requireWebAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, tc := range []struct {
		name, auth string
		want       int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer mauvaise-cle", http.StatusUnauthorized},
		{"valid", "Bearer ancienne-cle", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rr := httptest.NewRecorder()
			protected(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}
