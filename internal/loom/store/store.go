package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// store.go — l'unique endroit où Loom écrit son état.
//
// Avant, chaque réglage avait son fichier : config.env, webprefs.json,
// conversation.json, .api_key, .link_token, .agent_enabled, model_dirs.json…
// Une douzaine de formats, une douzaine de façons de rater une écriture
// concurrente, et un dossier de données illisible. Tout ça tient désormais dans
// une seule base bbolt — pur Go, un seul fichier, transactionnelle.
//
// Ce qui N'EST PAS en base, et pourquoi : les presets (presets/*.env) et les
// pages de mémoire (memory/*.md) restent des fichiers, parce qu'ils sont faits
// pour être lus, édités et sauvegardés à la main. Les modèles (.gguf) et les
// backends compilés restent des fichiers, évidemment.

// Buckets. Un par nature de donnée : ça garde les itérations bornées et rend le
// contenu de la base lisible au débogage.
const (
	BucketConfig             = "config"   // configuration de llama-server (ex-config.env)
	BucketPrefs              = "prefs"    // préférences de l'UI web
	BucketState              = "state"    // clés, jetons, drapeaux, listes de dossiers, MCP
	BucketChat               = "chat"     // conversation partagée
	BucketChatHist           = "chathist" // conversations archivées (historique) — une entrée JSON par conversation
	BucketChatMeta           = "chatmeta" // index LÉGER des sessions (id→métadonnées) pour lister sans parser les gros blobs
	BucketProjects           = "projects" // dossiers / projets de chats (id→{name,created})
	BucketTasks              = "tasks"    // tâches planifiées (une entrée JSON par tâche)
	BucketCapabilities       = "workspace_capabilities"
	BucketProviders          = "workspace_providers"
	BucketRuntimeSessions    = "workspace_sessions"
	BucketModelChoices       = "workspace_model_choices"
	BucketHarnessProfiles    = "workspace_harness_profiles"
	BucketHarnessConnections = "workspace_harness_connections"
	BucketUsagePrices        = "workspace_usage_prices"
)

// Le chemin est fourni par l’appelant ; store ne dépend pas de la résolution
// des chemins de Loom. dbMu, cacheMu et caches sont l’état de process existant,
// déplacé ici sans duplication. Aucun handle bbolt global n’est conservé.
//
// La base n'est PAS gardée ouverte entre deux opérations, et c'est délibéré.
//
// bbolt pose un verrou EXCLUSIF sur son fichier tant qu'il est ouvert. Or une
// machine installée fait tourner en permanence le service de lien, qui sert le
// tunnel et l'UI : s'il gardait la base ouverte, plus une seule commande ne
// fonctionnerait à côté — « loom status », « loom switch », « loom edit »
// échoueraient toutes sur un délai d'attente, sur la machine même où tout est
// censé marcher. On ouvre donc pour la durée d'une opération, puis on referme.
//
// Le coût est celui d'un open+close sur un fichier de quelques dizaines de Ko,
// négligeable devant le moindre appel au modèle. Le délai d'attente absorbe la
// contention entre process ; dbMu la sérialise à l'intérieur du process.
var dbMu sync.Mutex

// withDB ouvre la base, exécute fn, puis referme — toujours, même en erreur.
func withDB(path string, fn func(*bolt.DB) error) error {
	dbMu.Lock()
	defer dbMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	d, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("base %s inaccessible : %w", path, err)
	}
	defer d.Close()
	return fn(d)
}

// View exécute une lecture. Un bucket encore absent (base neuve) est traité
// comme vide : les buckets ne sont créés QU'À l'écriture, pour qu'une simple
// lecture n'ouvre jamais de transaction d'écriture — elle coûterait un fsync,
// et les lectures sont de loin les plus fréquentes.
func View(path, bucket string, fn func(b *bolt.Bucket) error) error {
	return withDB(path, func(d *bolt.DB) error {
		return d.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return nil
			}
			return fn(b)
		})
	})
}

// Update exécute une écriture, en créant le bucket au besoin.
func Update(path, bucket string, fn func(b *bolt.Bucket) error) error {
	cacheBust(bucket)
	return withDB(path, func(d *bolt.DB) error {
		return d.Update(func(tx *bolt.Tx) error {
			b, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return err
			}
			return fn(b)
		})
	})
}

// GetBytesErr lit une valeur en REMONTANT l'erreur d'accès. À réserver aux
// lecteurs pour qui « je n'ai pas pu lire » et « il n'y a rien » ne veulent pas
// dire la même chose — au premier chef les secrets : sans clé enregistrée, l'API
// de pilotage est ouverte, donc une lecture ratée traitée comme « pas de clé »
// ouvrirait l'API au lieu de la fermer. Voir readWebKeyErr.
func GetBytesErr(path, bucket, key string) ([]byte, error) {
	var out []byte
	err := View(path, bucket, func(b *bolt.Bucket) error {
		if v := b.Get([]byte(key)); v != nil {
			out = append([]byte(nil), v...) // la valeur ne survit pas à la transaction
		}
		return nil
	})
	return out, err
}

// GetBytes lit une valeur. Une base inaccessible se comporte comme une base
// vide : les appelants sont des lecteurs de réglages, aucun n'a de recours utile
// face à une erreur d'E/S, et tous ont déjà un défaut. Les lecteurs pour qui
// l'erreur CHANGE la décision prennent GetBytesErr.
func GetBytes(path, bucket, key string) []byte {
	out, _ := GetBytesErr(path, bucket, key)
	return out
}

