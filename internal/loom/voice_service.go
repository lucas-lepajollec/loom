package loom

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed voice/worker.py voice/bench-en.wav voice/bench-fr.wav
var voiceWorkerFS embed.FS

type voiceConfig struct {
	STT             string  `json:"stt"`
	TTS             string  `json:"tts"`
	VAD             string  `json:"vad"`
	Voice           int     `json:"voice"`
	Speed           float64 `json:"speed"`
	Language        string  `json:"language"`
	Threads         int     `json:"threads"`
	VADThreshold    float64 `json:"vad_threshold"`
	EndpointSilence float64 `json:"endpoint_silence"`
	MinSpeech       float64 `json:"min_speech"`
	IdleMinutes     int     `json:"idle_unload_minutes"`
	Boot            bool    `json:"boot"`
}

func readVoiceConfig() voiceConfig {
	c := voiceConfig{Speed: 1, Language: "en", Threads: 2, VADThreshold: .5, EndpointSilence: .7, MinSpeech: .25, IdleMinutes: 30}
	getStoreJSON(bkState, "voice_config", &c)
	return c
}
func validateVoiceConfig(c voiceConfig, installed bool) error {
	if c.Speed < .25 || c.Speed > 4 || c.Voice < 0 || c.Voice > 1024 || c.Threads < 1 || c.Threads > 32 || c.VADThreshold <= 0 || c.VADThreshold >= 1 || c.EndpointSilence < .1 || c.EndpointSilence > 5 || c.MinSpeech < .05 || c.MinSpeech > 5 || c.IdleMinutes < 0 || c.IdleMinutes > 1440 || len(c.Language) > 16 || strings.ContainsAny(c.Language, "\x00\r\n") {
		return errors.New("invalid voice parameters")
	}
	for kind, id := range map[string]string{"stt": c.STT, "tts": c.TTS, "vad": c.VAD} {
		if id == "" {
			if c.Boot {
				return errors.New("select installed STT, TTS and VAD before enabling voice at boot")
			}
			continue
		}
		m, err := voiceModel(id)
		if err != nil || m.Kind != kind {
			return fmt.Errorf("invalid %s model", kind)
		}
		if installed && !voiceModelInstalled(m) {
			return fmt.Errorf("download the %s model first", kind)
		}
	}
	return nil
}

type voiceEngineSelection struct {
	Version          string `json:"version"`
	Provider         string `json:"provider"`
	PreviousVersion  string `json:"previous_version,omitempty"`
	PreviousProvider string `json:"previous_provider,omitempty"`
	PreviousCopy     bool   `json:"previous_copy,omitempty"`
}

func readVoiceEngine() voiceEngineSelection {
	e := voiceEngineSelection{Version: curatedVoice.Version, Provider: "cpu"}
	getStoreJSON(bkState, "voice_engine_selection", &e)
	// Values originate only from the embedded catalog or a saved installation.
	if !validVoiceEngineSelection(e) {
		return voiceEngineSelection{Version: curatedVoice.Version, Provider: "cpu"}
	}
	return e
}
func validVoiceEngineSelection(e voiceEngineSelection) bool {
	if e.Provider != "cpu" && e.Provider != "cuda" {
		return false
	}
	if !regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`).MatchString(e.Version) {
		return false
	}
	return true
}
func voiceEngineDir() string {
	e := readVoiceEngine()
	return filepath.Join(voiceRoot(), "sherpa-onnx", e.Version, e.Provider)
}

func voiceEngineInstalled() bool {
	_, e := os.Stat(filepath.Join(voiceEngineDir(), ".verified.json"))
	_, err := voiceFindBinary(voiceEngineDir(), "sherpa-onnx-version")
	_, libraryErr := voiceLibraryPath(voiceEngineDir())
	return e == nil && err == nil && libraryErr == nil
}

type voiceWorkerEvent struct {
	Ready      bool   `json:"ready,omitempty"`
	Done       bool   `json:"done,omitempty"`
	Error      string `json:"error,omitempty"`
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	Audio      string `json:"audio,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Samples    int    `json:"samples,omitempty"`
}
type voiceWorker struct {
	gate   chan struct{}
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	scan   *bufio.Scanner
	done   chan struct{}
	cancel context.CancelFunc
	logs   *voiceBoundedLog
}

var voiceWorkerCommand = func(ctx context.Context, library, mode string) *exec.Cmd {
	script, _ := voiceWorkerFS.ReadFile("voice/worker.py")
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}
	// -I ignores Python path/environment customization; no packages are imported.
	return exec.CommandContext(ctx, python, "-I", "-u", "-c", string(script), library, mode)
}

