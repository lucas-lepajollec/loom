package loom

import (
	"os"
	"path/filepath"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
)

// Compatibilité pendant la migration par feuilles : Loom garde la résolution
// des chemins et les anciens noms ; store possède les opérations et leur état.
// Les helpers chiffrés de mem_store.go restent ici : ils dépendent du coffre
// et de la DEK en mémoire. Les snapshots gardent aussi leurs chemins mémoire.
const (
	bkConfig             = store.BucketConfig
	bkPrefs              = store.BucketPrefs
	bkState              = store.BucketState
	bkChat               = store.BucketChat
	bkChatHist           = store.BucketChatHist
	bkChatMeta           = store.BucketChatMeta
	bkProjects           = store.BucketProjects
	bkTasks              = store.BucketTasks
	bkCapabilities       = store.BucketCapabilities
	bkProviders          = store.BucketProviders
	bkRuntimeSessions    = store.BucketRuntimeSessions
	bkModelChoices       = store.BucketModelChoices
	bkHarnessProfiles    = store.BucketHarnessProfiles
	bkHarnessConnections = store.BucketHarnessConnections
	bkUsagePrices        = store.BucketUsagePrices
	memDEKLen            = store.DEKLen
	memSaltLen           = store.SaltLen
	argonTime            = store.ArgonTime
	argonMemory          = store.ArgonMemory
	argonThreads         = store.ArgonThreads
)

var (
	randBytes            = store.RandBytes
	deriveKEK            = store.DeriveKEK
	gcmSeal              = store.GCMSeal
	gcmOpen              = store.GCMOpen
	encPage              = store.EncryptPage
	decPage              = store.DecryptPage
	looksEncrypted       = store.LooksEncrypted
	memCryptoSelfTest    = store.CryptoSelfTest
	memWriteFileAtomic   = store.WriteFileAtomic
	memWriteFileVerified = store.WriteFileVerified
)

func dbPath() string {
	loomDB := filepath.Join(LoomHome(), "loom.db")
	if _, err := os.Stat(loomDB); err == nil {
		return loomDB
	}
	legacyDB := filepath.Join(LoomHome(), "loom.db")
	if _, err := os.Stat(legacyDB); err == nil {
		return legacyDB
	}
	return loomDB
}

func getBytesErr(bucket, key string) ([]byte, error) {
	return store.GetBytesErr(dbPath(), bucket, key)
}
func getBytes(bucket, key string) []byte { return store.GetBytes(dbPath(), bucket, key) }
func putBytes(bucket, key string, val []byte) error {
	return store.PutBytes(dbPath(), bucket, key, val)
}
func getStr(bucket, key string) string                   { return store.GetStr(dbPath(), bucket, key) }
func putStr(bucket, key, val string) error               { return store.PutStr(dbPath(), bucket, key, val) }
func getBool(bucket, key string) bool                    { return store.GetBool(dbPath(), bucket, key) }
func putBool(bucket, key string, on bool) error          { return store.PutBool(dbPath(), bucket, key, on) }
func getJSON(bucket, key string, dst any) bool           { return store.GetJSON(dbPath(), bucket, key, dst) }
func putJSON(bucket, key string, v any) error            { return store.PutJSON(dbPath(), bucket, key, v) }
func cachedKV(bucket string) map[string]string           { return store.CachedKV(dbPath(), bucket) }
func allKV(bucket string) map[string]string              { return store.AllKV(dbPath(), bucket) }
func replaceKV(bucket string, m map[string]string) error { return store.ReplaceKV(dbPath(), bucket, m) }
