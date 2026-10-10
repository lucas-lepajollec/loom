package loom

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

// Narrow read-only operations on existing modules. No arbitrary argv, shell,
// environment values, sign-in, generation or control-plane state are exposed.
func nodeObserveRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return false
	}
	if !nodeObserveEnabled() {
		sendJSON(w, 409, map[string]any{"ok": false, "unsupported": true, "error": nodeObserveDisabled})
		return false
	}
	return true
}
func (e *environment) nodeDocker(w http.ResponseWriter, r *http.Request) {
	if !nodeObserveRequest(w, r) {
		return
	}
	containers, err := e.readDocker(r.Context(), nil)
	result := map[string]any{"ok": err == nil, "containers": containers, "error": "", "unsupported": false, "transport": "node"}
	if err != nil {
		result["error"], result["containers"] = err.Error(), []EnvContainer{}
	}
	sendJSON(w, 200, result)
}
func (e *environment) nodeCheck(w http.ResponseWriter, r *http.Request) {
	if !nodeObserveRequest(w, r) {
		return
	}
	target := r.URL.Query().Get("target")
	if _, err := parseEnvTarget(target); err != nil {
		envFailure(w, 400, err)
		return
	}
	result := e.check(r.Context(), target, "local")
	result.Transport = "node"
	sendJSON(w, 200, result)
}

// Both Node and main construct the same fixed command. SSH quoting stays in
// harnessUsageCommand; the HTTP bridge carries only a validated reading name.
func nativeReadArgv(harness, reading string, days int) ([]string, error) {
	switch {
	case reading == "stats" && harness == "hermes":
		days, err := usageDays(days)
		if err != nil {
			return nil, err
		}
		return []string{"hermes", "insights", "--days", strconv.Itoa(days)}, nil
	case reading == "stats" && harness == "opencode":
		days, err := usageDays(days)
		if err != nil {
			return nil, err
		}
		return []string{"opencode", "stats", "--days", strconv.Itoa(days), "--models"}, nil
	case reading == "quota" && harness == "hermes":
		return []string{"hermes", "usage"}, nil
	}
	return nil, errors.New("read-only native reading unsupported on this transport")
}
func nodeUsageCommand(ctx context.Context, m RemoteMachine, harness string, argv []string) ([]byte, error) {
	reading := ""
	days := 0
	if len(argv) >= 4 {
		days, _ = strconv.Atoi(argv[3])
	}
	for _, kind := range []string{"stats", "quota"} {
		expected, err := nativeReadArgv(harness, kind, days)
		if err == nil && reflect.DeepEqual(expected, argv) {
			reading = kind
			break
		}
	}
	if reading == "" {
		return nil, errors.New("read-only native reading unsupported on this transport")
	}
	var result struct {
		OK     bool   `json:"ok"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	path := "/api/node/harness/usage?" + url.Values{"harness": {harness}, "reading": {reading}, "days": {strconv.Itoa(days)}}.Encode()
	if err := nodeMachineJSON(ctx, m, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		if result.Error == "" {
			return nil, errors.New("node native reading unavailable")
		}
		return nil, errors.New(result.Error)
	}
	return []byte(result.Output), nil
}
func handleNodeUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !nodeHarnessRequest(w, r) {
		return
	}
	harness, reading := r.URL.Query().Get("harness"), r.URL.Query().Get("reading")
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if r.URL.Query().Get("days") == "" {
		days, err = 0, nil
	}
	if err != nil {
		envFailure(w, 400, errors.New("days must be 7 or 30"))
		return
	}
	argv, err := nativeReadArgv(harness, reading, days)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "unsupported": true, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), nativeUsageTimeout)
	defer cancel()
	out, err := harnessUsageCommand(ctx, acpAgent{ID: harness}, argv)
	// JSON escaping remains within the bridge's 1 MiB response bound.
	if len(out) > 128<<10 {
		err = errors.New("native reading exceeds 128 KiB")
	}
	result := map[string]any{"ok": err == nil, "output": "", "error": "", "unsupported": false}
	if err != nil {
		result["error"] = err.Error()
	} else {
		result["output"] = string(out)
	}
	sendJSON(w, 200, result)
}
