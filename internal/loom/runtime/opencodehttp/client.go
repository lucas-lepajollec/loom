// Package opencodehttp speaks the official OpenCode HTTP/SSE server protocol.
package opencodehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const TestedVersion = "1.18.33"
const MaxFrame = 4 << 20

// MaxResponse bounds one JSON reply; /provider returns the whole models.dev
// catalogue, far larger than one SSE frame.
const MaxResponse = 64 << 20

type Client struct {
	BaseURL, Username, Password string
	HTTP                        *http.Client
}

func New(base, username, password string) *Client {
	return &Client{BaseURL: strings.TrimRight(base, "/"), Username: username, Password: password, HTTP: &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OpenCode redirect refused") }}}
}
func (c *Client) request(ctx context.Context, method, path, dir string, body any) (*http.Response, error) {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if len(data) > MaxFrame {
			return nil, errors.New("OpenCode request too large")
		}
	}
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if dir != "" {
		q.Set("directory", dir)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, MaxFrame))
		return nil, errors.New(ErrorMessage(b, fmt.Sprintf("OpenCode HTTP %d", resp.StatusCode)))
	}
	return resp, nil
}
func (c *Client) Call(ctx context.Context, method, path, dir string, body, result any) error {
	resp, err := c.request(ctx, method, path, dir, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponse+1))
	if err != nil {
		return err
	}
	if len(data) > MaxResponse {
		return errors.New("OpenCode response too large")
	}
	if result != nil && json.Unmarshal(data, result) != nil {
		return errors.New("invalid OpenCode response")
	}
	return nil
}
func ErrorMessage(raw []byte, fallback string) string {
	var v struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &v) == nil {
		if v.Data.Message != "" {
			return v.Data.Message
		}
		if v.Message != "" {
			return v.Message
		}
		if len(v.Error) > 0 {
			return ErrorMessage(v.Error, fallback)
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	if len(raw) > 0 && !json.Valid(raw) {
		return string(raw)
	}
	return fallback
}
func (c *Client) Health(ctx context.Context) (string, error) {
	var h struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	err := c.Call(ctx, "GET", "/global/health", "", nil, &h)
	if err == nil && !h.Healthy {
		err = errors.New("OpenCode server is unhealthy")
	}
	return h.Version, err
}
func (c *Client) Models(ctx context.Context, dir string) ([]json.RawMessage, error) {
	var p struct {
		All []struct {
			ID     string                     `json:"id"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"all"`
		Connected []string `json:"connected"`
	}
	if err := c.Call(ctx, "GET", "/provider", dir, nil, &p); err != nil {
		return nil, err
	}
	out := []json.RawMessage{}
	for _, provider := range p.All {
		connected := false
		for _, id := range p.Connected {
			if id == provider.ID {
				connected = true
			}
		}
		if !connected {
			continue
		}
		for id, raw := range provider.Models {
			var model map[string]any
			if json.Unmarshal(raw, &model) != nil {
				continue
			}
			model["id"] = id
			model["model"] = provider.ID + "/" + id
			model["provider"] = provider.ID
			b, _ := json.Marshal(model)
			out = append(out, b)
		}
	}
	return out, nil
}
