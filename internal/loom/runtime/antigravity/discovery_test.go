package antigravity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestModelCatalogOrderValidationAndBounds(t *testing.T) {
	models, err := ParseModels([]byte("header\n z-native \tdescription\na.native\tlabel\nz-native\tduplicate\n-invalid\tlabel\nwith/slash\tlabel\n"))
	if err != nil || !reflect.DeepEqual(models, []string{"z-native", "a.native"}) {
		t.Fatalf("catalog=%v err=%v", models, err)
	}
	if _, err := ParseModels([]byte("model without a tab\n")); err == nil {
		t.Fatal("unrecognized catalog accepted")
	}
	var body strings.Builder
	for i := 0; i < 129; i++ {
		fmt.Fprintf(&body, "native-%d\tlabel\n", i)
	}
	if _, err := ParseModels([]byte(body.String())); err == nil {
		t.Fatal("oversized catalog accepted")
	}
}

func TestBoundedReadOutput(t *testing.T) {
	b := boundedCLIOutput{limit: 4}
	if n, err := b.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := b.Write([]byte("5")); n != 0 || err == nil || !b.overflow || string(b.data) != "1234" {
		t.Fatalf("overflow changed retained output: %+v, %d, %v", b, n, err)
	}
}

func TestStreamCancellationAndUsageReplacement(t *testing.T) {
	body := `{"event":"step_update","step_update":{"step_index":1,"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}
{"event":"step_update","step_update":{"step_index":1,"usage":{"input_tokens":4,"output_tokens":6,"total_tokens":10}}}
{"event":"step_update","step_update":{"step_index":2,"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}
{"event":"result","result":{"status":"SUCCESS","response":"OK"}}
`
	var totals []int64
	_, err := ConsumeStream(context.Background(), strings.NewReader(body), func(e Event) bool {
		if e.Usage != nil {
			totals = append(totals, e.Usage.Total)
		}
		return true
	})
	if err != nil || !reflect.DeepEqual(totals, []int64{5, 10, 13}) {
		t.Fatalf("usage=%v err=%v", totals, err)
	}
	_, err = ConsumeStream(context.Background(), strings.NewReader(body), func(Event) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ConsumeStream(ctx, strings.NewReader(body), func(Event) bool { t.Fatal("event after cancellation"); return true })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
