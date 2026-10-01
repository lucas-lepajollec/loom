package store

// crypto.go — primitives de chiffrement de la mémoire.
//
// Modèle à enveloppe (voir docs/chiffrement-memoire-et-sauvegarde.md) :
//   - DEK (Data Encryption Key) : 32 octets aléatoires, chiffre chaque page
//     (AES-256-GCM, nonce aléatoire par écriture). Ne change jamais.
//   - KEK (Key Encryption Key) : dérivée d'un mot de passe par Argon2id, sert
//     UNIQUEMENT à (dé)wrapper la DEK (voir mem_vault.go).
//
// Aucune dépendance exotique : AES-GCM de la stdlib + Argon2id de x/crypto.
// La DEK en clair ne touche jamais le disque : elle vit en RAM le temps d'une
// session déverrouillée, et sur disque uniquement sous forme wrappée.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	DEKLen   = 32 // AES-256
	NonceLen = 12 // taille standard du nonce GCM
	SaltLen  = 16 // sel Argon2id
)

// Paramètres Argon2id par défaut. Stockés dans chaque wrap du keyvault pour que
// de futurs réglages n'invalident pas les coffres existants (on relit toujours
// les paramètres écrits, jamais ces constantes, lors du déballage).
const (
	ArgonTime    = 3
	ArgonMemory  = 64 * 1024 // 64 Mio
	ArgonThreads = 1
)

// En-tête d'une page chiffrée. Sert de marqueur de format ET d'AAD (le tag GCM
// couvre ainsi la version : impossible de rejouer un blob d'une autre version).
var memPageMagic = []byte("LOOMMEMv1")

// ErrNotEncrypted signale un contenu qui n'est pas une page chiffrée valide
// (mauvais magic) — utile pour distinguer « clair » de « chiffré » à la volée.
var ErrNotEncrypted = errors.New("contenu non chiffré (magic absent)")

// RandBytes renvoie n octets aléatoires cryptographiques.
func RandBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("source aléatoire indisponible : %w", err)
	}
	return b, nil
}

// DeriveKEK dérive une clé de 32 octets d'un mot de passe via Argon2id.
func DeriveKEK(password string, salt []byte, t, m uint32, p uint8) []byte {
	if t == 0 {
		t = ArgonTime
	}
	if m == 0 {
		m = ArgonMemory
	}
	if p == 0 {
		p = ArgonThreads
	}
	return argon2.IDKey([]byte(password), salt, t, m, p, DEKLen)
}

// GCMSeal chiffre plaintext avec key (AES-256-GCM) et renvoie nonce||ciphertext.
// aad (données authentifiées mais non chiffrées) lie le blob à un contexte.
func GCMSeal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err := RandBytes(NonceLen)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, aad)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// GCMOpen déchiffre un blob nonce||ciphertext produit par GCMSeal. Une clé
// erronée ou un blob altéré renvoie une erreur (jamais un contenu partiel).
func GCMOpen(key, blob, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < NonceLen {
		return nil, errors.New("blob trop court")
	}
	nonce, ct := blob[:NonceLen], blob[NonceLen:]
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("déchiffrement refusé (clé erronée ou donnée altérée) : %w", err)
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != DEKLen {
		return nil, fmt.Errorf("clé de %d octets, %d attendus", len(key), DEKLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptPage chiffre le contenu d'une page mémoire : magic || nonce || ciphertext.
// Le magic sert d'AAD (authentifié) et de marqueur de format.
func EncryptPage(dek, plaintext []byte) ([]byte, error) {
	sealed, err := GCMSeal(dek, plaintext, memPageMagic)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(memPageMagic)+len(sealed))
	out = append(out, memPageMagic...)
	out = append(out, sealed...)
	return out, nil
}

// DecryptPage déchiffre une page produite par EncryptPage. Renvoie ErrNotEncrypted si
// le magic manque (donc si on tombe sur une page restée en clair).
func DecryptPage(dek, blob []byte) ([]byte, error) {
	if !bytes.HasPrefix(blob, memPageMagic) {
		return nil, ErrNotEncrypted
	}
	return GCMOpen(dek, blob[len(memPageMagic):], memPageMagic)
}

// LooksEncrypted indique si un contenu disque est une page chiffrée (magic).
func LooksEncrypted(blob []byte) bool { return bytes.HasPrefix(blob, memPageMagic) }

// CryptoSelfTest vérifie que la chaîne crypto marche RÉELLEMENT sur cette
// machine/ce build avant qu'on chiffre la moindre donnée : round-trip DEK sur
// une page, plus un cycle KEK wrap/unwrap. Toute anomalie renvoie une erreur, et
// l'appelant DOIT alors refuser d'activer le chiffrement (on ne touche pas à la
// mémoire tant que ceci n'a pas réussi).
func CryptoSelfTest() error {
	dek, err := RandBytes(DEKLen)
	if err != nil {
		return err
	}
	sample := []byte("loom auto-test — éàçùô — 0123456789 — ✔")

	enc, err := EncryptPage(dek, sample)
	if err != nil {
		return fmt.Errorf("auto-test chiffrement page : %w", err)
	}
	dec, err := DecryptPage(dek, enc)
	if err != nil {
		return fmt.Errorf("auto-test déchiffrement page : %w", err)
	}
	if !bytes.Equal(dec, sample) {
		return errors.New("auto-test : round-trip page incohérent")
	}
	// Une DEK erronée DOIT échouer (sinon le GCM ne protège rien).
	badKey, _ := RandBytes(DEKLen)
	if _, err := DecryptPage(badKey, enc); err == nil {
		return errors.New("auto-test : une clé erronée a déchiffré (GCM cassé ?!)")
	}

	// Cycle KEK : wrap la DEK sous un mot de passe, puis déballe.
	salt, err := RandBytes(SaltLen)
	if err != nil {
		return err
	}
	kek := DeriveKEK("mot-de-passe-auto-test", salt, ArgonTime, ArgonMemory, ArgonThreads)
	box, err := GCMSeal(kek, dek, []byte("loom-vault-selftest"))
	if err != nil {
		return fmt.Errorf("auto-test wrap DEK : %w", err)
	}
	got, err := GCMOpen(kek, box, []byte("loom-vault-selftest"))
	if err != nil {
		return fmt.Errorf("auto-test unwrap DEK : %w", err)
	}
	if !bytes.Equal(got, dek) {
		return errors.New("auto-test : DEK déballée ≠ DEK d'origine")
	}
	return nil
}
