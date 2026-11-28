package loom

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

// cmdMCPBridge relays newline-delimited JSON-RPC from stdin to the stateless
// gateway and writes each response on stdout, for agents limited to stdio MCP.
// Discovery order:
//  1. Explicit --url flag / LOOM_MCP_URL + LOOM_MCP_TOKEN / LOOM_GATEWAY_*
//  2. Automatic per-session bridge file <loomDir>/mcp-sessions/$CODEMAN_SESSION_ID.json
//     written with 0600 permissions when Loom starts a session.
func cmdMCPBridge(args []string) error {
	fs := flag.NewFlagSet("mcp-bridge", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	urlFlag := fs.String("url", "", "Loom MCP gateway URL")
	homeFlag := fs.String("home", "", "Loom data directory (defaults to LOOM_HOME or ~/.loom)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sessionID := firstNonEmpty(
		strings.TrimSpace(os.Getenv("LOOM_SESSION_ID")),
		strings.TrimSpace(os.Getenv("CODEMAN_SESSION_ID")),
	)
	target := firstNonEmpty(
		strings.TrimSpace(*urlFlag),
		strings.TrimSpace(os.Getenv("LOOM_MCP_URL")),
		strings.TrimSpace(os.Getenv("LOOM_GATEWAY_URL")),
	)
	token := firstNonEmpty(
		strings.TrimSpace(os.Getenv("LOOM_MCP_TOKEN")),
		strings.TrimSpace(os.Getenv("LOOM_GATEWAY_TOKEN")),
	)
	if (target == "" || token == "") && sessionID != "" {
		if sf, ok := readSessionBridgeFile(strings.TrimSpace(*homeFlag), sessionID); ok {
			if target == "" {
				target = sf.URL
			}
			if token == "" && target == sf.URL {
				token = sf.Token
			}
		}
	}
	if target == "" {
		return errors.New("LOOM_MCP_URL or LOOM_GATEWAY_URL is required (or run inside a Loom-started Codeman session)")
	}
	return mcpBridgeWithSession(os.Stdin, os.Stdout, target, token, sessionID, &http.Client{Timeout: 11 * time.Minute})
}

func readSessionBridgeFile(customHome, sessionID string) (sessionBridgeFile, bool) {
	name := safeSessionFileName(sessionID)
	if name == "" {
		return sessionBridgeFile{}, false
	}
	dirs := []string{}
	if customHome != "" {
		dirs = append(dirs, customHome)
	}
	if envHome := strings.TrimSpace(os.Getenv("LOOM_HOME")); envHome != "" {
		dirs = append(dirs, envHome)
	}
	dirs = append(dirs, LoomHome())
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		root, err := os.OpenRoot(filepath.Join(dir, "mcp-sessions"))
		if err != nil {
			continue
		}
		f, err := root.Open(name)
		_ = root.Close()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 8193))
		_ = f.Close()
		if err != nil {
			continue
		}
		var sf sessionBridgeFile
		if len(raw) <= 8192 && json.Unmarshal(raw, &sf) == nil && sf.SessionID == sessionID && resources.ValidMCPURL(sf.URL) {
			return sf, true
		}
	}
	return sessionBridgeFile{}, false
}

func mcpBridge(in io.Reader, out io.Writer, url, token string, client *http.Client) error {
	return mcpBridgeWithSession(in, out, url, token, "", client)
}

func mcpBridgeWithSession(in io.Reader, out io.Writer, targetURL, token, sessionID string, client *http.Client) error {
	if !resources.ValidMCPURL(targetURL) {
		return errors.New("invalid gateway URL")
	}
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(line))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if sessionID != "" {
			req.Header.Set("X-Loom-Session-Id", sessionID)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if err := bridgeError(out, line, "gateway unavailable"); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			if err := bridgeError(out, line, fmt.Sprintf("gateway returned HTTP %d", resp.StatusCode)); err != nil {
				return err
			}
			continue
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
			_ = resp.Body.Close()
			continue
		}
		if strings.HasPrefix(ct, "text/event-stream") {
			sse := bufio.NewScanner(resp.Body)
			sse.Buffer(make([]byte, 64<<10), 8<<20)
			for sse.Scan() {
				l := sse.Text()
				if strings.HasPrefix(l, "data:") {
					payload := strings.TrimSpace(l[5:])
					if payload != "" {
						if _, werr := io.WriteString(out, payload+"\n"); werr != nil {
							_ = resp.Body.Close()
							return werr
						}
					}
				}
			}
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
		_ = resp.Body.Close()
		body = bytes.TrimSpace(body)
		// Notifications get 202 and no body; only responses go back.
		if err != nil || len(body) > 16<<20 || !json.Valid(body) {
			if err := bridgeError(out, line, "invalid or oversized gateway response"); err != nil {
				return err
			}
			continue
		}
		if _, err := out.Write(append(body, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}

func bridgeError(out io.Writer, request []byte, message string) error {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(request, &envelope) != nil || len(envelope.ID) == 0 {
		return nil
	}
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": envelope.ID, "error": map[string]any{"code": -32000, "message": message}})
}
