package discussion

import "testing"

func TestRetainedUsageUnknownZeroFallbackAndHarnessCosts(t *testing.T) {
	harness := func(id string) bool { return id == "h" }
	choice := func(provider, model string) string { return provider + ":" + model }
	rows := map[string]*ModelUsageSummary[testUsage]{
		"p:m":       {ChoiceID: "p:m", Name: "m", Provider: "B", RuntimeID: "cloud", Price: &UsagePrice{Input: 1, Output: 2}},
		"p:unknown": {ChoiceID: "p:unknown", Name: "unknown", Provider: "Z", RuntimeID: "cloud", Price: &UsagePrice{Input: 1}},
		"h:unused":  {RuntimeID: "h"},
	}
	ops := UsageOps[testUsage]{
		Valid: func(u testUsage) bool { return u.Input >= 0 && u.Output >= 0 && u.Total >= 0 },
		Add: func(sum *testUsage, u testUsage) {
			sum.Input += u.Input
			sum.Output += u.Output
			sum.Total += u.Total
			sum.Thinking += u.Thinking
			sum.Cached += u.Cached
		},
	}
	sessions := []RuntimeSession[testUsage, testStats]{
		{Turns: []RuntimeTurnRecord[testUsage, testStats]{
			{RuntimeID: "llama.cpp", ProviderID: "local", Model: "skip", Usage: &testUsage{Input: 999}},
			{RuntimeID: "cloud", ProviderID: "p", Model: "m", Usage: &testUsage{Input: 100, Output: 50, Total: 150, Thinking: 4, Cached: 3}},
			{RuntimeID: "cloud", ProviderID: "p", Model: "m"}, // unknown does not take the session usage before the last turn
			{RuntimeID: "cloud", ProviderID: "p", Model: "m", Usage: &testUsage{Input: -1}},
			{RuntimeID: "cloud", ProviderID: "p", Model: "m", Usage: &testUsage{}}, // reported zero
			{RuntimeID: "cloud", ProviderID: "p", Model: "m"},
		}, Usage: &testUsage{Input: 10, Output: 5, Total: 15}},
		{Turns: []RuntimeTurnRecord[testUsage, testStats]{{RuntimeID: "cloud", ProviderID: "p", Model: "unknown"}}},
		{ACPState: ACPState{ACPUsage: map[string]any{"cost": map[string]any{"amount": 2.5, "currency": "USD"}}}, Turns: []RuntimeTurnRecord[testUsage, testStats]{{RuntimeID: "h", Model: "old"}, {RuntimeID: "h", Model: "m", ProviderName: "A"}}},
		{ACPState: ACPState{ACPUsage: map[string]any{"cost": map[string]any{"amount": 0.5, "currency": "USD"}}}, Turns: []RuntimeTurnRecord[testUsage, testStats]{{RuntimeID: "h", Model: "m"}}},
		{ACPState: ACPState{ACPUsage: map[string]any{"cost": map[string]any{"amount": 99.0}}}, Turns: []RuntimeTurnRecord[testUsage, testStats]{{RuntimeID: "cloud", ProviderID: "p", Model: "m"}}},
	}
	AccumulateTurnUsage(rows, sessions, harness, choice, ops)
	if rows["p:m"].Kind != "cloud" || rows["h:m"].Kind != "harness" {
		t.Fatal("usage confused cloud execution with a native harness")
	}
	AccumulateHarnessCost(rows, sessions, harness)
	estimates := 0
	out := UsageSummaries(rows, harness, func(u testUsage, p UsagePrice) float64 {
		estimates++
		return (float64(u.Input)*p.Input + float64(u.Output)*p.Output) / 1e6
	})
	if rows["local:skip"] != nil || rows["p:m"].Turns != 6 || rows["p:m"].Reported != 3 || rows["p:m"].Usage.Total != 165 || rows["p:m"].Usage.Thinking != 4 || rows["p:m"].Usage.Cached != 3 || *rows["p:m"].EstimatedCost != 0.00022 || rows["p:m"].ReportedCost != nil {
		t.Fatalf("retained usage %+v", rows["p:m"])
	}
	if rows["p:unknown"].EstimatedCost != nil || rows["p:unknown"].Reported != 0 || estimates != 1 {
		t.Fatal("unknown usage became a zero estimate")
	}
	if rows["h:old"].ReportedCost != nil || rows["h:m"].ReportedCost == nil || *rows["h:m"].ReportedCost != 3 || rows["h:m"].Currency != "USD" {
		t.Fatal("cumulative cost lost or duplicated across turns")
	}
	if len(out) != 4 || out[0].ChoiceID != "h:m" || out[1].ChoiceID != "p:m" || out[2].ChoiceID != "p:unknown" || out[3].ChoiceID != "h:old" {
		t.Fatalf("order/unused harness row: %+v", out)
	}
}
