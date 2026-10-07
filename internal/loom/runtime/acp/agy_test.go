//go:build !windows

package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeAgy = `#!/bin/sh
case "$1" in
  --version) echo "1.2.14"; exit 0;;
  models) echo "Fetching"; printf 'gem-high\tGem High\ngem-low\tGem Low\n'; exit 0;;
esac
echo "$@" > "$AGY_ARGS"
cat > /dev/null
cat <<'EOS'
{"event":"init","conversation_id":"conv-1"}
{"event":"step_update","step_update":{"conversation_id":"conv-1","step_index":1,"step_type":"agent_response","state":"DONE","text_delta":"Je lis."}}
{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"ACTIVE","tool_name":"write_to_file","tool_info":{"name":"write_to_file","parameters":{"TargetFile":"note.txt"}}}}
EOS
sleep 0.3; printf 'salut' > note.txt
cat <<'EOS'
{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"DONE","tool_name":"write_to_file","tool_info":{"name":"write_to_file","parameters":{"TargetFile":"note.txt"}}}}
{"event":"step_update","step_update":{"step_index":3,"step_type":"tool","state":"DONE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"ls"},"error":{"message":"denied"}}}}
{"event":"step_update","step_update":{"step_index":4,"step_type":"tool","state":"DONE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo hi"},"output":"hi\r\n"}}}
{"event":"result","result":{"conversation_id":"conv-1","status":"SUCCESS","response":"Je lis."}}
EOS
`

func TestAgyBridgeRefusesUnavailableOrEmptyCatalog(t *testing.T) {
	for _, empty := range []bool{false, true} {
		var out bytes.Buffer
		bridge := AgyBridge{Read: func(context.Context, ...string) ([]byte, error) {
			if empty {
				return []byte("not a model catalog"), nil
			}
			return nil, errors.New("synthetic-private-native-error")
		}}
		bridge.Run(strings.NewReader("{\"id\":1,\"method\":\"session/new\"}\n{\"id\":2,\"method\":\"session/load\",\"params\":{\"sessionId\":\"old\"}}\n"), &out)
		if strings.Contains(out.String(), "synthetic-private") || strings.Count(out.String(), "\"error\"") != 2 || strings.Contains(out.String(), "configOptions") {
			t.Fatal("unavailable native account/catalog exposed a usable session")
		}
	}
}

func TestAgyBridgeSpeaksACP(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(fakeAgy), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("AGY_ARGS", argsFile)
	work := t.TempDir()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		(AgyBridge{Executable: filepath.Join(bin, "agy"), Read: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, filepath.Join(bin, "agy"), args...).Output()
		}}).Run(inR, outW)
		outW.Close()
	}()
	frames := make(chan map[string]any, 64)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var m map[string]any
			_ = json.Unmarshal(sc.Bytes(), &m)
			frames <- m
		}
		close(frames)
	}()
	send := func(id int, method string, params any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		_, _ = inW.Write(append(b, '\n'))
	}
	wait := func(id int) (map[string]any, []map[string]any) {
		updates := []map[string]any{}
		for {
			select {
			case f := <-frames:
				if f["method"] == "session/update" {
					updates = append(updates, f["params"].(map[string]any)["update"].(map[string]any))
					continue
				}
				if v, ok := f["id"].(float64); ok && int(v) == id {
					return f, updates
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("pas de réponse à %d", id)
			}
		}
	}
	send(1, "initialize", map[string]any{"protocolVersion": 1})
	if r, _ := wait(1); r["result"].(map[string]any)["agentInfo"].(map[string]any)["version"] != "1.2.14" {
		t.Fatalf("%v", r)
	}
	send(2, "session/new", map[string]any{"cwd": work, "additionalDirectories": []any{"/srv/lib"}, "mcpServers": []any{}})
	r, _ := wait(2)
	res := r["result"].(map[string]any)
	sid := res["sessionId"].(string)
	opts := res["configOptions"].([]any)[0].(map[string]any)
	if opts["currentValue"] != "gem-high" || len(opts["options"].([]any)) != 2 {
		t.Fatalf("modèles: %v", opts)
	}
	send(3, "session/set_mode", map[string]any{"sessionId": sid, "modeId": "full"})
	wait(3)
	send(4, "session/set_config_option", map[string]any{"sessionId": sid, "configId": "model", "value": "gem-low"})
	wait(4)
	send(5, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "écris"}}})
	r, updates := wait(5)
	if r["result"].(map[string]any)["stopReason"] != "end_turn" {
		t.Fatalf("%v", r)
	}
	kinds := []string{}
	for _, u := range updates {
		kinds = append(kinds, u["sessionUpdate"].(string))
	}
	if strings.Join(kinds, ",") != "agent_message_chunk,tool_call,tool_call_update,tool_call_update,tool_call_update" {
		t.Fatalf("mises à jour: %v", kinds)
	}
	write := updates[2]
	if c, ok := updates[1]["content"]; ok {
		t.Fatalf("pas de contenu avant l'écriture sans CodeContent: %v", c)
	}
	diff := write["content"].([]any)[0].(map[string]any)
	if write["kind"] != "edit" || diff["path"] != filepath.Join(work, "note.txt") || diff["newText"] != "salut" {
		t.Fatalf("écriture: %v", write)
	}
	if updates[3]["status"] != "failed" || updates[3]["title"] != "ls" || updates[3]["kind"] != "execute" {
		t.Fatalf("commande refusée: %v", updates[3])
	}
	if out := fmt.Sprint(updates[4]["content"]); !strings.Contains(out, "hi") {
		t.Fatalf("command output not forwarded: %v", updates[4])
	}
	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"--model gem-low", "--dangerously-skip-permissions", "--add-dir /srv/lib", "--output-format stream-json"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("argument %q absent: %s", want, args)
		}
	}
	// The next turn resumes agy's conversation.
	send(6, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "suite"}}})
	wait(6)
	args, _ = os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--conversation conv-1") {
		t.Fatalf("reprise absente: %s", args)
	}
	inW.Close()
}

