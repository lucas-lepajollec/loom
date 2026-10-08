package loom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	benchTestPerf   = "perf"
	benchTestsKey   = "bench_tests"
	benchRunsKey    = "bench_runs"
	benchJobKey     = "bench_job"
	benchMaxRuns    = 15
	benchHealthWait = 8 * time.Minute
)

// benchTest is a saved prompt (or the built-in raw-perf test).
type benchTest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Kind      string `json:"kind"` // perf | prompt
	Builtin   bool   `json:"builtin,omitempty"`
}

type benchJobRow struct {
	ChoiceID    string       `json:"choice_id,omitempty"`
	Kind        string       `json:"kind"`
	Provider    string       `json:"provider,omitempty"`
	Model       string       `json:"model"`
	Preset      string       `json:"preset,omitempty"`
	Name        string       `json:"name"`
	Status      string       `json:"status"` // pending|loading|running|ok|err|skip
	Error       string       `json:"error,omitempty"`
	Result      *benchResult `json:"result,omitempty"`
	Preview     string       `json:"preview,omitempty"`
	Output      string       `json:"output,omitempty"`
	RuntimeID   string       `json:"runtime_id,omitempty"`
	Measurement string       `json:"measurement,omitempty"`
}

type benchJob struct {
	engine   *engineNode   // Captured target; private credentials never serialized.
	ID       string        `json:"id"`
	TestID   string        `json:"test_id"`
	TestName string        `json:"test_name"`
	Kind     string        `json:"kind"`
	Status   string        `json:"status"` // running|done|cancel
	Started  int64         `json:"started"`
	Finished int64         `json:"finished,omitempty"`
	Index    int           `json:"index"`
	Rows     []benchJobRow `json:"rows"`
}

type benchPick struct {
	ChoiceID string `json:"choice_id,omitempty"`
	Model    string `json:"model"`
	Name     string `json:"name"`
	Preset   string `json:"preset,omitempty"`
}

var (
	benchJobMu  sync.Mutex
	benchBusy   atomic.Bool
	benchStop   atomic.Bool
	benchCancel context.CancelFunc // guarded by benchJobMu
)

func builtinBenchTests() []benchTest {
	return []benchTest{{
		ID: benchTestPerf, Name: "Raw performance", Kind: "perf", Builtin: true,
		MaxTokens: 300,
	}}
}

func loadCustomBenchTests() []benchTest {
	var list []benchTest
	getJSON(bkState, benchTestsKey, &list)
	if list == nil {
		return []benchTest{}
	}
	return list
}

func listBenchTests() []benchTest {
	out := append([]benchTest{}, builtinBenchTests()...)
	return append(out, loadCustomBenchTests()...)
}

func findBenchTest(id string) (benchTest, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = benchTestPerf
	}
	for _, t := range listBenchTests() {
		if t.ID == id {
			return t, true
		}
	}
	return benchTest{}, false
}

func saveCustomBenchTest(name, prompt string, maxTok int) (benchTest, error) {
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if len(name) > 100 || len(prompt) > 32<<10 {
		return benchTest{}, fmt.Errorf("test name or prompt too large")
	}
	if name == "" {
		return benchTest{}, fmt.Errorf("name required")
	}
	if prompt == "" {
		return benchTest{}, fmt.Errorf("prompt required")
	}
	if maxTok <= 0 {
		maxTok = 256
	}
	if maxTok > 4096 {
		maxTok = 4096
	}
	t := benchTest{
		ID:        fmt.Sprintf("t%d", time.Now().UnixNano()),
		Name:      name,
		Prompt:    prompt,
		MaxTokens: maxTok,
		Kind:      "prompt",
	}
	list := loadCustomBenchTests()
	list = append(list, t)
	if err := putJSON(bkState, benchTestsKey, list); err != nil {
		return benchTest{}, err
	}
	return t, nil
}

