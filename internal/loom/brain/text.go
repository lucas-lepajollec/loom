package brain

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch r {
		case 'œ':
			b.WriteString("oe")
		case 'æ':
			b.WriteString("ae")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type word struct {
	text       string
	start, end int
}

func words(s string) []word {
	rs := []rune(s)
	out := []word{}
	start := -1
	for i := 0; i <= len(rs); i++ {
		if i < len(rs) && (unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i]) || unicode.Is(unicode.Mn, rs[i])) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, word{fold(string(rs[start:i])), start, i})
			start = -1
		}
	}
	return out
}

// stopWords are frequent French and English words that say nothing about a
// passage; they are dropped from queries (unless the query is only them).
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a au aux avec ce ces cet cette d de des du dans en et est etre il ils elle elles je j l la le les leur lui ma mais me meme mes moi mon ne nos notre nous on ou par pas pour qu que qui sa se ses son sur ta te tes toi ton tu un une vos votre vous y c s n m t comment quel quelle quels quelles est-ce
		the a an and or of to in on at for with by from is are was were be been it its this that these those as not no do does did how what which who whom i you he she we they my your our their me us them`) {
		stopWords[w] = true
	}
}

// queryTerms are the meaningful terms of a query.
func queryTerms(s string) []string {
	all := terms(s)
	out := []string{}
	for _, t := range all {
		if !stopWords[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func terms(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range words(s) {
		if w.text != "" && !seen[w.text] {
			out = append(out, w.text)
			seen[w.text] = true
		}
	}
	return out
}

// ChunkText preserves ATX/setext heading ancestry and avoids interpreting
// fenced code as headings. Chunks are at most 1200 Unicode code points, with
// 100 characters of overlap inside each section.
func ChunkText(source, path, text string) []Chunk {
	out := []Chunk{}
	var heading []string
	var section strings.Builder
	flush := func() {
		rs := []rune(strings.TrimSpace(section.String()))
		for start := 0; start < len(rs); {
			end := min(start+1200, len(rs))
			if end < len(rs) {
				for i := end; i > start+900; i-- {
					if unicode.IsSpace(rs[i-1]) {
						end = i
						break
					}
				}
			}
			part := strings.TrimSpace(string(rs[start:end]))
			if part != "" {
				id := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", source, path, len(out), strings.Join(heading, "\x00"), part)))
				out = append(out, Chunk{fmt.Sprintf("%x", id[:16]), source, path, append([]string{}, heading...), part})
			}
			if end == len(rs) {
				break
			}
			start = end - 100
		}
		section.Reset()
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	fence := byte(0)
	fenceLen := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			n := 0
			for n < len(t) && t[n] == t[0] {
				n++
			}
			if fence == 0 {
				fence, fenceLen = t[0], n
			} else if t[0] == fence && n >= fenceLen && strings.TrimSpace(t[n:]) == "" {
				fence = 0
			}
			section.WriteString(line + "\n")
			continue
		}
		level, title := 0, ""
		indent := strings.TrimLeft(line, " ")
		if fence == 0 && len(line)-len(indent) <= 3 && !strings.HasPrefix(indent, "\t") {
			for level < len(t) && t[level] == '#' {
				level++
			}
			if level > 0 && level <= 6 && (level == len(t) || t[level] == ' ' || t[level] == '\t') {
				title = strings.TrimSpace(t[level:])
				// Closing hashes need preceding whitespace; C# is a title,
				// whereas "Title ###" has an optional ATX closing sequence.
				withoutHashes := strings.TrimRight(title, "#")
				if withoutHashes == "" || strings.HasSuffix(withoutHashes, " ") || strings.HasSuffix(withoutHashes, "\t") {
					title = strings.TrimSpace(withoutHashes)
				}
			} else {
				level = 0
			}
			if level == 0 && t != "" && i+1 < len(lines) {
				u := strings.TrimSpace(lines[i+1])
				if len(u) > 0 && strings.Trim(u, "=") == "" {
					level, title = 1, t
					i++
				} else if len(u) > 0 && strings.Trim(u, "-") == "" {
					level, title = 2, t
					i++
				}
			}
		}
		if level > 0 {
			// Path metadata must also be bounded for adversarial giant headings.
			if len([]rune(title)) > 512 {
				level = 0
			}
		}
		if level > 0 {
			flush()
			if len(heading) >= level {
				heading = heading[:level-1]
			}
			for len(heading) < level-1 {
				heading = append(heading, "")
			}
			heading = append(heading, title)
		}
		section.WriteString(line + "\n")
	}
	flush()
	return out
}

func snippet(text string, query []string) (string, []Range) {
	rs := []rune(text)
	set := map[string]bool{}
	for _, t := range query {
		set[t] = true
	}
	start := 0
	for _, w := range words(text) {
		if set[w.text] {
			start = max(0, w.start-70)
			break
		}
	}
	end := min(len(rs), start+320)
	s := string(rs[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(rs) {
		s += "…"
	}
	ranges := []Range{}
	for _, w := range words(s) {
		if set[w.text] {
			ranges = append(ranges, Range{w.start, w.end})
		}
	}
	return s, ranges
}

func phrases(q string) []string {
	re := regexp.MustCompile(`"([^"]+)"`)
	out := []string{}
	for _, m := range re.FindAllStringSubmatch(q, -1) {
		out = append(out, fold(m[1]))
	}
	return out
}
