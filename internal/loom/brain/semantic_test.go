package brain

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestCosineFixtures(t *testing.T) {
	for _, c := range []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{2, 0}, 1}, {[]float32{1, 0}, []float32{0, 3}, 0}, {[]float32{1, 0}, []float32{-1, 0}, -1}, {[]float32{1, 1}, []float32{1, 0}, 1 / math.Sqrt(2)}, {[]float32{0, 0}, []float32{1, 0}, 0}, {[]float32{1}, []float32{1, 0}, 0},
	} {
		if got := Cosine(c.a, c.b); math.Abs(got-c.want) > 1e-7 {
			t.Fatalf("%v %v: %g", c.a, c.b, got)
		}
	}
	if ValidVector([]float32{float32(math.NaN())}) || ValidVector([]float32{float32(math.Inf(1))}) {
		t.Fatal("invalid float accepted")
	}
}
func TestReciprocalRankFusionOrdering(t *testing.T) {
	h := func(id string) Hit { return Hit{ChunkID: id} }
	got := Fuse([]Hit{h("a"), h("b"), h("c")}, []Hit{h("b"), h("c"), h("d")})
	ids := []string{}
	for _, x := range got {
		ids = append(ids, x.ChunkID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "c", "a", "d"}) {
		t.Fatal(ids)
	}
	if math.Abs(got[0].Score-(1.0/62+1.0/61)) > 1e-12 {
		t.Fatal(got[0].Score)
	}
	tie := Fuse([]Hit{h("z")}, []Hit{h("a")})
	if tie[0].ChunkID != "a" {
		t.Fatal("unstable tie")
	}
}
func TestVectorStoreRoundTripAndContentInvalidation(t *testing.T) {
	e, dir, _ := fixture(t, map[string]string{"a.md": "old memory"}, "context")
	chunks, err := e.Chunks(SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	v := Vectors{Identity: "model-v1", Values: map[string][]float32{chunks[0].ID: {0.5, 1.25}}}
	data, err := v.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var restored Vectors
	if err = restored.UnmarshalBinary(data); err != nil || !reflect.DeepEqual(v, restored) {
		t.Fatalf("%v %#v", err, restored)
	}
	write(t, dir, "a.md", "changed memory text")
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, _ = e.Chunks(SearchRequest{})
	restored.Prune(chunks)
	if len(restored.Values) != 0 {
		t.Fatal("changed chunk retained its vector")
	}
	if restored.UnmarshalBinary(append(data, 1)) == nil || restored.UnmarshalBinary(data[:len(data)-1]) == nil {
		t.Fatal("corrupt vectors accepted")
	}
}
func TestHybridSemanticOnlyHitsPrivacyAndPack(t *testing.T) {
	e, _, _ := fixture(t, map[string]string{"car.md": "automobile repair", "fruit.md": "apple orchard"}, "personal")
	explicit := SearchRequest{Query: "vehicle", Sources: []string{"notes"}, Personal: true, Limit: 1}
	chunks, _ := e.Chunks(explicit)
	v := map[string][]float32{}
	for _, c := range chunks {
		if c.Path == "car.md" {
			v[c.ID] = []float32{1, 0}
		} else {
			v[c.ID] = []float32{0, 1}
		}
	}
	got, err := e.HybridSearch(explicit, []float32{1, 0}, v)
	if err != nil || len(got) != 1 || got[0].Path != "car.md" {
		t.Fatalf("%v %v", got, err)
	}
	pack, err := e.PackHits(PackRequest{Query: "vehicle", Sources: []string{"notes"}, Personal: true, BudgetTokens: 100}, got)
	if err != nil || len(pack.Chunks) != 1 {
		t.Fatalf("%v %v", pack, err)
	}
	for _, r := range []SearchRequest{{Query: "vehicle"}, {Query: "vehicle", Personal: true}, {Query: "vehicle", Sources: []string{"notes"}}} {
		got, err = e.HybridSearch(r, []float32{1, 0}, v)
		if err != nil || len(got) != 0 {
			t.Fatalf("personal leak %v %v", got, err)
		}
	}
}
