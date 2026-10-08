package brain

import (
	"math"
	"sort"
	"strings"
)

type FileContext struct{ Scope, File, Text, Reason string }

// FileMemoryContext includes both native indexes and a small user profile.
// Topic files use BM25 only at first-turn capture; later turns reuse the snapshot.
func FileMemoryContext(global MemoryFiles, project *MemoryFiles, query string, topics bool) []FileContext {
	out := []FileContext{}
	scopes := []struct {
		scope string
		files MemoryFiles
	}{{"global", global}}
	if project != nil {
		scopes = append(scopes, struct {
			scope string
			files MemoryFiles
		}{"project", *project})
	}
	profileRemaining := 1500
	for _, scope := range scopes {
		index, _ := BoundMemoryIndex(scope.files.Index)
		out = append(out, FileContext{scope.scope, "MEMORY.md", "Memory index (" + scope.scope + ", " + scope.files.Path + "):\n" + index, "session memory index"})
		for _, m := range scope.files.Items {
			if m.Type == "user" && !m.Malformed && profileRemaining > 0 {
				header := "User memory: " + m.Name + "\n"
				remaining := profileRemaining - len([]rune(header))
				if remaining <= 0 {
					continue
				}
				text := []rune(m.Text)
				text = text[:min(len(text), remaining)]
				profileRemaining -= len([]rune(header)) + len(text)
				out = append(out, FileContext{scope.scope, m.File, header + string(text), "user profile"})
			}
		}
	}
	if !topics || strings.TrimSpace(query) == "" {
		return out
	}
	type candidate struct {
		scope     string
		file      MemoryFile
		frequency map[string]int
		length    int
		score     float64
	}
	candidates := []candidate{}
	df := map[string]int{}
	total := 0
	for _, scope := range scopes {
		for _, m := range scope.files.Items {
			if m.Malformed || m.Type == "user" {
				continue
			}
			frequencies := map[string]int{}
			words := terms(m.Name + " " + m.Description + " " + m.Text)
			for _, t := range words {
				frequencies[t]++
			}
			for t := range frequencies {
				df[t]++
			}
			total += len(words)
			candidates = append(candidates, candidate{scope: scope.scope, file: m, frequency: frequencies, length: len(words)})
		}
	}
	if len(candidates) == 0 {
		return out
	}
	average := float64(total) / float64(len(candidates))
	if average == 0 {
		return out
	}
	for i := range candidates {
		c := &candidates[i]
		for _, t := range queryTerms(ContextQuery(query)) {
			tf := float64(c.frequency[t])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(candidates)-df[t])+.5)/(float64(df[t])+.5))
			c.score += idf * tf * 2.2 / (tf + 1.2*(.25+.75*float64(c.length)/average))
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].scope+"/"+candidates[i].file.File < candidates[j].scope+"/"+candidates[j].file.File
	})
	budget := 1500
	selected := 0
	for _, c := range candidates {
		if c.score <= 0 || selected == 3 {
			break
		}
		text := "Topic memory (" + c.scope + "/" + c.file.File + "): " + c.file.Name + "\n" + c.file.Text
		cost := Tokens(text + "\n\n")
		if cost > budget {
			continue
		}
		budget -= cost
		selected++
		out = append(out, FileContext{c.scope, c.file.File, text, "matches the first user message (BM25)"})
	}
	return out
}
