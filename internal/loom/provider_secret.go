package loom

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// Headless installations may not have a desktop Secret Service. A remembered
// credential can instead be sealed with a private, installation-local key.
// This protects accidental plaintext exposure, not a compromised OS account.
var providerSecretMu sync.Mutex
var providerSecretID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func providerSecretRoot() string { return filepath.Join(LoomHome(), "secrets", "providers") }
func providerSecretKey(create bool) ([]byte, error) {
	path := filepath.Join(providerSecretRoot(), ".key")
	b, err := os.ReadFile(path)
	if err == nil {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || len(b) != 32 || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("invalid provider encryption key or permissions")
		}
		return b, nil
	}
	if !create || !os.IsNotExist(err) {
		return nil, errors.New("provider encryption key unavailable")
	}
	// Never regenerate a lost key in an existing encrypted store.
	entries, _ := os.ReadDir(providerSecretRoot())
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sealed" {
			return nil, errors.New("provider encryption key missing; restore its backup")
		}
	}
	if err := os.MkdirAll(providerSecretRoot(), 0700); err != nil {
		return nil, err
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(b)
	if e := f.Close(); err == nil {
		err = e
	}
	return b, err
}
func sealProviderSecret(id, secret string) error {
	if !providerSecretID.MatchString(id) {
		return errors.New("invalid provider id")
	}
	providerSecretMu.Lock()
	defer providerSecretMu.Unlock()
	key, err := providerSecretKey(true)
	if err != nil {
		return err
	}
	box, err := gcmSeal(key, []byte(secret), []byte("loom/provider/"+id))
	if err != nil {
		return err
	}
	return memWriteFileAtomic(filepath.Join(providerSecretRoot(), id+".sealed"), box, 0600)
}
func readProviderSecret(id string) (string, error) {
	if !providerSecretID.MatchString(id) {
		return "", errors.New("invalid provider id")
	}
	providerSecretMu.Lock()
	defer providerSecretMu.Unlock()
	key, err := providerSecretKey(false)
	if err != nil {
		return "", err
	}
	path := filepath.Join(providerSecretRoot(), id+".sealed")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("provider credential unavailable")
	}
	box, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	b, err := gcmOpen(key, box, []byte("loom/provider/"+id))
	return string(b), err
}
func deleteProviderSecret(id string) error {
	if !providerSecretID.MatchString(id) {
		return errors.New("invalid provider id")
	}
	providerSecretMu.Lock()
	defer providerSecretMu.Unlock()
	err := os.Remove(filepath.Join(providerSecretRoot(), id+".sealed"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
