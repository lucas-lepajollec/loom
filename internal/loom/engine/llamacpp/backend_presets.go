package llamacpp

import (
	"regexp"
	"strings"
)

var nameLineRe = regexp.MustCompile(`(?mi)^[ \t]*#?[ \t]*NAME[ \t]*=.*$`)

// PresetDisplayName extracts the `# NAME=` value from a preset body, falling
// back to `fallback` (the filename id) when absent — keeps old presets working.
func PresetDisplayName(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		i := strings.IndexByte(s, '=')
		if i >= 0 && strings.EqualFold(strings.TrimSpace(s[:i]), "NAME") {
			if v := UnquoteValue(strings.TrimSpace(s[i+1:])); v != "" {
				return v
			}
		}
	}
	return fallback
}

// WithDisplayName ensures the body carries a `# NAME=<name>` line (replacing an
// existing one, or prepended otherwise).
func WithDisplayName(content, name string) string {
	line := "# NAME=" + name
	if nameLineRe.MatchString(content) {
		return nameLineRe.ReplaceAllString(content, line)
	}
	return line + "\n" + content
}