func (v *voiceWorker) alive() bool {
	select {
	case <-v.done:
		return false
	default:
		return true
	}
}
func (v *voiceWorker) stop() { v.cancel(); _ = v.stdin.Close(); <-v.done }
func (v *voiceWorker) request(ctx context.Context, req any, emit func(voiceWorkerEvent) error) error {
	select {
	case v.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-v.gate }()
	if !v.alive() {
		return errors.New("voice worker exited")
	}
	done := make(chan error, 1)
	go func() {
		b, err := json.Marshal(req)
		if err == nil {
			_, err = v.stdin.Write(append(b, '\n'))
		}
		if err == nil {
			err = v.read(emit, false)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			v.stop()
		}
		return err
	case <-ctx.Done():
		v.cancel()
		_ = v.stdin.Close()
		<-done
		<-v.done
		return ctx.Err()
	}
}
func (v *voiceWorker) read(emit func(voiceWorkerEvent) error, ready bool) error {
	for v.scan.Scan() {
		var e voiceWorkerEvent
		if err := json.Unmarshal(v.scan.Bytes(), &e); err != nil {
			return errors.New("invalid voice worker event")
		}
		if e.Error != "" {
			return errors.New(e.Error)
		}
		if ready && e.Ready || !ready && e.Done {
			return nil
		}
		if emit != nil {
			if err := emit(e); err != nil {
				v.cancel()
				return err
			}
		}
	}
	if err := v.scan.Err(); err != nil {
		return err
	}
	return errors.New("voice worker closed its stream")
}

type voiceService struct {
	mu       sync.Mutex
	stt, tts *voiceWorker
	inflight int
	last     time.Time
	err      string
	log      []string
}

var voiceRuntime = &voiceService{}

