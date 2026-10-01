package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemCryptoSelfTest(t *testing.T) {
	if err := CryptoSelfTest(); err != nil {
		t.Fatalf("auto-test crypto échoué : %v", err)
	}
}

func TestEncDecPageRoundTrip(t *testing.T) {
	dek, _ := RandBytes(DEKLen)
	cases := []string{"", "court", "# titre\n\nlignes\néàçù ✔\n", strings.Repeat("x", 100000)}
	for _, c := range cases {
		enc, err := EncryptPage(dek, []byte(c))
		if err != nil {
			t.Fatalf("EncryptPage: %v", err)
		}
		if !LooksEncrypted(enc) {
			t.Fatal("LooksEncrypted devrait être vrai sur une page chiffrée")
		}
		dec, err := DecryptPage(dek, enc)
		if err != nil {
			t.Fatalf("DecryptPage: %v", err)
		}
		if !bytes.Equal(dec, []byte(c)) {
			t.Fatalf("round-trip incohérent pour %q", c[:min(len(c), 20)])
		}
	}
}

func TestDecPagePlaintextDetected(t *testing.T) {
	dek, _ := RandBytes(DEKLen)
	if _, err := DecryptPage(dek, []byte("# page en clair")); err != ErrNotEncrypted {
		t.Fatalf("attendu ErrNotEncrypted, obtenu %v", err)
	}
}

func TestDecPageWrongKeyFails(t *testing.T) {
	dek, _ := RandBytes(DEKLen)
	bad, _ := RandBytes(DEKLen)
	enc, _ := EncryptPage(dek, []byte("secret"))
	if _, err := DecryptPage(bad, enc); err == nil {
		t.Fatal("une clé erronée n'aurait pas dû déchiffrer")
	}
}

func TestDecPageTamperedFails(t *testing.T) {
	dek, _ := RandBytes(DEKLen)
	enc, _ := EncryptPage(dek, []byte("secret"))
	enc[len(enc)-1] ^= 0xFF // altère le tag
	if _, err := DecryptPage(dek, enc); err == nil {
		t.Fatal("un blob altéré n'aurait pas dû déchiffrer")
	}
}

func TestWriteFileVerified(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.bin")
	data := []byte("contenu vérifié éàç")
	if err := WriteFileVerified(p, data, 0o600); err != nil {
		t.Fatalf("écriture vérifiée: %v", err)
	}
	back, _ := os.ReadFile(p)
	if !bytes.Equal(back, data) {
		t.Fatal("relecture ≠ écrit")
	}
}
