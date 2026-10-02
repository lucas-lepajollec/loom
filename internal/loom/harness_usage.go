package loom

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nativeUsageTTL = 5 * time.Minute
const nativeUsageTimeout = 30 * time.Second
const nativeUsageMaxFiles = 2000

type HarnessModelUsage struct {
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Sessions *int   `json:"sessions,omitempty"`
}
type HarnessUsage struct {
	RuntimeID       string              `json:"runtime_id"`
	Days            int                 `json:"days"`
	Sessions        int                 `json:"sessions"`
	InputTokens     int64               `json:"input_tokens"`
	OutputTokens    int64               `json:"output_tokens"`
	CacheReadTokens int64               `json:"cache_read_tokens"`
	TotalTokens     int64               `json:"total_tokens"`
	CostUSD         *float64            `json:"cost_usd"`
	ByModel         []HarnessModelUsage `json:"by_model"`
	Source          string              `json:"source"`
	FetchedAt       int64               `json:"fetched_at"`
	Error           string              `json:"error"`
}

type nativeUsageFlight struct {
	done   chan struct{}
	result HarnessUsage
}

var nativeUsageCache = struct {
	sync.Mutex
	items   map[string]HarnessUsage
	flights map[string]*nativeUsageFlight
}{items: map[string]HarnessUsage{}, flights: map[string]*nativeUsageFlight{}}

func usageDays(days int) (int, error) {
	if days == 0 {
		return 7, nil
	}
	if days != 7 && days != 30 {
		return 0, errors.New("days must be 7 or 30")
	}
	return days, nil
}
func cachedHarnessUsage(ctx context.Context, id string, days int, refresh bool) HarnessUsage {
	key := fmt.Sprintf("%s:%d", id, days)
	nativeUsageCache.Lock()
	if q, ok := nativeUsageCache.items[key]; ok && !refresh && time.Since(time.Unix(q.FetchedAt, 0)) < nativeUsageTTL {
		nativeUsageCache.Unlock()
		return q
	}
	if flight := nativeUsageCache.flights[key]; flight != nil {
		nativeUsageCache.Unlock()
		select {
		case <-flight.done:
			return flight.result
		case <-ctx.Done():
			return HarnessUsage{RuntimeID: id, Days: days, ByModel: []HarnessModelUsage{}, Error: "reading interrupted", FetchedAt: time.Now().Unix()}
		}
	}
	flight := &nativeUsageFlight{done: make(chan struct{})}
	nativeUsageCache.flights[key] = flight
	nativeUsageCache.Unlock()
	q := readNativeHarnessUsage(ctx, id, days)
	nativeUsageCache.Lock()
	nativeUsageCache.items[key] = q
	flight.result = q
	delete(nativeUsageCache.flights, key)
	close(flight.done)
	nativeUsageCache.Unlock()
	return q
}

func readNativeHarnessUsage(ctx context.Context, id string, days int) HarnessUsage {
	q := HarnessUsage{RuntimeID: id, Days: days, ByModel: []HarnessModelUsage{}, Source: "Native harness usage", FetchedAt: time.Now().Unix()}
	ctx, cancel := context.WithTimeout(ctx, nativeUsageTimeout)
	defer cancel()
	a := acpAgent{ID: id}
	if adapter, ok := registeredRuntimes.lookup(id); ok {
		if acp, ok := adapter.(*acpAdapter); ok {
			a = acp.agent
		}
	} else {
		q.Error = "unavailable"
		return q
	}
	harness := usageHarnessID(a)
	var err error
	switch harness {
	case "claude-code", "codex", "pi":
		if a.Remote {
			q.Error = "unavailable"
			return q
		}
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			q.Error = "unavailable"
			return q
		}
		roots := map[string]string{"claude-code": filepath.Join(home, ".claude", "projects"), "codex": filepath.Join(home, ".codex", "sessions"), "pi": filepath.Join(home, ".pi", "agent", "sessions")}
		q.Source = map[string]string{"claude-code": "Claude Code · local native logs", "codex": "Codex · local native logs", "pi": "Pi · local native logs"}[harness]
		err = readHarnessSessionFiles(ctx, roots[harness], harness, time.Now(), days, &q)
	case "opencode", "hermes":
		argv := []string{"opencode", "stats", "--days", strconv.Itoa(days), "--models"}
		q.Source = "opencode stats · every native session"
		if harness == "hermes" {
			argv = []string{"hermes", "insights", "--days", strconv.Itoa(days)}
			q.Source = "hermes insights · every native session"
		}
		var out []byte
		out, err = harnessUsageCommand(ctx, a, argv)
		if err == nil {
			err = parseHarnessStats(string(out), harness, &q)
		}
	default:
		q.Error = "unavailable"
	}
	if a.Remote {
		q.Source += " · SSH machine"
	}
	if err != nil {
		q.Error = err.Error()
	}
	if ctx.Err() != nil {
		q.Error = "reading interrupted (30-second limit)"
	}
	q.FetchedAt = time.Now().Unix()
	return q
}

