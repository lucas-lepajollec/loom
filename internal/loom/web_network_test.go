package loom

import (
	"strings"
	"testing"
)

func TestWebExposureNeedsAKey(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_WEB_HOST", "")
	firewallInert = true
	if err := webListenCheck("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := webListenCheck("0.0.0.0"); err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("ouverture sans clé acceptée: %v", err)
	}
	st, key, err := setWebExposure(true)
	if err != nil || !st.Exposed || !st.KeySet || !strings.HasPrefix(key, "loom-web-") {
		t.Fatalf("%v %+v %q", err, st, key)
	}
	if err := webListenCheck(webHost()); err != nil {
		t.Fatalf("clé créée mais ouverture refusée: %v", err)
	}
	if _, again, _ := setWebExposure(true); again != "" {
		t.Fatal("une clé existante ne doit pas être remplacée")
	}
	if st, _, _ := setWebExposure(false); st.Exposed || webHost() != "127.0.0.1" {
		t.Fatalf("%+v", st)
	}
}

func TestWebHostOverrideKeepsAuthenticationAndSavedSettings(t *testing.T) {
	testHome(t)
	if err := SetConfigKey(webHostKey, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOM_WEB_HOST", " 192.0.2.20 ")
	if webHost() != "192.0.2.20" {
		t.Fatal("foreground listener override was ignored")
	}
	if err := webListenCheck(webHost()); err == nil {
		t.Fatal("LAN listener accepted without authentication")
	}
	if err := saveWebPassword("fixture-password-only", nil); err != nil {
		t.Fatal(err)
	}
	if err := webListenCheck(webHost()); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()[webHostKey] != "127.0.0.1" {
		t.Fatal("foreground override changed saved network settings")
	}
	t.Setenv("LOOM_WEB_HOST", "")
	if webHost() != "127.0.0.1" {
		t.Fatal("saved listener did not recover after clearing override")
	}
}
