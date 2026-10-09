package doctor

import (
	"context"
	"testing"
	"time"
)

func TestConcurrentBoundedChecksAndRerun(t *testing.T) {
	gate := make(chan struct{})
	jobs := []Job{{ID: "good", Area: "core", Run: func(context.Context) Check { return Check{Status: "ok", Detail: "observed"} }}}
	for _, id := range []string{"slow-a", "slow-b", "slow-c"} {
		jobs = append(jobs, Job{ID: id, Area: "agents", Run: func(context.Context) Check { <-gate; return Check{Status: "ok"} }})
	}
	defer close(gate)
	start := time.Now()
	r := Run(context.Background(), jobs, "", 30*time.Millisecond)
	if time.Since(start) > 150*time.Millisecond || len(r.Checks) != 4 || r.At == 0 {
		t.Fatal(r, time.Since(start))
	}
	for _, c := range r.Checks {
		if c.ID != "good" && (c.Status != "warn" || c.Detail != "check_timeout") {
			t.Fatal(c)
		}
	}
	r = Run(context.Background(), jobs, "good", time.Second)
	if len(r.Checks) != 1 || r.Checks[0].ID != "good" || r.Checks[0].Status != "ok" {
		t.Fatal(r)
	}
}
func TestCancelledCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Run(ctx, []Job{{ID: "machine", Area: "machines", Run: func(ctx context.Context) Check { <-ctx.Done(); return Check{Status: "warn"} }}}, "", time.Second)
	if len(r.Checks) != 1 || r.Checks[0].Status != "warn" {
		t.Fatal(r)
	}
}
