// agents-watch checks candidate releases without prompts or account credentials.
// Local runs only write report artifacts. --publish explicitly enables GitHub writes.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/opencodehttp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

type candidate struct {
	ID, Package, Binary, Version string
	Probe, Schema                string
	Problems                     []string
}

var candidates = []candidate{
	{ID: "codex", Package: "@openai/codex", Binary: "codex"},
	{ID: "opencode", Package: "opencode-ai", Binary: "opencode"},
	{ID: "claude-acp", Package: "@agentclientprotocol/claude-agent-acp", Binary: "claude-agent-acp"},
	{ID: "pi", Package: "@earendil-works/pi-coding-agent", Binary: "pi"},
}

func run(timeout time.Duration, env []string, argv ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if env == nil {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
func authRequired(err error) bool {
	var rpc *acp.RPCError
	if errors.As(err, &rpc) {
		return strings.Contains(strings.ToLower(rpc.Message), "auth")
	}
	text := strings.ToLower(err.Error())
	for _, needle := range []string{"authentication required", "not authenticated", "not logged in", "unauthorized", "login required", "please log in", "sign in", "api key is required", "missing api key"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
func probe(c candidate, env []string, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if c.ID == "opencode" {
		server := &opencodehttp.Server{}
		defer server.Close()
		client, err := server.Client(ctx, []string{c.Binary}, env)
		if err != nil {
			return "", err
		}
		models, err := client.Models(ctx, dir)
		return fmt.Sprintf("health + model listing (%d models)", len(models)), err
	}
	args := []string{}
	switch c.ID {
	case "codex":
		args = []string{"app-server"}
	case "pi":
		args = []string{"--mode", "rpc", "--no-session"}
	}
	cmd := exec.Command(c.Binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	if c.ID == "claude-acp" {
		client, err := acp.NewClient(cmd)
		if err != nil {
			return "", err
		}
		defer client.Close()
		client.Handler = func(*acp.Frame) (any, error) {
			return nil, &acp.RPCError{Code: -32601, Message: "watch probes do not accept interactive requests"}
		}
		client.Notify = func(acp.Frame) {}
		if err = client.Start(); err != nil {
			return "", err
		}
		var init struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if err = client.Call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": acp.ClientCapabilities(false, false), "clientInfo": map[string]string{"name": "loom-agents-watch", "version": "1"}}, &init); err != nil {
			return "", err
		}
		if init.ProtocolVersion != 1 {
			return "", fmt.Errorf("unexpected ACP protocol %d", init.ProtocolVersion)
		}
		var session acp.SessionResponse
		if err = client.Call(ctx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}}, &session); err != nil {
			if authRequired(err) {
				return "initialize passed; model listing skipped (account required)", nil
			}
			return "", err
		}
		return fmt.Sprintf("initialize + session model listing (%d options)", len(session.Options())), nil
	}
	client, err := agentstdio.New(cmd)
	if err != nil {
		return "", err
	}
	defer client.Close()
	sink := func(agent.AgentEvent) bool { return true }
	broker := agent.NewRequestBroker(c.ID, sink)
	defer broker.Cancel()
	if c.ID == "codex" {
		session := codexapp.New(client, broker, sink)
		if err = client.Start(); err == nil {
			err = session.Initialize(ctx)
		}
		if err != nil {
			return "", err
		}
		models, err := session.Models(ctx)
		if err != nil && authRequired(err) {
			return "initialize passed; model listing skipped (account required)", nil
		}
		return fmt.Sprintf("initialize + model listing (%d models)", len(models)), err
	}
	session := pirpc.New(client, broker, sink)
	if err = client.Start(); err == nil {
		_, err = session.State(ctx)
	}
	if err != nil {
		return "", err
	}
	models, err := session.Models(ctx)
	return fmt.Sprintf("state + model listing (%d models)", len(models)), err
}

func main() {
	install := flag.Bool("install", false, "install latest candidates into a temporary npm prefix")
	publish := flag.Bool("publish", false, "open/update review PR or attention issues with gh and GITHUB_TOKEN")
	outDir := flag.String("out", ".project-local/agents-watch", "report and candidate schema directory")
	probeID := flag.String("probe", "", "internal isolated handshake probe")
	binary := flag.String("binary", "", "internal probe executable")
	dir := flag.String("dir", "", "internal probe directory")
	flag.Parse()
	if *probeID != "" {
		for _, c := range candidates {
			if c.ID == *probeID {
				c.Binary = *binary
				message, err := probe(c, os.Environ(), *dir)
				failure := ""
				if err != nil {
					if authRequired(err) {
						message = "skipped (account required)"
					} else {
						failure = err.Error()
					}
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"message": message, "error": failure})
				return
			}
		}
		os.Exit(2)
	}
	if err := watch(*install, *publish, *outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func watch(install, publish bool, outDir string) error {
	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run agents-watch from the repository root")
	}
	if publish {
		status, err := run(time.Second, nil, "git", "status", "--porcelain")
		if err != nil {
			return err
		}
		if len(status) != 0 {
			return errors.New("--publish requires a clean checkout of the revision being checked")
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "loom-agents-watch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// Use empty native profiles and a small env allowlist. Probes never see the
	// publishing token, provider credentials or a developer's existing accounts.
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + tmp, "XDG_CONFIG_HOME=" + tmp, "XDG_DATA_HOME=" + tmp, "XDG_CACHE_HOME=" + tmp, "CI=true", "NO_COLOR=1", "TERM=dumb"}
	for i := range candidates {
		c := &candidates[i]
		if install {
			prefix := filepath.Join(tmp, c.ID)
			out, err := run(5*time.Minute, env, "npm", "install", "--global", "--prefix", prefix, c.Package+"@latest")
			_ = os.WriteFile(filepath.Join(outDir, c.ID+"-install.txt"), out, 0644)
			if err != nil {
				c.Problems = append(c.Problems, "installation failed: "+err.Error())
				c.Version = "unknown"
				continue
			}
			c.Binary = filepath.Join(prefix, "bin", c.Binary)
		}
		out, err := versionOutput(c.Binary, env)
		if err != nil {
			c.Problems = append(c.Problems, "version command failed: "+err.Error())
			c.Version = "unknown"
			continue
		}
		c.Version = strings.TrimSpace(string(out))
		// npm's published adapter version is authoritative if the adapter's
		// --version prints banners or the SDK's version instead of its own.
		if c.ID == "claude-acp" && install {
			data, err := os.ReadFile(filepath.Join(tmp, c.ID, "lib/node_modules", c.Package, "package.json"))
			var pkg struct {
				Version string `json:"version"`
			}
			if err == nil {
				err = json.Unmarshal(data, &pkg)
			}
			if err != nil || pkg.Version == "" {
				c.Problems = append(c.Problems, "cannot read installed adapter version")
				continue
			}
			c.Version = pkg.Version
		}
		if c.Version == "" || len(c.Version) > 200 || strings.Contains(c.Version, "\n") {
			c.Problems = append(c.Problems, "unrecognized version output")
			continue
		}
		c.Probe, err = isolatedProbe(*c, env, tmp)
		if err != nil {
			if authRequired(err) {
				c.Probe = "skipped (account required)"
			} else {
				c.Problems = append(c.Problems, "probe failed: "+err.Error())
			}
		}
		c.Schema, err = checkSchema(*c, env, outDir)
		if err != nil {
			c.Problems = append(c.Problems, err.Error())
		}
	}
	fixtureOut, fixtureErr := run(5*time.Minute, nil, "go", "test", "./internal/loom/runtime/...", "-run", "Fixture")
	_ = os.WriteFile(filepath.Join(outDir, "fixtures.txt"), fixtureOut, 0644)
	// Application ACP fixtures include the bidirectional permission/form path.
	appOut, appErr := run(5*time.Minute, nil, "go", "test", "./internal/loom", "-run", "Fixture")
	_ = os.WriteFile(filepath.Join(outDir, "application-fixtures.txt"), appOut, 0644)
	if fixtureErr != nil || appErr != nil {
		for i := range candidates {
			candidates[i].Problems = append(candidates[i].Problems, "fixture tests failed; see report artifacts")
		}
	}
	registry, registryErr := fetchRegistry()
	if registryErr == nil {
		registryErr = os.WriteFile(filepath.Join(outDir, "acp_registry_snapshot.json"), registry, 0644)
	}
	var report strings.Builder
	fmt.Fprintf(&report, "# Agents compatibility watch — %s\n\nNo prompts, account sign-in or paid turns were run. Auth-only failures are explicit skips; they do not establish live turn compatibility.\n\n", time.Now().UTC().Format("2006-01-02"))
	if repository, runID := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID"); repository != "" && runID != "" {
		fmt.Fprintf(&report, "[Workflow run and report artifacts](https://github.com/%s/actions/runs/%s)\n\n", repository, runID)
	}
	failed := registryErr != nil
	for _, c := range candidates {
		fmt.Fprintf(&report, "## %s %s\n\nPackage: `%s`. Probe: %s. Schema: %s.\n\n", c.ID, c.Version, c.Package, c.Probe, c.Schema)
		for _, problem := range c.Problems {
			fmt.Fprintf(&report, "- %s\n", problem)
			failed = true
		}
		if len(c.Problems) == 0 {
			report.WriteString("Checks passed.\n")
		}
		report.WriteString("\n")
	}
	if registryErr != nil {
		fmt.Fprintf(&report, "Registry refresh failed: %s\n", registryErr)
	} else {
		report.WriteString("ACP registry snapshot refreshed and validated.\n")
	}
	reportPath := filepath.Join(outDir, "report.md")
	if err := os.WriteFile(reportPath, []byte(report.String()), 0644); err != nil {
		return err
	}
	fmt.Println("Report:", reportPath)
	if publish {
		if err := publishReview(outDir, reportPath, registry, failed); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("agents-watch found changes or failures requiring review")
	}
	return nil
}
func fetchRegistry() ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("registry too large")
	}
	if err := validateRegistry(data); err != nil {
		return nil, err
	}
	return data, nil
}

// OpenCode's supervisor inherits the current environment. A separate process
// gives every transport the same empty profiles and credential-free env.
func isolatedProbe(c candidate, env []string, dir string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	out, err := run(time.Minute, env, exe, "--probe", c.ID, "--binary", c.Binary, "--dir", dir)
	if err != nil {
		return "", err
	}
	var result struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("invalid probe response: %w", err)
	}
	if result.Error != "" {
		return result.Message, errors.New(result.Error)
	}
	return result.Message, nil
}
func generateOpenAPI(binary string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "generate")
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("OpenCode schema generation: %w (%s)", err, stderr.String())
	}
	return data, nil
}

func versionOutput(binary string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Env = env
	// Version stdout is data; Codex may emit a PATH-helper warning on stderr.
	return cmd.Output()
}
