package loom

import "testing"

// Une lecture ratée ne doit pas se confondre avec « aucune clé » : sans clé,
// l'API de pilotage est OUVERTE, donc l'erreur doit remonter pour que
// requireWebAuth ferme au lieu d'ouvrir.
func TestWebKeyDistingueVideEtErreur(t *testing.T) {
	testHome(t)
	k, err := readWebKeyErr()
	if err != nil || k != "" {
		t.Fatalf("base neuve : attendu (\"\", nil), reçu (%q, %v)", k, err)
	}
	if err := putStr(bkState, "web_key", "secret"); err != nil {
		t.Fatal(err)
	}
	if k, err := readWebKeyErr(); err != nil || k != "secret" {
		t.Fatalf("attendu (\"secret\", nil), reçu (%q, %v)", k, err)
	}
}
