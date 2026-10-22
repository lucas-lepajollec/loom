package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Détection d'un Codeman sur la machine de Loom, pour une connexion en un
// clic. On lit les mêmes emplacements que Codeman lui-même : le .env de son
// dossier de données (lu par ses clients) puis son service systemd ou launchd
// (relu par son installateur). Les mots de passe trouvés restent côté
// serveur : seule leur présence est annoncée au navigateur.

type codemanLocal struct {
	URL      string
	Username string
	Password string
	Source   string
}

func codemanHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseEnvAssignments(text string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || !envNameRe.MatchString(strings.TrimSpace(k)) {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// Une ligne Environment= de systemd : affectations séparées par des espaces,
// chacune éventuellement entre guillemets, avec \" et \\ échappés.
func systemdEnvironment(value string, into map[string]string) {
	var cur strings.Builder
	quoted, escaped := false, false
	flush := func() {
		if k, v, ok := strings.Cut(cur.String(), "="); ok && k != "" {
			into[k] = v
		}
		cur.Reset()
	}
	for _, r := range value {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quoted:
			escaped = true
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
}

var codemanPortFlag = regexp.MustCompile(`--port[= ]+(\d{1,5})`)
var codemanHostFlag = regexp.MustCompile(`--host[= ]+(\S+)`)

// Lit un service systemd utilisateur de Codeman et ses fichiers d'override.
func readCodemanSystemd(unit string) (env map[string]string, execStart string, ok bool) {
	files := []string{unit}
	drop, _ := filepath.Glob(unit + ".d/*.conf")
	sort.Strings(drop)
	files = append(files, drop...)
	env = map[string]string{}
	for i, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			if i == 0 {
				return nil, "", false
			}
			continue
		}
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if v, found := strings.CutPrefix(line, "Environment="); found {
				systemdEnvironment(v, env)
			} else if v, found := strings.CutPrefix(line, "ExecStart="); found && v != "" {
				execStart = v
			}
		}
	}
	return env, execStart, true
}

var plistKeyRe = regexp.MustCompile(`<key>([A-Z_]+)</key>\s*<string>([^<]*)</string>`)

func xmlUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}

func codemanBase(host, port string, https bool) string {
	scheme := "http"
	if https {
		scheme = "https"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "3000"
	}
	return scheme + "://" + strings.Trim(host, "[]") + ":" + port
}

// Toutes les installations locales trouvées, dans l'ordre de confiance.
func localCodemanInstalls() []codemanLocal {
	home := codemanHome()
	var out []codemanLocal
	add := func(l codemanLocal) {
		for i, o := range out {
			if o.URL == l.URL {
				if out[i].Password == "" && l.Password != "" {
					out[i].Password, out[i].Source = l.Password, l.Source
				}
				if out[i].Username == "" {
					out[i].Username = l.Username
				}
				return
			}
		}
		out = append(out, l)
	}
	dataDir := os.Getenv("CODEMAN_DATA_DIR")
	if dataDir == "" && home != "" {
		dataDir = filepath.Join(home, ".codeman")
	}
	fileEnv := map[string]string{}
	if dataDir != "" {
		if b, err := os.ReadFile(filepath.Join(dataDir, ".env")); err == nil && len(b) < 1<<20 {
			fileEnv = parseEnvAssignments(string(b))
		}
	}
	if home != "" {
		units, _ := filepath.Glob(filepath.Join(home, ".config", "systemd", "user", "codeman-web*.service"))
		sort.Strings(units)
		for _, unit := range units {
			env, exec, ok := readCodemanSystemd(unit)
			if !ok {
				continue
			}
			port, host := env["CODEMAN_PORT"], env["CODEMAN_HOST"]
			if m := codemanPortFlag.FindStringSubmatch(exec); m != nil {
				port = m[1]
			}
			if m := codemanHostFlag.FindStringSubmatch(exec); m != nil {
				host = m[1]
			}
			base := codemanBase(host, port, strings.Contains(exec, "--https"))
			if p := strings.Trim(env["CODEMAN_BASE_URL"], "/"); p != "" {
				base += "/" + p
			}
			add(codemanLocal{URL: base, Username: env["CODEMAN_USERNAME"], Password: env["CODEMAN_PASSWORD"], Source: "systemd"})
		}
		if b, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "com.codeman.web.plist")); err == nil {
			env := map[string]string{}
			for _, m := range plistKeyRe.FindAllStringSubmatch(string(b), -1) {
				env[m[1]] = xmlUnescape(m[2])
			}
			add(codemanLocal{URL: codemanBase(env["CODEMAN_HOST"], env["CODEMAN_PORT"], false), Username: env["CODEMAN_USERNAME"], Password: env["CODEMAN_PASSWORD"], Source: "launchd"})
		}
	}
	// Le port par défaut, même sans service connu (lancé à la main, Docker).
	add(codemanLocal{URL: codemanBase("", os.Getenv("CODEMAN_PORT"), false)})
	if os.Getenv("CODEMAN_PORT") != "" {
		add(codemanLocal{URL: codemanDefaultURL})
	}
	// Le .env et l'environnement de Loom valent pour toute installation locale
	// qui n'a pas son propre mot de passe (c'est l'ordre des clients Codeman).
	for i := range out {
		if out[i].Password == "" {
			if p := os.Getenv("CODEMAN_PASSWORD"); p != "" {
				out[i].Password, out[i].Source = p, "environment"
			} else if p := fileEnv["CODEMAN_PASSWORD"]; p != "" {
				out[i].Password, out[i].Source = p, "env-file"
			}
		}
		if out[i].Username == "" {
			out[i].Username = firstNonEmpty(os.Getenv("CODEMAN_USERNAME"), fileEnv["CODEMAN_USERNAME"], codemanDefaultUser)
		}
	}
	return out
}

