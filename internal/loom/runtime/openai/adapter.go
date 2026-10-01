// A deliberately small Chat Completions adapter. No local tool loop, model
// loading, compaction or hidden fallback is applied to a remote provider.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config contains only the provider settings consumed by a text turn.
// Persistent records, credential storage and model selection belong to Loom.
type Config struct {
	Endpoint  string
	Model     string
	UsageMode string
}

type ProviderSource interface{ ProviderConfig() Config }
type CredentialSource interface{ APIKey() string }
type ClientSource interface{ HTTPClient() *http.Client }

// Adapter consumes resolved application inputs without reading application state.
// The client is copied for each request to enforce the no-redirect boundary.
type Adapter struct {
	Provider    ProviderSource
	Credentials CredentialSource
	Client      ClientSource
}

// Messages are marshaled directly to preserve the caller's existing JSON shape.
type Turn struct {
	Messages  any
	MaxTokens int
}

type Event struct {
	Content string
	Usage   *Usage
}

func (a Adapter) Run(ctx context.Context, turn Turn, emit func(Event) bool) (string, error) {
	provider := a.Provider.ProviderConfig()
	endpoint, err := ValidateEndpoint(provider.Endpoint)
	if err != nil {
		return "", err
	}
	key := a.Credentials.APIKey()
	if strings.TrimSpace(key) == "" {
		return "", errors.New("clé absente : reconnectez le provider dans Modèles → Providers")
	}
	payload := map[string]any{
		"model": provider.Model, "messages": turn.Messages, "stream": true,
	}
	if turn.MaxTokens > 0 {
		payload["max_tokens"] = turn.MaxTokens
	}
	if provider.UsageMode != "none" {
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("messages invalides")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", errors.New("destination invalide")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+key)
	client := a.Client.HTTPClient()
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
			return "", ctx.Err()
		}
		return "", errors.New("provider injoignable ou délai dépassé ; vérifiez la connexion")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never echo a provider body: it may contain the credential or prompt.
		return "", fmt.Errorf("le provider a refusé la requête (HTTP %d) ; vérifiez la clé, le modèle et votre quota", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return "", errors.New("le provider ne renvoie pas un flux SSE compatible")
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
			Usage *Usage          `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			return errors.New("événement provider invalide")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("le provider a signalé une erreur pendant la réponse")
		}
		if chunk.Usage != nil && !emit(Event{Usage: chunk.Usage}) {
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
				if !emit(Event{Content: part}) {
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
				return "", err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return "", errors.New("flux interrompu : la réponse partielle est conservée")
	}
	if len(data) > 0 {
		if err := consume(); err != nil {
			return "", err
		}
	}
	if !done && finish == "" {
		return "", errors.New("flux interrompu avant la fin de la réponse")
	}
	// Reaching an explicitly requested benchmark budget is a completed measurement.
	if finish != "" && finish != "stop" && !(finish == "length" && turn.MaxTokens > 0) {
		return "", fmt.Errorf("réponse incomplète (fin : %s)", SafeFinishReason(finish))
	}
	if answer.Len() == 0 {
		return "", errors.New("le provider a terminé sans réponse textuelle")
	}
	return answer.String(), nil
}

func SafeFinishReason(reason string) string {
	switch reason {
	case "length":
		return "limite de longueur"
	case "content_filter":
		return "filtre du provider"
	default:
		return "fonction non prise en charge"
	}
}
