package loom

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

// Provider keys can be remembered in the operating system's keychain
// (Windows Credential Manager, macOS Keychain, Secret Service on Linux) when
// the user asks. Loom never writes them to its own files. Without a keychain,
// the key stays in memory for the session only.

const keyringService = "loom"

func keyringUser(providerID string) string { return "provider:" + providerID }

// Tests replace these to avoid touching the real keychain.
var (
	keyringSet    = func(id, secret string) error { return keyring.Set(keyringService, keyringUser(id), secret) }
	keyringGet    = func(id string) (string, error) { return keyring.Get(keyringService, keyringUser(id)) }
	keyringDelete = func(id string) error { return keyring.Delete(keyringService, keyringUser(id)) }
)

var errNoKeychain = errors.New("system keyring unavailable: the key stays in memory until restart")

// rememberProviderKey stores or forgets the key in the keychain.
func rememberProviderKey(id, key string, remember bool) error {
	if !remember {
		if err := keyringDelete(id); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return nil
	}
	if strings.TrimSpace(key) == "" {
		return nil // keep the stored one
	}
	if err := keyringSet(id, strings.TrimSpace(key)); err != nil {
		return errNoKeychain
	}
	return nil
}

// loadRememberedProviderKeys restores keys of providers marked "remember" at startup.
func loadRememberedProviderKeys() {
	for _, p := range workspaceSessions.providers() {
		if !p.Remember {
			continue
		}
		if key, err := keyringGet(p.ID); err == nil && key != "" {
			workspaceSessions.mu.Lock()
			if workspaceSessions.keys[p.ID] == "" {
				workspaceSessions.keys[p.ID] = key
			}
			workspaceSessions.mu.Unlock()
		}
	}
}
