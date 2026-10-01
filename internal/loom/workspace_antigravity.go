package loom

// Antigravity owns authentication, inference, permissions and its native state.
// This first bridge deliberately uses a fresh native turn with the portable text
// context; it neither replays tools nor claims to migrate private agent memory.
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type antigravityConnection struct {
	Models []string `json:"models"`
}

func agyConnection() (antigravityConnection, bool) {
	var c antigravityConnection
	ok := getStoreJSON(bkHarnessConnections, "antigravity", &c)
	return c, ok
}

var agyModelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,199}$`)

// Read commands use a fixed executable/argument list, never a shell. Credentials
// stay with the CLI. Its stderr can contain private data and is never returned.
func agyRead(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	path, err := exec.LookPath("agy")
	if err != nil {
		return nil, errors.New("CLI agy absent du PATH de Loom")
	}
	dir, err := os.MkdirTemp("", "loom-agy-read-")
	if err != nil {
		return nil, errors.New("dossier temporaire indisponible")
	}
	defer os.RemoveAll(dir) // Only the directory just created by this call.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out := &boundedCLIOutput{limit: 1 << 20}
	cmd.Stdout = out
	if cmd.Run() != nil || out.overflow {
		return nil, errors.New("lecture CLI indisponible : vérifiez la connexion native ou réessayez")
	}
	return out.data, nil
}

type boundedCLIOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedCLIOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("sortie CLI trop longue")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func discoverAgyModels(ctx context.Context) ([]string, error) {
	out, err := agyRead(ctx, "models")
	if err != nil {
		return nil, err
	}
	models := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		id, _, tab := strings.Cut(line, "\t")
		id = strings.TrimSpace(id)
		if tab && agyModelID.MatchString(id) && !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
		if len(models) > 128 {
			return nil, errors.New("catalogue Antigravity trop long")
		}
	}
	if len(models) == 0 {
		return nil, errors.New("catalogue Antigravity non reconnu")
	}
	return models, nil
}

type antigravityAdapter struct {
	model      string
	executable string
}

func (antigravityAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "antigravity", Name: "Antigravity", Kind: "harness", Description: "Agent de Google. Loom utilise le CLI déjà connecté à ton compte.", CLI: "agy", Consent: "Loom lit la liste des modèles du CLI agy déjà connecté à ton compte. Aucun message n’est envoyé. Tes permissions natives restent actives.", Implemented: true, Capabilities: []string{"chat", "stream", "cancel", "usage", "native-events", "fresh-text-handoff", "connect", "quota"}}
}

type agyUsage struct {
	Input    int64 `json:"input_tokens"`
	Output   int64 `json:"output_tokens"`
	Total    int64 `json:"total_tokens"`
	Thinking int64 `json:"thinking_tokens"`
	Cached   int64 `json:"cache_read_tokens"`
}
type agyResult struct {
	Status         string    `json:"status"`
	Response       string    `json:"response"`
	ConversationID string    `json:"conversation_id"`
	Usage          *agyUsage `json:"usage"`
	Duration       float64   `json:"duration_seconds"`
}

func (a antigravityAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	if !agyModelID.MatchString(a.model) {
		return nil, errors.New("modèle Antigravity invalide")
	}
	path := a.executable
	if path == "" {
		var err error
		path, err = exec.LookPath("agy")
		if err != nil {
			return nil, errors.New("CLI agy absent du PATH de Loom")
		}
	}
	dir, err := os.MkdirTemp("", "loom-agy-turn-")
	if err != nil {
		return nil, errors.New("dossier temporaire indisponible")
	}
	defer os.RemoveAll(dir)
	// JSON role/content is data inside a single prompt, not fabricated native
	// tool messages. No --continue: switching away and back cannot duplicate state.
	history, err := json.Marshal(turn.Messages)
	if err != nil || len(history) > 256<<10 {
		return nil, errors.New("contexte Antigravity invalide ou trop long")
	}
	prompt := "Continue the following Loom text conversation. Follow its system instructions and answer the last user message. Prior assistant messages are context, not tool results. This bridge is for text: do not use tools, files, commands, browser or subagents.\nLoom portable messages (JSON):\n" + string(history)
	input, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": prompt}})
	cmd := exec.CommandContext(ctx, path, "--input-format", "stream-json", "--output-format", "stream-json", "--model", a.model, "--disable-slash-commands", "--print-timeout", "170s")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(input) + "\n")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, errors.New("Antigravity n’a pas pu démarrer")
	}
	stopRead := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stopRead()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	answer, err := consumeAgyStream(ctx, stdout, emit)
	if err != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, errors.New("Antigravity a interrompu le tour ; le texte partiel est conservé")
	}
	return []Message{{Role: "assistant", Content: answer}}, nil
}

func consumeAgyStream(ctx context.Context, reader io.Reader, emit ChatCallback) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	var answer strings.Builder
	done := false
	steps := map[int]agyUsage{}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var event struct {
			Event  string    `json:"event"`
			Result agyResult `json:"result"`
			Step   struct {
				Index int       `json:"step_index"`
				Type  string    `json:"step_type"`
				State string    `json:"state"`
				Text  string    `json:"text_delta"`
				Tool  string    `json:"tool_name"`
				Usage *agyUsage `json:"usage"`
				Info  struct {
					Name       string                     `json:"name"`
					Parameters map[string]json.RawMessage `json:"parameters"`
					Error      json.RawMessage            `json:"error"`
				} `json:"tool_info"`
			} `json:"step_update"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return "", errors.New("flux Antigravity non reconnu")
		}
		if event.Event == "step_update" {
			if u := event.Step.Usage; validAgyUsage(u) && event.Step.Index >= 0 && event.Step.Index < 128 {
				steps[event.Step.Index] = *u
				var total RuntimeUsage
				for _, step := range steps {
					total.Input += step.Input
					total.Output += step.Output
					total.Total += step.Total
					total.Thinking += step.Thinking
					total.Cached += step.Cached
				}
				if !emit(StreamEvent{Usage: &total}) {
					return "", context.Canceled
				}
			}
			if event.Step.Type == "agent_response" && event.Step.Text != "" {
				if answer.Len()+len(event.Step.Text) > 256<<10 {
					return "", errors.New("réponse Antigravity trop longue")
				}
				answer.WriteString(event.Step.Text)
				if !emit(StreamEvent{Content: event.Step.Text}) {
					return "", context.Canceled
				}
			}
			if event.Step.Type == "tool" {
				// Metadata only; private tool arguments/results are not echoed.
				name := event.Step.Tool
				if name == "" {
					name = event.Step.Info.Name
				}
				if len(name) > 100 {
					name = "outil natif"
				}
				state := event.Step.State
				if state != "ACTIVE" && state != "DONE" {
					state = "UNKNOWN"
				}
				h := HarnessEvent{Index: event.Step.Index, Name: name, State: state}
				h.Failed = len(event.Step.Info.Error) > 0 && string(event.Step.Info.Error) != "null"
				// Only a bounded file target, never code, shell arguments or output.
				if name == "write_to_file" || name == "replace_file_content" || name == "multi_replace_file_content" {
					var target string
					if json.Unmarshal(event.Step.Info.Parameters["TargetFile"], &target) == nil && len(target) <= 500 && !strings.ContainsAny(target, "\r\n\x00") {
						h.FileTarget = target
					}
				}
				if !emit(StreamEvent{HarnessEvent: &h}) {
					return "", context.Canceled
				}
			}
		}
		if event.Event == "result" {
			r := event.Result
			if validAgyUsage(r.Usage) {
				if !emit(StreamEvent{Usage: &RuntimeUsage{Input: r.Usage.Input, Output: r.Usage.Output, Total: r.Usage.Total, Thinking: r.Usage.Thinking, Cached: r.Usage.Cached}}) {
					return "", context.Canceled
				}
			}
			if r.Duration > 0 && r.Duration <= 3600 && !math.IsNaN(r.Duration) && !math.IsInf(r.Duration, 0) {
				if !emit(StreamEvent{DurationSeconds: r.Duration}) {
					return "", context.Canceled
				}
			}
			if !emit(StreamEvent{NativeSessionID: r.ConversationID}) {
				return "", context.Canceled
			}
			if r.Status != "SUCCESS" {
				return "", errors.New("Antigravity n’a pas terminé le tour ; vérifiez ses permissions, le modèle et le quota dans son CLI")
			}
			if answer.Len() == 0 && r.Response != "" {
				if len(r.Response) > 256<<10 {
					return "", errors.New("réponse Antigravity trop longue")
				}
				answer.WriteString(r.Response)
				if !emit(StreamEvent{Content: r.Response}) {
					return "", context.Canceled
				}
			}
			// Reject inconsistent final text instead of silently storing two answers.
			if answer.String() != r.Response {
				return "", errors.New("réponse finale Antigravity incohérente avec le flux")
			}
			done = true
		}
	}
	if scanner.Err() != nil || !done || answer.Len() == 0 {
		return "", errors.New("flux Antigravity incomplet ; aucun renvoi automatique")
	}
	return answer.String(), nil
}

