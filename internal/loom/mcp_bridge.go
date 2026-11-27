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
			if token == "" {
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
		raw, err := os.ReadFile(filepath.Join(dir, "mcp-sessions", name))
		if err != nil {
			continue
		}
		var sf sessionBridgeFile
		if json.Unmarshal(raw, &sf) == nil && sf.URL != "" {
			return sf, true
		}
	}
	return sessionBridgeFile{}, false
}

func mcpBridge(in io.Reader, out io.Writer, url, token string, client *http.Client) error {
	return mcpBridgeWithSession(in, out, url, token, "", client)
}

func mcpBridgeWithSession(in io.Reader, out io.Writer, targetURL, token, sessionID string, client *http.Client) error {
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
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "loom mcp-bridge:", err)
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
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		body = bytes.TrimSpace(body)
		// Notifications get 202 and no body; only responses go back.
		if err != nil || len(body) == 0 {
			continue
		}
		if _, err := out.Write(append(body, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}
