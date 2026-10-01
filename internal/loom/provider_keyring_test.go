package loom

import (
	"errors"
	"testing"
)

func TestProviderKeyRememberedInKeychainOnly(t *testing.T) {
	testHome(t)
	vault := map[string]string{}
	oldSet, oldGet, oldDel := keyringSet, keyringGet, keyringDelete
	t.Cleanup(func() { keyringSet, keyringGet, keyringDelete = oldSet, oldGet, oldDel })
	keyringSet = func(id, s string) error { vault[id] = s; return nil }
	keyringGet = func(id string) (string, error) { return vault[id], nil }
	keyringDelete = func(id string) error { delete(vault, id); return nil }
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "P", Endpoint: "https://example.com/v1", Model: "m", Remember: true}, "sk-test")
	if err != nil || vault[p.ID] != "sk-test" {
		t.Fatalf("%v %v", err, vault)
	}
	var stored CloudProvider
	getStoreJSON(bkProviders, p.ID, &stored)
	if !stored.Remember {
		t.Fatal("remember flag lost")
	}
	// Restart: memory is empty, the keychain restores the key.
	workspaceSessions = newRuntimeSessions()
	loadRememberedProviderKeys()
	if workspaceSessions.keys[p.ID] != "sk-test" {
		t.Fatal("key not restored")
	}
	workspaceSessions.disconnect(p.ID)
	if _, ok := vault[p.ID]; ok || workspaceSessions.keys[p.ID] != "" {
		t.Fatal("forget must clear keychain and memory")
	}
	keyringSet = func(string, string) error { return errors.New("no secret service") }
	if _, err := workspaceSessions.saveProvider(CloudProvider{ID: p.ID, Name: "P", Endpoint: "https://example.com/v1", Model: "m", Remember: true}, "sk-2"); err == nil {
		t.Fatal("missing keychain must be reported")
	}
	if workspaceSessions.keys[p.ID] != "sk-2" {
		t.Fatal("key must stay usable in memory")
	}
}
