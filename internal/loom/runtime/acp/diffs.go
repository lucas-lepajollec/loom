package acp

import (
	"fmt"
	"strings"
)

func Lines(Text string) []string {
	if Text == "" {
		return nil
	}
	lines := strings.SplitAfter(Text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type DiffLine struct {
	Kind byte
	Text string
}

// Line LCS on the changed middle; equal prefixes/suffixes are retained. A
// bounded fallback is still a valid diff for very large unrelated rewrites.
func LineDiff(before, after string) []DiffLine {
	a, b := Lines(before), Lines(after)
	out := []DiffLine{}
	first := 0
	for first < len(a) && first < len(b) && a[first] == b[first] {
		out = append(out, DiffLine{' ', a[first]})
		first++
	}
	last := 0
	for last < len(a)-first && last < len(b)-first && a[len(a)-1-last] == b[len(b)-1-last] {
		last++
	}
	x, y := a[first:len(a)-last], b[first:len(b)-last]
	if len(x) > 0 && len(y) > 0 && len(x) <= 1000000/len(y) {
		width := len(y) + 1
		dp := make([]int, (len(x)+1)*width)
		for i := len(x) - 1; i >= 0; i-- {
			for j := len(y) - 1; j >= 0; j-- {
				if x[i] == y[j] {
					dp[i*width+j] = dp[(i+1)*width+j+1] + 1
				} else {
					dp[i*width+j] = max(dp[(i+1)*width+j], dp[i*width+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(x) || j < len(y) {
			if i < len(x) && j < len(y) && x[i] == y[j] {
				out = append(out, DiffLine{' ', x[i]})
				i++
				j++
			} else if i < len(x) && (j == len(y) || dp[(i+1)*width+j] >= dp[i*width+j+1]) {
				out = append(out, DiffLine{'-', x[i]})
				i++
			} else {
				out = append(out, DiffLine{'+', y[j]})
				j++
			}
		}
	} else {
		for _, line := range x {
			out = append(out, DiffLine{'-', line})
		}
		for _, line := range y {
			out = append(out, DiffLine{'+', line})
		}
	}
	for _, line := range a[len(a)-last:] {
		out = append(out, DiffLine{' ', line})
	}
	return out
}
func LineCounts(before, after string) (add, del int) {
	for _, line := range LineDiff(before, after) {
		if line.Kind == '+' {
			add++
		}
		if line.Kind == '-' {
			del++
		}
	}
	return
}
func UnifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", path, path, min(1, len(Lines(before))), len(Lines(before)), min(1, len(Lines(after))), len(Lines(after)))
	for _, line := range LineDiff(before, after) {
		out.WriteByte(line.Kind)
		out.WriteString(line.Text)
		if !strings.HasSuffix(line.Text, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
	return out.String()
}