func (s *voiceService) busyLocked() bool {
	return s.inflight > 0 || s.stt != nil && s.stt.alive() || s.tts != nil && s.tts.alive()
}
func voiceLibraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libsherpa-onnx-c-api.dylib"
	case "windows":
		return "sherpa-onnx-c-api.dll"
	default:
		return "libsherpa-onnx-c-api.so"
	}
}
func voiceLibraryPath(root string) (string, error) {
	name := voiceLibraryName()
	var found string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == name {
			found = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("sherpa shared C API library missing")
	}
	return found, nil
}
func (s *voiceService) workerLocked(ctx context.Context, mode string) (*voiceWorker, error) {
	dst := &s.stt
	if mode == "tts" {
		dst = &s.tts
	}
	if *dst != nil && (*dst).alive() {
		return *dst, nil
	}
	c := readVoiceConfig()
	if err := validateVoiceConfig(c, true); err != nil {
		return nil, err
	}
	if !voiceEngineInstalled() {
		return nil, errors.New("install the voice engine first")
	}
	id := c.STT
	if mode == "tts" {
		id = c.TTS
	}
	if id == "" {
		return nil, errors.New("select an installed voice model first")
	}
	m, _ := voiceModel(id)
	files, err := voiceResolveFiles(voiceModelDir(id), m)
	if err != nil {
		return nil, err
	}
	library, err := voiceLibraryPath(voiceEngineDir())
	if err != nil {
		return nil, err
	}
	provider := readVoiceEngine().Provider
	payload := map[string]any{"model": map[string]any{"family": m.Family, "files": files}, "provider": provider, "threads": c.Threads, "language": c.Language, "vad_threshold": c.VADThreshold, "endpoint_silence": c.EndpointSilence, "min_speech": c.MinSpeech}
	if mode == "stt" && !m.Streaming {
		vad, err := voiceModel(c.VAD)
		if err != nil {
			return nil, errors.New("select an installed VAD for offline STT")
		}
		vf, err := voiceResolveFiles(voiceModelDir(vad.ID), vad)
		if err != nil {
			return nil, err
		}
		payload["vad_model"] = vf["model"]
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	cmd := voiceWorkerCommand(workerCtx, library, mode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Starting happens under the service lock. Log writes are consumed after
	// startup and never hold the process pipes hostage to that lock.
	logs := &voiceBoundedLog{}
	cmd.Stderr = logs
	v := &voiceWorker{gate: make(chan struct{}, 1), cmd: cmd, stdin: stdin, scan: bufio.NewScanner(stdout), done: make(chan struct{}), cancel: cancel, logs: logs}
	v.scan.Buffer(make([]byte, 64<<10), 8<<20)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go func() { _ = cmd.Wait(); cancel(); close(v.done) }()
	startupCtx, stop := context.WithTimeout(ctx, 60*time.Second)
	defer stop()
	first := make(chan error, 1)
	go func() {
		b, _ := json.Marshal(payload)
		_, err := stdin.Write(append(b, '\n'))
		if err == nil {
			err = v.read(nil, true)
		}
		first <- err
	}()
	select {
	case err = <-first:
	case <-startupCtx.Done():
		cancel()
		_ = stdin.Close()
		<-first
		err = startupCtx.Err()
	}
	if err != nil {
		v.stop()
		s.err = err.Error()
		s.recordLogLocked(logs.String())
		return nil, err
	}
	*dst = v
	s.last = time.Now()
	s.err = ""
	s.recordLogLocked("started " + mode + " " + id)
	return v, nil
}

// Process stderr is bounded and excludes protocol input (text/audio).
type voiceBoundedLog struct {
	mu sync.Mutex
	b  string
}

func (l *voiceBoundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b += string(p)
	if len(l.b) > 32768 {
		l.b = l.b[len(l.b)-32768:]
	}
	return len(p), nil
}
func (l *voiceBoundedLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b }
func (s *voiceService) acquire(ctx context.Context, mode string) (*voiceWorker, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if voiceJob.snapshot().Running {
		return nil, nil, errors.New("voice installation in progress")
	}
	v, err := s.workerLocked(ctx, mode)
	if err != nil {
		return nil, nil, err
	}
	s.inflight++
	s.last = time.Now()
	return v, func() { s.mu.Lock(); defer s.mu.Unlock(); s.inflight--; s.last = time.Now() }, nil
}
func (s *voiceService) stopLocked() {
	if s.stt != nil {
		s.stt.stop()
		s.stt = nil
	}
	if s.tts != nil {
		s.tts.stop()
		s.tts = nil
	}
	s.recordLogLocked("voice stopped")
}
func (s *voiceService) action(ctx context.Context, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight > 0 {
		return errors.New("voice is in use")
	}
	if action == "stop" || action == "restart" {
		s.stopLocked()
	}
	if action == "stop" {
		return nil
	}
	if action != "start" && action != "restart" {
		return errors.New("action must be start, stop or restart")
	}
	if voiceJob.snapshot().Running {
		return errors.New("voice installation in progress")
	}
	if _, err := s.workerLocked(ctx, "stt"); err != nil {
		return err
	}
	if _, err := s.workerLocked(ctx, "tts"); err != nil {
		s.stopLocked()
		return err
	}
	return nil
}
func (s *voiceService) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	stt := s.stt != nil && s.stt.alive()
	tts := s.tts != nil && s.tts.alive()
	log := append([]string{}, s.log...)
	for _, v := range []*voiceWorker{s.stt, s.tts} {
		if v != nil && v.logs != nil {
			log = append(log, v.logs.String())
		}
	}
	err := s.err
	if err == "" && (s.stt != nil && !s.stt.alive() || s.tts != nil && !s.tts.alive()) {
		err = "voice worker exited; retry or restart voice"
	}
	if len(log) > 128 {
		log = log[len(log)-128:]
	}
	return map[string]any{"running": stt || tts, "stt_running": stt, "tts_running": tts, "in_flight": s.inflight, "last_used": s.last, "error": err, "log": append([]string{}, log...)}
}
func voiceLifecycle(ctx context.Context) {
	if isEngineWorker() && !nodeVoiceEnabled() {
		return
	}
	if readVoiceConfig().Boot {
		_ = voiceRuntime.action(ctx, "start")
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer func() { voiceJob.stop(); voiceRuntime.mu.Lock(); voiceRuntime.stopLocked(); voiceRuntime.mu.Unlock() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			voiceRuntime.unloadIdle(time.Now())
		}
	}
}

func voiceBootReady() bool {
	c := readVoiceConfig()
	c.Boot = true
	return voiceEngineInstalled() && validateVoiceConfig(c, true) == nil && (!isEngineWorker() || nodeVoiceEnabled())
}

func (s *voiceService) unloadIdle(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idle := readVoiceConfig().IdleMinutes
	if idle > 0 && s.inflight == 0 && now.Sub(s.last) > time.Duration(idle)*time.Minute && s.busyLocked() {
		s.stopLocked()
	}
}

func (s *voiceService) recordLogLocked(text string) {
	s.log = append(s.log, text)
	if len(s.log) > 128 {
		s.log = append([]string(nil), s.log[len(s.log)-128:]...)
	}
}

var errVoiceHealthBusy = errors.New("voice service is in use")

// Health observes the existing protocol only. A timeout reports failure without
// killing/restarting a worker; the pending read drains on recovery or stop.
func (v *voiceWorker) health(ctx context.Context) error {
	if !v.alive() {
		return errors.New("voice worker exited")
	}
	select {
	case v.gate <- struct{}{}:
	default:
		return errVoiceHealthBusy
	}
	done := make(chan error, 1)
	go func() {
		_, err := v.stdin.Write([]byte("{\"op\":\"health\"}\n"))
		if err == nil {
			err = v.read(nil, false)
		}
		<-v.gate
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *voiceService) health(ctx context.Context) error {
	s.mu.Lock()
	if s.inflight > 0 {
		s.mu.Unlock()
		return errVoiceHealthBusy
	}
	workers := []*voiceWorker{s.stt, s.tts}
	s.inflight++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.inflight--; s.mu.Unlock() }()
	for _, v := range workers {
		if v != nil {
			if err := v.health(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
