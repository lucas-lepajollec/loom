// Package diagnostics builds an opt-in, bounded support archive. Application
// code selects metadata sources; this package never traverses a data directory.
package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const MaxSourceBytes = 128 << 10
const MaxBundleBytes = 2 << 20

type Source struct {
	Name  string
	Value any
	Log   string
	IsLog bool
}
type File struct {
	Path     string   `json:"path"`
	Bytes    int      `json:"bytes,omitempty"`
	Redacted []string `json:"redacted"`
}
type Manifest struct {
	Version  int      `json:"version"`
	Files    []File   `json:"files"`
	Excluded []string `json:"excluded"`
}
type Redactor struct {
	Secrets []string
	Homes   []string
}

var emailPattern = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9.-]+\.[a-z]{2,}`)
var homePattern = regexp.MustCompile(`(?i)(?:/home/|/Users/|[a-z]:[\\/]Users[\\/])[^\s/\\"'<>]+`)
var urlPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
var credentialPattern = regexp.MustCompile(`(?i)(?:bearer\s+[^\s"'<>]+|(?:sk-|ghp_|github_pat_|xox[baprs]-)[a-z0-9_-]+)`)
var privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)

func secretKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	for _, word := range []string{"password", "token", "secret", "credential", "authorization", "apikey", "webkey", "privatekey", "salt", "cookie", "recoverykey"} {
		if strings.Contains(k, word) {
			return true
		}
	}
	return k == "key" || k == "hash" || k == "env" || k == "headers"
}
func contentKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range []string{"prompt", "instruction", "messages", "transcript", "brain_content", "raw", "payload", "summary", "title", "content", "rawinput", "rawoutput"} {
		if strings.Contains(k, word) {
			return true
		}
	}
	return k == "error" || k == "warning" || k == "args" || k == "command" || k == "log"
}
func mark(kinds map[string]bool, kind string) { kinds[kind] = true }
func (r Redactor) text(s string, kinds map[string]bool) string {
	// Replace literal and common transport encodings before any structural
	// filtering, including when a secret appears in a source's map key.
	secrets := append([]string{}, r.Secrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, variant := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret), base64.StdEncoding.EncodeToString([]byte(secret)), base64.RawURLEncoding.EncodeToString([]byte(secret))} {
			if strings.Contains(s, variant) {
				s = strings.ReplaceAll(s, variant, "[REDACTED]")
				mark(kinds, "known_secret")
			}
		}
	}
	for _, home := range r.Homes {
		if home != "" && len(home) > 1 && strings.Contains(s, home) {
			s = strings.ReplaceAll(s, home, "[HOME]")
			mark(kinds, "home_path")
		}
	}
	replace := func(re *regexp.Regexp, replacement, kind string) {
		if re.MatchString(s) {
			s = re.ReplaceAllString(s, replacement)
			mark(kinds, kind)
		}
	}
	replace(privateKeyPattern, "[REDACTED]", "private_key")
	replace(credentialPattern, "[REDACTED]", "credential")
	replace(emailPattern, "[EMAIL]", "email")
	replace(homePattern, "[HOME]", "home_path")
	s = urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			mark(kinds, "url")
			return "[URL]"
		}
		// Endpoints and paths can contain bearer tokens too. The hostname alone
		// is sufficient for diagnosing an endpoint family.
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			mark(kinds, "url_credentials_path_query")
		}
		return u.Scheme + "://" + u.Host
	})
	return s
}
func (r Redactor) value(v any, kinds map[string]bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			key := r.text(k, kinds)
			if secretKey(k) {
				out[key] = "[REDACTED]"
				mark(kinds, "credential_field")
			} else if contentKey(k) {
				out[key] = "[OMITTED]"
				mark(kinds, "private_content")
			} else {
				out[key] = r.value(value, kinds)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = r.value(value, kinds)
		}
		return out
	case string:
		return r.text(x, kinds)
	default:
		return v
	}
}
func (r Redactor) JSON(value any) ([]byte, []string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > MaxSourceBytes {
		return nil, nil, errors.New("diagnostic source too large")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, nil, err
	}
	kinds := map[string]bool{}
	v = r.value(v, kinds)
	raw, err = json.MarshalIndent(v, "", "  ")
	return raw, sortedKinds(kinds), err
}
func sortedKinds(kinds map[string]bool) []string {
	out := []string{}
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Logs are untrusted arbitrary text and may contain unlabelled prompts. Retain
// line positions and recognizable severity only; never export free-text bodies.
// This is stronger than guessing which upstream log lines contain a prompt.
func (r Redactor) Log(raw string, lines int) ([]byte, []string) {
	if len(raw) > MaxSourceBytes {
		raw = raw[len(raw)-MaxSourceBytes:]
	}
	list := strings.Split(strings.TrimRight(raw, "\r\n"), "\n")
	if len(list) > lines {
		list = list[len(list)-lines:]
	}
	if raw == "" {
		list = nil
	}
	var out strings.Builder
	for _, line := range list {
		severity := "log"
		lower := strings.ToLower(line)
		for _, level := range []string{"error", "warn", "info", "debug"} {
			if strings.Contains(lower, level) {
				severity = level
				break
			}
		}
		out.WriteString(severity + " [REDACTED:log-content]\n")
	}
	return []byte(out.String()), []string{"log_content", "prompts", "credentials", "paths"}
}
func Build(sources []Source, r Redactor) ([]byte, Manifest, error) {
	manifest := Manifest{Version: 1, Files: []File{}, Excluded: []string{"prompts", "discussion_text", "brain_content", "protocol_frames", "unstructured_log_bodies", "databases", "environment"}}
	if len(sources) > 64 {
		return nil, manifest, errors.New("too many diagnostic sources")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	total := 0
	seen := map[string]bool{"manifest.json": true}
	for _, source := range sources {
		if source.Name != path.Clean(source.Name) || strings.HasPrefix(source.Name, "/") || strings.Contains(source.Name, "..") || strings.ContainsAny(source.Name, "\\\r\n") || seen[source.Name] {
			return nil, manifest, errors.New("invalid diagnostic file name")
		}
		seen[source.Name] = true
		var data []byte
		var kinds []string
		var err error
		if source.IsLog {
			data, kinds = r.Log(source.Log, 100)
		} else {
			data, kinds, err = r.JSON(source.Value)
		}
		if err != nil {
			return nil, manifest, err
		}
		total += len(data)
		if total > MaxBundleBytes {
			return nil, manifest, errors.New("diagnostic bundle too large")
		}
		file, err := writer.Create(source.Name)
		if err != nil {
			return nil, manifest, err
		}
		if _, err = file.Write(data); err != nil {
			return nil, manifest, err
		}
		manifest.Files = append(manifest.Files, File{source.Name, len(data), kinds})
	}
	manifest.Files = append(manifest.Files, File{Path: "manifest.json", Redacted: []string{}})
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, manifest, err
	}
	file, err := writer.Create("manifest.json")
	if err != nil {
		return nil, manifest, err
	}
	if _, err = file.Write(raw); err != nil {
		return nil, manifest, err
	}
	if err = writer.Close(); err != nil {
		return nil, manifest, err
	}
	return output.Bytes(), manifest, nil
}
