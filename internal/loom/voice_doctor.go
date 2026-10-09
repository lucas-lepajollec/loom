package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/doctor"
)

func voiceDoctorJobs() []doctor.Job {
	jobs := []doctor.Job{}
	for _, kind := range []string{"installed", "version", "models", "service"} {
		jobs = append(jobs, doctor.Job{ID: "voice." + kind, Area: "voice", Run: func(ctx context.Context) doctor.Check { return doctorVoice(ctx, kind) }})
	}
	return jobs
}
func doctorVoice(ctx context.Context, kind string) doctor.Check {
	const href = "#/models"
	if !usageVaultAccessStream() {
		return doctorCheck("skip", "vault_locked", "")
	}
	if machine := voiceSelectedMachine(); !isEngineWorker() && machine != "local" {
		access, err := nodeMachineAccessForModule(machine, "voice")
		if err != nil {
			return doctorCheck("fail", "voice_node_unavailable", href)
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, access.URL+"/api/voice/doctor", nil)
		req.Header.Set("Authorization", "Bearer "+access.Token)
		resp, err := nodeClient.Do(req)
		if err != nil {
			return doctorCheck("fail", "voice_node_unreachable", href)
		}
		defer resp.Body.Close()
		var response struct {
			Checks map[string]doctor.Check `json:"checks"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&response) != nil {
			return doctorCheck("fail", "voice_node_invalid_status", href)
		}
		check, ok := response.Checks[kind]
		if !ok {
			return doctorCheck("fail", "voice_node_invalid_status", href)
		}
		return check
	}
	if !voiceEngineInstalled() {
		return doctorCheck("skip", "voice_engine_not_installed", href)
	}
	switch kind {
	case "version":
		if _, err := voiceCheckVersion(ctx, voiceEngineDir()); err != nil {
			return doctorCheck("fail", "voice_version_check_failed", href)
		}
	case "models":
		c := readVoiceConfig()
		c.Boot = true
		if validateVoiceConfig(c, true) != nil {
			return doctorCheck("warn", "voice_models_missing_or_not_selected", href)
		}
	case "service":
		s := voiceRuntime.snapshot()
		if s["error"] != "" {
			return doctorCheck("fail", "voice_service_failed", href)
		}
		if s["running"] != true {
			return doctorCheck("skip", "voice_service_stopped", href)
		}
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := voiceRuntime.health(probe); errors.Is(err, errVoiceHealthBusy) {
			return doctorCheck("skip", "voice_service_in_use", "")
		} else if err != nil {
			return doctorCheck("fail", "voice_service_unreachable", href)
		}
	}
	return doctorCheck("ok", "voice_"+kind, "")
}
