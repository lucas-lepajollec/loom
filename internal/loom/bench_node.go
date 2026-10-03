package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Mixed queues live on the control plane. Only a local row is sent to the
// linked engine. Reuse the node's existing queue API (also works with v0.2.1),
// so neither cloud keys nor native accounts ever cross the engine link.
func benchNodeRequest(ctx context.Context, n engineNode, path string, body any, out any) error {
	method := http.MethodGet
	var data io.Reader
	jobID := ""
	if path == "/api/bench/queue/cancel" {
		if v, ok := body.(map[string]any); ok {
			jobID, _ = v["job_id"].(string)
			body = map[string]any{}
		}
	}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = bytes.NewReader(raw)
		method = http.MethodPost
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, n.URL+path, data)
	if err != nil {
		return errors.New("invalid benchmark node address")
	}
	req.Header.Set("Authorization", "Bearer "+n.WebKey)
	if jobID != "" {
		req.Header.Set("X-Loom-Bench-Job", jobID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := nodeClient.Do(req)
	if err != nil {
		return errors.New("benchmark node unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("benchmark node rejected the request; check its access and version")
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		return err
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out) != nil {
		return errors.New("invalid benchmark node response")
	}
	return nil
}

func runNodeBenchTest(ctx context.Context, t benchTest, row benchJobRow, n engineNode) (*benchResult, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	id := t.ID
	if t.Kind != "perf" && t.ID != benchTestPerf {
		var saved struct {
			OK   bool      `json:"ok"`
			Test benchTest `json:"test"`
		}
		if err := benchNodeRequest(ctx, n, "/api/bench/tests", map[string]any{"name": t.Name, "prompt": t.Prompt, "max_tokens": t.MaxTokens}, &saved); err != nil {
			return nil, "", err
		}
		if !saved.OK || saved.Test.ID == "" {
			return nil, "", errors.New("node could not prepare the benchmark")
		}
		id = saved.Test.ID
		defer func() {
			_ = benchNodeRequest(context.Background(), n, "/api/bench/tests/delete", map[string]string{"id": id}, nil)
		}()
	}
	var response struct {
		ScopedCancel bool      `json:"scoped_cancel"`
		OK           bool      `json:"ok"`
		Job          *benchJob `json:"job"`
	}
	err := benchNodeRequest(ctx, n, "/api/bench/queue", map[string]any{"test_id": id, "models": []benchPick{{Model: row.Model, Preset: row.Preset, Name: row.Name}}}, &response)
	if err != nil {
		return nil, "", err
	}
	if !response.OK || response.Job == nil || response.Job.ID == "" {
		return nil, "", errors.New("node benchmark queue unavailable or busy")
	}
	jobID := response.Job.ID
	// A user may have started a different queue. Never cancel it or adopt its
	// results. Cancel only the still-current job created by this invocation.
	completed := false
	scopedCancel := response.ScopedCancel
	defer func() {
		if completed || !scopedCancel {
			return
		}
		var current struct {
			Job *benchJob `json:"job"`
		}
		if benchNodeRequest(context.Background(), n, "/api/bench/queue", nil, &current) == nil && current.Job != nil && current.Job.ID == jobID && current.Job.Status == "running" {
			_ = benchNodeRequest(context.Background(), n, "/api/bench/queue/cancel", map[string]any{"job_id": jobID}, nil)
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		j := response.Job
		if j == nil || j.ID != jobID || len(j.Rows) != 1 {
			return nil, "", errors.New("node benchmark queue changed")
		}
		if j.Status != "running" {
			completed = true
			r := j.Rows[0]
			if r.Status != "ok" || r.Result == nil {
				return nil, "", errors.New("node benchmark failed or was cancelled; check the model and engine")
			}
			output := r.Output
			if output == "" {
				output = r.Preview
			} // old nodes retained only a short preview.
			if len(output) > 64<<10 {
				return nil, "", errors.New("node benchmark response too large")
			}
			return r.Result, output, nil
		}
		select {
		case <-ctx.Done():
			if !scopedCancel {
				return nil, "", errors.New("main benchmark stopped; this older node needs a manual stop or update")
			}
			return nil, "", ctx.Err()
		case <-ticker.C:
		}
		if err := benchNodeRequest(ctx, n, "/api/bench/queue", nil, &response); err != nil {
			return nil, "", err
		}
	}
}
