package loom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

var voiceStreamSlots = make(chan struct{}, 8)

func voiceReserveStream() (func(), error) {
	select {
	case voiceStreamSlots <- struct{}{}:
		return func() { <-voiceStreamSlots }, nil
	default:
		return nil, errors.New("voice session limit reached")
	}
}

// Browser input is little-endian PCM16 at 16 kHz mono. No microphone/device,
// discussion, provider, wake word or orchestration is started by this protocol.
func handleVoiceStream(w http.ResponseWriter, r *http.Request) {
	if isEngineWorker() && !nodeVoiceEnabled() {
		sendJSON(w, 403, map[string]any{"ok": false, "error": "node voice module is disabled"})
		return
	}
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	slot, err := voiceReserveStream()
	if err != nil {
		sendJSON(w, 429, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer slot()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	worker, release, err := voiceRuntime.acquire(ctx, "stt")
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	defer release()
	conn, err := web.AcceptWebSocket(w, r, 64<<10)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	session, err := newEngineSecret()
	if err != nil {
		return
	}
	var outMu sync.Mutex
	var generation uint64
	var speechCancel context.CancelFunc
	var speechWG sync.WaitGroup
	defer func() {
		cancel()
		outMu.Lock()
		if speechCancel != nil {
			speechCancel()
		}
		outMu.Unlock()
		speechWG.Wait()
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = worker.request(cleanup, map[string]any{"op": "close", "session": session}, nil)
	}()
	sendLocked := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return conn.Write(writeCtx, websocket.MessageText, data)
	}
	send := func(value any) error { outMu.Lock(); defer outMu.Unlock(); return sendLocked(value) }
	if err := send(map[string]any{"type": "ready", "input": map[string]any{"format": "pcm_s16le", "sample_rate": 16000, "channels": 1}, "stt_streaming": func() bool { m, _ := voiceModel(readVoiceConfig().STT); return m.Streaming }()}); err != nil {
		return
	}
	// Keep active browser streams subject to the same access revocation as chat.
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-worker.done:
				_ = send(map[string]any{"type": "error", "error": "voice STT worker exited"})
				cancel()
				conn.Close(websocket.StatusInternalError, "voice STT worker exited")
				return
			case <-ticker.C:
				if isEngineWorker() && !nodeVoiceEnabled() {
					cancel()
					conn.Close(websocket.StatusPolicyViolation, "voice module disabled")
					return
				}
				if !isEngineWorker() && !usageVaultAccessStream() {
					cancel()
					conn.Close(websocket.StatusPolicyViolation, "access revoked")
					return
				}
			}
		}
	}()
	samples := 0
	result := func(e voiceWorkerEvent) error {
		if e.Type == "partial" || e.Type == "final" {
			if e.Type == "final" {
				samples = 0
			}
			return send(map[string]any{"type": e.Type, "text": e.Text})
		}
		return nil
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if len(data) == 0 || len(data)%2 != 0 {
				_ = send(map[string]any{"type": "error", "error": "PCM frames must contain signed 16-bit samples"})
				continue
			}
			samples += len(data) / 2
			if samples > 16000*60 {
				_ = send(map[string]any{"type": "error", "error": "finish the utterance within 60 seconds"})
				conn.Close(websocket.StatusPolicyViolation, "utterance too long")
				return
			}
			err = voiceStreamRequest(ctx, worker, map[string]any{"op": "pcm", "session": session, "pcm": base64.StdEncoding.EncodeToString(data)}, result)
			if err != nil {
				_ = send(map[string]any{"type": "error", "error": err.Error()})
				return
			}
			continue
		}
		var message struct {
			Type  string               `json:"type"`
			ID    string               `json:"id"`
			Text  string               `json:"text"`
			Voice jarvisVoiceOverrides `json:"voice"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			_ = send(map[string]any{"type": "error", "error": "invalid JSON control frame"})
			continue
		}
		switch message.Type {
		case "finish":
			if err := voiceStreamRequest(ctx, worker, map[string]any{"op": "finish", "session": session}, result); err != nil {
				_ = send(map[string]any{"type": "error", "error": err.Error()})
				return
			}
			samples = 0
			_ = send(map[string]any{"type": "stt_done"})
		case "cancel", "barge_in":
			// Increment while holding the output lock before acknowledging. Old audio
			// can never follow the cancellation acknowledgement.
			outMu.Lock()
			generation++
			if speechCancel != nil {
				speechCancel()
				speechCancel = nil
			}
			_ = sendLocked(map[string]any{"type": "cancelled", "id": message.ID})
			outMu.Unlock()
		case "speak":
			if len(message.ID) > 80 || strings.ContainsAny(message.ID, "\x00\r\n") || len(message.Text) > 4096 || strings.TrimSpace(message.Text) == "" {
				_ = send(map[string]any{"type": "error", "error": "invalid speech id or text"})
				continue
			}
			outMu.Lock()
			if speechCancel != nil {
				outMu.Unlock()
				_ = send(map[string]any{"type": "error", "error": "speech is active; cancel before speaking again"})
				continue
			}
			speechCtx, stop := context.WithCancel(ctx)
			speechCancel = stop
			generation++
			current := generation
			speechWG.Add(1)
			outMu.Unlock()
			go func(id, text string, voice jarvisVoiceOverrides) {
				defer speechWG.Done()
				defer stop()
				started := false
				_, _, err := voiceSynthesizeWithVoice(speechCtx, text, voice, func(e voiceWorkerEvent) error {
					outMu.Lock()
					defer outMu.Unlock()
					if current != generation {
						return context.Canceled
					}
					if e.SampleRate > 0 && !started {
						started = true
						if err := sendLocked(map[string]any{"type": "audio_start", "id": id, "format": "pcm_s16le", "sample_rate": e.SampleRate, "channels": 1}); err != nil {
							return err
						}
					}
					if e.Audio != "" {
						if !started {
							return errors.New("engine sent audio before sample rate")
						}
						pcm, err := base64.StdEncoding.DecodeString(e.Audio)
						if err != nil {
							return err
						}
						writeCtx, cancel := context.WithTimeout(speechCtx, 5*time.Second)
						defer cancel()
						return conn.Write(writeCtx, websocket.MessageBinary, pcm)
					}
					return nil
				})
				outMu.Lock()
				defer outMu.Unlock()
				if current != generation {
					return
				}
				speechCancel = nil
				if err != nil {
					_ = sendLocked(map[string]any{"type": "error", "id": id, "error": err.Error()})
				} else {
					_ = sendLocked(map[string]any{"type": "audio_end", "id": id})
				}
			}(message.ID, message.Text, message.Voice)
		case "ping":
			_ = send(map[string]any{"type": "pong"})
		default:
			_ = send(map[string]any{"type": "error", "error": "unknown voice control type"})
		}
	}
}

func voiceStreamRequest(ctx context.Context, worker *voiceWorker, req any, emit func(voiceWorkerEvent) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return worker.request(ctx, req, emit)
}
