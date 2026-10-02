package loom

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones are also available on Windows and minimal installs.
)

// Keep remaining_percent for existing clients; remaining is the portable fraction.
func (w QuotaWindow) MarshalJSON() ([]byte, error) {
	type legacy QuotaWindow
	var fraction *float64
	if w.Remaining != nil && *w.Remaining >= 0 && *w.Remaining <= 100 {
		v := *w.Remaining / 100
		fraction = &v
	}
	return json.Marshal(struct {
		legacy
		Remaining *float64 `json:"remaining"`
	}{legacy(w), fraction})
}

// Only exact saved-machine IDs are recognized. Arbitrary custom launchers must
// never accidentally read this computer's account based on a suffix or logo.
func usageHarnessID(a acpAgent) string {
	if a.Machine != "" && a.Remote {
		prefix := "custom-" + a.Machine + "-"
		if strings.HasPrefix(a.ID, prefix) {
			return strings.TrimPrefix(a.ID, prefix)
		}
		return ""
	}
	if a.Custom || a.Remote {
		return ""
	}
	return a.ID
}
func harnessHasQuota(a acpAgent) bool {
	switch usageHarnessID(a) {
	case "hermes", "claude-code":
		return true
	}
	return false
}
func readHarnessQuota(ctx context.Context, a acpAgent) (QuotaSnapshot, error) {
	now := time.Now()
	q := QuotaSnapshot{RuntimeID: a.ID, Name: a.Name, Windows: []QuotaWindow{}, FetchedAt: now.Unix()}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch usageHarnessID(a) {
	case "claude-code":
		q.Source = "claude /usage · native account"
		out, err := harnessUsageCommand(ctx, a, []string{"claude", "-p", "/usage", "--output-format", "json", "--no-session-persistence"})
		if err != nil {
			return q, err
		}
		var result struct {
			Result string `json:"result"`
		}
		if json.Unmarshal(out, &result) != nil || result.Result == "" {
			q.Note = string(out)
			return q, nil
		}
		q.Windows, q.Note = parseClaudeUsage(result.Result, now)
	case "hermes":
		q.Source = "hermes usage · native account"
		out, err := harnessUsageCommand(ctx, a, []string{"hermes", "usage"})
		if err != nil {
			return q, err
		}
		q.Windows, q.Note = parseHermesQuota(string(out))
	default:
		return q, errors.New("quotas unavailable")
	}
	return q, nil
}

var claudeQuotaLine = regexp.MustCompile(`^(.+?):\s*([0-9]+(?:\.[0-9]+)?)%\s+used\s*[·•]\s*resets\s+(.+)\s+\(([^()]+)\)\s*$`)

func parseClaudeUsage(text string, now time.Time) ([]QuotaWindow, string) {
	windows := []QuotaWindow{}
	unknown := false
	for _, line := range strings.Split(cleanUsageText(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := claudeQuotaLine.FindStringSubmatch(line)
		if m == nil {
			unknown = true
			continue
		}
		used, _ := strconv.ParseFloat(m[2], 64)
		if used > 100 {
			unknown = true
			continue
		}
		remaining := 100 - used
		reset, ok := claudeQuotaReset(m[3], m[4], now)
		if !ok {
			unknown = true
		}
		windows = append(windows, QuotaWindow{Group: "Claude Code", Name: m[1], Remaining: &remaining, ResetAt: reset})
	}
	if unknown || len(windows) == 0 {
		return windows, text
	}
	return windows, ""
}
func claudeQuotaReset(date, zone string, now time.Time) (*int64, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, false
	}
	date = strings.ToLower(strings.TrimSpace(date))
	// Go's abbreviated month parser is case-insensitive.
	date = strings.ReplaceAll(date, " ,", ",")
	for _, layout := range []string{"Jan 2, 3:04pm 2006", "Jan 2, 3pm 2006", "January 2, 3:04pm 2006", "Jan 2, 15:04 2006"} {
		t, err := time.ParseInLocation(layout, date+" "+strconv.Itoa(now.In(loc).Year()), loc)
		if err != nil {
			continue
		}
		if t.Before(now) {
			t, err = time.ParseInLocation(layout, date+" "+strconv.Itoa(now.In(loc).Year()+1), loc)
		}
		if err != nil {
			return nil, false
		}
		unix := t.Unix()
		return &unix, true
	}
	return nil, false
}

var hermesQuotaLine = regexp.MustCompile(`^(.+?):\s*([0-9]+(?:\.[0-9]+)?)%\s+remaining\b.*`)
var hermesQuotaDate = regexp.MustCompile(`\((\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?)\s+(UTC|[A-Za-z_]+/[A-Za-z_/]+)\)`)

func parseHermesQuota(text string) ([]QuotaWindow, string) {
	windows := []QuotaWindow{}
	group := "Hermes"
	unknown := false
	for _, line := range strings.Split(cleanUsageText(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Provider:") {
			group = strings.TrimSpace(strings.TrimPrefix(line, "Provider:"))
			continue
		}
		m := hermesQuotaLine.FindStringSubmatch(line)
		if m == nil {
			unknown = true
			continue
		}
		remaining, _ := strconv.ParseFloat(m[2], 64)
		if remaining > 100 {
			unknown = true
			continue
		}
		w := QuotaWindow{Group: group, Name: m[1], Remaining: &remaining}
		if d := hermesQuotaDate.FindStringSubmatch(line); d != nil {
			if loc, err := time.LoadLocation(d[2]); err == nil {
				layout := "2006-01-02 15:04"
				if len(d[1]) > 16 {
					layout += ":05"
				}
				if reset, err := time.ParseInLocation(layout, d[1], loc); err == nil {
					unix := reset.Unix()
					w.ResetAt = &unix
				}
			}
		}
		if w.ResetAt == nil {
			unknown = true
		}
		windows = append(windows, w)
	}
	if unknown || len(windows) == 0 {
		return windows, text
	}
	return windows, ""
}
