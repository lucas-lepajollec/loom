package llamacpp

import (
	"strings"
	"testing"
)

func TestArgsToPresetOptionsTranslatesAndDropsRouterOwnedFlags(t *testing.T) {
	flags := ParseLlamaHelp(fakeRouterHelp)
	args := []string{"-m", "/models/q.gguf", "-ngl", "999", "-c", "32768", "-np", "4",
		"-fa", "on", "--no-mmap", "--reasoning-budget", "-1", "--api-key", "secret",
		"--slots", "--host", "127.0.0.1", "--port", "18081"}
	got, err := ArgsToPresetOptions(flags, args)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"m", "/models/q.gguf"}, {"ngl", "999"}, {"c", "32768"}, {"np", "4"},
		{"fa", "on"}, {"no-mmap", "true"}, {"reasoning-budget", "-1"}, {"slots", "true"}}
	if len(got) != len(want) {
		t.Fatalf("options = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("option %d = %v, want %v", i, got[i], want[i])
		}
	}
	if RouterEntryName(got) != RouterEntryName(want) || !strings.HasPrefix(RouterEntryName(got), "loom-") {
		t.Fatal("le nom de section doit être déterministe")
	}
}

func TestArgsToPresetOptionsRejectsMultilineValues(t *testing.T) {
	flags := ParseLlamaHelp(fakeRouterHelp)
	if _, err := ArgsToPresetOptions(flags, []string{"-m", "a\nb"}); err == nil {
		t.Fatal("une valeur multiligne casserait le fichier INI")
	}
}
