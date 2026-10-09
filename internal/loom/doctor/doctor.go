// Package doctor runs bounded, read-only support checks. Checks never start a
// model, install software, sign in or generate a response.
package doctor

import (
	"context"
	"sort"
	"sync"
	"time"
)

type Fix struct {
	Label  string `json:"label"`
	Action string `json:"action,omitempty"`
	Href   string `json:"href,omitempty"`
}
type Check struct {
	ID     string `json:"id"`
	Area   string `json:"area"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Fix    *Fix   `json:"fix,omitempty"`
}
type Report struct {
	Checks []Check `json:"checks"`
	At     int64   `json:"at"`
}
type Job struct {
	ID, Area string
	Run      func(context.Context) Check
}

func Run(ctx context.Context, jobs []Job, id string, timeout time.Duration) Report {
	out := Report{Checks: []Check{}, At: time.Now().UnixMilli()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, job := range jobs {
		if id != "" && job.ID != id {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			ready := make(chan Check, 1)
			go func() { ready <- job.Run(checkCtx) }()
			var check Check
			select {
			case check = <-ready:
			case <-checkCtx.Done():
				check = Check{Status: "warn", Detail: "check_timeout"}
			}
			check.ID, check.Area = job.ID, job.Area
			if check.Title == "" {
				check.Title = job.ID
			}
			mu.Lock()
			out.Checks = append(out.Checks, check)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(out.Checks, func(i, j int) bool { return out.Checks[i].ID < out.Checks[j].ID })
	return out
}