func deleteCustomBenchTest(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || id == benchTestPerf {
		return fmt.Errorf("this test cannot be deleted")
	}
	list := loadCustomBenchTests()
	out := list[:0]
	found := false
	for _, t := range list {
		if t.ID == id {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return fmt.Errorf("test not found")
	}
	return putJSON(bkState, benchTestsKey, out)
}

func recoverBenchJob() *benchJob {
	benchJobMu.Lock()
	defer benchJobMu.Unlock()
	j := loadBenchJob()
	if j != nil && !benchBusy.Load() && (j.Status == "running" || j.Status == "cancel" && j.Finished == 0) {
		j.Status = "cancel"
		j.Finished = time.Now().Unix()
		for i := range j.Rows {
			if j.Rows[i].Status == "pending" || j.Rows[i].Status == "running" || j.Rows[i].Status == "loading" {
				j.Rows[i].Status = "skip"
				j.Rows[i].Error = "benchmark interrupted; a legacy remote node may need a manual stop"
			}
		}
		saveBenchJob(j)
		archiveBenchJob(*j)
	}
	return j
}

func loadBenchJob() *benchJob {
	var j benchJob
	if !getJSON(bkState, benchJobKey, &j) || j.ID == "" {
		return nil
	}
	return &j
}

func saveBenchJob(j *benchJob) {
	if j == nil {
		return
	}
	_ = putJSON(bkState, benchJobKey, j)
}

func loadBenchRuns() []benchJob {
	var list []benchJob
	getJSON(bkState, benchRunsKey, &list)
	if list == nil {
		return []benchJob{}
	}
	return list
}

func archiveBenchJob(j benchJob) {
	list := loadBenchRuns()
	out := []benchJob{j}
	for _, old := range list {
		if old.ID == j.ID {
			continue
		}
		out = append(out, old)
		if len(out) >= benchMaxRuns {
			break
		}
	}
	_ = putJSON(bkState, benchRunsKey, out)
}

func benchJobClone(j *benchJob) *benchJob {
	if j == nil {
		return nil
	}
	cp := *j
	cp.Rows = append([]benchJobRow(nil), j.Rows...)
	for i := range cp.Rows {
		if cp.Rows[i].Result != nil {
			r := *cp.Rows[i].Result
			cp.Rows[i].Result = &r
		}
	}
	return &cp
}

func benchRowsFromPicks(picks []benchPick) ([]benchJobRow, error) {
	if len(picks) > 16 {
		return nil, fmt.Errorf("maximum 16 benchmark selections")
	}
	var rows []benchJobRow
	seen := map[string]bool{}
	for _, p := range picks {
		if choiceID := strings.TrimSpace(p.ChoiceID); choiceID != "" {
			if isEngineWorker() {
				return nil, fmt.Errorf("engine nodes support local benchmarks only; cloud choices belong to the main Loom")
			}
			if seen[choiceID] {
				continue
			}
			seen[choiceID] = true
			row := benchJobRow{ChoiceID: choiceID, Kind: "cloud", Name: strings.TrimSpace(p.Name), Status: "pending", Measurement: "api_stream"}
			if choice, agent, ok := benchHarnessChoice(choiceID); ok {
				if reason := benchHarnessReason(choice, agent); reason != "" {
					return nil, fmt.Errorf("native model-only benchmark unavailable: %s", reason)
				}
				row.Kind, row.Model, row.Provider, row.RuntimeID, row.Measurement = "account", choice.Model, choice.ProviderName, agent.ID, "native_cli"
				row.Name = choice.Name
			}
			if provider, ok := benchCloudProvider(choiceID); ok {
				row.Model, row.Provider = provider.Model, provider.Name
			}
			if row.Name == "" {
				row.Name = row.Model
			}
			if row.Name == "" {
				row.Name = choiceID
			}
			rows = append(rows, row)
			continue
		}
		if preset := strings.TrimSpace(p.Preset); preset != "" {
			path, err := safePresetPath(preset)
			if err != nil {
				return nil, err
			}
			key := "p:" + preset
			if seen[key] {
				continue
			}
			if _, err := os.Stat(path); err != nil && currentEngineNode() == nil {
				return nil, fmt.Errorf("preset %s not found", preset)
			}
			seen[key] = true
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = preset
			}
			rows = append(rows, benchJobRow{Kind: "local", Preset: preset, Model: strings.TrimSpace(p.Model), Name: name, Status: "pending"})
			continue
		}
		model := strings.TrimSpace(p.Model)
		if model == "" || seen[normDir(model)] {
			continue
		}
		if ggufIsMmproj(filepath.Base(model)) {
			continue
		}
		seen[normDir(model)] = true
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = filepath.Base(model)
		}
		rows = append(rows, benchJobRow{Kind: "local", Model: model, Name: name, Status: "pending"})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("choose at least one model or preset")
	}
	return rows, nil
}

