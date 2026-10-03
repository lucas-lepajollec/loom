package discussion

import "sort"

type UsagePrice struct {
	ChoiceID  string  `json:"choice_id"`
	Input     float64 `json:"input_per_million"`
	Output    float64 `json:"output_per_million"`
	Currency  string  `json:"currency"`
	UpdatedAt int64   `json:"updated_at"`
}
type ModelUsageSummary[Usage any] struct {
	ChoiceID      string      `json:"choice_id"`
	Name          string      `json:"name"`
	Provider      string      `json:"provider"`
	RuntimeID     string      `json:"runtime_id"`
	Kind          string      `json:"kind"` // execution type, independent of model name
	Turns         int         `json:"turns"`
	Reported      int         `json:"reported_turns"`
	Usage         Usage       `json:"usage"`
	Price         *UsagePrice `json:"price"`
	EstimatedCost *float64    `json:"estimated_cost"`
	// Cost declared by the harness itself (ACP usage_update), summed over
	// discussions; nil when no harness reported one.
	ReportedCost *float64 `json:"reported_cost"`
	Currency     string   `json:"currency,omitempty"`
}

// UsageOps preserves the application's usage type, including private presence
// flags. Unknown/negative usage stays unreported; aggregation adds tokens only.
type UsageOps[Usage any] struct {
	Valid func(Usage) bool
	Add   func(*Usage, Usage)
}

func AccumulateTurnUsage[Usage, Stats any](rows map[string]*ModelUsageSummary[Usage], sessions []RuntimeSession[Usage, Stats], isHarness func(string) bool, choiceID func(string, string) string, ops UsageOps[Usage]) {
	for _, s := range sessions {
		for i, t := range s.Turns {
			if t.RuntimeID == "llama.cpp" {
				continue
			}
			id := choiceID(t.ProviderID, t.Model)
			if isHarness(t.RuntimeID) {
				id = t.RuntimeID + ":" + t.Model
			}
			row := rows[id]
			if row == nil {
				row = &ModelUsageSummary[Usage]{ChoiceID: id, Name: t.Model, Provider: t.ProviderName, RuntimeID: t.RuntimeID}
				rows[id] = row
			}
			row.Turns++
			row.Kind = "cloud"
			if isHarness(t.RuntimeID) {
				row.Kind = "harness"
			}
			u := t.Usage
			if u == nil && i == len(s.Turns)-1 {
				u = s.Usage
			}
			if u != nil && ops.Valid(*u) {
				row.Reported++
				ops.Add(&row.Usage, *u)
			}
		}
	}
}

func AccumulateHarnessCost[Usage, Stats any](rows map[string]*ModelUsageSummary[Usage], sessions []RuntimeSession[Usage, Stats], isHarness func(string) bool) {
	// The harness cost is cumulative per native session: attribute it to the
	// choice of the discussion's last harness turn.
	for _, s := range sessions {
		cost, _ := s.ACPUsage["cost"].(map[string]any)
		amount, ok := cost["amount"].(float64)
		if !ok || len(s.Turns) == 0 {
			continue
		}
		t := s.Turns[len(s.Turns)-1]
		if row := rows[t.RuntimeID+":"+t.Model]; row != nil && isHarness(t.RuntimeID) {
			v := amount
			if row.ReportedCost != nil {
				v += *row.ReportedCost
			}
			row.ReportedCost = &v
			row.Currency, _ = cost["currency"].(string)
		}
	}
}

func UsageSummaries[Usage any](rows map[string]*ModelUsageSummary[Usage], isHarness func(string) bool, estimate func(Usage, UsagePrice) float64) []ModelUsageSummary[Usage] {
	out := []ModelUsageSummary[Usage]{}
	for _, r := range rows {
		if isHarness(r.RuntimeID) && r.Turns == 0 {
			continue
		}
		if r.Price != nil && r.Reported > 0 {
			v := estimate(r.Usage, *r.Price)
			r.EstimatedCost = &v
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider+out[i].Name < out[j].Provider+out[j].Name })
	return out
}
