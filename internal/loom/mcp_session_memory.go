package loom

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Agents Loom launches get their memory without any config: the gateway is
// passed per session (HTTP when the agent supports it, else a stdio bridge),
// authenticated by a token that lives only in this process.
var sessionGatewayToken = func() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}()

func sessionGatewayAuthorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(sessionGatewayToken)) == 1
}

// sessionGatewayServer is the ACP mcpServers entry for Loom's own memory, or
// nil when the agent runs on another machine (127.0.0.1 would not be Loom).
func sessionGatewayServer(caps map[string]any, remote bool) map[string]any {
	if remote || os.Getenv("LOOM_NO_SESSION_MEMORY") == "1" {
		return nil
	}
	httpCaps, _ := caps["mcpCapabilities"].(map[string]any)
	if httpCaps["http"] == true {
		return map[string]any{"type": "http", "name": "loom", "url": gatewayURL(), "headers": []any{map[string]string{"name": "Authorization", "value": "Bearer " + sessionGatewayToken}}}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return map[string]any{"name": "loom", "command": exe, "args": []string{"mcp-bridge"}, "env": []any{map[string]string{"name": "LOOM_GATEWAY_URL", "value": gatewayURL()}, map[string]string{"name": "LOOM_GATEWAY_TOKEN", "value": sessionGatewayToken}}}
}

// cmdMCPBridge relays newline-delimited JSON-RPC from stdin to the stateless
// gateway and writes each response on stdout, for agents limited to stdio MCP.
func cmdMCPBridge(args []string) error {
	url, token := os.Getenv("LOOM_GATEWAY_URL"), os.Getenv("LOOM_GATEWAY_TOKEN")
	if url == "" {
		return errors.New("LOOM_GATEWAY_URL is required")
	}
	return mcpBridge(os.Stdin, os.Stdout, url, token, &http.Client{Timeout: 90 * time.Second})
}

func mcpBridge(in io.Reader, out io.Writer, url, token string, client *http.Client) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		req, err := http.NewRequest("POST", url, bytes.NewReader(line))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "loom mcp-bridge:", err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
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
