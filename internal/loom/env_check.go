package loom

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type EnvCheckResult struct {
	From      string `json:"from"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error"`
}

func (e *environment) check(ctx context.Context, target, from string) (result EnvCheckResult) {
	if from == "" {
		from = "local"
	}
	result.From = from
	start := time.Now()
	defer func() { result.LatencyMS = time.Since(start).Milliseconds() }()
	t, err := parseEnvTarget(target)
	if err != nil {
		result.Error = err.Error()
		return
	}
	m, err := e.machine(from)
	if err != nil {
		result.Error = err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case e.checks <- struct{}{}:
		defer func() { <-e.checks }()
	case <-ctx.Done():
		result.Error = ctx.Err().Error()
		return
	}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return
	}
	if m != nil {
		result.Status, err = e.remoteCheck(ctx, *m, t)
	} else if t.url != "" {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		if err == nil {
			client := &http.Client{Transport: e.transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			var resp *http.Response
			resp, err = client.Do(req)
			if err == nil {
				result.Status = resp.StatusCode
				_ = resp.Body.Close()
			}
		}
	} else {
		var conn net.Conn
		conn, err = e.dial(ctx, "tcp", net.JoinHostPort(t.host, t.port))
		if err == nil {
			_ = conn.Close()
		}
	}
	if err != nil {
		result.Error = err.Error()
		return
	}
	result.OK = result.Status == 0 || (result.Status >= 200 && result.Status < 400)
	if !result.OK {
		result.Error = fmt.Sprintf("HTTP %d", result.Status)
	}
	return
}

func (e *environment) remoteCheck(ctx context.Context, m RemoteMachine, t envTarget) (int, error) {
	// Host and port are arguments to bash, never interpolated in its program.
	// curl failures do not become successful TCP checks; fallback is only for
	// machines without curl. The surrounding SSH context also bounds /dev/tcp.
	tcp := "if command -v bash >/dev/null 2>&1; then\n" +
		"bash -c 'exec 3<>/dev/tcp/\"$1\"/\"$2\"' -- " + shellQuote(t.host) + " " + shellQuote(t.port) + " && printf 'LOOM-TCP\\n'\n" +
		"else echo 'bash is required for TCP checks' >&2; exit 1; fi\n"
	script := tcp
	if t.url != "" {
		script = "if command -v curl >/dev/null 2>&1; then\n" +
			"code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 -- " + shellQuote(t.url) + ") || exit $?\n" +
			"printf 'LOOM-HTTP %s\\n' \"$code\"\nelse\n" + tcp + "fi\n"
	}
	out, err := e.remoteCommand(ctx, m, script)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "LOOM-TCP" {
			return 0, nil
		}
		if strings.HasPrefix(line, "LOOM-HTTP ") {
			status, err := strconv.Atoi(strings.TrimPrefix(line, "LOOM-HTTP "))
			if err == nil && status >= 100 && status <= 599 {
				return status, nil
			}
		}
	}
	return 0, errors.New("invalid remote reachability response")
}

func (e *environment) checkHandler(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Target string `json:"target"`
		From   string `json:"from"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if _, err := parseEnvTarget(req.Target); err != nil {
		envFailure(w, 400, err)
		return
	}
	if _, err := e.machine(req.From); err != nil {
		envFailure(w, 400, err)
		return
	}
	sendJSON(w, 200, e.check(r.Context(), req.Target, req.From))
}

type EnvMatrixRow struct {
	ServiceID string `json:"service_id"`
	Target    string `json:"target"`
	EnvCheckResult
}

func (e *environment) matrix(ctx context.Context, services []EnvService, machines []RemoteMachine) []EnvMatrixRow {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	from := []string{"local"}
	seen := map[string]bool{"local": true}
	for _, m := range machines {
		if !seen[m.ID] {
			seen[m.ID] = true
			from = append(from, m.ID)
		}
	}
	rows := make([]EnvMatrixRow, 0, len(services)*len(from))
	for _, s := range services {
		for _, f := range from {
			rows = append(rows, EnvMatrixRow{ServiceID: s.ID, Target: s.URL, EnvCheckResult: EnvCheckResult{From: f}})
		}
	}
	// A fixed worker pool creates no goroutine per cell and preserves row order.
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < envConcurrency; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rows[i].EnvCheckResult = e.check(ctx, rows[i].Target, rows[i].From)
			}
		}()
	}
	for i := range rows {
		select {
		case jobs <- i:
		case <-ctx.Done():
			for j := i; j < len(rows); j++ {
				rows[j].Error = ctx.Err().Error()
			}
			close(jobs)
			wg.Wait()
			return rows
		}
	}
	close(jobs)
	wg.Wait()
	return rows
}

func (e *environment) matrixHandler(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	services, err := envServices()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "results": e.matrix(r.Context(), services, e.machines())})
}
