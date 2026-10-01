package llamacpp

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

const srvHistMax = 40

type Slot struct {
	ID           int
	IDTask       int
	NCtx         int
	IsProcessing bool
	NDecoded     int
	NRemain      int
	PromptN      int
	PredTokS     float64
}

type slotWatch struct {
	slot     Slot
	started  time.Time
	lastN    int
	lastT    time.Time
	emaTokS  float64
	recorded bool
}

type Recent struct {
	Slot    int     `json:"slot"`
	Task    int     `json:"task"`
	Tokens  int     `json:"tokens"`
	Prompt  int     `json:"prompt"`
	Ended   int64   `json:"ended"`
	Started int64   `json:"started,omitempty"`
	Ms      int64   `json:"ms"`
	TokS    float64 `json:"toks"`
	State   string  `json:"state"`
}

func (o *Supervisor) ResetServerWatch() {
	o.slotsMu.Lock()
	defer o.slotsMu.Unlock()
	o.slots = map[int]slotWatch{}
	o.recent = nil
	o.completed = 0
	o.genTok = 0
	o.promptTok = 0
	o.tokSSum = 0
	o.sessionAt = time.Now()
}

func anyInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case bool:
		if t {
			return 1
		}
	}
	return 0
}

func anyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return 0
}

func anyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case json.Number:
		n, _ := t.Int64()
		return n != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	}
	return false
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func extractNextToken(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if arr, ok := v.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			return m
		}
	}
	return nil
}

func slotFromMap(m map[string]any) Slot {
	nt := extractNextToken(m["next_token"])
	tm := asMap(m["timings"])
	s := Slot{
		ID:           anyInt(m["id"]),
		NCtx:         anyInt(m["n_ctx"]),
		IsProcessing: anyBool(m["is_processing"]),
	}
	if v, ok := m["id_task"]; ok {
		s.IDTask = anyInt(v)
	} else {
		s.IDTask = anyInt(m["task_index"])
	}
	s.NDecoded = anyInt(m["n_decoded"])
	if s.NDecoded == 0 && nt != nil {
		s.NDecoded = anyInt(nt["n_decoded"])
	}
	if s.NDecoded == 0 && tm != nil {
		s.NDecoded = anyInt(tm["predicted_n"])
	}
	if nt != nil {
		s.NRemain = anyInt(nt["n_remain"])
	}
	s.PromptN = anyInt(m["n_prompt_tokens"])
	if s.PromptN == 0 {
		s.PromptN = anyInt(m["n_prompt_tokens_processed"])
	}
	if s.PromptN == 0 && tm != nil {
		s.PromptN = anyInt(tm["prompt_n"])
	}
	if tm != nil {
		s.PredTokS = anyFloat(tm["predicted_per_second"])
	}
	return s
}

func ParseSlots(raw []byte) []Slot {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '{' {
		var wrap map[string]any
		if json.Unmarshal(raw, &wrap) != nil {
			return nil
		}
		if inner, ok := wrap["slots"]; ok {
			b, err := json.Marshal(inner)
			if err != nil {
				return nil
			}
			return ParseSlots(b)
		}
		return nil
	}
	if raw[0] != '[' {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := make([]Slot, 0, len(arr))
	for _, m := range arr {
		out = append(out, slotFromMap(m))
	}
	return out
}

func roundTokS(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*10) / 10
}