func benchCheckConsent(picks []benchPick, consent bool) error {
	for _, p := range picks {
		if strings.TrimSpace(p.ChoiceID) != "" && !consent {
			return fmt.Errorf("confirm sending the test prompt to external providers or native accounts; requests consume credits or subscription quota")
		}
	}
	return nil
}

func startBenchQueue(testID string, picks []benchPick, consent ...bool) (*benchJob, error) {
	if err := benchCheckConsent(picks, len(consent) > 0 && consent[0]); err != nil {
		return nil, err
	}
	t, ok := findBenchTest(testID)
	if !ok {
		return nil, fmt.Errorf("unknown test")
	}
	rows, err := benchRowsFromPicks(picks)
	if err != nil {
		return nil, err
	}
	benchJobMu.Lock()
	defer benchJobMu.Unlock()
	if !benchBusy.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("a queue is already running")
	}
	j := &benchJob{
		ID: fmt.Sprintf("j%d", time.Now().UnixNano()), TestID: t.ID, TestName: t.Name,
		Kind: t.Kind, Status: "running", Started: time.Now().Unix(), Rows: rows,
	}
	if n := currentEngineNode(); n != nil {
		copy := *n
		j.engine = &copy
	}
	benchStop.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	benchCancel = cancel
	saveBenchJob(j)
	initial := benchJobClone(j)
	go runBenchQueue(ctx, j, t)
	return initial, nil
}

func cancelBenchQueue() *benchJob { return cancelBenchQueueID("") }

func cancelBenchQueueID(id string) *benchJob {
	benchJobMu.Lock()
	defer benchJobMu.Unlock()
	j := loadBenchJob()
	if id != "" && (j == nil || j.ID != id) {
		return j
	}
	benchStop.Store(true)
	if benchCancel != nil {
		benchCancel()
	}
	if j != nil && j.Status == "running" {
		j.Status = "cancel"
		saveBenchJob(j)
	}
	return benchJobClone(j)
}

func runBenchQueue(ctx context.Context, j *benchJob, t benchTest) {
	defer func() {
		benchJobMu.Lock()
		defer benchJobMu.Unlock()
		benchCancel()
		benchCancel = nil
		benchBusy.Store(false)
	}()
	for i := range j.Rows {
		if benchStop.Load() {
			benchJobMu.Lock()
			for k := i; k < len(j.Rows); k++ {
				if j.Rows[k].Status == "pending" || j.Rows[k].Status == "loading" {
					j.Rows[k].Status = "skip"
				}
			}
			j.Status = "cancel"
			j.Finished = time.Now().Unix()
			saveBenchJob(j)
			archiveBenchJob(*j)
			benchJobMu.Unlock()
			return
		}
		benchJobMu.Lock()
		j.Index = i
		j.Rows[i].Status = "loading"
		saveBenchJob(j)
		benchJobMu.Unlock()

		n := currentEngineNode()
		changed := (n == nil) != (j.engine == nil) || (n != nil && j.engine != nil && *n != *j.engine)
		if j.Rows[i].Kind == "local" && changed {
			benchJobMu.Lock()
			j.Rows[i].Status = "err"
			j.Rows[i].Error = "selected engine changed; start a new benchmark"
			saveBenchJob(j)
			benchJobMu.Unlock()
			continue
		}
		if benchStop.Load() {
			benchJobMu.Lock()
			j.Rows[i].Status = "skip"
			saveBenchJob(j)
			benchJobMu.Unlock()
			continue
		}
		benchJobMu.Lock()
		j.Rows[i].Status = "running"
		saveBenchJob(j)
		benchJobMu.Unlock()

		var res *benchResult
		var preview string
		var err error
		if j.Rows[i].Kind == "cloud" {
			res, preview, err = runCloudBenchTest(ctx, t, j.Rows[i].ChoiceID)
		} else if j.Rows[i].Kind == "account" {
			res, preview, err = runAccountBenchTest(ctx, t, j.Rows[i].ChoiceID)
		} else if j.engine != nil && !j.engine.Direct {
			res, preview, err = runNodeBenchTest(ctx, t, j.Rows[i], *j.engine)
		} else if j.engine != nil && j.engine.Direct {
			res, preview, err = runDirectBenchTest(ctx, t, j.Rows[i], *j.engine)
		} else {
			// La sélection voyage dans la requête : aucun préchargement ne peut
			// couper un stream externe avant de rejoindre la file du front.
			model := j.Rows[i].Model
			if j.Rows[i].Preset != "" {
				model = j.Rows[i].Preset
			}
			prompt, n := benchPrompt(t)
			res, preview, err = runCompletionBenchModelContext(ctx, prompt, n, nil, model)
			if err == nil && (t.Kind == "perf" || t.ID == benchTestPerf) {
				weights := j.Rows[i].Model
				if e, ok := resolveOAIModel(model); ok {
					weights = e.Model
				}
				saveLastBenchForModel(res, weights)
				if j.Rows[i].Preset != "" {
					saveBenchForPreset(res, j.Rows[i].Preset, weights)
				}
			}
		}
		benchJobMu.Lock()
		if ctx.Err() != nil {
			j.Rows[i].Status = "skip"
			if err != nil {
				j.Rows[i].Error = err.Error()
			}
		} else if err != nil {
			j.Rows[i].Status = "err"
			j.Rows[i].Error = err.Error()
		} else {
			j.Rows[i].Status = "ok"
			j.Rows[i].Result = res
			j.Rows[i].Preview = clipBenchPreview(preview)
			j.Rows[i].Output = preview
		}
		saveBenchJob(j)
		benchJobMu.Unlock()
	}
	benchJobMu.Lock()
	if benchStop.Load() {
		j.Status = "cancel"
	} else if j.Status != "cancel" {
		j.Status = "done"
	}
	j.Finished = time.Now().Unix()
	saveBenchJob(j)
	archiveBenchJob(*j)
	benchJobMu.Unlock()
}

