package loom

import (
	"bufio"
	"bytes"

	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"time"
)

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
