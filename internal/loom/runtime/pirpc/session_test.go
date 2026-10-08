package pirpc

import (
	"encoding/json"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

func TestToolArgumentIdentity(t *testing.T) {
	m := Mapper{}
	for _, raw := range []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":1,"id":"native-tool","toolName":"bash"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{}"}}`,
	} {
		var f agentstdio.Frame
		_ = json.Unmarshal([]byte(raw), &f)
		f.Raw = json.RawMessage(raw)
		e := m.Notification(f)[0]
		if e.ItemID != "native-tool" {
			t.Fatalf("tool correlation: %+v", e)
		}
	}
}

func TestUnknownContextUsage(t *testing.T) {
	f := agentstdio.Frame{Data: json.RawMessage(`{"contextUsage":{"tokens":null,"contextWindow":200000}}`), Raw: json.RawMessage(`{}`)}
	e := ContextObservation(f)
	if e == nil || e.Usage.Total != nil || e.Usage.ContextWindow == nil || *e.Usage.ContextWindow != 200000 {
		t.Fatalf("unknown occupancy must stay unknown: %+v", e)
	}
}
