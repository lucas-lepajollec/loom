package loom

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/doctor"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

func nodeVoiceEnabled() bool { return !getBool(bkState, "node_voice_disabled") }

var voiceRoutes = []string{"/api/voice", "/api/voice/node", "/api/voice/models", "/api/voice/models/download", "/api/voice/models/delete", "/api/voice/install", "/api/voice/install/rollback", "/api/voice/packs", "/api/voice/download/status", "/api/voice/download/cancel", "/api/voice/service", "/api/voice/test/tts", "/api/voice/test/stt", "/api/voice/bench", "/api/voice/stream", "/api/voice/doctor"}

func registerVoiceRoutes(api func(string, http.HandlerFunc), node bool) {
	for _, path := range voiceRoutes {
		handler := handleVoice
		if path == "/api/voice/stream" {
			handler = handleVoiceStream
		}
		if !node {
			handler = voiceNodeAware(handler)
		}
		api(path, handler)
	}
}
func voiceSelectedMachine() string {
	target := getStr(bkState, "voice_machine")
	if target == "" {
		target = "local"
	}
	return target
}
func voiceNodeAware(local http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/voice/node" {
			handleVoiceNode(w, r)
			return
		}
		if !usageVaultAccess(w) {
			return
		}
		machine := voiceSelectedMachine()
		// Optional per-request targeting lets the Voice page inspect/install on a
		// machine without changing the saved execution target.
		if target := r.URL.Query().Get("machine"); target != "" {
			machine = target
		}
		if machine == "local" {
			local(w, r)
			return
		}
		access, err := nodeMachineAccessForModule(machine, "voice")
		if err != nil {
			body := map[string]any{"ok": false, "error": err.Error()}
			if code, _, ok := strings.Cut(err.Error(), ":"); ok && strings.HasPrefix(code, "jarvis_") {
				body["code"] = code
			}
			sendJSON(w, 409, body)
			return
		}
		if r.Method == http.MethodPost && voiceInstallPath(r.URL.Path) {
			if err := workspaceSessions.authorizePolicy(r.Context(), policy.Input{Subject: "voice.install", MachineID: machine, Fallback: policy.Allow}, false); err != nil {
				sendJSON(w, 403, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		cloned := r.Clone(r.Context())
		u := *r.URL
		cloned.URL = &u
		q := u.Query()
		q.Del("machine")
		u.RawQuery = q.Encode()
		proxyEngineNode(w, cloned, &engineNode{URL: access.URL, WebKey: access.Token, Hostname: machine, Role: "engine-node"})
	}
}
func handleVoiceNode(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Machine string `json:"machine"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Machine == "" {
			req.Machine = "local"
		}
		if req.Machine != "local" {
			if _, err := nodeMachineAccessForModule(req.Machine, "voice"); err != nil {
				sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		if err := putStr(bkState, "voice_machine", req.Machine); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	} else if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	machines := []map[string]string{{"id": "local", "name": "Local"}}
	for _, m := range loadRemoteMachines() {
		if hasNodeModule(m.Modules, "voice") && savedMachineNode(m) != nil {
			machines = append(machines, map[string]string{"id": m.ID, "name": m.Name})
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "machine": voiceSelectedMachine(), "machines": machines})
}
func voiceInstallPath(path string) bool {
	return path == "/api/voice/install" || path == "/api/voice/packs" || path == "/api/voice/models/download" || path == "/api/voice/install/rollback"
}
func voiceAPIError(w http.ResponseWriter, err error) {
	sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
}
func handleVoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if isEngineWorker() && !nodeVoiceEnabled() {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "node voice module is disabled"})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		workspaceMethod(w, r, http.MethodGet)
		return
	}
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodPost && voiceInstallPath(r.URL.Path) {
		if err := workspaceSessions.authorizePolicy(r.Context(), policy.Input{Subject: "voice.install", MachineID: "local", Fallback: policy.Allow}, false); err != nil {
			sendJSON(w, 403, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	switch r.URL.Path {
	case "/api/voice/doctor":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		checks := map[string]doctor.Check{}
		for _, kind := range []string{"installed", "version", "models", "service"} {
			checks[kind] = doctorVoice(r.Context(), kind)
		}
		sendJSON(w, 200, map[string]any{"ok": true, "checks": checks})
	case "/api/voice/node":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "machine": "local"})
	case "/api/voice":
		if r.Method == http.MethodPost {
			var c voiceConfig
			if !workspaceDecode(w, r, &c) {
				return
			}
			if err := validateVoiceConfig(c, true); err != nil {
				voiceAPIError(w, err)
				return
			}
			if err := voiceRuntime.configure(r.Context(), c); err != nil {
				voiceAPIError(w, err)
				return
			}
		}
		a, err := voiceEngineArtifact(runtime.GOOS, runtime.GOARCH, "cpu")
		sendJSON(w, 200, map[string]any{"ok": true, "engine": map[string]any{"id": "sherpa-onnx", "version": curatedVoice.Version, "installed": voiceEngineInstalled(), "provider": readVoiceEngine().Provider, "selection": readVoiceEngine(), "cpu_installable": err == nil && voiceHashReady(a), "nvidia": voiceNVIDIA()}, "config": readVoiceConfig(), "service": voiceRuntime.snapshot(), "download": voiceJob.snapshot(), "disk_bytes": voiceDiskUsage(voiceRoot()), "capabilities": []string{"stt", "tts", "vad", "stream", "cancel", "bench"}, "runtime_requires": "python3"})
	case "/api/voice/models":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		rows := []map[string]any{}
		max, _ := strconv.ParseInt(r.URL.Query().Get("max_size"), 10, 64)
		for _, m := range curatedVoice.Models {
			if kind := r.URL.Query().Get("kind"); kind != "" && m.Kind != kind {
				continue
			}
			if lang := r.URL.Query().Get("language"); lang != "" {
				match := false
				for _, l := range m.Languages {
					match = match || l == lang || l == "multi"
				}
				if !match {
					continue
				}
			}
			if max > 0 && m.Size > max {
				continue
			}
			rows = append(rows, map[string]any{"id": m.ID, "kind": m.Kind, "languages": m.Languages, "size": m.Size, "streaming": m.Streaming, "family": m.Family, "installed": voiceModelInstalled(m), "disk_bytes": voiceDiskUsage(voiceModelDir(m.ID)), "installable": voiceHashReady(m), "sha256": m.SHA256, "url": m.URL})
		}
		sendJSON(w, 200, map[string]any{"ok": true, "models": rows, "disk_bytes": voiceDiskUsage(voiceRoot())})
	case "/api/voice/packs":
		if r.Method == http.MethodGet {
			rows := []map[string]any{}
			for _, p := range curatedVoice.Packs {
				ready := true
				var size int64
				for _, id := range []string{p.STT, p.TTS, p.VAD} {
					m, _ := voiceModel(id)
					ready = ready && voiceHashReady(m)
					size += m.Size
				}
				e, err := voiceEngineArtifact(runtime.GOOS, runtime.GOARCH, p.Hardware)
				ready = ready && err == nil && voiceHashReady(e)
				rows = append(rows, map[string]any{"pack": p, "size": size, "installable": ready, "requires_nvidia": p.Hardware == "cuda"})
			}
			sendJSON(w, 200, map[string]any{"ok": true, "packs": rows})
			return
		}
		var req struct {
			ID   string `json:"id"`
			CUDA bool   `json:"cuda"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		var p *voicePack
		for _, candidate := range curatedVoice.Packs {
			if candidate.ID == req.ID {
				p = &candidate
				break
			}
		}
		if p == nil {
			voiceAPIError(w, errors.New("unknown voice pack"))
			return
		}
		if p.Hardware == "cuda" && (!req.CUDA || !voiceNVIDIA()) {
			voiceAPIError(w, errors.New("CUDA voice pack requires NVIDIA hardware and explicit cuda:true"))
			return
		}
		a, err := voiceEngineArtifact(runtime.GOOS, runtime.GOARCH, p.Hardware)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		artifacts := []voiceArtifact{a}
		for _, id := range []string{p.STT, p.TTS, p.VAD} {
			m, _ := voiceModel(id)
			artifacts = append(artifacts, m)
		}
		for _, m := range artifacts {
			if !voiceHashReady(m) {
				voiceAPIError(w, fmt.Errorf("verification_unavailable: SHA256 TODO for %s", m.ID))
				return
			}
		}
		err = voiceJob.begin(func(ctx context.Context) error {

			for _, m := range artifacts[1:] {
				if voiceModelInstalled(m) {
					continue
				}
				if err := voiceInstallArtifact(ctx, m, false); err != nil {
					return err
				}
			}
			previousEngine := readVoiceEngine()
			if err := voiceInstallArtifact(ctx, a, true); err != nil {
				return err
			}
			voiceRuntime.mu.Lock()
			defer voiceRuntime.mu.Unlock()
			c := readVoiceConfig()
			c.STT, c.TTS, c.VAD, c.Language = p.STT, p.TTS, p.VAD, p.Language
			if c.Language == "multi" {
				c.Language = "en"
			}
			c.Voice = 0
			if err := putStoreJSON(bkState, "voice_config", c); err != nil {
				_ = putStoreJSON(bkState, "voice_engine_selection", previousEngine)
				return err
			}
			return nil
		})
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 202, map[string]any{"ok": true, "download": voiceJob.snapshot()})
	case "/api/voice/install", "/api/voice/models/download":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			ID   string `json:"id"`
			CUDA bool   `json:"cuda"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		engine := r.URL.Path == "/api/voice/install"
		var a voiceArtifact
		var err error
		if engine {
			provider := "cpu"
			if req.CUDA {
				if !voiceNVIDIA() {
					voiceAPIError(w, errors.New("CUDA requires an NVIDIA GPU"))
					return
				}
				provider = "cuda"
			}
			a, err = voiceEngineArtifact(runtime.GOOS, runtime.GOARCH, provider)
		} else {
			a, err = voiceModel(req.ID)
		}
		if err == nil && !voiceHashReady(a) {
			err = fmt.Errorf("verification_unavailable: SHA256 TODO for %s", a.ID)
		}
		if err == nil {
			err = voiceJob.begin(func(ctx context.Context) error { return voiceInstallArtifact(ctx, a, engine) })
		}
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 202, map[string]any{"ok": true, "download": voiceJob.snapshot()})
	case "/api/voice/download/status":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "download": voiceJob.snapshot()})
	case "/api/voice/download/cancel":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		voiceJob.stop()
		sendJSON(w, 200, map[string]any{"ok": true})
	case "/api/voice/models/delete":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if _, err := voiceModel(req.ID); err != nil {
			voiceAPIError(w, err)
			return
		}
		if err := voiceDeleteModel(req.ID); err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true})
	case "/api/voice/install/rollback":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		if err := voiceRollback(r.Context()); err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true})
	case "/api/voice/service":
		if r.Method == http.MethodPost {
			var req struct {
				Action string `json:"action"`
			}
			if !workspaceDecode(w, r, &req) {
				return
			}
			if err := voiceRuntime.action(r.Context(), req.Action); err != nil {
				voiceAPIError(w, err)
				return
			}
		}
		sendJSON(w, 200, map[string]any{"ok": true, "service": voiceRuntime.snapshot()})
	case "/api/voice/test/tts":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		pcm, rate, err := voiceSynthesize(r.Context(), req.Text, nil)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(voiceWAV(pcm, rate))
	case "/api/voice/test/stt":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "WAV exceeds 60 seconds"})
			return
		}
		pcm, rate, err := voiceReadWAV(raw)
		if err != nil || rate != 16000 {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "expected PCM16 mono WAV at 16000 Hz (at most 60 seconds)"})
			return
		}
		text, err := voiceTranscribe(r.Context(), pcm)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "text": text})
	case "/api/voice/bench":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		// Fixed text for synthesis and a fixed embedded WAV for recognition.
		// No download, microphone, or external speech provider.
		sample := "The quick brown fox jumps over the lazy dog."
		if readVoiceConfig().Language == "fr" {
			sample = "Bonjour, ceci est un test de la voix locale de Loom."
		}
		start := time.Now()
		firstAudio := time.Duration(0)
		pcm, rate, err := voiceSynthesize(r.Context(), sample, func(e voiceWorkerEvent) error {
			if e.Audio != "" && firstAudio == 0 {
				firstAudio = time.Since(start)
			}
			return nil
		})
		ttsTime := time.Since(start)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		duration := float64(len(pcm)) / 2 / float64(rate)
		lang := "en"
		if readVoiceConfig().Language == "fr" {
			lang = "fr"
		}
		wav, _ := voiceWorkerFS.ReadFile("voice/bench-" + lang + ".wav")
		fixture, _, err := voiceReadWAV(wav)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		sttDuration := float64(len(fixture)) / 32000
		start = time.Now()
		text, err := voiceTranscribe(r.Context(), fixture)
		sttTime := time.Since(start)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "sample_text": sample, "sample_source": "embedded_espeak_ng_fixture", "tts_audio_seconds": duration, "stt_audio_seconds": sttDuration, "tts": map[string]any{"latency_ms": ttsTime.Seconds() * 1000, "first_audio_ms": firstAudio.Seconds() * 1000, "real_time_factor": ttsTime.Seconds() / duration}, "stt": map[string]any{"latency_ms": sttTime.Seconds() * 1000, "real_time_factor": sttTime.Seconds() / sttDuration, "text": text}})
	default:
		http.NotFound(w, r)
	}
}
func voiceSynthesize(ctx context.Context, text string, emit func(voiceWorkerEvent) error) ([]byte, int, error) {
	return voiceSynthesizeWithVoice(ctx, text, jarvisVoiceOverrides{}, emit)
}

func voiceSynthesizeWithVoice(ctx context.Context, text string, voice jarvisVoiceOverrides, emit func(voiceWorkerEvent) error) ([]byte, int, error) {
	if strings.TrimSpace(text) == "" || len(text) > 4096 || strings.ContainsRune(text, 0) {
		return nil, 0, errors.New("text must contain 1–4096 bytes without NUL")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := voice.validate(); err != nil {
		return nil, 0, err
	}
	worker, release, c, err := voiceRuntime.acquireVoice(ctx, "tts", voice)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	var audio bytes.Buffer
	rate := 0
	err = worker.request(ctx, map[string]any{"op": "tts", "text": text, "voice": c.Voice, "speed": c.Speed}, func(e voiceWorkerEvent) error {
		if e.SampleRate > 0 {
			rate = e.SampleRate
		}
		if e.Audio != "" {
			pcm, err := base64.StdEncoding.DecodeString(e.Audio)
			if err != nil {
				return err
			}
			if audio.Len()+len(pcm) > 16<<20 {
				return errors.New("generated audio exceeds limit")
			}
			audio.Write(pcm)
		}
		if emit != nil {
			return emit(e)
		}
		return nil
	})
	if err == nil && (rate <= 0 || audio.Len() == 0) {
		err = errors.New("voice engine returned no audio")
	}
	return audio.Bytes(), rate, err
}
func voiceTranscribe(ctx context.Context, pcm []byte) (string, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > 16000*2*60 {
		return "", errors.New("PCM16 input must be 1–60 seconds")
	}
	slot, err := voiceReserveStream()
	if err != nil {
		return "", err
	}
	defer slot()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	worker, release, err := voiceRuntime.acquire(ctx, "stt")
	if err != nil {
		return "", err
	}
	defer release()
	id, err := newEngineSecret()
	if err != nil {
		return "", err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = worker.request(cleanup, map[string]any{"op": "close", "session": id}, nil)
	}()
	var parts []string
	result := func(e voiceWorkerEvent) error {
		if e.Type == "final" && e.Text != "" {
			parts = append(parts, e.Text)
		}
		return nil
	}
	for offset := 0; offset < len(pcm); offset += 3200 {
		end := min(offset+3200, len(pcm))
		if err := worker.request(ctx, map[string]any{"op": "pcm", "session": id, "pcm": base64.StdEncoding.EncodeToString(pcm[offset:end])}, result); err != nil {
			return "", err
		}
	}
	err = worker.request(ctx, map[string]any{"op": "finish", "session": id}, result)
	return strings.Join(parts, " "), err
}
func voiceWAV(pcm []byte, rate int) []byte {
	b := make([]byte, 44+len(pcm))
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	copy(b[44:], pcm)
	return b
}
func voiceReadWAV(b []byte) ([]byte, int, error) {
	bad := errors.New("invalid WAV")
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || int64(binary.LittleEndian.Uint32(b[4:]))+8 != int64(len(b)) {
		return nil, 0, bad
	}
	rate := 0
	valid := false
	var pcm []byte
	for off := 12; off+8 <= len(b); {
		n := int64(binary.LittleEndian.Uint32(b[off+4:]))
		end := int64(off) + 8 + n
		if end > int64(len(b)) {
			return nil, 0, bad
		}
		data := b[off+8 : int(end)]
		switch string(b[off : off+4]) {
		case "fmt ":
			if len(data) < 16 {
				return nil, 0, bad
			}
			valid = binary.LittleEndian.Uint16(data) == 1 && binary.LittleEndian.Uint16(data[2:]) == 1 && binary.LittleEndian.Uint16(data[14:]) == 16
			rate = int(binary.LittleEndian.Uint32(data[4:]))
		case "data":
			pcm = data
		}
		off = int(end + n%2)
	}
	if !valid || len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) > 1920000 {
		return nil, 0, bad
	}
	return pcm, rate, nil
}