// Output is bounded independently from process lifetime. Stderr is discarded:
// native diagnostics can contain private paths or account credentials.
type usageOutput struct{ bytes.Buffer }

func (b *usageOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		return 0, errors.New("native output too large")
	}
	return b.Buffer.Write(p)
}
func harnessUsageCommand(ctx context.Context, a acpAgent, argv []string) ([]byte, error) {
	var cmd *exec.Cmd
	if a.Remote {
		var machine *RemoteMachine
		for _, m := range loadRemoteMachines() {
			if m.ID == a.Machine {
				copy := m
				machine = &copy
				break
			}
		}
		if machine == nil {
			return nil, errors.New("machine unavailable")
		}
		key, _, err := loomSSHKey()
		if err != nil {
			return nil, errors.New("SSH key unavailable")
		}
		if lifecycleOS(machine) == "windows" {
			remote := windowsRemoteLifecycleCommand(*machine, key, argv)
			cmd = exec.CommandContext(ctx, remote[0], remote[1:]...)
		} else {
			parts := make([]string, len(argv))
			for i, arg := range argv {
				parts[i] = shellQuote(arg)
			}
			script := remotePathPreamble
			// Include directories learned during linking, without executing stored ACP argv.
			for _, tool := range machine.Tools {
				if tool.Path != "" {
					script += "PATH=" + shellQuote(filepath.ToSlash(filepath.Dir(tool.Path))) + ":\"$PATH\"\n"
				}
			}
			script += "export PATH\nexec " + strings.Join(parts, " ") + "\n"
			cmd = exec.CommandContext(ctx, "ssh", sshArgs(*machine, key, "sh", "-s")...)
			cmd.Stdin = strings.NewReader(script)
		}
	} else {
		native, err := harnessNativeArgv(argv)
		if err != nil {
			return nil, errors.New("not installed on this machine")
		}
		cmd = exec.CommandContext(ctx, native[0], native[1:]...)
	}
	dir, err := os.MkdirTemp("", "loom-native-usage-")
	if err != nil {
		return nil, errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(dir)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	var out usageOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		return nil, errors.New("native reading unavailable")
	}
	return out.Bytes(), nil
}

func handleNativeUsage(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	days := 0
	if raw := r.URL.Query().Get("days"); raw != "" {
		var err error
		days, err = strconv.Atoi(raw)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "days must be 7 or 30"})
			return
		}
	}
	days, err := usageDays(days)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rows := []HarnessUsage{}
	for _, d := range runtimeCatalog() {
		if d.Kind == "harness" {
			rows = append(rows, HarnessUsage{RuntimeID: d.ID})
		}
	}
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows[i] = cachedHarnessUsage(r.Context(), rows[i].RuntimeID, days, false)
		}(i)
	}
	wg.Wait()
	if !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "harnesses": rows})
}
func handleNativeUsageRefresh(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		RuntimeID string `json:"runtime_id"`
		Days      int    `json:"days"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	days, err := usageDays(req.Days)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adapter, ok := registeredRuntimes.lookup(req.RuntimeID)
	if !ok || adapter.Descriptor().Kind != "harness" {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness not found"})
		return
	}
	q := cachedHarnessUsage(r.Context(), req.RuntimeID, days, true)
	if !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "harness": q})
}
