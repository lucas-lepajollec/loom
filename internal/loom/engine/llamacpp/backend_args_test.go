package llamacpp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func argumentInputs() ArgumentInputs {
	return ArgumentInputs{
		BinaryPath: "llama-server", ModelPath: "model.gguf", BackendPort: 18081,
		RuntimeGet: func(string) string { return "" },
		ContextArg: func(cfg map[string]string, _ string) string {
			if v := cfg["CTX"]; v != "" {
				return v
			}
			if cfg["FIT"] == "on" {
				return ""
			}
			return "131072"
		},
		ResolveModelPath:  func(path string) (string, error) { return path, nil },
		HasFlag:           func(string) bool { return true },
		BooleanFlag:       func(string) bool { return true },
		APIKey:            func() (string, error) { return "", nil },
		AppendRuntimeArgs: func(args []string) []string { return args },
		Warnings:          &bytes.Buffer{},
	}
}

func TestServerArgsPreservePrecedenceAndConfig(t *testing.T) {
	in := argumentInputs()
	cfg := map[string]string{
		"FIT": "on", "NGL": "20", "CTX": "4096", "KV_TYPE": "q8_0",
		"KV_TYPE_V": "f16", "NP": "4", "API_KEY": "config-fixture",
		"REASONING": "auto", "REASONING_EFFORT": "high",
		"EXTRA_ARGS": `--alias "model with spaces" -np 6 --host public --port 80`,
	}
	before := cloneConfig(cfg)
	in.RuntimeGet = func(key string) string {
		if key == "NP" {
			return "8"
		}
		if key == "REASONING_BUDGET" {
			return "123"
		}
		return ""
	}
	in.APIKey = func() (string, error) { return "machine-fixture", nil }
	in.AppendRuntimeArgs = func(args []string) []string { return append(args, "-np", "10") }
	got, err := BuildServerArgs(cfg, in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"llama-server", "-m", "model.gguf", "--fit", "on", "-ngl", "20", "-c", "4096",
		"-t", "0", "-tb", "0", "-b", "2048", "-ub", "512", "-np", "8",
		"-ctk", "q8_0", "-ctv", "f16", "--reasoning", "auto", "--reasoning-budget", "123",
		"--reasoning-effort", "high", "--api-key", "machine-fixture", "--slots",
		"--alias", "model with spaces", "-np", "6", "--host", "public", "--port", "80",
		"-np", "10", "--host", "127.0.0.1", "--port", "18081",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("launch construction changed config")
	}
}

func TestServerArgsFitAndInstalledHelp(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cfg       map[string]string
		supported bool
		ctx, ngl  bool
	}{
		{"automatic", map[string]string{"FIT": "on"}, true, false, false},
		{"extra off", map[string]string{"FIT": "on", "EXTRA_ARGS": "--fit off"}, true, true, true},
		{"extra on", map[string]string{"FIT": "off", "EXTRA_ARGS": "--fit=on"}, true, false, false},
		{"unsupported", map[string]string{"FIT": "on"}, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := argumentInputs()
			in.HasFlag = func(string) bool { return tc.supported }
			argv, err := BuildServerArgs(tc.cfg, in)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(argv, " ")
			if strings.Contains(got, "-c 131072") != tc.ctx || strings.Contains(got, "-ngl 999") != tc.ngl {
				t.Fatalf("unexpected memory defaults: %s", got)
			}
			if !tc.supported && strings.Contains(got, "--fit ") {
				t.Fatal("unsupported fit emitted")
			}
		})
	}
	in := argumentInputs()
	in.HasFlag = func(string) bool { return false }
	in.BooleanFlag = func(string) bool { return false }
	argv, err := BuildServerArgs(map[string]string{"REASONING": "off", "REASONING_EFFORT": "high"}, in)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	if strings.Contains(got, "--reasoning") || strings.Contains(got, "--slots") || in.Warnings.(*bytes.Buffer).Len() == 0 {
		t.Fatalf("unsupported flags/warning: %s", got)
	}
}

func TestServerArgsAuxiliaryModelsAndCredentialErrors(t *testing.T) {
	in := argumentInputs()
	path := filepath.Join(t.TempDir(), "aux model.gguf")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	in.ResolveModelPath = func(string) (string, error) { return path, nil }
	cfg := map[string]string{"MMPROJ": "vision.gguf", "MODEL_DRAFT": "draft.gguf", "API_KEY": "fallback-fixture"}
	argv, err := BuildServerArgs(cfg, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--mmproj", "--model-draft", "--api-key"} {
		expected := path
		if flag == "--api-key" {
			expected = "fallback-fixture"
		}
		found := false
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] == flag && argv[i+1] == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %q", flag, argv)
		}
	}
	keyErr := errors.New("credential preparation failed")
	in.APIKey = func() (string, error) { return "", keyErr }
	if _, err := BuildServerArgs(cfg, in); !errors.Is(err, keyErr) {
		t.Fatalf("credential error = %v", err)
	}
	in.ResolveModelPath = func(string) (string, error) { return "", os.ErrNotExist }
	if _, err := BuildServerArgs(cfg, in); err == nil || !strings.Contains(err.Error(), "projecteur vision introuvable") {
		t.Fatalf("vision error = %v", err)
	}
}
