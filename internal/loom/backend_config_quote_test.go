package loom

import (
	"testing"
)

// L'éditeur doit proposer les clés utiles même quand la configuration est vide,
// et ne jamais perdre une clé déjà définie.
func TestConfigEditorText(t *testing.T) {
	txt := configEditorText(map[string]string{"MODEL": "x.gguf", "INCONNUE": "1"})
	round := parseEnv(txt)
	if round["MODEL"] != "x.gguf" || round["INCONNUE"] != "1" {
		t.Fatalf("clés perdues : %v", round)
	}
	if len(round) != 2 {
		t.Fatalf("les clés non renseignées doivent rester commentées : %v", round)
	}
	if !contains(txt, "#BIN=") {
		t.Fatal("le squelette doit documenter BIN")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
