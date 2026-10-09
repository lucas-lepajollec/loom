package loom

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestVoiceWorkerNativeConfigAndCallback(t *testing.T) {
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Skip("Python 3 is required for the voice worker")
	}
	worker, err := voiceWorkerFS.ReadFile("voice/worker.py")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("voice/worker_test.py")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-I", "-c", "__name__ = 'worker_test'\n"+string(worker)+"\n"+string(fixture))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native config/callback fixture: %v\n%s", err, out)
	}
}

func voiceInspectWorker(t *testing.T, worker *voiceWorker) map[string]any {
	t.Helper()
	var config map[string]any
	err := worker.request(context.Background(), map[string]string{"op": "inspect"}, func(e voiceWorkerEvent) error { return json.Unmarshal([]byte(e.Text), &config) })
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestVoiceThreadsReachBothWorkersAndConfigRestarts(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	c := readVoiceConfig()
	c.Threads = 4
	if err := voiceRuntime.configure(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if voiceRuntime.snapshot()["running"] != false {
		t.Fatal("saving stopped configuration started workers")
	}
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	oldSTT, oldTTS := voiceRuntime.stt, voiceRuntime.tts
	for _, worker := range []*voiceWorker{oldSTT, oldTTS} {
		if got := voiceInspectWorker(t, worker)["threads"]; got != float64(4) {
			t.Fatal("startup threads", got)
		}
	}
	c.Threads = 6
	raw, _ := json.Marshal(c)
	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/voice", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		handleVoice(w, r)
		return w
	}
	_, release, err := voiceRuntime.acquire(context.Background(), "stt")
	if err != nil {
		t.Fatal(err)
	}
	if w := post(); w.Code != 409 || readVoiceConfig().Threads != 4 {
		t.Fatal("active request reconfigured", w.Code, w.Body.String())
	}
	release()
	if w := post(); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if oldSTT.alive() || oldTTS.alive() || oldSTT == voiceRuntime.stt || oldTTS == voiceRuntime.tts {
		t.Fatal("thread change did not replace both owned processes")
	}
	for _, worker := range []*voiceWorker{voiceRuntime.stt, voiceRuntime.tts} {
		if got := voiceInspectWorker(t, worker)["threads"]; got != float64(6) {
			t.Fatal("replacement threads", got)
		}
	}
}

func TestJarvisVoiceOverridesAreRequestScoped(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	if err := voiceRuntime.action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	original := readVoiceConfig()
	original.Voice = 3
	original.Speed = 1.1
	if err := voiceRuntime.configure(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	stt, tts := voiceRuntime.stt, voiceRuntime.tts
	// A second installed catalog model uses the same synthetic fixture files.
	model, _ := voiceModel("piper-fr")
	model.ID = "fixture-jarvis-tts"
	curatedVoice.Models = append(curatedVoice.Models, model)
	// Reuse a known installed directory via an in-root copy.
	oldDir, newDir := voiceModelDir("piper-fr"), voiceModelDir(model.ID)
	if err := os.CopyFS(newDir, os.DirFS(oldDir)); err != nil {
		t.Fatal(err)
	}
	zero, speed := 0, 1.25
	override := jarvisVoiceOverrides{TTSModel: model.ID, VoiceID: &zero, Speed: &speed}
	if _, _, err := voiceSynthesizeWithVoice(context.Background(), "hello", override, nil); err != nil {
		t.Fatal(err)
	}
	if voiceRuntime.stt != stt || !stt.alive() || tts.alive() || readVoiceConfig() != original {
		t.Fatal("Jarvis override changed STT or saved engine config")
	}
	if voiceRuntime.tts.config.TTS != model.ID {
		t.Fatal("Jarvis model not loaded")
	}
	speech := voiceInspectWorker(t, voiceRuntime.tts)["last_tts"].(map[string]any)
	if speech["voice"] != float64(0) || speech["speed"] != 1.25 {
		t.Fatal("worker request lost voice override", speech)
	}
	if got := override.apply(original); got.Voice != 0 || got.Speed != 1.25 {
		t.Fatal(got)
	}
	_, release, _, err := voiceRuntime.acquireVoice(context.Background(), "tts", override)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := voiceSynthesize(context.Background(), "hello", nil); err == nil {
		t.Fatal("active TTS replaced")
	}
	release()
	jarvisTTS := voiceRuntime.tts
	if _, _, err := voiceSynthesize(context.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if jarvisTTS.alive() || voiceRuntime.tts.config.TTS != original.TTS || readVoiceConfig() != original {
		t.Fatal("ordinary speech did not restore engine config")
	}
	speech = voiceInspectWorker(t, voiceRuntime.tts)["last_tts"].(map[string]any)
	if speech["voice"] != float64(3) || speech["speed"] != 1.1 {
		t.Fatal("ordinary speech did not inherit voice/speed", speech)
	}
}

func TestVoiceBenchMeasuresFirstPCMNotAnnouncement(t *testing.T) {
	voiceTestSetup(t)
	voiceTestModels(t)
	t.Setenv("LOOM_VOICE_FAKE_TIMINGS", "1")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/voice/bench", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	handleVoice(w, r)
	var result struct {
		TTS struct {
			Latency float64 `json:"latency_ms"`
			First   float64 `json:"first_audio_ms"`
		} `json:"tts"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if result.TTS.First < 70 || result.TTS.Latency-result.TTS.First < 90 {
		t.Fatal("first audio should exclude the rate announcement and precede completion", w.Body.String())
	}
}
