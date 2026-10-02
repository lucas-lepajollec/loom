package loom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// sys_service.go holds the platform-neutral pieces of service management. The
// actual start/stop/restart/status/logs implementation is platform-specific:
//   - sys_service_linux.go   → systemd (systemctl/journalctl)
//   - sys_service_darwin.go  → launchd (launchctl)
//   - sys_service_windows.go → PID-file background process supervisor
//
// editConfig and showVram live here because they work the same everywhere.

// preflightEngine vérifie ce sans quoi le moteur ne PEUT pas démarrer, avant de
// lancer le service. Sinon llama-server sortait en erreur, systemd le relançait
// toutes les 3 s, et `loom start` affichait un « activating » rassurant pendant
// que `loom test` répondait « /health ne répond pas ». Le diagnostic n'était
// visible que dans le journal.
func preflightEngine() error {
	cfg := ReadConfig()
	bin := strings.TrimSpace(cfg["BIN"])
	if bin == "" {
		return fmt.Errorf("BIN not set — point to an already compiled llama-server using %s (BIN key), or as a last resort %s",
			bold("loom edit"), bold("loom llamacpp install"))
	}
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(LoomHome(), bin)
	}
	if _, err := os.Stat(prebuiltResolveBin(bin)); err != nil {
		return fmt.Errorf("engine not found: %s — fix BIN using %s", bin, bold("loom edit"))
	}
	model := strings.TrimSpace(cfg["MODEL"])
	if model == "" {
		// Standby mode : le moteur peut tourner en veille sans modèle chargé (0 VRAM).
		return nil
	}
	p, err := resolveServeModelPath(model)
	if err != nil {
		return fmt.Errorf("MODEL=%s : %w", model, err)
	}
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("model not found: %s — fix MODEL using %s", p, bold("loom edit"))
	}
	// Modèle en plusieurs fichiers : une tranche manquante ne se voit qu'au moment
	// où llama-server réclame un tenseur absent, dans le journal du service.
	if missing := shardFamilyMissing(filepath.Dir(p), filepath.Base(p)); len(missing) > 0 {
		return fmt.Errorf("incomplete model: missing %s in %s — this model has %d files; download them all",
			strings.Join(missing, ", "), filepath.Dir(p), len(shardFamily(filepath.Base(p))))
	}
	return nil
}

// configTemplate est le squelette commenté proposé par `loom edit` : la
// configuration vit en base, donc sur une installation neuve le fichier
// temporaire était QUASI VIDE — impossible de deviner quoi écrire. On y déroule
// donc les clés utiles avec leur rôle, les valeurs déjà définies telles quelles,
// les autres commentées.
var configTemplate = []struct{ key, help string }{
	{"BIN", "path to an already compiled llama-server (e.g. …/llama.cpp/build/bin/llama-server)"},
	{"MODEL", ".gguf file name or full path"},
	{"HOST", "engine listen address (default 127.0.0.1)"},
	{"PORT", "engine port (default 8081)"},
	{"CTX", "context size (empty = native GGUF context for a bare model)"},
	{"NGL", "layers offloaded to the GPU (default 999 = all)"},
	{"BATCH", "batch (default 2048)"},
	{"UBATCH", "micro-batch (default 512)"},
	{"THREADS", "threads CPU, 0 = auto"},
	{"THREADS_BATCH", "prefill CPU threads, 0 = auto"},
	{"KV_TYPE", "KV cache quantization (q8_0, q4_0…); KV_TYPE_K / KV_TYPE_V to set them separately"},
	{"REASONING", "reasoning mode passthrough (on/auto/deepseek)"},
	{"REASONING_BUDGET", "reasoning token limit; -1 = unlimited"},
	{"COMPACT", "automatic context compaction (off to disable)"},
	{"MEM_MODE", "AI memory: off / ondemand"},
	{"EXTRA_ARGS", "added as is to the llama-server command line"},
}

// configEditorText rend la configuration au format présenté dans $EDITOR.
func configEditorText(cfg map[string]string) string {
	var b strings.Builder
	b.WriteString("# Loom engine configuration (loom-engine).\n")
	b.WriteString("# One key per line: KEY=value. Commented lines (#) are ignored:\n")
	b.WriteString("# uncomment the ones you need. “loom restart” applies them.\n\n")
	seen := map[string]bool{}
	for _, f := range configTemplate {
		seen[f.key] = true
		fmt.Fprintf(&b, "# %s\n", f.help)
		if v, ok := cfg[f.key]; ok && v != "" {
			fmt.Fprintf(&b, "%s=%s\n\n", f.key, quoteValue(v))
		} else {
			fmt.Fprintf(&b, "#%s=\n\n", f.key)
		}
	}
	// Tout ce que le squelette ne connaît pas (clés d'une version plus récente,
	// réglages posés par l'UI) : conservé tel quel, en fin de fichier.
	rest := map[string]string{}
	for k, v := range cfg {
		if !seen[k] {
			rest[k] = v
		}
	}
	if len(rest) > 0 {
		b.WriteString("# --- other keys already defined ---\n")
		b.WriteString(formatEnv(rest))
	}
	return b.String()
}

// editConfig ouvre la configuration dans $EDITOR. La configuration vit en base
// (voir store.go) : on la déroule dans un fichier temporaire au format clé=valeur,
// on laisse l'éditeur faire son travail, puis on relit. Le contenu n'est réécrit
// que si l'éditeur sort proprement — un éditeur avorté ne doit rien effacer.
func editConfig() error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = defaultEditor()
	}
	tmp, err := os.CreateTemp("", "loom-config-*.env")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.WriteString(configEditorText(ReadConfig())); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command(editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := WriteConfig(parseEnv(string(b))); err != nil {
		return err
	}
	fmt.Println(dim("[info] loom restart to apply"))
	return nil
}

// showVram parses `nvidia-smi --query-gpu=...` and renders a colored bar.
// nvidia-smi is available on both Linux and Windows when an NVIDIA driver is
// installed, so this is platform-neutral.
func showVram() error {
	out, err := hideCmd(exec.Command("nvidia-smi",
		"--query-gpu=name,memory.used,memory.total,utilization.gpu,temperature.gpu",
		"--format=csv,noheader,nounits")).Output()
	if err != nil {
		return fmt.Errorf("nvidia-smi unavailable: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) != 5 {
			continue
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		name := parts[0]
		used, _ := strconv.Atoi(parts[1])
		total, _ := strconv.Atoi(parts[2])
		util, _ := strconv.Atoi(parts[3])
		temp, _ := strconv.Atoi(parts[4])
		pct := 0
		if total > 0 {
			pct = used * 100 / total
		}
		full := pct / 5
		bar := strings.Repeat("█", full) + strings.Repeat("░", 20-full)
		fmt.Printf("\n  %s\n", cyan(name))
		fmt.Printf("  VRAM  %s  %3d%%   %.1f / %.1f GiB\n", green(bar), pct, float64(used)/1024, float64(total)/1024)
		fmt.Printf("  GPU   %3d%%      Temp  %d°C\n\n", util, temp)
	}
	return nil
}
