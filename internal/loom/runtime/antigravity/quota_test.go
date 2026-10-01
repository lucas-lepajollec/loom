package antigravity

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestQuotaNativeReadCommandsAndUnknownValues(t *testing.T) {
	for _, credit := range []string{"0", "null", "unavailable"} {
		t.Run(credit, func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			q, err := ReadQuota(ctx, func(got context.Context, args ...string) ([]byte, error) {
				command := "/usage"
				if calls == 1 {
					command = "/credits"
				}
				calls++
				if got != ctx || !reflect.DeepEqual(args, []string{"-p", command, "--output-format", "json", "--print-timeout", "15s"}) {
					t.Fatal("native read changed", args)
				}
				if command == "/usage" {
					return []byte(`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"group","buckets":[{"name":"zero","remaining_fraction":0,"reset_time":"2026-09-27T13:00:00Z"},{"name":"unknown","remaining_fraction":null,"reset_time":"invalid"},{"name":"invalid","remaining_fraction":2}]}]}}}`), nil
				}
				if credit == "unavailable" {
					return nil, errors.New("private diagnostic")
				}
				return []byte(`{"status":"SUCCESS","command":{"name":"credits","data":{"remaining_credits":` + credit + `}}}`), nil
			})
			if err != nil || calls != 2 || len(q.Windows) != 3 || q.FetchedAt == 0 || q.Windows[0].Remaining == nil || *q.Windows[0].Remaining != 0 || q.Windows[0].ResetAt == nil || q.Windows[1].Remaining != nil || q.Windows[1].ResetAt != nil || q.Windows[2].Remaining != nil {
				t.Fatalf("quota=%+v err=%v calls=%d", q, err, calls)
			}
			if credit == "0" {
				if q.Credits == nil || *q.Credits != 0 {
					t.Fatal("reported zero credits lost")
				}
			} else if q.Credits != nil {
				t.Fatal("unknown credits became known")
			}
		})
	}
}

func TestQuotaRejectsUsageBeforeCredits(t *testing.T) {
	for _, body := range []string{"bad", `{"status":"ERROR"}`, `{"status":"SUCCESS","command":{"name":"credits"}}`, `{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[]}}}`} {
		calls := 0
		q, err := ReadQuota(context.Background(), func(context.Context, ...string) ([]byte, error) { calls++; return []byte(body), nil })
		if err == nil || calls != 1 || q.Windows == nil || len(q.Windows) != 0 || q.FetchedAt != 0 || q.Credits != nil {
			t.Fatalf("quota=%+v err=%v calls=%d", q, err, calls)
		}
	}
}