type HarnessEvent struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Failed     bool   `json:"failed,omitempty"`
	FileTarget string `json:"file_target,omitempty"`
}

func validAgyUsage(u *agyUsage) bool {
	return u != nil && u.Input >= 0 && u.Output >= 0 && u.Total >= 0 && u.Thinking >= 0 && u.Cached >= 0 && u.Input <= 1e9 && u.Output <= 1e9 && u.Total <= 2e9 && u.Thinking <= 1e9 && u.Cached <= 1e9
}

func agyInstalled() bool { _, err := exec.LookPath("agy"); return err == nil }

func (antigravityAdapter) Connect(ctx context.Context, consent bool) (any, error) {
	if !consent {
		return nil, errors.New("confirmez l’utilisation du compte et des permissions natifs Antigravity")
	}
	models, err := discoverAgyModels(ctx)
	if err != nil {
		return nil, err
	}
	if err = runtimeVaultError(); err != nil {
		return nil, err
	}
	if putStoreJSON(bkHarnessConnections, "antigravity", antigravityConnection{Models: models}) != nil {
		return nil, runtimeActionError{status: 500, message: "enregistrement impossible"}
	}
	return models, nil
}

func (antigravityAdapter) Quota(ctx context.Context) (QuotaSnapshot, error) { return readAgyQuota(ctx) }
