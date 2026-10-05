package loom

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderSealedStoreConfinesSecretsAndDetectsCorruption(t *testing.T) {
	testHome(t)
	const secret = "synthetic-credential-do-not-use"
	if err := sealProviderSecret("fixture", secret); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(providerSecretRoot(), "fixture.sealed")
	box, err := os.ReadFile(path)
	if err != nil || bytes.Contains(box, []byte(secret)) {
		t.Fatal("credential was not encrypted")
	}
	for _, p := range []string{path, filepath.Join(providerSecretRoot(), ".key")} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm()&0077 != 0 {
			t.Fatal("credential file permissions")
		}
	}
	if value, err := readProviderSecret("fixture"); err != nil || value != secret {
		t.Fatal("credential did not decrypt")
	}
	if err := os.WriteFile(filepath.Join(providerSecretRoot(), "other.sealed"), box, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProviderSecret("other"); err == nil {
		t.Fatal("credential accepted under another provider identity")
	}
	box[len(box)-1] ^= 1
	_ = os.WriteFile(path, box, 0600)
	if _, err := readProviderSecret("fixture"); err == nil {
		t.Fatal("tampered credential accepted")
	}
	if err := sealProviderSecret("../escape", secret); err == nil {
		t.Fatal("provider path traversal accepted")
	}
	_ = os.Remove(filepath.Join(providerSecretRoot(), ".key"))
	if err := sealProviderSecret("new", secret); err == nil {
		t.Fatal("lost master key silently replaced")
	}
	if _, err := os.Stat(filepath.Join(providerSecretRoot(), ".key")); !os.IsNotExist(err) {
		t.Fatal("lost master key regenerated")
	}
}
