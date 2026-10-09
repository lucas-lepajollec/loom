package loom

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

type voiceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f voiceRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func voiceTestSetup(t *testing.T) {
	t.Helper()
	testHome(t)
	oldRuntime, oldJob, oldCommand, oldHTTP, oldCatalog := voiceRuntime, voiceJob, voiceWorkerCommand, voiceHTTPClient, curatedVoice
	voiceRuntime = &voiceService{}
	voiceJob = &voiceDownloadJob{}
	t.Cleanup(func() {
		voiceJob.stop()
		voiceRuntime.mu.Lock()
		voiceRuntime.stopLocked()
		voiceRuntime.mu.Unlock()
		voiceRuntime = oldRuntime
		voiceJob = oldJob
		voiceWorkerCommand = oldCommand
		voiceHTTPClient = oldHTTP
		curatedVoice = oldCatalog
	})
}
func voiceTestModels(t *testing.T) {
	t.Helper()
	c := readVoiceConfig()
	c.STT, c.TTS, c.VAD = "zipformer-fr", "piper-fr", "silero-v5"
	c.Language = "fr"
	for _, id := range []string{c.STT, c.TTS, c.VAD} {
		m, _ := voiceModel(id)
		root := voiceModelDir(id)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		for _, pattern := range m.Files {
			path := filepath.Join(root, strings.ReplaceAll(pattern, "*", ""))
			var err error
			if strings.Contains(pattern, "data") {
				err = os.Mkdir(path, 0700)
			} else {
				err = os.WriteFile(path, []byte("fake"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, ".verified.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := voiceEngineDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	bin := "sherpa-onnx-version"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if err := os.WriteFile(filepath.Join(root, bin), []byte("#!/bin/sh\nprintf 'sherpa-onnx version : 1.13.8\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	lib := "libsherpa-onnx-c-api.so"
	if runtime.GOOS == "darwin" {
		lib = "libsherpa-onnx-c-api.dylib"
	}
	if runtime.GOOS == "windows" {
		lib = "sherpa-onnx-c-api.dll"
	}
	if err := os.WriteFile(filepath.Join(root, lib), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".verified.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, "voice_config", c); err != nil {
		t.Fatal(err)
	}
	voiceWorkerCommand = func(ctx context.Context, library, mode string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVoiceFakeWorkerProcess$", "--", mode)
		cmd.Env = append(os.Environ(), "LOOM_VOICE_FAKE_WORKER=1")
		return cmd
	}
}
func TestVoiceFakeWorkerProcess(t *testing.T) {
	if os.Getenv("LOOM_VOICE_FAKE_WORKER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	emit := func(v any) { b, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(b, '\n')) }
	if !scan.Scan() {
		os.Exit(1)
	}
	var config map[string]any
	if json.Unmarshal(scan.Bytes(), &config) != nil {
		os.Exit(2)
	}
	emit(map[string]any{"ready": true})
	var lastTTS map[string]any
	for scan.Scan() {
		var req map[string]any
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(2)
		}
		switch req["op"] {
		case "inspect":
			config["last_tts"] = lastTTS
			b, _ := json.Marshal(config)
			emit(map[string]any{"text": string(b)})
		case "pcm":
			emit(map[string]any{"type": "partial", "text": "bonjour"})
		case "finish":
			emit(map[string]any{"type": "final", "text": "bonjour Loom"})
		case "tts":
			lastTTS = req
			emit(map[string]any{"sample_rate": 16000})
			if os.Getenv("LOOM_VOICE_FAKE_TIMINGS") == "1" {
				time.Sleep(80 * time.Millisecond)
			}
			count := 1
			if req["text"] == "slow" {
				count = 1000
			}
			for i := 0; i < count; i++ {
				emit(map[string]any{"audio": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1, 0}, 320))})
				if count > 1 {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if os.Getenv("LOOM_VOICE_FAKE_TIMINGS") == "1" {
				time.Sleep(100 * time.Millisecond)
			}
		case "health":
			if os.Getenv("LOOM_VOICE_FAKE_HEALTH_HANG") == "1" {
				time.Sleep(time.Hour)
			}
		case "crash":
			os.Exit(3)
		case "hang":
			select {}
		}
		emit(map[string]any{"done": true})
	}
	os.Exit(0)
}
func TestVoiceDownloadRequiresPinnedHashAndExactSize(t *testing.T) {
	voiceTestSetup(t)
	payload := []byte("verified bytes")
	sum := sha256.Sum256(payload)
	calls := 0
	voiceHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	a := voiceArtifact{ID: "fixture", URL: "https://example.invalid/archive", Size: int64(len(payload)), SHA256: "TODO_SHA256"}
	dest := filepath.Join(t.TempDir(), "download")
	if err := voiceDownload(context.Background(), a, dest, func(int64) {}); err == nil || calls != 0 {
		t.Fatal("missing hash did not fail before network", err, calls)
	}
	a.SHA256 = hex.EncodeToString(sum[:])
	if err := voiceDownload(context.Background(), a, dest, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	a.SHA256 = strings.Repeat("0", 64)
	if err := voiceDownload(context.Background(), a, dest+"bad", func(int64) {}); err == nil {
		t.Fatal("bad hash accepted")
	}
	a.Size++
	if err := voiceDownload(context.Background(), a, dest+"truncated", func(int64) {}); err == nil {
		t.Fatal("bad size accepted")
	}
}
func voiceZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		if _, err = f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestVoiceInstallStagingVerificationAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture version binary is a POSIX shell script")
	}
	voiceTestSetup(t)
	payload := voiceZip(t, map[string]string{"bin/sherpa-onnx-version": "#!/bin/sh\nprintf 'sherpa-onnx version : 1.13.8\\n'\n", "lib/marker": "first", "lib/" + voiceLibraryName(): "fake shared library"})
	sum := sha256.Sum256(payload)
	a := voiceArtifact{ID: "test-engine", URL: "https://example.invalid/sherpa.zip", Archive: "sherpa.zip", Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]), Provider: "cpu"}
	voiceHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	if err := voiceInstallArtifact(context.Background(), a, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(voiceEngineDir(), "lib", "marker"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := voiceInstallArtifact(context.Background(), a, true); err != nil {
		t.Fatal(err)
	}
	if err := voiceRollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(voiceEngineDir(), "lib", "marker"))
	if err != nil || string(b) != "old" {
		t.Fatal("rollback did not retain previous", string(b), err)
	}
	a.SHA256 = strings.Repeat("0", 64)
	if err := voiceInstallArtifact(context.Background(), a, true); err == nil {
		t.Fatal("unverified engine installed")
	}
	b, _ = os.ReadFile(filepath.Join(voiceEngineDir(), "lib", "marker"))
	if string(b) != "old" {
		t.Fatal("working engine replaced on verification failure")
	}
	entries, _ := os.ReadDir(voiceRoot())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".install-") {
			t.Fatal("staging leaked")
		}
	}
}
func TestVoiceModelArchiveCannotEscapeAndMustBeComplete(t *testing.T) {
	voiceTestSetup(t)
	m := voiceArtifact{ID: "fixture", Kind: "vad", Files: map[string]string{"model": "vad.onnx"}, Archive: "fixture.zip", URL: "https://example.invalid/fixture.zip"}
	for _, files := range []map[string]string{{"../escape": "bad"}, {"other": "missing"}, {"one/vad.onnx": "first", "two/vad.onnx": "second"}} {
		payload := voiceZip(t, files)
		sum := sha256.Sum256(payload)
		m.SHA256 = hex.EncodeToString(sum[:])
		m.Size = int64(len(payload))
		voiceHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
		})}
		if err := voiceInstallArtifact(context.Background(), m, false); err == nil {
			t.Fatal("invalid model archive installed", files)
		}
	}
}
func TestVoiceServiceFakeWorkersTestsAndCancel(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if voiceRuntime.snapshot()["running"] != true {
		t.Fatal("service not running")
	}
	pcm, rate, err := voiceSynthesize(context.Background(), "hello", nil)
	if err != nil || rate != 16000 || len(pcm) != 640 {
		t.Fatal("tts", rate, len(pcm), err)
	}
	text, err := voiceTranscribe(context.Background(), pcm)
	if err != nil || text != "bonjour Loom" {
		t.Fatal("stt", text, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _, err = voiceSynthesize(ctx, "slow", func(e voiceWorkerEvent) error {
		if e.Audio != "" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("TTS cancellation did not stop worker", err)
	}
	if voiceRuntime.snapshot()["stt_running"] != true {
		t.Fatal("TTS cancellation stopped STT")
	}
	pcm, _, err = voiceSynthesize(context.Background(), "hello", nil)
	if err != nil || len(pcm) != 640 {
		t.Fatal("worker did not recover after cancel", err)
	}
	worker, release, err := voiceRuntime.acquire(context.Background(), "stt")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.request(context.Background(), map[string]any{"op": "crash"}, nil); err == nil {
		t.Fatal("worker exit not observed")
	}
	release()
	if voiceRuntime.snapshot()["error"] == "" {
		t.Fatal("worker failure hidden in status")
	}
	if _, err := voiceTranscribe(context.Background(), pcm); err != nil {
		t.Fatal("worker did not recover after unexpected exit", err)
	}
	if err := voiceRuntime.action(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	if err := voiceRuntime.action(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("owned workers left running")
	}
}
func TestVoiceAPIRawWAVMethodsAndFilters(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	send := func(method, path string, body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		controlRequests(strings.Split(path, "?")[0], handleVoice)(w, r)
		return w
	}
	w := send("POST", "/api/voice/test/stt", voiceWAV([]byte{0, 0, 1, 0}, 16000))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "bonjour Loom") {
		t.Fatal("WAV middleware", w.Code, w.Body.String())
	}
	w = send("GET", "/api/voice/models?kind=tts&language=fr&max_size=30000000", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "piper-fr") || strings.Contains(w.Body.String(), "piper-en") {
		t.Fatal("catalog filters", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/voice/install", "/api/voice/models/download", "/api/voice/models/delete", "/api/voice/download/cancel", "/api/voice/test/tts", "/api/voice/test/stt", "/api/voice/bench", "/api/voice/install/rollback"} {
		w = send("GET", path, nil)
		if w.Code != 405 {
			t.Fatal("GET action", path, w.Code)
		}
	}
	w = send("POST", "/api/voice/bench", []byte(`{}`))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "real_time_factor") || !strings.Contains(w.Body.String(), "embedded_espeak_ng_fixture") {
		t.Fatal("bench", w.Code, w.Body.String())
	}
	w = send("POST", "/api/voice/test/stt", voiceWAV([]byte{0, 0}, 44100))
	if w.Code != 400 {
		t.Fatal("unsupported WAV accepted")
	}
}

// net.Pipe exercises an actual HTTP WebSocket upgrade without requiring
// loopback network permission in the sandbox.
type voicePipeListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func (l *voicePipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *voicePipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *voicePipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }
func voicePipeHTTP(t *testing.T, h http.Handler) *http.Client {
	t.Helper()
	l := &voicePipeListener{ch: make(chan net.Conn), done: make(chan struct{})}
	var handlers sync.WaitGroup
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		h.ServeHTTP(w, r)
	})}
	go func() { _ = srv.Serve(l) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		a, b := net.Pipe()
		select {
		case l.ch <- b:
			return a, nil
		case <-ctx.Done():
			a.Close()
			b.Close()
			return nil, ctx.Err()
		}
	}}
	t.Cleanup(func() { transport.CloseIdleConnections(); _ = srv.Close(); handlers.Wait() })
	return &http.Client{Transport: transport}
}
func TestVoiceWebSocketPCMPartialFinalAudioAndBargeIn(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	client := voicePipeHTTP(t, http.HandlerFunc(handleVoiceStream))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://voice.test/api/voice/stream", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	read := func() voiceWorkerEvent {
		t.Helper()
		typ, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary {
			return voiceWorkerEvent{Type: "audio", Audio: base64.StdEncoding.EncodeToString(b)}
		}
		var event voiceWorkerEvent
		if err := json.Unmarshal(b, &event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	write := func(v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	if e := read(); e.Type != "ready" {
		t.Fatal(e)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{0, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if e := read(); e.Type != "partial" || e.Text != "bonjour" {
		t.Fatal(e)
	}
	write(map[string]string{"type": "finish"})
	if e := read(); e.Type != "final" || e.Text != "bonjour Loom" {
		t.Fatal(e)
	}
	if e := read(); e.Type != "stt_done" {
		t.Fatal(e)
	}
	write(map[string]any{"type": "speak", "id": "a", "text": "hello", "voice": map[string]any{"voice_id": 0, "speed": 1.25}})
	if e := read(); e.Type != "audio_start" {
		t.Fatal(e)
	}
	if e := read(); e.Type != "audio" {
		t.Fatal(e)
	}
	if e := read(); e.Type != "audio_end" {
		t.Fatal(e)
	}
	speech := voiceInspectWorker(t, voiceRuntime.tts)["last_tts"].(map[string]any)
	if speech["voice"] != float64(0) || speech["speed"] != 1.25 {
		t.Fatal("WebSocket lost Jarvis voice override", speech)
	}
	write(map[string]string{"type": "speak", "id": "b", "text": "slow"})
	if e := read(); e.Type != "audio_start" {
		t.Fatal(e)
	}
	write(map[string]string{"type": "barge_in", "id": "b"})
	for {
		e := read()
		if e.Type == "cancelled" {
			break
		}
		if e.Type != "audio" {
			t.Fatal(e)
		}
	}
	write(map[string]string{"type": "ping"})
	if e := read(); e.Type != "pong" {
		t.Fatal("late audio after cancel", e)
	}
	// STT remains usable after speech cancellation.
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{1, 0}); err != nil {
		t.Fatal(err)
	}
	if e := read(); e.Type != "partial" {
		t.Fatal(e)
	}
}
func TestVoicePolicyAndDisabledNodeModule(t *testing.T) {
	voiceTestSetup(t)
	if !hasNodeModule(nodeModules(), "voice") {
		t.Fatal("voice not advertised")
	}
	if err := putBool(bkState, "node_voice_disabled", true); err != nil {
		t.Fatal(err)
	}
	if hasNodeModule(nodeModules(), "voice") {
		t.Fatal("disabled module advertised")
	}
	result := policy.Evaluate([]policy.Rule{{ID: "deny", Scope: "global", Subject: "voice.install", Decision: policy.Deny}}, policy.Input{Subject: "voice.install", Fallback: policy.Allow})
	if result.Decision != policy.Deny {
		t.Fatal(result)
	}
	result = policy.Evaluate([]policy.Rule{{ID: "deny", Scope: "global", Subject: "data.send_provider", Decision: policy.Deny}}, policy.Input{Subject: "voice.install", Fallback: policy.Allow})
	if result.Decision != policy.Allow {
		t.Fatal("voice changed provider consent policy")
	}
}
func TestVoiceDownloadCancellationAndIdleUnload(t *testing.T) {
	voiceTestSetup(t)
	entered := make(chan struct{})
	voiceHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	a := voiceArtifact{ID: "blocked", URL: "https://example.invalid/blocked", Archive: "blocked.zip", SHA256: strings.Repeat("0", 64), Size: 100}
	if err := voiceJob.begin(func(ctx context.Context) error { return voiceInstallArtifact(ctx, a, false) }); err != nil {
		t.Fatal(err)
	}
	<-entered
	voiceJob.stop()
	deadline := time.After(3 * time.Second)
	for voiceJob.snapshot().Running {
		select {
		case <-deadline:
			t.Fatal("cancelled download left running")
		case <-time.After(time.Millisecond):
		}
	}
	if voiceJob.snapshot().Phase != "cancelled" {
		t.Fatal(voiceJob.snapshot())
	}
	voiceTestModels(t)
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	voiceRuntime.mu.Lock()
	voiceRuntime.last = time.Now().Add(-time.Hour)
	voiceRuntime.mu.Unlock()
	voiceRuntime.unloadIdle(time.Now())
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("idle voice models not unloaded")
	}
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	worker, release, err := voiceRuntime.acquire(context.Background(), "stt")
	if err != nil {
		t.Fatal(err)
	}
	_ = worker
	voiceRuntime.mu.Lock()
	voiceRuntime.last = time.Now().Add(-time.Hour)
	voiceRuntime.mu.Unlock()
	voiceRuntime.unloadIdle(time.Now())
	if voiceRuntime.snapshot()["running"] != true {
		t.Fatal("in-flight stream unloaded")
	}
	release()
}
func TestVoiceStartupAndDoctorObserveWithoutGeneration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Startup systemd API is Linux only")
	}
	voiceTestSetup(t)
	voiceTestModels(t)
	old := startupCommand
	startupCommand = func(context.Context, bool, bool, ...string) ([]byte, error) {
		return nil, errors.New("systemd unavailable")
	}
	defer func() { startupCommand = old }()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/startup", strings.NewReader(`{"voice_boot":true}`))
	r.Header.Set("Content-Type", "application/json")
	handleStartup(w, r)
	if w.Code != 200 || !readVoiceConfig().Boot {
		t.Fatal("startup voice setting", w.Code, w.Body.String())
	}
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("startup setting started voice now")
	}
	for _, kind := range []string{"installed", "version", "models"} {
		check := doctorVoice(context.Background(), kind)
		if check.Status != "ok" {
			t.Fatal(kind, check)
		}
	}
	if c := doctorVoice(context.Background(), "service"); c.Status != "skip" {
		t.Fatal(c)
	}
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("doctor started generation/service")
	}
}
func TestVoiceInstallPolicyDeniesBeforeJobAndProviderPolicyIsIndependent(t *testing.T) {
	voiceTestSetup(t)
	doc := policy.Document{Version: 1, Rules: []policy.Rule{{ID: "deny-voice", Scope: "global", Subject: "voice.install", Decision: policy.Deny}}}
	if err := putStoreJSON(bkState, policyStateKey, doc); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/voice/install", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	handleVoice(w, r)
	if w.Code != 403 || voiceJob.snapshot().Running {
		t.Fatal("denied install executed", w.Code, w.Body.String())
	}
}
func TestVoicePairedNodeRoutingAndMachineCredentials(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	token := "voice-paired-machine-token"
	n := &engineNode{URL: "http://voice-node.test", WebKey: token, Role: "engine-node", NodeID: "voice-node", Modules: []string{"engine", "voice"}, Handshake: nodeHandshake}
	m := RemoteMachine{ID: "voice-machine", Name: "Voice machine", Host: "voice-node.test", NodeID: n.NodeID, Modules: n.Modules, Handshake: nodeHandshake}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, n); err != nil {
		t.Fatal(err)
	}
	if err := putStr(bkState, "voice_machine", m.ID); err != nil {
		t.Fatal(err)
	}
	oldTransport := http.DefaultTransport
	client := voicePipeHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" || r.URL.Query().Get("machine") != "" {
			t.Error("node credential/target isolation failed")
			w.WriteHeader(403)
			return
		}
		handleVoiceStream(w, r)
	}))
	http.DefaultTransport = client.Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	mainClient := voicePipeHTTP(t, http.HandlerFunc(voiceNodeAware(handleVoiceStream)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://voice-main.test/api/voice/stream?machine=voice-machine", &websocket.DialOptions{HTTPClient: mainClient, HTTPHeader: http.Header{"Authorization": {"Bearer browser-key"}, "Cookie": {"browser=private"}, "Origin": {"http://voice-main.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, b, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(b), `"type":"ready"`) {
		t.Fatal("node WS proxy", string(b), err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"speak","id":"node-speech","text":"hello","voice":{"tts_model":"piper-fr","voice_id":0,"speed":1.25}}`)); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"audio_start", "pcm", "audio_end"} {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if expected == "pcm" {
			if typ != websocket.MessageBinary || len(b) == 0 {
				t.Fatal("node PCM missing", typ, string(b))
			}
		} else if !strings.Contains(string(b), `"type":"`+expected+`"`) {
			t.Fatal("node speech event", expected, string(b))
		}
	}
	speech := voiceInspectWorker(t, voiceRuntime.tts)["last_tts"].(map[string]any)
	if speech["voice"] != float64(0) || speech["speed"] != 1.25 {
		t.Fatal("paired node lost Jarvis voice override", speech)
	}
	if currentEngineNode() != nil {
		t.Fatal("voice routing changed the LLM engine target")
	}
}
func TestVoiceWorkerRoutesRequireMachineAuthAndRespectDisable(t *testing.T) {
	voiceTestSetup(t)
	t.Setenv("LOOM_UI_SERVICE", "loom-node")
	mux := newEngineWorkerMux("voice-machine-token")
	request := func(key, origin string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/voice/models", nil)
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		r.Header.Set("Origin", origin)
		mux.ServeHTTP(w, r)
		return w
	}
	if w := request("", ""); w.Code != 401 {
		t.Fatal("missing node auth accepted", w.Code)
	}
	if w := request("voice-machine-token", "http://foreign.test"); w.Code != 403 {
		t.Fatal("browser reached node", w.Code)
	}
	if w := request("voice-machine-token", ""); w.Code != 200 {
		t.Fatal("authenticated voice route unavailable", w.Code, w.Body.String())
	}
	if err := putBool(bkState, "node_voice_disabled", true); err != nil {
		t.Fatal(err)
	}
	if w := request("voice-machine-token", ""); w.Code != 403 {
		t.Fatal("disabled voice executed", w.Code)
	}
}
func TestVoiceHealthObservesWithoutRestartOrGeneration(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if err := voiceRuntime.health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if check := doctorVoice(context.Background(), "service"); check.Status != "ok" {
		t.Fatal(check)
	}
	_, release, err := voiceRuntime.acquire(context.Background(), "stt")
	if err != nil {
		t.Fatal(err)
	}
	if err := voiceRuntime.health(context.Background()); !errors.Is(err, errVoiceHealthBusy) {
		t.Fatal("probe interfered with in-flight request", err)
	}
	release()
	if err := voiceRuntime.action(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOM_VOICE_FAKE_HEALTH_HANG", "1")
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := voiceRuntime.health(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("hung health did not time out", err)
	}
	if voiceRuntime.snapshot()["stt_running"] != true {
		t.Fatal("read-only probe killed a worker")
	}
	if err := voiceRuntime.action(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
}
func TestVoiceSessionLimitRejectsBeforeLoadingModels(t *testing.T) {
	voiceTestSetup(t)
	var releases []func()
	for i := 0; i < cap(voiceStreamSlots); i++ {
		release, err := voiceReserveStream()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/voice/stream", nil)
	handleVoiceStream(w, r)
	if w.Code != 429 || voiceRuntime.snapshot()["running"] != false {
		t.Fatal("stream limit did not precede startup", w.Code, w.Body.String())
	}
}
func TestVoicePackOneClickPublishesVerifiedModelsAndConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture version binary is a POSIX shell script")
	}
	voiceTestSetup(t)
	catalogBytes, _ := json.Marshal(curatedVoice)
	var fixtureCatalog voiceCatalog
	if err := json.Unmarshal(catalogBytes, &fixtureCatalog); err != nil {
		t.Fatal(err)
	}
	curatedVoice = fixtureCatalog
	payloads := map[string][]byte{}
	for i, m := range curatedVoice.Models {
		files := map[string]string{}
		for key, pattern := range m.Files {
			name := strings.ReplaceAll(pattern, "*", "")
			if key == "data_dir" {
				name += "/"
			}
			files[name] = "model fixture"
		}
		payload := voiceZip(t, files)
		sum := sha256.Sum256(payload)
		m.Archive = m.ID + ".zip"
		m.URL = "https://voice-fixture.invalid/" + m.Archive
		m.Size = int64(len(payload))
		m.SHA256 = hex.EncodeToString(sum[:])
		curatedVoice.Models[i] = m
		payloads["/"+m.Archive] = payload
	}
	for i, a := range curatedVoice.Engines {
		if a.OS != runtime.GOOS || a.Arch != runtime.GOARCH || a.Provider != "cpu" {
			continue
		}
		payload := voiceZip(t, map[string]string{"bin/sherpa-onnx-version": "#!/bin/sh\nprintf 'sherpa-onnx version : 1.13.8\\n'\n", "lib/" + voiceLibraryName(): "fake shared library"})
		sum := sha256.Sum256(payload)
		a.Archive = "engine.zip"
		a.URL = "https://voice-fixture.invalid/engine.zip"
		a.Size = int64(len(payload))
		a.SHA256 = hex.EncodeToString(sum[:])
		curatedVoice.Engines[i] = a
		payloads["/engine.zip"] = payload
	}
	voiceHTTPClient = &http.Client{Transport: voiceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		payload, ok := payloads[r.URL.Path]
		if !ok {
			return nil, errors.New("unexpected download")
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/voice/packs", strings.NewReader(`{"id":"fr-cpu-small"}`))
	r.Header.Set("Content-Type", "application/json")
	handleVoice(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.After(5 * time.Second)
	for voiceJob.snapshot().Running {
		select {
		case <-deadline:
			t.Fatal("pack did not complete")
		case <-time.After(time.Millisecond):
		}
	}
	if state := voiceJob.snapshot(); state.Error != "" {
		t.Fatal(state)
	}
	c := readVoiceConfig()
	if c.STT != "zipformer-fr" || c.TTS != "piper-fr" || c.VAD != "silero-v5" || c.Language != "fr" {
		t.Fatal(c)
	}
	if !voiceEngineInstalled() || validateVoiceConfig(c, true) != nil {
		t.Fatal("pack published incomplete installation")
	}
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("pack started inference")
	}
}
