package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ReadFunc supplies bounded native reads without application cache or consent.
type ReadFunc func(context.Context, ...string) ([]byte, error)

type QuotaWindow struct {
	Group     string   `json:"group"`
	Name      string   `json:"name"`
	Remaining *float64 `json:"remaining_percent"`
	ResetAt   *int64   `json:"reset_at"`
}

// Quota contains only Antigravity's native observations; the shared account
// snapshot, cache and throttling stay in Loom. Missing credits remain unknown.
type Quota struct {
	Windows   []QuotaWindow
	Credits   *float64
	FetchedAt int64
}

func ReadQuota(ctx context.Context, read ReadFunc) (Quota, error) {
	q := Quota{Windows: []QuotaWindow{}}
	out, err := read(ctx, "-p", "/usage", "--output-format", "json", "--print-timeout", "15s")
	if err != nil {
		return q, err
	}
	var envelope struct {
		Status  string `json:"status"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						Name      string   `json:"name"`
						Remaining *float64 `json:"remaining_fraction"`
						Reset     string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(out, &envelope) != nil || envelope.Status != "SUCCESS" || envelope.Command.Name != "usage" || len(envelope.Command.Data.Groups) > 128 {
		return q, errors.New("format des quotas Antigravity non reconnu")
	}
	for _, g := range envelope.Command.Data.Groups {
		if len(g.Buckets) > 32 {
			return q, errors.New("trop de fenêtres de quota")
		}
		for _, b := range g.Buckets {
			w := QuotaWindow{Group: g.Name, Name: b.Name}
			if b.Remaining != nil && *b.Remaining >= 0 && *b.Remaining <= 1 {
				p := *b.Remaining * 100
				w.Remaining = &p
			}
			if date, err := time.Parse(time.RFC3339, b.Reset); err == nil {
				t := date.Unix()
				w.ResetAt = &t
			}
			q.Windows = append(q.Windows, w)
		}
	}
	if len(q.Windows) == 0 {
		return q, errors.New("aucune fenêtre de quota communiquée")
	}
	// AI credits are not reset credits. Keep the two fields distinct.
	if out, err := read(ctx, "-p", "/credits", "--output-format", "json", "--print-timeout", "15s"); err == nil {
		var c struct {
			Status  string `json:"status"`
			Command struct {
				Name string `json:"name"`
				Data struct {
					Remaining *float64 `json:"remaining_credits"`
				} `json:"data"`
			} `json:"command"`
		}
		if json.Unmarshal(out, &c) == nil && c.Status == "SUCCESS" && c.Command.Name == "credits" {
			q.Credits = c.Command.Data.Remaining
		}
	}
	q.FetchedAt = time.Now().Unix()
	return q, nil
}
