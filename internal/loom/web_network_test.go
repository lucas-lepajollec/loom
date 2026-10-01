package loom

import (
	"strings"
	"testing"
)

func TestWebExposureNeedsAKey(t *testing.T) {
	testHome(t)
	firewallInert = true
	if err := webListenCheck("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := webListenCheck("0.0.0.0"); err == nil || !strings.Contains(err.Error(), "clé") {
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
