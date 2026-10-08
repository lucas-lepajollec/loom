package loom

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/platform"
)

// MachineMetrics describes a read-only sample, independently of any engine.
// Memory and disk sizes are bytes; At is Unix milliseconds. Partial marks
// best-effort samples whose zero values can include unavailable observations.
type MachineMetrics struct {
	At            int64               `json:"at"`
	CPU           float64             `json:"cpu"`
	Load1         float64             `json:"load1"`
	Cores         int                 `json:"cores"`
	RAMUsed       uint64              `json:"ram_used"`
	RAMTotal      uint64              `json:"ram_total"`
	Disk          MachineDiskMetrics  `json:"disk"`
	GPUs          []MachineGPUMetrics `json:"gpus"`
	UptimeSeconds int64               `json:"uptime_seconds"`
	OS            string              `json:"os"`
	Partial       bool                `json:"partial,omitempty"`
}

type MachineDiskMetrics struct {
	Path  string `json:"path"`
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

type MachineGPUMetrics struct {
	Name      string  `json:"name"`
	Util      float64 `json:"util"`
	VRAMUsed  uint64  `json:"vram_used"`
	VRAMTotal uint64  `json:"vram_total"`
}

func procCPU(text string) (total, idle uint64, cores int, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.HasPrefix(f[0], "cpu") && f[0] != "cpu" {
			if _, err := strconv.ParseUint(strings.TrimPrefix(f[0], "cpu"), 10, 32); err == nil {
				cores++
			}
		}
		if f[0] != "cpu" || len(f) < 5 {
			continue
		}
		// guest and guest_nice are already included in user/nice, respectively.
		for i := 1; i < len(f) && i <= 8; i++ {
			n, err := strconv.ParseUint(f[i], 10, 64)
			if err != nil || n > math.MaxUint64-total {
				return 0, 0, cores, false
			}
			total += n
			if i == 4 || i == 5 {
				idle += n
			}
		}
		ok = true
	}
	return
}

// parseProcMachineMetrics takes text, so local reads and a single SSH command
// share the exact same CPU delta, memory, load and uptime interpretation.
func parseProcMachineMetrics(stat1, stat2, meminfo, loadavg, uptime string) MachineMetrics {
	m := MachineMetrics{At: time.Now().UnixMilli(), OS: "linux", GPUs: []MachineGPUMetrics{}}
	t1, i1, _, ok1 := procCPU(stat1)
	t2, i2, cores, ok2 := procCPU(stat2)
	m.Cores = cores
	if ok1 && ok2 && t2 > t1 && i2 >= i1 && i2-i1 <= t2-t1 {
		m.CPU = 100 * float64((t2-t1)-(i2-i1)) / float64(t2-t1)
	} else {
		m.Partial = true
	}
	total, available, haveAvailable := platform.ParseProcMeminfo(meminfo)
	m.RAMTotal = total
	if m.RAMTotal != 0 && haveAvailable && available <= m.RAMTotal {
		m.RAMUsed = m.RAMTotal - available
	} else {
		m.Partial = true
	}
	if n, ok := procFloat(loadavg); ok {
		m.Load1 = n
	} else {
		m.Partial = true
	}
	if n, ok := procFloat(uptime); ok && n < float64(math.MaxInt64) {
		m.UptimeSeconds = int64(n)
	} else {
		m.Partial = true
	}
	if cores == 0 {
		m.Partial = true
	}
	return m
}

func procFloat(text string) (float64, bool) {
	f := strings.Fields(text)
	if len(f) == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(f[0], 64)
	return n, err == nil && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0)
}

func machineGPUs(gpus []map[string]any) []MachineGPUMetrics {
	out := make([]MachineGPUMetrics, 0, len(gpus))
	for _, g := range gpus {
		name, _ := g["name"].(string)
		out = append(out, MachineGPUMetrics{Name: name, Util: math.Min(100, float64(amdPos(amdNum(g["util"])))), VRAMUsed: uint64(amdPos(amdNum(g["used"]))) << 20, VRAMTotal: uint64(amdPos(amdNum(g["total"]))) << 20})
	}
	return out
}

func collectLocalMachineMetrics(ctx context.Context) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	m := MachineMetrics{At: time.Now().UnixMilli(), OS: runtime.GOOS, Cores: runtime.NumCPU(), Partial: true}
	if runtime.GOOS == "linux" {
		read := func(name string) string { b, _ := os.ReadFile("/proc/" + name); return string(b) }
		first := read("stat")
		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		m = parseProcMachineMetrics(first, read("stat"), read("meminfo"), read("loadavg"), read("uptime"))
	} else {
		used, total := ramUsageMB()
		m.RAMUsed, m.RAMTotal = uint64(used)<<20, uint64(total)<<20
	}
	home, err := os.UserHomeDir()
	m.Disk.Path = home
	if err == nil {
		used, total, err := platform.DiskUsageAt(home)
		if err == nil {
			m.Disk.Used, m.Disk.Total = used, total
		} else {
			m.Partial = true
		}
	} else {
		m.Partial = true
	}
	m.GPUs = machineGPUs(liveGPUsWithRunner(gpuRunner(ctx)))
	if ctx.Err() != nil {
		m.Partial = true
	}
	return m, nil
}

