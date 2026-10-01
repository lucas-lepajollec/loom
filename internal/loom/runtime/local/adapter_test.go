package local

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

func TestAdapterForwardsOriginalTurnAndResult(t *testing.T) {
	type message struct{ text string }
	type caps struct{ tools bool }
	type callback func(string) bool
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	messages := []message{{"original"}}
	result := []message{{"partial"}}
	wantErr := errors.New("runner error")
	calls := 0
	a := Adapter[message, caps, callback]{RunChat: func(gotCtx context.Context, got []message, temperature float64, capabilities caps, emit callback) ([]message, error) {
		calls++
		if gotCtx != ctx || &got[0] != &messages[0] || temperature != 0.75 || !capabilities.tools {
			t.Fatal("turn inputs changed")
		}
		if emit("runner event") {
			t.Fatal("callback cancellation changed")
		}
		return result, wantErr
	}}
	got, err := a.Run(ctx, runtime.RuntimeTurn[message, caps]{Messages: messages, Temperature: 0.75, Caps: caps{true}, MaxTokens: 64}, func(event string) bool {
		if event != "runner event" {
			t.Fatal(event)
		}
		return false
	})
	if calls != 1 || err != wantErr || &got[0] != &result[0] {
		t.Fatalf("result changed: %v, %v, calls=%d", got, err, calls)
	}
}

func TestDescriptorSnapshots(t *testing.T) {
	a := Adapter[string, bool, func(string) bool]{}
	d := a.Descriptor()
	if d.ID != "llama.cpp" || d.Kind != "local" || !d.Implemented || !reflect.DeepEqual(d.Capabilities, []string{"chat", "stream", "tools", "attachments", "cancel"}) {
		t.Fatalf("descriptor changed: %+v", d)
	}
	d.Capabilities[0] = "changed"
	if a.Descriptor().Capabilities[0] != "chat" {
		t.Fatal("descriptor retained caller mutation")
	}
}
