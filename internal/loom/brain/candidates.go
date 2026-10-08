package brain

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type CandidateDraft struct {
	Class string
	Text  string
	// Explicit: the user asked to be remembered ("retiens…", "sache que…"),
	// so the item is kept directly instead of waiting for review.
	Explicit bool
}

// An explicit memory request; its sentence becomes a reflex when it tells the
// agent how to answer or act, otherwise a fact about the user.
var explicitMemory = candidatePattern(`retiens|souviens-toi|n'oublie pas|sache que|saches que|je voudrais que tu saches|je veux que tu saches|pour mes (futures|prochaines) demandes|à l'avenir|remember|keep in mind|for future requests|in the future`)
var mentionsProject = candidatePattern(`ce projet|le projet|this project|the project|ce dépôt|this repo`)
var instructionLike = candidatePattern(`réponds|répond|répondre|utilise|écris|parle|fais|évite|structur\p{L}*|answer|reply|respond|use|write|avoid`)

var candidateRules = []struct {
	class string
	match *regexp.Regexp
}{
	{"semantic", candidatePattern(`retiens|souviens-toi|n'oublie pas|sache que|saches que|je voudrais que tu saches|je veux que tu saches|pour mes (futures|prochaines) demandes|à l'avenir|remember|keep in mind|note that|for future requests|in the future`)},
	// Rules only: a leading or obligatory "always/never", not any sentence using the word.
	{"reflex", candidatePattern(`désormais|dorénavant|à partir de maintenant|from now on|(tu dois|il faut|vous devez|you must|you should|make sure to) (toujours|jamais|always|never)|ne jamais|n'utilise jamais`)},
	{"reflex", regexp.MustCompile(`(?i)^[\s"'«(]*(toujours|jamais|always|never)([^\p{L}\p{N}_]|$)`)},
	{"semantic", candidatePattern(`je préfère|j'aime pas|je veux pas|i prefer|i don't like`)},
	{"procedural", candidatePattern(`pour publier|la procédure|les étapes|steps to|the way to`)},
}

func candidatePattern(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])(` + pattern + `)([^\p{L}\p{N}_]|$)`)
}
func CandidatesFromMessage(text string) []CandidateDraft {
	out := []CandidateDraft{}
	if len(text) > 4000 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return out
	}
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]+" \t\r") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:1]
			n := len(trimmed) - len(strings.TrimLeft(trimmed, marker))
			fence = strings.Repeat(marker, n)
			continue
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		start := 0
		collect := func(sentence string) {
			sentence = strings.TrimSpace(sentence)
			if len(out) >= 3 || utf8.RuneCountInString(sentence) < 12 || strings.HasSuffix(strings.TrimRight(sentence, ".!"), "?") {
				return
			}
			match := strings.ReplaceAll(sentence, "’", "'")
			for _, rule := range candidateRules {
				if rule.match.MatchString(match) {
					class, explicit := rule.class, explicitMemory.MatchString(match)
					if explicit && instructionLike.MatchString(match) {
						class = "reflex"
					}
					if len(sentence) > 500 {
						end := 500
						for !utf8.RuneStart(sentence[end]) {
							end--
						}
						sentence = strings.TrimSpace(sentence[:end])
					}
					out = append(out, CandidateDraft{Class: class, Text: sentence, Explicit: explicit})
					return
				}
			}
		}
		for i, r := range line {
			if strings.ContainsRune(".!?", r) {
				if i+1 < len(line) && strings.ContainsRune(".!?", rune(line[i+1])) {
					continue
				}
				collect(line[start : i+1])
				start = i + 1
			}
		}
		collect(line[start:])
	}
	return out
}
func normalizedCandidateText(text string) string {
	return strings.TrimFunc(normalizedMemoryText(text), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
}

// Dedupe and pending limits share the store lock with review and other writes.
func (s *MemoryStore) AddCandidates(drafts []CandidateDraft, scope string, provenance MemoryProvenance) ([]MemoryItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []MemoryItem{}
	root, err := s.open()
	if err != nil {
		return out, err
	}
	defer root.Close()
	seen := map[string]bool{}
	pending := 0
	for _, item := range s.items {
		if item.Status != "superseded" && item.Status != "expired" {
			seen[normalizedCandidateText(item.Text)] = true
		}
		if item.Status == "candidate" {
			pending++
		}
	}
	for _, draft := range drafts {
		text := normalizedCandidateText(draft.Text)
		if pending >= 50 {
			break
		}
		if seen[text] {
			continue
		}
		now := time.Now().UnixMilli()
		item := MemoryItem{ID: newMemoryID(), Class: draft.Class, Scope: scope, Text: draft.Text, Tags: []string{"auto"}, Importance: .5, Confidence: .4, CreatedAt: now, UpdatedAt: now, Provenance: provenance, Status: "candidate"}
		if draft.Explicit {
			item.Status, item.Importance, item.Confidence = "active", .8, .7
			// A preference about the user holds everywhere unless it names the project.
			if !mentionsProject.MatchString(strings.ReplaceAll(draft.Text, "’", "'")) {
				item.Scope = "global"
			}
		}
		if err := validateMemory(item); err != nil {
			return out, err
		}
		if err := s.save(root, item); err != nil {
			return out, err
		}
		seen[text] = true
		pending++
		out = append(out, cloneMemory(item))
	}
	return out, nil
}
