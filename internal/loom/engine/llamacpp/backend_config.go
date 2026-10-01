package llamacpp

import (
	"fmt"
	"sort"
	"strings"
)

// UnquoteValue retire les guillemets ENTOURANTS d'une valeur, et seulement
// eux : une paire ouvrante/fermante du même caractère.
//
// Un simple Trim(v, `"`) mangeait le guillemet d'un argument interne — par
// exemple EXTRA_ARGS=--chat-template-file "/etc/loom/tpl.jinja" perdait son
// guillemet final et repartait déséquilibré dans le preset (issue #17 : contenu
// tronqué / guillemets non appariés à la création d'un preset).
func UnquoteValue(v string) string {
	if len(v) >= 2 {
		if q := v[0]; (q == '"' || q == '\'') && v[len(v)-1] == q {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// ParseEnv lit un fichier au format clé=valeur (les presets). Les lignes vides
// et les commentaires sont ignorés, les guillemets entourants retirés.
func ParseEnv(text string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(s, "export "), "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := UnquoteValue(strings.TrimSpace(v))
		if key == "SYSPROMPT" {
			val = strings.ReplaceAll(val, `\n`, "\n")
		}
		m[key] = val
	}
	return m
}

// QuoteValue rend une valeur telle que ParseEnv la relise à l'identique. On
// n'entoure de guillemets que ce qui en a besoin (espaces), et jamais une valeur
// qui contient déjà un guillemet : elle est écrite telle quelle, UnquoteValue ne
// touchant qu'à une paire entourante.
func QuoteValue(v string) string {
	if v == "" || strings.ContainsRune(v, '"') {
		return v
	}
	if strings.ContainsAny(v, " \t") || UnquoteValue(v) != v {
		return `"` + v + `"`
	}
	return v
}

// FormatEnv rend une configuration au format des presets, clés triées pour que
// deux écritures du même contenu donnent le même fichier.
func FormatEnv(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := m[k]
		if k == "SYSPROMPT" {
			v = strings.ReplaceAll(v, "\n", `\n`)
		}
		fmt.Fprintf(&b, "%s=%s\n", k, QuoteValue(v))
	}
	return b.String()
}

// SplitArgs découpe EXTRA_ARGS comme le ferait un shell : sur les espaces, mais
// en respectant les guillemets, pour qu'un chemin qui en contient reste UN seul
// argument (--chat-template-file "/mes modèles/tpl.jinja"). Le découpage
// précédent, sur le seul caractère espace, le coupait en deux et llama-server
// refusait de démarrer.
func SplitArgs(s string) []string {
	out := []string{}
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				// Guillemets vides ("" ) : on garde l'argument vide explicite.
				if cur.Len() == 0 {
					out = append(out, "")
				}
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