// A tool agy starts but never finishes (refused in Cautious mode) must not stay
// "in progress" forever: it is closed as failed with an explanation.
func TestAgyBridgeClosesUnfinishedTools(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\ncat <<'EOS'\n" +
		`{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","state":"ACTIVE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"git status -sb"}}}}` + "\n" +
		`{"event":"result","result":{"status":"SUCCESS"}}` + "\nEOS\n"
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	updates := []map[string]any{}
	session := &agySession{mode: "default", cwd: t.TempDir()}
	stop, err := (AgyBridge{Executable: filepath.Join(bin, "agy")}).turn(context.Background(), session, "status", func(u map[string]any) { updates = append(updates, u) })
	if err != nil || stop != "end_turn" || len(updates) != 2 {
		t.Fatalf("%v %v %v", stop, err, updates)
	}
	if updates[1]["sessionUpdate"] != "tool_call_update" || updates[1]["status"] != "failed" || updates[1]["toolCallId"] != "agy-1" {
		t.Fatalf("unfinished tool not closed: %v", updates[1])
	}
	if strings.Join(session.denied, ",") != "git status -sb" {
		t.Fatalf("refused command not reported for approval: %v", session.denied)
	}
}

// In Cautious mode a refused command becomes a Loom permission request; once
// allowed, the bridge resumes the conversation with that action permitted.
func TestAgyBridgeAsksApprovalForRefusedCommands(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
case "$1" in
  --version) echo "1.3.1"; exit 0;;
  models) printf 'gem\tGem\n'; exit 0;;
esac
cat > /dev/null
case "$*" in
  *--dangerously-skip-permissions*)
    echo '{"event":"step_update","step_update":{"step_index":3,"step_type":"tool","state":"DONE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"touch probe.txt"},"output":"ok"}}}'
    echo '{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS"}}';;
  *)
    echo '{"event":"init","conversation_id":"c1"}'
    echo '{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"ACTIVE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"touch probe.txt"}}}}'
    echo '{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"DONE","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"touch probe.txt"}}}}'
    echo '{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","denied_actions":[{"action":"command","display_name":"RunCommand"}]}}';;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "agy")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		(AgyBridge{Executable: exe, Read: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, exe, args...).Output()
		}}).Run(inR, outW)
		outW.Close()
	}()
	sc := bufio.NewScanner(outR)
	send := func(v map[string]any) { b, _ := json.Marshal(v); _, _ = inW.Write(append(b, '\n')) }
	next := func() map[string]any {
		if !sc.Scan() {
			t.Fatal("bridge closed")
		}
		var m map[string]any
		_ = json.Unmarshal(sc.Bytes(), &m)
		return m
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "session/new", "params": map[string]any{"cwd": t.TempDir()}})
	sid := next()["result"].(map[string]any)["sessionId"].(string)
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/prompt", "params": map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "touch"}}}})
	asked, outputs := false, ""
	for {
		m := next()
		if m["method"] == "session/request_permission" {
			asked = true
			title := m["params"].(map[string]any)["toolCall"].(map[string]any)["title"]
			if title != "touch probe.txt" {
				t.Fatalf("permission title: %v", title)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow"}}})
			continue
		}
		if m["method"] == "session/update" {
			outputs += fmt.Sprint(m["params"].(map[string]any)["update"].(map[string]any)["content"])
			continue
		}
		if m["id"] == float64(2) {
			break
		}
	}
	inW.Close()
	if !asked || !strings.Contains(outputs, "ok") {
		t.Fatalf("asked=%v outputs=%s", asked, outputs)
	}
}