func (o *Supervisor) NoteServerSlots(slots []Slot) ([]Recent, []map[string]any) {
	now := time.Now()
	o.slotsMu.Lock()
	defer o.slotsMu.Unlock()
	seen := map[int]bool{}
	items := make([]map[string]any, 0, len(slots))
	for _, s := range slots {
		seen[s.ID] = true
		prev, ok := o.slots[s.ID]
		finished := ok && prev.slot.IDTask != 0 && !prev.recorded && (s.IDTask != prev.slot.IDTask || (!s.IsProcessing && prev.slot.IsProcessing))
		if finished {
			prev.recorded = true
			ms := int64(0)
			if !prev.started.IsZero() {
				ms = now.Sub(prev.started).Milliseconds()
				if ms < 0 {
					ms = 0
				}
			}
			decoded := prev.slot.NDecoded
			if s.NDecoded > decoded {
				decoded = s.NDecoded
			}
			promptN := prev.slot.PromptN
			if s.PromptN > promptN {
				promptN = s.PromptN
			}
			toks := 0.0
			if prev.slot.PredTokS > 0 {
				toks = prev.slot.PredTokS
			} else if prev.emaTokS > 0 {
				toks = prev.emaTokS
			} else if ms > 0 && decoded > 0 {
				toks = float64(decoded) / (float64(ms) / 1000)
			}
			toks = roundTokS(toks)
			startedMs := int64(0)
			if !prev.started.IsZero() {
				startedMs = prev.started.UnixMilli()
			}
			rec := Recent{
				Slot:    prev.slot.ID,
				Task:    prev.slot.IDTask,
				Tokens:  decoded,
				Prompt:  promptN,
				Ended:   now.UnixMilli(),
				Started: startedMs,
				Ms:      ms,
				TokS:    toks,
				State:   "terminé",
			}
			o.recent = append(o.recent, rec)
			if len(o.recent) > srvHistMax {
				o.recent = o.recent[len(o.recent)-srvHistMax:]
			}
			o.completed++
			o.genTok += decoded
			o.promptTok += promptN
			if toks > 0 {
				o.tokSSum += toks
			}
		}
		newTask := !ok || finished || (s.IDTask != 0 && s.IDTask != prev.slot.IDTask)
		startedBusy := s.IsProcessing && (!ok || !prev.slot.IsProcessing || newTask)
		if startedBusy {
			prev.started = now
			prev.lastN = s.NDecoded
			prev.lastT = now
			prev.emaTokS = 0
			prev.recorded = false
		} else if ok && s.IsProcessing {
			dt := now.Sub(prev.lastT).Seconds()
			if dt > 0.04 && s.NDecoded >= prev.lastN {
				inst := float64(s.NDecoded-prev.lastN) / dt
				if prev.emaTokS <= 0 {
					prev.emaTokS = inst
				} else {
					prev.emaTokS = 0.45*inst + 0.55*prev.emaTokS
				}
			}
			prev.lastN = s.NDecoded
			prev.lastT = now
		}
		prev.slot = s
		if !s.IsProcessing {
			prev.emaTokS = 0
		}
		o.slots[s.ID] = prev

		toks := 0.0
		if s.IsProcessing {
			if s.PredTokS > 0 {
				toks = s.PredTokS
			} else {
				toks = prev.emaTokS
			}
		}
		elapsed := int64(0)
		if s.IsProcessing && !prev.started.IsZero() {
			elapsed = now.Sub(prev.started).Milliseconds()
		}
		items = append(items, map[string]any{
			"id":         s.ID,
			"task":       s.IDTask,
			"busy":       s.IsProcessing,
			"tokens":     s.NDecoded,
			"prompt":     s.PromptN,
			"ctx":        s.NCtx,
			"remaining":  s.NRemain,
			"toks":       roundTokS(toks),
			"elapsed_ms": elapsed,
		})
	}
	for id := range o.slots {
		if !seen[id] {
			delete(o.slots, id)
		}
	}
	out := make([]Recent, len(o.recent))
	copy(out, o.recent)
	return out, items
}

func (o *Supervisor) ServerStats(busy int, liveTokS float64) map[string]any {
	o.slotsMu.Lock()
	defer o.slotsMu.Unlock()
	avg := 0.0
	if o.completed > 0 && o.tokSSum > 0 {
		avg = o.tokSSum / float64(o.completed)
	}
	return map[string]any{
		"inflight":  busy,
		"completed": o.completed,
		"tokens":    o.genTok,
		"prompt":    o.promptTok,
		"avg_toks":  roundTokS(avg),
		"live_toks": roundTokS(liveTokS),
		"since":     o.sessionAt.UnixMilli(),
	}
}
