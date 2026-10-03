package runtime

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

type testCallback = EventSink[string]
type testAdapter struct{ descriptor RuntimeDescriptor }

func (a testAdapter) Descriptor() RuntimeDescriptor { return a.descriptor }
func (testAdapter) Run(context.Context, RuntimeTurn[string, bool], testCallback) ([]string, error) {
	return nil, nil
}

type testAccountAdapter struct{ testAdapter }

func (testAccountAdapter) Connect(context.Context, bool) (any, error) { return nil, nil }
func (testAccountAdapter) Quota(context.Context) (int, error)         { return 0, nil }

func TestRegistryValidation(t *testing.T) {
	reg := NewRegistry[string, bool, string, int]()
	for _, tc := range []struct {
		name string
		a    RuntimeAdapter[string, bool, string]
		want string
	}{
		{"nil", nil, "runtime adapter required"},
		{"empty", testAdapter{}, "invalid runtime ID"},
		{"whitespace", testAdapter{RuntimeDescriptor{ID: " id"}}, "invalid runtime ID"},
		{"path", testAdapter{RuntimeDescriptor{ID: "bad/id"}}, "invalid runtime ID"},
		{"planned", testAdapter{RuntimeDescriptor{ID: "planned", Capabilities: []string{"chat"}}}, "a planned runtime cannot declare capabilities"},
		{"connect", testAdapter{RuntimeDescriptor{ID: "connect", Implemented: true, Capabilities: []string{"connect"}}}, "connect capability without Connectable interface"},
		{"quota", testAdapter{RuntimeDescriptor{ID: "quota", Implemented: true, Capabilities: []string{"quota"}}}, "quota capability without QuotaReader interface"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := reg.Register(tc.a); err == nil || err.Error() != tc.want {
				t.Fatalf("Register error = %v, want %q", err, tc.want)
			}
		})
	}
	account := testAccountAdapter{testAdapter{RuntimeDescriptor{ID: "account", Implemented: true, Capabilities: []string{"connect", "quota"}}}}
	if err := reg.Register(account); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(account); err == nil || err.Error() != "runtime already registered" {
		t.Fatalf("duplicate error = %v", err)
	}
	// A quota method with a different result type must not satisfy this registry.
	other := NewRegistry[string, bool, string, string]()
	if err := other.Register(account); err == nil || err.Error() != "quota capability without QuotaReader interface" {
		t.Fatalf("mismatched quota accepted: %v", err)
	}
	if HasRuntimeCapability(RuntimeDescriptor{Capabilities: []string{"chat"}}, "chat") {
		t.Fatal("planned descriptor advertised a capability")
	}
}

func TestRegistryOwnersAndMutations(t *testing.T) {
	reg := NewRegistry[string, bool, string, int]()
	other := NewRegistry[string, bool, string, int]()
	first := testAdapter{RuntimeDescriptor{ID: "z", Implemented: true, Capabilities: []string{"chat"}}}
	if err := reg.Register(first); err != nil {
		t.Fatal(err)
	}
	reg.Upsert(testAdapter{RuntimeDescriptor{ID: "a"}})
	replacement := testAdapter{RuntimeDescriptor{ID: "z", Name: "replacement", Implemented: true, Capabilities: []string{"chat"}}}
	reg.Upsert(replacement)
	catalog := reg.List()
	if len(catalog) != 2 || catalog[0].Name != "replacement" || catalog[1].ID != "a" || catalog[1].Capabilities == nil {
		t.Fatalf("order, replacement or empty capability shape changed: %+v", catalog)
	}
	catalog[0].Capabilities[0] = "corrupted"
	if !HasRuntimeCapability(reg.List()[0], "chat") {
		t.Fatal("list exposed the adapter capability slice")
	}
	if a, ok := reg.Lookup("z"); !ok || !reflect.DeepEqual(a, replacement) {
		t.Fatal("lookup did not return the replacement")
	}
	reg.Remove("unknown")
	reg.Remove("z")
	reg.Remove("z")
	reg.Upsert(first)
	if got := reg.List(); len(got) != 2 || got[0].ID != "a" || got[1].ID != "z" {
		t.Fatalf("remove/reinsert order = %+v", got)
	}
	if got := other.List(); got == nil || len(got) != 0 {
		t.Fatalf("independent registry shared state or null list: %+v", got)
	}
	if _, ok := other.Lookup("z"); ok {
		t.Fatal("independent registry resolved another owner's adapter")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry[string, bool, string, int]()
	var workers sync.WaitGroup
	for _, id := range []string{"a", "b", "c", "d"} {
		workers.Go(func() {
			for range 50 {
				reg.Upsert(testAdapter{RuntimeDescriptor{ID: id}})
				reg.Lookup(id)
				reg.List()
				reg.Remove(id)
			}
		})
	}
	workers.Wait()
	if got := reg.List(); len(got) != 0 {
		t.Fatalf("registry retained removed entries: %+v", got)
	}
}
