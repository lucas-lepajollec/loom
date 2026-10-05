package loom

import (
	"errors"
	"testing"
)

func TestProviderKeyRememberedAndHeadlessFallback(t *testing.T) {
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
	if _, err := workspaceSessions.saveProvider(CloudProvider{ID: p.ID, Name: "P", Endpoint: "https://example.com/v1", Model: "m", Remember: true}, "sk-2"); err != nil {
		t.Fatal("headless persistence failed")
	}
	keyringGet = func(string) (string, error) { return "", errors.New("no secret service") }
	workspaceSessions = newRuntimeSessions()
	loadRememberedProviderKeys()
	if workspaceSessions.keys[p.ID] != "sk-2" {
		t.Fatal("sealed key not restored after restart")
	}
	workspaceSessions.disconnect(p.ID)
	if _, err := readProviderSecret(p.ID); err == nil {
		t.Fatal("forget retained sealed credential")
	}
}

func TestProviderForgetReportsDeletionFailure(t *testing.T) {
	testHome(t)
	oldSet, oldDel := keyringSet, keyringDelete
	t.Cleanup(func() { keyringSet, keyringDelete = oldSet, oldDel })
	keyringSet = func(string, string) error { return nil }
	keyringDelete = func(string) error { return errors.New("synthetic deletion denied") }
	m := newRuntimeSessions()
	p, err := m.saveProvider(CloudProvider{Name: "Fixture", Endpoint: "https://example.com/v1", Model: "m", Remember: true}, "synthetic-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.disconnect(p.ID); err == nil {
		t.Fatal("failed credential deletion claimed success")
	}
	var stored CloudProvider
	getStoreJSON(bkProviders, p.ID, &stored)
	if !stored.Remember || m.keys[p.ID] != "synthetic-key" {
		t.Fatal("failed disconnect discarded connection state")
	}
}
