// agents-watch checks candidate releases without prompts or account credentials.
// Local runs write report artifacts; --update-capabilities refreshes baselines. --publish explicitly enables GitHub writes.
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

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

type candidate struct {
	ID, Package, Binary, Version string
	Probe, Schema                string
	Problems                     []string
	Capabilities, Notes          string
	Snapshot                     []byte
	CapabilityChanged            bool
	PackageDir                   string
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

func main() {
	install := flag.Bool("install", false, "install latest candidates into a temporary npm prefix")
	publish := flag.Bool("publish", false, "open/update checked-version PR and attention issues with gh and GITHUB_TOKEN")
	updateCapabilities := flag.Bool("update-capabilities", false, "refresh committed capability snapshots from credential-free probes")
	outDir := flag.String("out", ".project-local/agents-watch", "report and candidate schema directory")
	probeID := flag.String("probe", "", "internal isolated handshake probe")
	binary := flag.String("binary", "", "internal probe executable")
	dir := flag.String("dir", "", "internal probe directory")
	flag.Parse()
	if *probeID != "" {
		for _, c := range candidates {
			if c.ID == *probeID {
				c.Binary = *binary
				result, err := probe(c, os.Environ(), *dir)
				failure := ""
				if err != nil {
					if authRequired(err) {
						result.Message = "skipped (account required)"
					} else {
						failure = err.Error()
					}
				}
				result.Error = failure
				_ = json.NewEncoder(os.Stdout).Encode(result)
				return
			}
		}
		os.Exit(2)
	}
	if err := watch(*install, *publish, *updateCapabilities, *outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func watch(install, publish, updateCapabilities bool, outDir string) error {
	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run agents-watch from the repository root")
	}
	if publish && updateCapabilities {
		return errors.New("use --update-capabilities locally; --publish writes snapshots in the review worktree")
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
	versionsData, err := os.ReadFile("internal/loom/harness/tested_versions.json")
	if err != nil {
		return err
	}
	var tested map[string][]string
	if err := json.Unmarshal(versionsData, &tested); err != nil {
		return err
	}
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
			c.PackageDir = filepath.Join(prefix, "lib/node_modules", c.Package)
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
		if c.PackageDir == "" {
			c.PackageDir = installedPackageDir(*c)
		}
		if !versionIncluded(tested[c.ID], c.Version) {
			previous := ""
			if v := tested[c.ID]; len(v) > 0 {
				previous = v[len(v)-1]
			}
			c.Notes = releaseNotes(*c, previous, &http.Client{Timeout: 10 * time.Second}, "https://api.github.com")
		}
		result, probeErr := isolatedProbe(*c, env, tmp)
		c.Probe, err = result.Message, probeErr
		if err != nil {
			if authRequired(err) {
				c.Probe = "skipped (account required)"
			} else {
				c.Problems = append(c.Problems, "probe failed: "+err.Error())
			}
		}
		if len(result.Capabilities) > 0 {
			c.Capabilities, c.Snapshot, err = checkCapabilities(c.ID, result.Capabilities, capabilitiesDir, outDir, updateCapabilities && probeErr == nil)
			if err != nil {
				c.Problems = append(c.Problems, "capability snapshot failed: "+err.Error())
			}
			c.CapabilityChanged = err == nil && c.Capabilities != "" && strings.HasPrefix(c.Capabilities, "- ")
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
		if c.Probe == "" {
			c.Probe = "unavailable (probe did not complete)"
		}
		if c.Capabilities == "" {
			c.Capabilities = "unavailable (probe did not complete)"
		}
		fmt.Fprintf(&report, "## %s %s\n\nPackage: `%s`. Probe: %s. Schema: %s.\n\n", c.ID, c.Version, c.Package, c.Probe, c.Schema)
		if c.CapabilityChanged {
			fmt.Fprintf(&report, "Capabilities changed (review required):\n\n%s\n\n", c.Capabilities)
		} else {
			fmt.Fprintf(&report, "Capabilities: %s.\n\n", c.Capabilities)
		}
		if c.CapabilityChanged {
			failed = true
		}
		if c.Notes != "" {
			report.WriteString(c.Notes + "\n\n")
		}
		for _, problem := range c.Problems {
			fmt.Fprintf(&report, "- %s\n", problem)
			failed = true
		}
		if len(c.Problems) == 0 && !c.CapabilityChanged {
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
		if err := publishReview(reportPath, registry, registryErr); err != nil {
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
func isolatedProbe(c candidate, env []string, dir string) (probeResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return probeResult{}, err
	}
	out, err := run(time.Minute, env, exe, "--probe", c.ID, "--binary", c.Binary, "--dir", dir)
	if err != nil {
		return probeResult{}, err
	}
	var result probeResult
	if err := decodeSchema(out, &result); err != nil {
		return result, fmt.Errorf("invalid probe response: %w", err)
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
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