func runBenchTest(t benchTest) (*benchResult, string, error) {
	return runBenchTestContext(context.Background(), t)
}

func runBenchTestContext(ctx context.Context, t benchTest) (*benchResult, string, error) {
	prompt, n := benchPrompt(t)
	res, output, err := runCompletionBenchContext(ctx, prompt, n, nil)
	if err == nil && (t.Kind == "perf" || t.ID == benchTestPerf) {
		saveLastBench(res)
		saveBenchForActivePreset(res)
	}
	return res, output, err
}

func clipBenchPreview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 280 {
		return string(r[:280]) + "…"
	}
	return s
}

func benchModelReady(model string) bool {
	if !healthCheck() {
		return false
	}
	cur := strings.TrimSpace(ReadConfig()["MODEL"])
	if cur == "" {
		return false
	}
	if normDir(cur) == normDir(model) {
		return true
	}
	if p, err := resolveServeModelPath(model); err == nil && normDir(cur) == normDir(p) {
		return true
	}
	if filepath.Base(cur) == filepath.Base(model) {
		return true
	}
	return false
}

func waitLLMHealth(timeout time.Duration) error {
	return waitLLMHealthContext(context.Background(), timeout)
}

func waitLLMHealthContext(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthCheck() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return fmt.Errorf("engine not ready after %s", timeout.Round(time.Second))
}

func benchPresetReady(preset string) bool {
	if !healthCheck() {
		return false
	}
	active := strings.TrimSpace(getStr(bkState, "active_preset"))
	return active != "" && active == strings.TrimSpace(preset)
}

func benchLoadAndWait(model, preset string) error {
	return benchLoadAndWaitContext(context.Background(), model, preset)
}

func benchLoadAndWaitContext(ctx context.Context, model, preset string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	preset = strings.TrimSpace(preset)
	if preset != "" {
		if benchPresetReady(preset) {
			return nil
		}
		path, err := safePresetPath(preset)
		if err != nil {
			return err
		}
		if err := SwitchToPreset(path); err != nil {
			return err
		}
		return waitLLMHealthContext(ctx, benchHealthWait)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model required")
	}
	if benchModelReady(model) {
		return nil
	}
	if err := loadNakedModel(model); err != nil {
		return err
	}
	if err := restartLlamaEngine(); err != nil {
		return err
	}
	return waitLLMHealthContext(ctx, benchHealthWait)
}
