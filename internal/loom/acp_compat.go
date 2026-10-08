package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

const acpMaxFrame = acp.MaxFrame
const acpLegacyModelKey = acp.LegacyModelKey

var errACPClosed = acp.ErrClosed

type acpFrame = acp.Frame
type acpRPCError = acp.RPCError
type acpSessionResponse acp.SessionResponse
type acpOptionValue = acp.OptionValue

// Retain the historical callbacks and done channel at the application boundary.
type acpClient struct {
	*acp.Client
	done    <-chan struct{}
	handler func(*acpFrame) (any, error)
	notify  func(acpFrame)
	cleanup func()
}

func startACPClient(command string, args []string, cwd string, env ...string) (*acpClient, error) {
	argv, err := harnessNativeArgv(append([]string{command}, args...))
	if err != nil {
		return nil, errors.New("could not find the ACP launcher")
	}
	bridgeEnv, cleanup, err := nodeBridgeLaunchEnv(args)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, env...)
	}
	cmd.Env = append(cmd.Env, bridgeEnv...)
	c, err := acp.NewClient(cmd)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &acpClient{Client: c, done: c.Done(), notify: c.Notify, handler: c.Handler, cleanup: cleanup}, nil
}
func (c *acpClient) start() error {
	c.Client.Handler, c.Client.Notify = c.handler, c.notify
	return c.Client.Start()
}
func (c *acpClient) close() {
	c.Client.Close()
	if c.cleanup != nil {
		c.cleanup()
	}
}
func (c *acpClient) call(ctx context.Context, method string, params, result any) error {
	return c.Client.Call(ctx, method, params, result)
}
func (c *acpClient) notification(method string, params any) error {
	return c.Client.Notification(method, params)
}
func acpProcessGroup(cmd *exec.Cmd)                                 { acp.ProcessGroup(cmd) }
func acpKillProcessGroup(cmd *exec.Cmd)                             { acp.KillProcessGroup(cmd) }
func acpClip(text string, n int) string                             { return acp.Clip(text, n) }
func acpCompact(value any, n int) string                            { return acp.Compact(value, n) }
func acpCloneMap(value map[string]any) map[string]any               { return acp.CloneMap(value) }
func (p *acpBinding) tool(update map[string]any) map[string]any     { return acp.Tool(p.tools, update) }
func acpParseUpdate(raw json.RawMessage) (acp.SessionUpdate, error) { return acp.ParseUpdate(raw) }
func (r acpSessionResponse) options() []map[string]any              { return acp.SessionResponse(r).Options() }
func acpModelOption(config []map[string]any) map[string]any         { return acp.ModelOption(config) }
func acpOptionValues(option map[string]any) []acpOptionValue        { return acp.OptionValues(option) }
func acpConfigValueAllowed(option map[string]any, value any) bool {
	return acp.ConfigValueAllowed(option, value)
}
func acpLineCounts(before, after string) (int, int)    { return acp.LineCounts(before, after) }
func acpUnifiedDiff(path, before, after string) string { return acp.UnifiedDiff(path, before, after) }
func newReplayBuilder() *acp.ReplayBuilder[Message, RuntimeTurnRecord, DiscussionEvent] {
	return acp.NewReplayBuilder(
		func(role, content string) Message { return Message{Role: role, Content: content} },
		func(index int, id, name, native string, events []DiscussionEvent) RuntimeTurnRecord {
			return RuntimeTurnRecord{MessageIndex: index, RuntimeID: id, ProviderName: name, Model: "default", NativeSessionID: native, ACPEvents: events}
		},
	)
}
func runAgyACP(in io.Reader, out io.Writer) {
	(acp.AgyBridge{Read: agyRead, Executable: "agy"}).Run(in, out)
}
func discoverAgyModelsNamed(ctx context.Context) ([][2]string, error) {
	return acp.DiscoverAgyModelsNamed(ctx, agyRead)
}
func firstNonEmpty(v ...string) string { return acp.FirstNonEmpty(v...) }

func acpUpdateEvent(update map[string]any, tools map[string]map[string]any) DiscussionEvent {
	return acp.UpdateEvent(update, tools)
}