// PutBytes écrit une valeur. Une valeur nil supprime la clé.
func PutBytes(path, bucket, key string, val []byte) error {
	return Update(path, bucket, func(b *bolt.Bucket) error {
		if val == nil {
			return b.Delete([]byte(key))
		}
		return b.Put([]byte(key), val)
	})
}

func GetStr(path, bucket, key string) string { return string(GetBytes(path, bucket, key)) }

// PutStr écrit une chaîne ; une chaîne vide supprime la clé, pour que « absent »
// et « vide » ne soient jamais deux états distincts à distinguer.
func PutStr(path, bucket, key, val string) error {
	if val == "" {
		return PutBytes(path, bucket, key, nil)
	}
	return PutBytes(path, bucket, key, []byte(val))
}

func GetBool(path, bucket, key string) bool { return GetStr(path, bucket, key) == "1" }

func PutBool(path, bucket, key string, on bool) error {
	if !on {
		return PutBytes(path, bucket, key, nil)
	}
	return PutStr(path, bucket, key, "1")
}

// GetJSON décode une valeur JSON dans dst. Renvoie false si la clé est absente
// ou illisible — dans les deux cas l'appelant garde son zéro.
func GetJSON(path, bucket, key string, dst any) bool {
	b := GetBytes(path, bucket, key)
	if len(b) == 0 {
		return false
	}
	return json.Unmarshal(b, dst) == nil
}

func PutJSON(path, bucket, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return PutBytes(path, bucket, key, b)
}

// --- Cache de lecture ---------------------------------------------------------
//
// La base est rouverte à CHAQUE opération (choix délibéré, voir plus haut), ce
// qui est parfait pour un réglage lu de temps en temps mais coûteux dans le
// chemin chaud : la boucle d'inférence relit le port, la clé, le mode
// raisonnement et le seuil de compactage à chaque itération, et le compactage
// se re-teste après chaque appel d'outil. Un tour agentique un peu fourni
// rouvrait la base une centaine de fois.
//
// CachedKV garde donc le contenu d'un bucket en mémoire, invalidé par :
//   - une écriture de CE process (cacheBust, appelé par Update/ReplaceKV) ;
//   - un changement de taille ou de date du fichier, qui trahit l'écriture d'un
//     AUTRE process (la CLI pendant que le service tourne) ;
//   - l'âge, plafonné à une seconde, filet pour le cas limite où deux écritures
//     rapprochées laisseraient date et taille inchangées.
//
// Il ne sert PAS aux secrets : eux se lisent directement (voir GetBytesErr).
const cacheMaxAge = time.Second

type kvCache struct {
	kv    map[string]string
	when  time.Time
	mtime time.Time
	size  int64
}

var (
	cacheMu sync.Mutex
	caches  = map[string]kvCache{}
)

// cacheBust vide le cache d'un bucket après une écriture locale.
func cacheBust(bucket string) {
	cacheMu.Lock()
	delete(caches, bucket)
	cacheMu.Unlock()
}

// dbStamp renvoie la date et la taille du fichier de base — de quoi repérer
// l'écriture d'un autre process pour le prix d'un stat.
func dbStamp(path string) (time.Time, int64) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, -1
	}
	return fi.ModTime(), fi.Size()
}

// CachedKV renvoie tout le contenu d'un bucket, depuis le cache quand il est
// encore valable. La carte renvoyée appartient à l'appelant (copie).
func CachedKV(path, bucket string) map[string]string {
	mtime, size := dbStamp(path)
	cacheMu.Lock()
	c, ok := caches[bucket]
	fresh := ok && c.size == size && c.mtime.Equal(mtime) && time.Since(c.when) < cacheMaxAge
	cacheMu.Unlock()
	if !fresh {
		kv := AllKV(path, bucket)
		c = kvCache{kv: kv, when: time.Now(), mtime: mtime, size: size}
		cacheMu.Lock()
		caches[bucket] = c
		cacheMu.Unlock()
	}
	out := make(map[string]string, len(c.kv))
	for k, v := range c.kv {
		out[k] = v
	}
	return out
}

// AllKV renvoie tout le contenu d'un bucket. Utilisé par la configuration, dont
// les clés ne sont pas connues à l'avance (EXTRA_ARGS et consorts).
func AllKV(path, bucket string) map[string]string {
	m := map[string]string{}
	_ = View(path, bucket, func(b *bolt.Bucket) error {
		return b.ForEach(func(k, v []byte) error {
			m[string(k)] = string(v)
			return nil
		})
	})
	return m
}

// ReplaceKV remplace tout le contenu d'un bucket en une seule transaction.
// C'est ce qu'exige l'application d'un preset : à aucun instant la config ne
// doit être un mélange de l'ancienne et de la nouvelle.
func ReplaceKV(path, bucket string, m map[string]string) error {
	cacheBust(bucket)
	return withDB(path, func(d *bolt.DB) error {
		return d.Update(func(tx *bolt.Tx) error {
			if err := tx.DeleteBucket([]byte(bucket)); err != nil && !errors.Is(err, bolterrors.ErrBucketNotFound) {
				return err
			}
			b, err := tx.CreateBucket([]byte(bucket))
			if err != nil {
				return err
			}
			for k, v := range m {
				if v == "" {
					continue
				}
				if err := b.Put([]byte(k), []byte(v)); err != nil {
					return err
				}
			}
			return nil
		})
	})
}