type codemanProbe struct {
	URL     string `json:"url"`
	Found   bool   `json:"found"`
	Auth    string `json:"auth"` // open | required | ok | rejected
	Version string `json:"version,omitempty"`
	Local   bool   `json:"local_credentials"`
	Source  string `json:"source,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Sonde une adresse : Codeman répond à /api/v1/status (200 sans mot de
// passe, 401 avec le realm « Codeman » sinon).
func probeCodeman(ctx context.Context, base, username, password string) codemanProbe {
	p := codemanProbe{URL: base, Auth: "unknown"}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	do := func(auth bool) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/status", nil)
		if err != nil {
			return nil, err
		}
		if auth {
			req.SetBasicAuth(username, password)
		}
		return codemanHTTP(base).Do(req)
	}
	resp, err := do(false)
	if err != nil {
		p.Error = "unreachable"
		return p
	}
	readVersion := func(r *http.Response) bool {
		var st struct {
			Success bool `json:"success"`
			Data    struct {
				Version string `json:"version"`
			} `json:"data"`
		}
		ok := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&st) == nil && st.Success
		p.Version = st.Data.Version
		return ok
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized && strings.Contains(resp.Header.Get("WWW-Authenticate"), "Codeman"):
		p.Found, p.Auth = true, "required"
		if password != "" {
			if r2, err := do(true); err == nil {
				defer r2.Body.Close()
				if r2.StatusCode == http.StatusOK && readVersion(r2) {
					p.Auth = "ok"
				} else if r2.StatusCode == http.StatusUnauthorized {
					p.Auth = "rejected"
				}
			}
		}
	case resp.StatusCode == http.StatusOK && readVersion(resp):
		p.Found, p.Auth = true, "open"
	default:
		p.Error = "not-codeman"
	}
	return p
}

var codemanDiscoverMu sync.Mutex

func handleCodemanDiscover(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	codemanDiscoverMu.Lock()
	defer codemanDiscoverMu.Unlock()
	installs := localCodemanInstalls()
	probes := make([]codemanProbe, len(installs))
	var wg sync.WaitGroup
	for i, l := range installs {
		wg.Add(1)
		go func(i int, l codemanLocal) {
			defer wg.Done()
			probes[i] = probeCodeman(r.Context(), l.URL, l.Username, l.Password)
			probes[i].Local = l.Password != "" && probes[i].Auth == "ok"
			if probes[i].Local {
				probes[i].Source = l.Source
			}
		}(i, l)
	}
	wg.Wait()
	found := []codemanProbe{}
	for _, p := range probes {
		if p.Found {
			found = append(found, p)
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "found": found})
}

// Connexion en une étape : tester d'abord, n'enregistrer qu'en cas de succès.
func handleCodemanConnect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		UseLocal bool   `json:"use_local"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	base, err := normalizeCodemanURL(req.URL)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "code": "invalid"})
		return
	}
	user, pass := strings.TrimSpace(req.Username), req.Password
	if req.UseLocal {
		for _, l := range localCodemanInstalls() {
			if l.URL == base {
				user, pass = l.Username, l.Password
			}
		}
	}
	if user == "" {
		user = codemanDefaultUser
	}
	if !codemanUserRe.MatchString(user) || len(pass) > 1024 || strings.ContainsAny(pass, "\r\n") {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid credentials", "code": "invalid"})
		return
	}
	p := probeCodeman(r.Context(), base, user, pass)
	switch {
	case !p.Found:
		sendJSON(w, 200, map[string]any{"ok": false, "code": firstNonEmpty(p.Error, "not-codeman")})
		return
	case p.Auth == "required":
		sendJSON(w, 200, map[string]any{"ok": false, "code": "password"})
		return
	case p.Auth == "rejected":
		sendJSON(w, 200, map[string]any{"ok": false, "code": "rejected"})
		return
	}
	codemanMu.Lock()
	defer codemanMu.Unlock()
	if p.Auth == "open" {
		pass = ""
		if err := deleteProviderSecret(codemanSecretID); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "could not update the Codeman password"})
			return
		}
	} else if err := sealProviderSecret(codemanSecretID, pass); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not store the Codeman password"})
		return
	}
	prev := loadCodemanConfig()
	ui := prev.UIURL
	if prev.URL != base {
		ui = ""
	}
	if err := saveCodemanConfig(codemanConfig{URL: base, Username: user, UIURL: ui}); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save the Codeman configuration"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "url": base, "version": p.Version, "auth": p.Auth})
}

// Déconnexion : oublie l'adresse et le mot de passe.
func handleCodemanDisconnect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	codemanMu.Lock()
	defer codemanMu.Unlock()
	if err := deleteProviderSecret(codemanSecretID); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not remove the Codeman password"})
		return
	}
	if err := os.Remove(codemanConfigPath()); err != nil && !os.IsNotExist(err) {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not remove the Codeman configuration"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