type machineMetricsEntry struct {
	done  chan struct{}
	at    time.Time
	value any
	err   error
}

// In-flight samples are shared too. Waiting requests can cancel independently;
// errors are cached to keep an unreachable SSH machine from causing a storm.
type machineMetricsCache struct {
	mu      sync.Mutex
	entries map[string]*machineMetricsEntry
	now     func() time.Time
}

func (c *machineMetricsCache) get(ctx context.Context, key string, ttl time.Duration, collect func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	if c.entries == nil {
		c.entries = map[string]*machineMetricsEntry{}
	}
	e := c.entries[key]
	if e == nil || (!e.at.IsZero() && now.Sub(e.at) >= ttl) {
		// Discard expired entries, including removed machines, on every refresh.
		for id, old := range c.entries {
			if !old.at.IsZero() && now.Sub(old.at) >= 10*time.Second {
				delete(c.entries, id)
			}
		}
		e = &machineMetricsEntry{done: make(chan struct{})}
		c.entries[key] = e
		go func() {
			// A disconnected requester must not abort a sample shared by others.
			sampleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			value, err := collect(sampleCtx)
			c.mu.Lock()
			e.value, e.err, e.at = value, err, time.Now()
			if c.now != nil {
				e.at = c.now()
			}
			close(e.done)
			c.mu.Unlock()
		}()
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.done:
		return e.value, e.err
	}
}

var localMachineMetricsCache = &machineMetricsCache{}
var sshMachineMetricsCache = &machineMetricsCache{}

func localMachineMetrics(ctx context.Context) (any, error) {
	return localMachineMetricsCache.get(ctx, "local", 2*time.Second, collectLocalMachineMetrics)
}

const nodeObserveDisabledKey = "node_observe_disabled"
const nodeObserveDisabled = "node observe module is disabled"

func nodeObserveEnabled() bool { return !getBool(bkState, nodeObserveDisabledKey) }

func handleNodeObserve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if !nodeObserveEnabled() {
		sendJSON(w, 409, map[string]any{"error": nodeObserveDisabled})
		return
	}
	sendMachineMetrics(w, r, localMachineMetrics)
}

func handleLocalMachineMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendMachineMetrics(w, r, localMachineMetrics)
}

func sendMachineMetrics(w http.ResponseWriter, r *http.Request, fetch func(context.Context) (any, error)) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, err := fetch(ctx)
	if err != nil {
		sendJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	sendJSON(w, 200, value)
}

func remoteMachineMetrics(ctx context.Context, m RemoteMachine) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if hasNodeModule(m.Modules, "observe") {
		return nodeMachineMetrics(ctx, m)
	}
	if m.NodeID != "" && m.User == "" {
		return nil, errors.New("node observe module is unavailable; enable it and refresh the machine link")
	}
	// Include transport properties in the key: editing an SSH link invalidates it.
	key := strings.Join([]string{m.ID, m.Host, m.User, strconv.Itoa(m.Port), m.OS}, "\x00")
	return sshMachineMetricsCache.get(ctx, key, 10*time.Second, func(ctx context.Context) (any, error) { return sshMachineMetrics(ctx, m) })
}

func handleMachineMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	sendMachineMetrics(w, r, func(ctx context.Context) (any, error) { return remoteMachineMetrics(ctx, m) })
}

func handleMachinesMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	type result struct {
		id    string
		value any
		err   error
	}
	machines := loadRemoteMachines()
	results := make(chan result, len(machines)+1)
	go func() { v, err := localMachineMetrics(ctx); results <- result{"local", v, err} }()
	pending := map[string]bool{"local": true}
	for _, m := range machines {
		pending[m.ID] = true
		go func() { v, err := remoteMachineMetrics(ctx, m); results <- result{m.ID, v, err} }()
	}
	metrics := map[string]any{}
	for len(pending) > 0 {
		select {
		case got := <-results:
			if got.err != nil {
				got.value = map[string]any{"error": got.err.Error()}
			}
			metrics[got.id] = got.value
			delete(pending, got.id)
		case <-ctx.Done():
			for id := range pending {
				metrics[id] = map[string]any{"error": "machine metrics timed out"}
			}
			sendJSON(w, 200, map[string]any{"metrics": metrics})
			return
		}
	}
	sendJSON(w, 200, map[string]any{"metrics": metrics})
}
