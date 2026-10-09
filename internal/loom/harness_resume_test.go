package loom

import "testing"

func TestNativeResumeCommand(t *testing.T) {
	cases := map[string]string{
		"claude-code": "claude --resume ae8d0813-fbad-4779-9d21-0a0fdc930472",
		"codex":       "codex resume ae8d0813-fbad-4779-9d21-0a0fdc930472",
		"hermes":      "hermes --resume ae8d0813-fbad-4779-9d21-0a0fdc930472",
		"antigravity": "agy --conversation ae8d0813-fbad-4779-9d21-0a0fdc930472",
	}
	for harness, want := range cases {
		if got := nativeResumeCommand(harness, "ae8d0813-fbad-4779-9d21-0a0fdc930472"); got != want {
			t.Errorf("%s: %q, want %q", harness, got, want)
		}
	}
	for _, bad := range []string{"", "x; rm -rf /", "$(id)", "a b", "-flag"} {
		if got := nativeResumeCommand("claude-code", bad); got != "" {
			t.Errorf("unsafe id %q accepted: %q", bad, got)
		}
	}
}
