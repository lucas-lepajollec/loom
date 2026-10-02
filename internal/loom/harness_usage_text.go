package loom

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var usageANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func cleanUsageText(s string) string {
	return strings.ReplaceAll(usageANSI.ReplaceAllString(s, ""), "\r", "")
}

var usageNumeric = regexp.MustCompile(`^\$?([0-9]+(?:[,.][0-9]+)*)([kKmMbB]?)$`)

func usageNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimLeft(strings.Trim(s, "$€"), "~$"))
	m := usageNumeric.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "k":
		n *= 1e3
	case "m":
		n *= 1e6
	case "b":
		n *= 1e9
	}
	return n, !math.IsInf(n, 0) && n <= 1e15
}

var usageStatLine = regexp.MustCompile(`(?i)^(sessions|total sessions|input(?: tokens)?|output(?: tokens)?|cache read(?: tokens)?|cached(?: tokens)?|cache write(?: tokens)?|total tokens|tokens|total cost|estimated cost|estimated|cost)\s*:?\s+(\$?\S+)`)
var usageColumns = regexp.MustCompile(`\s{2,}|\t+`)

func statKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	if s == "tokens" {
		return "total"
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "tokens"))
	switch s {
	case "total sessions":
		return "sessions"
	case "cache read", "cached":
		return "cached"
	case "cache write":
		return "write"
	case "total", "tokens":
		return "total"
	case "total cost", "estimated cost", "estimated", "cost":
		return "cost"
	}
	return s
}
func usageCells(line string) []string {
	line = strings.ReplaceAll(line, "│", "|")
	line = strings.ReplaceAll(line, "┃", "|")
	if strings.Contains(line, "|") {
		cells := strings.Split(strings.Trim(line, " |"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		return cells
	}
	// Whitespace tables may use padded columns with multi-word headings.
	return usageColumns.Split(strings.TrimSpace(line), -1)
}

// Both CLIs provide observations, not an invoice. No prices are reconstructed.
var multiStatPair = regexp.MustCompile(`[A-Za-z][A-Za-z ./()-]*?:\s+\S+`)

func parseHarnessStats(text, harness string, q *HarnessUsage) error {
	seen := map[string]bool{}
	models := map[string]HarnessModelUsage{}
	var headers []string
	inModels := false
	var block *HarnessModelUsage
	var cacheWrite int64
	blockParts := map[string]int64{}
	finishBlock := func() {
		if block == nil {
			return
		}
		if block.Tokens == 0 {
			for _, key := range []string{"input", "output", "cached", "write"} {
				block.Tokens += blockParts[key]
			}
		}
		if len(blockParts) > 0 {
			models[block.Model] = *block
		}
		block = nil
		blockParts = map[string]int64{}
	}
	// Hermes prints two stats per line ("Input tokens: 1  Output tokens: 2"):
	// split such lines into one stat each.
	var lines []string
	for _, raw := range strings.Split(cleanUsageText(text), "\n") {
		line := strings.TrimSpace(strings.Trim(raw, " │┃|"))
		if parts := multiStatPair.FindAllString(line, -1); len(parts) > 1 && usageStatLine.MatchString(parts[0]) {
			lines = append(lines, parts...)
			continue
		}
		lines = append(lines, line)
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		// A section title starting with an emoji (Hermes) ends a models table.
		if r := []rune(line)[0]; r > 0x2000 && !(r >= 0x2500 && r <= 0x259F) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			finishBlock()
			l := strings.ToLower(line)
			inModels = strings.Contains(l, "model")
			headers = nil
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "tool usage") {
			finishBlock()
			break
		}
		if strings.Contains(lower, "models used") || lower == "models" || lower == "models:" || strings.Contains(lower, "model breakdown") || lower == "model usage" {
			inModels = true
			continue
		}
		cells := usageCells(line)
		if len(cells) >= 2 && (strings.EqualFold(cells[0], "model") || strings.EqualFold(cells[0], "model name")) {
			inModels = true
			headers = cells
			continue
		}
		if inModels {
			if strings.HasPrefix(lower, "messages") {
				continue
			}
			// OpenCode versions also print a model name followed by indented metrics.
			if strings.HasPrefix(lower, "model:") {
				finishBlock()
				block = &HarnessModelUsage{Model: strings.TrimSpace(line[len("model:"):])}
				continue
			}
			if m := usageStatLine.FindStringSubmatch(line); m != nil && block != nil {
				key := statKey(m[1])
				n, ok := usageNumber(m[2])
				if !ok {
					continue
				}
				if key == "sessions" {
					count := int(n)
					block.Sessions = &count
				} else if key == "total" {
					block.Tokens = int64(n)
					blockParts[key] = int64(n)
				} else if key != "cost" {
					blockParts[key] = int64(n)
				}
				continue
			}
			if len(headers) >= 2 && len(cells) == len(headers) {
				row := HarnessModelUsage{Model: cells[0]}
				known := false
				parts := map[string]int64{}
				for i := 1; i < len(cells); i++ {
					n, ok := usageNumber(cells[i])
					if !ok {
						continue
					}
					key := statKey(headers[i])
					switch key {
					case "sessions":
						count := int(n)
						row.Sessions = &count
					case "total":
						row.Tokens = int64(n)
						known = true
					case "input", "output", "cached", "write":
						parts[key] = int64(n)
						known = true
					}
				}
				if !known {
					continue
				}
				if _, hasTotal := findUsageHeader(headers, "total"); !hasTotal {
					for _, n := range parts {
						row.Tokens += n
					}
				}
				models[row.Model] = row
				continue
			}
			if len(headers) == 0 && len(cells) == 1 && !strings.ContainsAny(line, "─━┌┐└┘┬┴┼═+") && usageStatLine.FindStringSubmatch(line) == nil {
				finishBlock()
				block = &HarnessModelUsage{Model: line}
				continue
			}
		}
		m := usageStatLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, ok := usageNumber(m[2])
		if !ok {
			continue
		}
		key := statKey(m[1])
		seen[key] = true
		switch key {
		case "sessions":
			q.Sessions = int(n)
		case "input":
			q.InputTokens = int64(n)
		case "output":
			q.OutputTokens = int64(n)
		case "cached":
			q.CacheReadTokens = int64(n)
		case "write":
			cacheWrite = int64(n)
		case "total":
			q.TotalTokens = int64(n)
		case "cost":
			value := n
			q.CostUSD = &value
		}
	}
	finishBlock()
	if !seen["total"] {
		q.TotalTokens = q.InputTokens + q.OutputTokens + q.CacheReadTokens + cacheWrite
	}
	for _, row := range models {
		q.ByModel = append(q.ByModel, row)
	}
	sort.Slice(q.ByModel, func(i, j int) bool { return q.ByModel[i].Model < q.ByModel[j].Model })
	if !seen["sessions"] || !seen["input"] || !seen["output"] {
		return errors.New("format des statistiques " + harness + " non reconnu")
	}
	return nil
}
func findUsageHeader(headers []string, key string) (int, bool) {
	for i, h := range headers {
		if statKey(h) == key {
			return i, true
		}
	}
	return 0, false
}
