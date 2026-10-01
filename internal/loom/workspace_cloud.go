package loom

// A deliberately small Chat Completions adapter. No local tool loop, model
// loading, compaction or hidden fallback is applied to a remote provider.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CloudProvider struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Endpoint  string   `json:"endpoint"`
	Model     string   `json:"model"`
	Models    []string `json:"models,omitempty"`
	UsageMode string   `json:"usage_mode,omitempty"` // "none" for APIs rejecting stream_options.
	// Remember: the key is kept in the OS keychain (never in Loom files).
	Remember bool `json:"remember,omitempty"`
	// Credentials are intentionally absent from this persistent record.
	Ready bool `json:"ready"`
}

type RuntimeUsage struct {
	Input          int64 `json:"prompt_tokens"`
	Output         int64 `json:"completion_tokens"`
	Total          int64 `json:"total_tokens"`
	Thinking       int64 `json:"thinking_tokens,omitempty"`
	Cached         int64 `json:"cache_read_tokens,omitempty"`
	inputReported  bool
	outputReported bool
}

// Keep field presence for consumers that must distinguish missing usage from zero.
func (u *RuntimeUsage) UnmarshalJSON(data []byte) error {
	type usageAlias RuntimeUsage
	var wire struct {
		usageAlias
		Input  *int64 `json:"prompt_tokens"`
		Output *int64 `json:"completion_tokens"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*u = RuntimeUsage(wire.usageAlias)
	if wire.Input != nil {
		u.Input, u.inputReported = *wire.Input, true
	}
	if wire.Output != nil {
		u.Output, u.outputReported = *wire.Output, true
	}
	return nil
}

func validateCloudEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("URL de base HTTPS requise, sans identifiants, paramètres ni fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && localNetworkHost(u.Hostname())) {
		return "", errors.New("HTTPS requis (HTTP autorisé seulement sur cette machine ou le réseau local)")
	}
	if len(raw) > 2048 {
		return "", errors.New("URL trop longue")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// localNetworkHost reports whether plain HTTP stays on this machine or the local
// network: loopback, private, link-local or CGNAT (Tailscale) addresses, and
// local host names (localhost, single label, .local, .lan, .home.arpa…).
func localNetworkHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(h); ip != nil {
		_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
	}
	if h == "localhost" || !strings.Contains(h, ".") {
		return h != ""
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".home", ".internal", ".home.arpa", ".ts.net"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

type cloudRuntimeAdapter struct {
	provider CloudProvider
	key      string
	client   *http.Client
}

func (cloudRuntimeAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "openai-compatible", Name: "API compatible Chat Completions", Kind: "cloud", Description: "API cloud compatible Chat Completions, configurée explicitement.", Implemented: true, Capabilities: []string{"chat", "stream", "cancel", "usage"}}
}

func (a cloudRuntimeAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	endpoint, err := validateCloudEndpoint(a.provider.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.key) == "" {
		return nil, errors.New("clé absente : reconnectez le provider dans Modèles → Providers")
	}
	payload := map[string]any{
		"model": a.provider.Model, "messages": turn.Messages, "stream": true,
	}
	if turn.MaxTokens > 0 {
		payload["max_tokens"] = turn.MaxTokens
	}
	if a.provider.UsageMode != "none" {
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("messages invalides")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("destination invalide")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+a.key)
	client := a.client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	// Even same-origin redirects are rejected: an explicit endpoint is a consent
	// boundary. Never forward credentials or context to a redirected destination.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("provider injoignable ou délai dépassé ; vérifiez la connexion")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never echo a provider body: it may contain the credential or prompt.
		return nil, fmt.Errorf("le provider a refusé la requête (HTTP %d) ; vérifiez la clé, le modèle et votre quota", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return nil, errors.New("le provider ne renvoie pas un flux SSE compatible")
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	var answer strings.Builder
	finish := ""
	done := false
	var data []string
	consume := func() error {
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "" {
			return nil
		}
		if payload == "[DONE]" {
			done = true
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string          `json:"content"`
					Refusal   string          `json:"refusal"`
					ToolCalls json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage *RuntimeUsage   `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			return errors.New("événement provider invalide")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("le provider a signalé une erreur pendant la réponse")
		}
		if chunk.Usage != nil && !emit(StreamEvent{Usage: chunk.Usage}) {
			return context.Canceled
		}
		if len(chunk.Choices) > 0 {
			c := chunk.Choices[0]
			if len(c.Delta.ToolCalls) > 0 && string(c.Delta.ToolCalls) != "null" && string(c.Delta.ToolCalls) != "[]" {
				return errors.New("ce connecteur texte n’exécute pas d’outils")
			}
			part := c.Delta.Content + c.Delta.Refusal
			if answer.Len()+len(part) > 256<<10 {
				return errors.New("réponse trop longue (256 Kio maximum)")
			}
			if part != "" {
				answer.WriteString(part)
				if !emit(StreamEvent{Content: part}) {
					return context.Canceled
				}
			}
			if c.Finish != "" {
				finish = c.Finish
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := consume(); err != nil {
				return nil, err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("flux interrompu : la réponse partielle est conservée")
	}
	if len(data) > 0 {
		if err := consume(); err != nil {
			return nil, err
		}
	}
	if !done && finish == "" {
		return nil, errors.New("flux interrompu avant la fin de la réponse")
	}
	// Reaching an explicitly requested benchmark budget is a completed measurement.
	if finish != "" && finish != "stop" && !(finish == "length" && turn.MaxTokens > 0) {
		return nil, fmt.Errorf("réponse incomplète (fin : %s)", safeFinishReason(finish))
	}
	if answer.Len() == 0 {
		return nil, errors.New("le provider a terminé sans réponse textuelle")
	}
	return []Message{{Role: "assistant", Content: answer.String()}}, nil
}

func safeFinishReason(reason string) string {
	switch reason {
	case "length":
		return "limite de longueur"
	case "content_filter":
		return "filtre du provider"
	default:
		return "fonction non prise en charge"
	}
}
