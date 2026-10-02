package loom

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Typed projections intentionally discard content, prompts, tools and paths.
// Claude message IDs are retained only during this file's scan to collapse
// streaming fragments of the same assistant message.
type nativeTokenCounts struct {
	Input        *int64 `json:"input_tokens"`
	Output       *int64 `json:"output_tokens"`
	Cached       *int64 `json:"cached_input_tokens"`
	CacheRead    *int64 `json:"cache_read_input_tokens"`
	CacheWrite   *int64 `json:"cache_creation_input_tokens"`
	Total        *int64 `json:"total_tokens"`
	PiInput      *int64 `json:"input"`
	PiOutput     *int64 `json:"output"`
	PiCacheRead  *int64 `json:"cacheRead"`
	PiCacheWrite *int64 `json:"cacheWrite"`
	PiTotal      *int64 `json:"totalTokens"`
	Cost         *struct {
		Total *float64 `json:"total"`
	} `json:"cost"`
}
type nativeUsageRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string             `json:"id"`
		Role  string             `json:"role"`
		Model string             `json:"model"`
		Usage *nativeTokenCounts `json:"usage"`
	} `json:"message"`
	Payload struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  *struct {
			Total *nativeTokenCounts `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}
type usageNumbers struct {
	input, output, cached, total int64
	cost                         *float64
}

func tokenValue(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func (u nativeTokenCounts) numbers(harness string) (usageNumbers, bool) {
	input, output, cached, write, total := u.Input, u.Output, u.CacheRead, u.CacheWrite, u.Total
	if harness == "codex" {
		cached = u.Cached
	}
	if harness == "pi" {
		input, output, cached, write, total = u.PiInput, u.PiOutput, u.PiCacheRead, u.PiCacheWrite, u.PiTotal
	}
	if input == nil || output == nil {
		return usageNumbers{}, false
	}
	for _, p := range []*int64{input, output, cached, write, total} {
		if p != nil && (*p < 0 || *p > 1e15) {
			return usageNumbers{}, false
		}
	}
	n := usageNumbers{input: tokenValue(input), output: tokenValue(output), cached: tokenValue(cached), total: tokenValue(total)}
	if total == nil {
		n.total = n.input + n.output
		if harness != "codex" {
			n.total += n.cached + tokenValue(write)
		}
	}
	if u.Cost != nil && u.Cost.Total != nil && *u.Cost.Total >= 0 && !math.IsInf(*u.Cost.Total, 0) && !math.IsNaN(*u.Cost.Total) {
		n.cost = u.Cost.Total
	}
	return n, true
}

type nativeSessionUsage struct {
	usageNumbers
	models      map[string]int64
	counted     bool
	costUnknown bool
}

func newNativeSessionUsage() nativeSessionUsage {
	return nativeSessionUsage{models: map[string]int64{}}
}
func (s *nativeSessionUsage) add(model string, n usageNumbers) {
	// "<synthetic>" entries are messages the harness wrote itself (no model call).
	if strings.HasPrefix(model, "<") && n.total == 0 && n.input == 0 && n.output == 0 {
		return
	}
	if strings.HasPrefix(model, "<") {
		model = ""
	}
	s.counted = true
	s.input += n.input
	s.output += n.output
	s.cached += n.cached
	s.total += n.total
	if model == "" {
		model = "unknown"
	}
	s.models[model] += n.total
	if n.cost == nil {
		s.costUnknown = true
	} else {
		if s.cost == nil {
			v := 0.0
			s.cost = &v
		}
		*s.cost += *n.cost
	}
}
func mergeNativeSession(q *HarnessUsage, s nativeSessionUsage) {
	if !s.counted {
		return
	}
	// Keep null once any native record lacks a cost; never extrapolate prices.
	if q.Sessions == 0 && !s.costUnknown {
		q.CostUSD = s.cost
	} else if q.CostUSD != nil {
		if s.costUnknown || s.cost == nil {
			q.CostUSD = nil
		} else {
			*q.CostUSD += *s.cost
		}
	}
	q.Sessions++
	q.InputTokens += s.input
	q.OutputTokens += s.output
	q.CacheReadTokens += s.cached
	q.TotalTokens += s.total
	for model, tokens := range s.models {
		found := false
		for i := range q.ByModel {
			if q.ByModel[i].Model == model {
				q.ByModel[i].Tokens += tokens
				*q.ByModel[i].Sessions++
				found = true
				break
			}
		}
		if !found {
			count := 1
			q.ByModel = append(q.ByModel, HarnessModelUsage{Model: model, Tokens: tokens, Sessions: &count})
		}
	}
}

func readHarnessSessionFiles(ctx context.Context, root, harness string, now time.Time, days int, q *HarnessUsage) error {
	cutoff := now.AddDate(0, 0, -days)
	files := 0
	var partial error
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if path == root {
				return errors.New("unavailable")
			}
			partial = errors.New("partial reading: some logs are inaccessible")
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			partial = errors.New("partial reading: some logs are inaccessible")
			return nil
		}
		if !info.Mode().IsRegular() || info.ModTime().Before(cutoff) {
			return nil
		}
		if files == nativeUsageMaxFiles {
			return errors.New("partial reading: 2000-file limit")
		}
		files++
		f, err := os.Open(path)
		if err != nil {
			partial = errors.New("partial reading: some logs are inaccessible")
			return nil
		}
		s, err := parseHarnessJSONL(ctx, f, harness, cutoff, now)
		_ = f.Close()
		mergeNativeSession(q, s)
		if err != nil {
			partial = err
		}
		return nil
	})
	sort.Slice(q.ByModel, func(i, j int) bool { return q.ByModel[i].Model < q.ByModel[j].Model })
	if err != nil {
		return err
	}
	return partial
}

func parseHarnessJSONL(ctx context.Context, r io.Reader, harness string, cutoff, now time.Time) (nativeSessionUsage, error) {
	s := newNativeSessionUsage()
	// Lines are read one by one and dropped: huge lines (pasted files,
	// images) are skipped, never buffered whole; no transcript is retained.
	reader := bufio.NewReaderSize(r, 1<<20)
	readLine := func() ([]byte, bool, error) {
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			for err == bufio.ErrBufferFull {
				_, err = reader.ReadSlice('\n')
			}
			if err != nil && err != io.EOF {
				return nil, false, err
			}
			return nil, true, err
		}
		return line, false, err
	}
	type messageUsage struct {
		model   string
		numbers usageNumbers
	}
	messages := map[string]messageUsage{}
	model := ""
	var previous usageNumbers
	havePrevious := false
	var partial error
	var readErr error
	for {
		if ctx.Err() != nil {
			return s, ctx.Err()
		}
		line, skipped, err := readLine()
		if err != nil && err != io.EOF {
			readErr = err
			break
		}
		last := err == io.EOF
		// Usage lines are small; anything else (or a skipped huge line) is ignored.
		if skipped || len(bytes.TrimSpace(line)) == 0 || !(bytes.Contains(line, []byte(`usage`)) || bytes.Contains(line, []byte(`token_count`)) || bytes.Contains(line, []byte(`turn_context`))) {
			if last {
				break
			}
			continue
		}
		var record nativeUsageRecord
		if json.Unmarshal(line, &record) != nil {
			if last {
				break
			}
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
		if harness == "codex" && record.Type == "turn_context" {
			model = record.Payload.Model
			continue
		}
		if err != nil || stamp.After(now) {
			continue
		}
		if harness == "codex" {
			if record.Type != "event_msg" || record.Payload.Type != "token_count" || record.Payload.Info == nil || record.Payload.Info.Total == nil {
				continue
			}
			n, ok := record.Payload.Info.Total.numbers(harness)
			if !ok {
				partial = errors.New("partial reading: native counters not recognized")
				continue
			}
			if !stamp.Before(cutoff) {
				delta := n
				if havePrevious && n.total >= previous.total && n.input >= previous.input && n.output >= previous.output && n.cached >= previous.cached {
					delta = usageNumbers{input: n.input - previous.input, output: n.output - previous.output, cached: n.cached - previous.cached, total: n.total - previous.total}
				} else if havePrevious {
					s = newNativeSessionUsage()
				} // last cumulative total after a native reset
				// Duplicate cumulative reports add zero, and cannot change attribution.
				if delta.total > 0 || !s.counted {
					s.add(model, delta)
				}
			}
			previous = n
			havePrevious = true
			continue
		}
		if stamp.Before(cutoff) || record.Message.Usage == nil {
			continue
		}
		if harness == "claude-code" && record.Type != "assistant" {
			continue
		}
		if record.Message.Role != "assistant" {
			continue
		}
		n, ok := record.Message.Usage.numbers(harness)
		if !ok {
			partial = errors.New("partial reading: native counters not recognized")
			continue
		}
		if harness == "claude-code" && record.Message.ID != "" {
			if len(messages) >= 100000 {
				partial = errors.New("partial reading: too many native messages")
				break
			}
			messages[record.Message.ID] = messageUsage{record.Message.Model, n}
		} else {
			s.add(record.Message.Model, n)
		}
	}
	for _, m := range messages {
		s.add(m.model, m.numbers)
	}
	if readErr != nil {
		partial = errors.New("partial reading: unreadable log")
	}
	return s, partial
}
