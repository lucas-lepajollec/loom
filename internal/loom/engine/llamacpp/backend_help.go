package llamacpp

import (
	"regexp"
	"strconv"
	"strings"
)

// LlamaFlag est un drapeau lu dans `llama-server --help`. On n'invente pas de
// flags : le catalogue suit le binaire pointé par BIN, et se met à jour tout
// seul après un rebuild. L'UI choisit curseur / toggle / liste selon Kind.
type LlamaFlag struct {
	ID         string   `json:"id"`
	Flag       string   `json:"flag"`
	Short      string   `json:"short,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
	Arg        string   `json:"arg,omitempty"`
	Choices    []string `json:"choices,omitempty"`
	Default    string   `json:"default,omitempty"`
	Help       string   `json:"help"`
	Group      string   `json:"group"`
	Kind       string   `json:"kind"`
	Min        *float64 `json:"min,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Key        string   `json:"key,omitempty"`
	Tier       string   `json:"tier"`
	Deprecated bool     `json:"deprecated,omitempty"`
	Hidden     bool     `json:"hidden,omitempty"`
}

var (
	reHelpGroup   = regexp.MustCompile(`^-----+\s*(.+?)\s*-----+$`)
	reHelpDefault = regexp.MustCompile(`(?i)(?:\(|,\s*)default:\s*(?:'([^']*)'|"([^"]*)"|([^,)]+))`)
	reHelpEnv     = regexp.MustCompile(`(?i)\(env:\s*[^)]+\)`)
	reHelpRange   = regexp.MustCompile(`<(-?\d+(?:\.\d+)?)\s*\.\.\.?\s*(-?\d+(?:\.\d+)?)>`)
	reHelpChoiceB = regexp.MustCompile(`\[([^\[\]]+)\]`)
	reHelpChoiceC = regexp.MustCompile(`\{([^{}]+)\}`)
	reHelpAllowed = regexp.MustCompile(`(?i)allowed values:\s*([a-z0-9_.,+\- \t]+)`)
	reHelpIdent   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+.-]*$`)
	reHelpBullet  = regexp.MustCompile(`^\s*-\s+([A-Za-z0-9_+-]+)\s*:`)
	reHelpRemoved = regexp.MustCompile(`(?i)the argument has been removed`)
)

// flagToConfigKey : drapeaux llama.cpp qui ont déjà une clé config.env
// première classe (pas EXTRA_ARGS). Les alias du même drapeau y aboutissent.
var flagToConfigKey = map[string]string{
	"ctx-size":         "CTX",
	"fit":              "FIT",
	"gpu-layers":       "NGL",
	"n-gpu-layers":     "NGL",
	"threads":          "THREADS",
	"threads-batch":    "THREADS_BATCH",
	"batch-size":       "BATCH",
	"ubatch-size":      "UBATCH",
	"parallel":         "NP",
	"cache-type-k":     "KV_TYPE_K",
	"cache-type-v":     "KV_TYPE_V",
	"mmproj":           "MMPROJ",
	"model-draft":      "MODEL_DRAFT",
	"reasoning":        "REASONING",
	"reasoning-budget": "REASONING_BUDGET",
	"reasoning-effort": "REASONING_EFFORT",
	"temp":             "TEMP",
	"temperature":      "TEMP",
	"top-p":            "TOP_P",
	"top-k":            "TOP_K",
	"min-p":            "MIN_P",
	"presence-penalty": "PRESENCE_PENALTY",
	"repeat-penalty":   "REPEAT_PENALTY",
}

var flagTierBase = map[string]bool{
	"ctx-size":         true,
	"gpu-layers":       true,
	"n-gpu-layers":     true,
	"reasoning":        true,
	"reasoning-effort": true,
	"mmproj":           true,
	"mmproj-auto":      true,
	"temp":             true,
	"temperature":      true,
}

var flagTierAdv = map[string]bool{
	"cache-type-k":      true,
	"cache-type-v":      true,
	"parallel":          true,
	"spec-type":         true,
	"spec-draft-n-max":  true,
	"model-draft":       true,
	"batch-size":        true,
	"ubatch-size":       true,
	"flash-attn":        true,
	"load-mode":         true,
	"mlock":             true,
	"mmap":              true,
	"threads":           true,
	"threads-batch":     true,
	"n-cpu-moe":         true,
	"cpu-moe":           true,
	"kv-unified":        true,
	"jinja":             true,
	"chat-template":     true,
	"split-mode":        true,
	"tensor-split":      true,
	"fit":               true,
	"no-mmproj-offload": true,
	"mmproj-offload":    true,
}

func ParseLlamaHelp(text string) []LlamaFlag {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	group := "common"
	var flags []LlamaFlag
	var cur *LlamaFlag
	flush := func() {
		if cur == nil {
			return
		}
		FinishLlamaFlag(cur)
		flags = append(flags, *cur)
		cur = nil
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if m := reHelpGroup.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 2 {
			flush()
			group = strings.TrimSpace(strings.TrimSuffix(m[1], " params"))
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "-----") {
			flush()
			f := ParseLlamaHelpFlagLine(line)
			f.Group = group
			cur = &f
			continue
		}
		if cur != nil && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			if cur.Help != "" {
				cur.Help += " "
			}
			cur.Help += t
			if m := reHelpBullet.FindStringSubmatch(t); len(m) == 2 {
				name := strings.TrimSpace(m[1])
				if name != "" && !ContainsFold(cur.Choices, name) {
					cur.Choices = append(cur.Choices, name)
				}
			}
		}
	}
	flush()
	return flags
}

func ParseLlamaHelpFlagLine(line string) LlamaFlag {
	flagsPart, help := SplitHelpFlagAndDesc(line)
	f := LlamaFlag{Help: strings.TrimSpace(help)}
	parts := SplitHelpComma(flagsPart)
	var longs, shorts []string
	arg := ""
	var choices []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		tok, rest := HelpTokenArg(p)
		if rest != "" {
			if arg == "" {
				arg = rest
			}
			if c := ParseHelpChoices(rest); len(c) > 0 {
				choices = MergeUniq(choices, c)
			}
			if m := reHelpRange.FindStringSubmatch(rest); len(m) == 3 {
				lo, _ := strconv.ParseFloat(m[1], 64)
				hi, _ := strconv.ParseFloat(m[2], 64)
				f.Min, f.Max = &lo, &hi
			}
		}
		switch {
		case strings.HasPrefix(tok, "--"):
			longs = append(longs, tok)
		case strings.HasPrefix(tok, "-"):
			shorts = append(shorts, tok)
		}
	}
	canon := CanonicalHelpFlag(longs)
	f.Flag = canon
	f.ID = strings.TrimPrefix(canon, "--")
	if len(shorts) > 0 {
		f.Short = shorts[0]
	}
	seen := map[string]bool{canon: true}
	for _, s := range shorts {
		if !seen[s] {
			f.Aliases = append(f.Aliases, s)
			seen[s] = true
		}
	}
	for _, l := range longs {
		if !seen[l] {
			f.Aliases = append(f.Aliases, l)
			seen[l] = true
		}
	}
	f.Arg = arg
	f.Choices = choices
	return f
}

func SplitHelpFlagAndDesc(line string) (flags, desc string) {
	last := -1
	for i := 0; i < len(line)-1; i++ {
		if line[i] != ' ' || line[i+1] != ' ' {
			continue
		}
		rest := strings.TrimSpace(line[i:])
		if rest == "" || strings.HasPrefix(rest, "-") {
			continue
		}
		last = i
	}
	if last < 0 {
		return strings.TrimSpace(line), ""
	}
	return strings.TrimSpace(line[:last]), strings.TrimSpace(line[last:])
}

func SplitHelpComma(s string) []string {
	var out []string
	depth := 0
	cur := strings.Builder{}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '[', '{', '<', '(':
			depth++
			cur.WriteRune(r)
		case ']', '}', '>', ')':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case ',':
			if depth == 0 {
				rest := strings.TrimSpace(string(runes[i+1:]))
				if strings.HasPrefix(rest, "-") {
					out = append(out, strings.TrimSpace(cur.String()))
					cur.Reset()
					continue
				}
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

func HelpTokenArg(p string) (token, arg string) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ""
	}
	fields := strings.Fields(p)
	if len(fields) == 0 {
		return p, ""
	}
	token = fields[0]
	if len(fields) > 1 {
		arg = strings.Join(fields[1:], " ")
	}
	return token, arg
}

func CanonicalHelpFlag(longs []string) string {
	var first string
	for _, l := range longs {
		if !strings.HasPrefix(l, "--") || strings.HasPrefix(l, "--no-") {
			continue
		}
		if first == "" {
			first = l
		}
		id := strings.TrimPrefix(l, "--")
		if flagToConfigKey[id] != "" || flagTierBase[id] || flagTierAdv[id] {
			return l
		}
	}
	if first != "" {
		return first
	}
	if len(longs) == 0 {
		return ""
	}
	l := longs[0]
	if strings.HasPrefix(l, "--no-") {
		return "--" + strings.TrimPrefix(l, "--no-")
	}
	return l
}

func ParseHelpChoices(s string) []string {
	s = strings.TrimSpace(s)
	var raw string
	switch {
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		raw = s[1 : len(s)-1]
	case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
		raw = s[1 : len(s)-1]
	case strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && !strings.Contains(s, "..."):
		inner := s[1 : len(s)-1]
		if strings.Contains(inner, "|") {
			raw = inner
		}
	}
	if raw == "" {
		parts := strings.Split(s, ",")
		if len(parts) >= 2 {
			ok := true
			var cleaned []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" || !reHelpIdent.MatchString(p) {
					ok = false
					break
				}
				cleaned = append(cleaned, p)
			}
			if ok {
				return cleaned
			}
		}
		return nil
	}
	sep := ","
	if strings.Contains(raw, "|") {
		sep = "|"
	}
	var out []string
	for _, p := range strings.Split(raw, sep) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func FinishLlamaFlag(f *LlamaFlag) {
	help := strings.TrimSpace(f.Help)
	help = reHelpEnv.ReplaceAllString(help, "")
	help = strings.Join(strings.Fields(help), " ")
	f.Help = help
	if m := reHelpDefault.FindStringSubmatch(help); len(m) > 1 {
		def := m[1]
		if def == "" {
			def = m[2]
		}
		if def == "" {
			def = strings.TrimSpace(m[3])
		}
		f.Default = strings.Trim(def, " .;")
	}
	if c := ParseHelpChoices(f.Arg); len(c) > 0 {
		f.Choices = MergeUniq(f.Choices, c)
	}
	if extra := ParseHelpChoicesFromHelp(help); len(extra) > 0 && len(f.Choices) == 0 {
		f.Choices = extra
	}
	if allowed := ParseHelpAllowed(help); len(allowed) > 0 {
		f.Choices = MergeUniq(f.Choices, allowed)
	}
	f.Deprecated = strings.Contains(strings.ToLower(help), "deprecated") || reHelpRemoved.MatchString(help)
	f.Kind = InferHelpKind(*f)
	f.Key = LookupFlagConfigKey(*f)
	f.Hidden = IsHiddenLlamaFlag(*f)
	f.Tier = FlagTierFor(*f)
}

func LookupFlagConfigKey(f LlamaFlag) string {
	if k := flagToConfigKey[f.ID]; k != "" {
		return k
	}
	for _, a := range append([]string{f.Flag, f.Short}, f.Aliases...) {
		id := strings.TrimPrefix(strings.TrimPrefix(a, "--"), "-")
		if k := flagToConfigKey[id]; k != "" {
			return k
		}
	}
	return ""
}

func FlagTierFor(f LlamaFlag) string {
	if f.Hidden {
		return ""
	}
	ids := []string{f.ID}
	for _, a := range append([]string{f.Flag, f.Short}, f.Aliases...) {
		ids = append(ids, strings.TrimPrefix(strings.TrimPrefix(a, "--"), "-"))
	}
	for _, id := range ids {
		if flagTierBase[id] {
			return "base"
		}
	}
	for _, id := range ids {
		if flagTierAdv[id] {
			return "advanced"
		}
	}
	return "expert"
}

func ParseHelpAllowed(help string) []string {
	m := reHelpAllowed.FindStringSubmatch(help)
	if len(m) < 2 {
		return nil
	}
	return ParseHelpChoices(strings.TrimSpace(m[1]))
}

func ParseHelpChoicesFromHelp(help string) []string {
	if m := reHelpChoiceB.FindStringSubmatch(help); len(m) == 2 && strings.Contains(m[1], "|") {
		return ParseHelpChoices("[" + m[1] + "]")
	}
	if m := reHelpChoiceC.FindStringSubmatch(help); len(m) == 2 {
		return ParseHelpChoices("{" + m[1] + "}")
	}
	return nil
}

func InferHelpKind(f LlamaFlag) string {
	if len(f.Choices) > 0 {
		return "enum"
	}
	arg := strings.TrimSpace(f.Arg)
	low := strings.ToLower(arg)
	if arg == "" {
		return "bool"
	}
	if strings.Contains(low, "on|off") || strings.Contains(low, "true|false") {
		return "enum"
	}
	switch {
	case strings.Contains(low, "fname") || strings.Contains(low, "file") || strings.Contains(low, "path") || strings.Contains(low, "dir") || strings.Contains(low, "url"):
		return "path"
	}
	if arg == "N" || strings.EqualFold(arg, "SEED") || reHelpRange.MatchString(arg) {
		if strings.Contains(f.Default, ".") || strings.Contains(strings.ToLower(f.Help), "float") {
			return "float"
		}
		return "int"
	}
	if strings.ContainsAny(f.Default, ".") && LooksNumeric(f.Default) {
		return "float"
	}
	return "string"
}

func LooksNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func IsHiddenLlamaFlag(f LlamaFlag) bool {
	id := f.ID
	switch id {
	case "help", "usage", "version", "completion-bash", "cache-list", "list-devices",
		"model", "model-url", "docker-repo", "hf-repo", "hf-file", "hf-token",
		"host", "port", "api-key", "api-key-file",
		"spec-default":
		return true
	}
	for _, p := range []string{"fim-", "gpt-oss-", "vision-gemma-", "embd-gemma"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

func MergeUniq(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range append(a, b...) {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

func ContainsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// FlagConfigKey returns the historical first-class config key for a native flag.
func FlagConfigKey(id string) (string, bool) { key, ok := flagToConfigKey[id]; return key, ok }
