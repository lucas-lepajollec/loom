package loom

import (
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
	Model   string       `json:"model"`
	Preset  string       `json:"preset,omitempty"`
	Name    string       `json:"name"`
	Status  string       `json:"status"` // pending|loading|running|ok|err|skip
	Error   string       `json:"error,omitempty"`
	Result  *benchResult `json:"result,omitempty"`
	Preview string       `json:"preview,omitempty"`
}

type benchJob struct {
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
	Model  string `json:"model"`
	Name   string `json:"name"`
	Preset string `json:"preset,omitempty"`
}

var (
	benchJobMu sync.Mutex
	benchBusy  atomic.Bool
	benchStop  atomic.Bool
)

func builtinBenchTests() []benchTest {
	return []benchTest{{
		ID: benchTestPerf, Name: "Perfs brutes", Kind: "perf", Builtin: true,
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
	if name == "" {
		return benchTest{}, fmt.Errorf("nom requis")
	}
	if prompt == "" {
		return benchTest{}, fmt.Errorf("prompt requis")
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
		return fmt.Errorf("ce test ne se supprime pas")
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
		return fmt.Errorf("test introuvable")
	}
	return putJSON(bkState, benchTestsKey, out)
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
	var rows []benchJobRow
	seen := map[string]bool{}
	for _, p := range picks {
		if preset := strings.TrimSpace(p.Preset); preset != "" {
			path, err := safePresetPath(preset)
			if err != nil {
				return nil, err
			}
			key := "p:" + preset
			if seen[key] {
				continue
			}
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("preset %s introuvable", preset)
			}
			seen[key] = true
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = preset
			}
			rows = append(rows, benchJobRow{Preset: preset, Model: strings.TrimSpace(p.Model), Name: name, Status: "pending"})
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
		rows = append(rows, benchJobRow{Model: model, Name: name, Status: "pending"})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("choisis au moins un modèle ou un preset")
	}
	return rows, nil
}

func startBenchQueue(testID string, picks []benchPick) (*benchJob, error) {
	t, ok := findBenchTest(testID)
	if !ok {
		return nil, fmt.Errorf("test inconnu")
	}
	rows, err := benchRowsFromPicks(picks)
	if err != nil {
		return nil, err
	}
	if !benchBusy.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("une file tourne déjà")
	}
	j := &benchJob{
		ID: fmt.Sprintf("j%d", time.Now().UnixNano()), TestID: t.ID, TestName: t.Name,
		Kind: t.Kind, Status: "running", Started: time.Now().Unix(), Rows: rows,
	}
	benchStop.Store(false)
	saveBenchJob(j)
	go runBenchQueue(j, t)
	return benchJobClone(j), nil
}

func cancelBenchQueue() *benchJob {
	benchStop.Store(true)
	benchJobMu.Lock()
	defer benchJobMu.Unlock()
	j := loadBenchJob()
	if j != nil && j.Status == "running" {
		j.Status = "cancel"
		saveBenchJob(j)
	}
	return benchJobClone(j)
}

func runBenchQueue(j *benchJob, t benchTest) {
	defer benchBusy.Store(false)
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

		if err := benchLoadAndWait(j.Rows[i].Model, j.Rows[i].Preset); err != nil {
			benchJobMu.Lock()
			j.Rows[i].Status = "err"
			j.Rows[i].Error = err.Error()
			saveBenchJob(j)
			benchJobMu.Unlock()
			continue
		}
		if benchStop.Load() {
			continue
		}
		benchJobMu.Lock()
		j.Rows[i].Status = "running"
		saveBenchJob(j)
		benchJobMu.Unlock()

		res, preview, err := runBenchTest(t)
		benchJobMu.Lock()
		if err != nil {
			j.Rows[i].Status = "err"
			j.Rows[i].Error = err.Error()
		} else {
			j.Rows[i].Status = "ok"
			j.Rows[i].Result = res
			j.Rows[i].Preview = clipBenchPreview(preview)
		}
		saveBenchJob(j)
		benchJobMu.Unlock()
	}
	benchJobMu.Lock()
	if j.Status != "cancel" {
		j.Status = "done"
	}
	j.Finished = time.Now().Unix()
	saveBenchJob(j)
	archiveBenchJob(*j)
	benchJobMu.Unlock()
}

func runBenchTest(t benchTest) (*benchResult, string, error) {
	if t.Kind == "perf" || t.ID == benchTestPerf {
		res, err := runBench(2000, 300)
		return res, "", err
	}
	n := t.MaxTokens
	if n <= 0 {
		n = 256
	}
	return runCompletionBench(t.Prompt, n)
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
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthCheck() {
			return nil
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return fmt.Errorf("moteur pas prêt après %s", timeout.Round(time.Second))
}

func benchPresetReady(preset string) bool {
	if !healthCheck() {
		return false
	}
	active := strings.TrimSpace(getStr(bkState, "active_preset"))
	return active != "" && active == strings.TrimSpace(preset)
}

func benchLoadAndWait(model, preset string) error {
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
		return waitLLMHealth(benchHealthWait)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("modèle requis")
	}
	if benchModelReady(model) {
		return nil
	}
	if err := loadNakedModel(model); err != nil {
		return err
	}
	if err := serviceAction("restart"); err != nil {
		return err
	}
	return waitLLMHealth(benchHealthWait)
}
