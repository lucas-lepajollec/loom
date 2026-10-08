package brain

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

type ContinuityState struct {
	Objective string   `json:"objective"`
	Done      []string `json:"done"`
	Next      []string `json:"next"`
	Open      []string `json:"open"`
}
type ContinuityFact struct {
	Class string `json:"class"`
	Text  string `json:"text"`
}
type ContinuityResult struct {
	Summary string           `json:"summary"`
	State   ContinuityState  `json:"state"`
	Facts   []ContinuityFact `json:"facts"`
}

func (s ContinuityState) Markdown() string {
	text := "## Objective\n\n" + s.Objective
	for _, section := range []struct {
		name   string
		values []string
	}{{"Done", s.Done}, {"Next", s.Next}, {"Open", s.Open}} {
		text += "\n\n## " + section.name + "\n"
		for _, value := range section.values {
			text += "\n- " + value
		}
	}
	return text
}
func ParseContinuity(raw string) (ContinuityResult, error) {
	var result ContinuityResult
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return result, errors.New("continuity requires strict JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || result.State.Done == nil || result.State.Next == nil || result.State.Open == nil || result.Facts == nil || len(result.Facts) > 32 {
		return result, errors.New("invalid continuity fields")
	}
	valid := func(text string) bool {
		return strings.TrimSpace(text) != "" && len(text) <= 8192 && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
	}
	if !valid(result.Summary) || !valid(result.State.Objective) || !valid(result.State.Markdown()) {
		return result, errors.New("invalid continuity summary or state")
	}
	for _, values := range [][]string{result.State.Done, result.State.Next, result.State.Open} {
		if len(values) > 32 {
			return result, errors.New("too many continuity state entries")
		}
		for _, text := range values {
			if !valid(text) {
				return result, errors.New("invalid continuity state entry")
			}
		}
	}
	for _, fact := range result.Facts {
		if !contains([]string{"semantic", "procedural", "reflex"}, fact.Class) || !valid(fact.Text) {
			return result, errors.New("invalid continuity fact")
		}
	}
	return result, nil
}
