package loom

import (
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

// Remembered keys use the OS keychain where available, or the sealed service
// credential store on headless hosts. Provider JSON never contains credentials.

const keyringService = "loom"

func keyringUser(providerID string) string { return "provider:" + providerID }

// Tests replace these to avoid touching the real keychain.
var (
	keyringSet = func(id, secret string) error {
		if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			return errNoKeychain
		}
		return keyring.Set(keyringService, keyringUser(id), secret)
	}
	keyringGet = func(id string) (string, error) {
		if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			return "", errNoKeychain
		}
		return keyring.Get(keyringService, keyringUser(id))
	}
	keyringDelete = func(id string) error {
		if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			return keyring.ErrNotFound
		}
		return keyring.Delete(keyringService, keyringUser(id))
	}
)

var errNoKeychain = errors.New("credential persistence unavailable: the key stays in memory until restart")

// rememberProviderKey stores or forgets the key in the keychain.
func rememberProviderKey(id, key string, remember bool) error {
	if !remember {
		sealedErr := deleteProviderSecret(id)
		if err := keyringDelete(id); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return sealedErr
	}
	if strings.TrimSpace(key) == "" {
		return nil // keep the stored one
	}
	if err := keyringSet(id, strings.TrimSpace(key)); err != nil {
		if sealProviderSecret(id, strings.TrimSpace(key)) != nil {
			return errNoKeychain
		}
		return nil
	}
	if err := deleteProviderSecret(id); err != nil {
		return err
	}
	return nil
}

// loadRememberedProviderKeys restores keys of providers marked "remember" at startup.
func loadRememberedProviderKeys() {
	for _, p := range workspaceSessions.providers() {
		if !p.Remember {
			continue
		}
		key, err := readProviderSecret(p.ID)
		if err != nil || key == "" {
			key, err = keyringGet(p.ID)
		}
		if err == nil && key != "" {
			workspaceSessions.mu.Lock()
			if workspaceSessions.keys[p.ID] == "" {
				workspaceSessions.keys[p.ID] = key
			}
			workspaceSessions.mu.Unlock()
		}
	}
}
