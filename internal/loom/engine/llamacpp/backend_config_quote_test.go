package llamacpp

import (
	"reflect"
	"testing"
)

// Un guillemet INTERNE (argument cité dans EXTRA_ARGS) ne doit pas être mangé :
// l'ancien Trim retirait le guillemet final et renvoyait une valeur déséquilibrée
// dans le preset (issue #17).
func TestParseEnvKeepsInnerQuotes(t *testing.T) {
	cases := map[string]string{
		`EXTRA_ARGS=--jinja --chat-template-file "/etc/loom/tpl.jinja"`: `--jinja --chat-template-file "/etc/loom/tpl.jinja"`,
		`EXTRA_ARGS="--jinja --flash-attn"`:                             `--jinja --flash-attn`,
		`MODEL='/mnt/d/x.gguf'`:                                         `/mnt/d/x.gguf`,
		`MODEL=/mnt/d/x.gguf`:                                           `/mnt/d/x.gguf`,
	}
	for line, want := range cases {
		m := ParseEnv(line)
		var got string
		for _, v := range m {
			got = v
		}
		if got != want {
			t.Errorf("ParseEnv(%q) = %q, veut %q", line, got, want)
		}
	}
}

// Écrire puis relire doit rendre exactement la même configuration.
func TestFormatEnvRoundTrip(t *testing.T) {
	in := map[string]string{
		"MODEL":      "/mnt/mes modèles/x.gguf",
		"EXTRA_ARGS": `--jinja --chat-template-file "/etc/loom/tpl.jinja"`,
		"CTX":        "32768",
		"SYSPROMPT":  "ligne 1\nligne 2",
	}
	out := ParseEnv(FormatEnv(in))
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("aller-retour cassé :\n%v\n%v", in, out)
	}
}

func TestSplitArgs(t *testing.T) {
	got := SplitArgs(`--jinja  --chat-template-file "/etc/mes modèles/tpl.jinja" --flash-attn`)
	want := []string{"--jinja", "--chat-template-file", "/etc/mes modèles/tpl.jinja", "--flash-attn"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitArgs = %q, veut %q", got, want)
	}
	if len(SplitArgs("")) != 0 {
		t.Fatal("EXTRA_ARGS vide doit donner zéro argument")
	}
}
