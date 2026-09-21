package loom

import "testing"

func TestEffectiveSysPromptFallback(t *testing.T) {
	testHome(t)
	if got := effectiveSysPrompt(); got != "" {
		t.Fatalf("vide = %q", got)
	}
	if err := saveSysPrompt("global"); err != nil {
		t.Fatal(err)
	}
	if got := effectiveSysPrompt(); got != "global" {
		t.Fatalf("global = %q", got)
	}
	if err := SetConfigKey("SYSPROMPT", "modèle"); err != nil {
		t.Fatal(err)
	}
	if got := effectiveSysPrompt(); got != "modèle" {
		t.Fatalf("modèle = %q", got)
	}
	if err := SetConfigKey("SYSPROMPT", "  "); err != nil {
		t.Fatal(err)
	}
	if got := effectiveSysPrompt(); got != "global" {
		t.Fatalf("après vidage = %q", got)
	}
	if err := saveSysPrompt(""); err != nil {
		t.Fatal(err)
	}
	if got := effectiveSysPrompt(); got != "" {
		t.Fatalf("les deux vides = %q", got)
	}
}

func TestEffectiveSysPromptFromEnvNewlines(t *testing.T) {
	testHome(t)
	if err := WriteConfig(parseEnv("SYSPROMPT=\"ligne 1\\nligne 2\"\n")); err != nil {
		t.Fatal(err)
	}
	if got := effectiveSysPrompt(); got != "ligne 1\nligne 2" {
		t.Fatalf("unescape = %q", got)
	}
}
