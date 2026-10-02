package loom

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// La clé de pilotage n'est plus stockée en CLAIR : seule son EMPREINTE (SHA-256)
// est persistée, sous bkState "web_key_hash". Ainsi le serveur peut VALIDER un
// Bearer présenté (il compare les empreintes) mais ne détient jamais la clé
// elle-même — condition pour que cette même clé serve à ouvrir le coffre du
// chiffrement sans que le serveur puisse l'ouvrir seul.

// webKeyHashErr renvoie l'empreinte stockée, ou celle d'une ancienne clé en
// clair encore en attente de migration. Il distingue « aucune clé » d'une
// lecture ratée (voir requireWebAuth : une lecture ratée FERME l'API).
func webKeyHashErr() (string, error) {
	b, err := getBytesErr(bkState, "web_key_hash")
	if err != nil || len(b) != 0 {
		return string(b), err
	}
	// A pre-migration database can still contain the old plaintext key. Keep
	// protecting requests if the migration has not completed successfully.
	legacy, err := getBytesErr(bkState, "web_key")
	if err != nil || len(legacy) == 0 {
		return "", err
	}
	return hashWebKey(string(legacy)), nil
}

// webKeyConfigured indique qu'une clé de pilotage est définie (empreinte présente).
func webKeyConfigured() bool { h, _ := webKeyHashErr(); return h != "" }

// storeWebKey enregistre la clé de pilotage sous forme d'EMPREINTE uniquement
// (jamais le clair), et efface tout ancien clair résiduel. key vide = protection
// retirée.
func storeWebKey(key string) error {
	hash := ""
	if key != "" {
		hash = hashWebKey(key)
	}
	if err := putStr(bkState, "web_key_hash", hash); err != nil {
		return err
	}
	return putStr(bkState, "web_key", "") // le clair ne doit plus jamais traîner
}

// migrateWebKeyToHash convertit une ancienne clé stockée en clair vers son
// empreinte (une fois), puis efface le clair. Appelé au démarrage.
func migrateWebKeyToHash() {
	plain := getStr(bkState, "web_key")
	if plain == "" {
		return
	}
	hash, err := getBytesErr(bkState, "web_key_hash")
	if err != nil {
		return
	}
	if len(hash) == 0 {
		if err := putStr(bkState, "web_key_hash", hashWebKey(plain)); err != nil {
			return // the legacy key still protects access until the next attempt
		}
	}
	_ = putStr(bkState, "web_key", "") // le clair ne doit plus jamais traîner
}

// web_auth.go protège l'API de pilotage (loom web) quand elle est exposée sur
// internet — c.-à-d. l'API que tout client (navigateur, app mobile, script…)
// utilise pour switcher de preset, redémarrer le service, lire le status, etc.
//
// La clé de pilotage est volontairement DISTINCTE de .api_key (qui, elle,
// protège llama-server / les complétions). On veut pouvoir donner à un client
// un accès aux complétions sans lui donner le droit de redémarrer la machine,
// et inversement. Elle est relue à chaque requête (pas de cache) pour qu'un
// changement de clé prenne effet sans redémarrer le serveur web.

// readWebKeyErr renvoie la clé de pilotage en distinguant « aucune clé » d'une
// LECTURE RATÉE. La nuance est tout sauf cosmétique : sans clé, l'API est
// ouverte. Confondre les deux, c'est ouvrir l'API parce que la base était
// momentanément verrouillée par une commande CLI — une panne d'E/S qui désarme
// l'authentification. requireWebAuth refuse donc plutôt que d'ouvrir.
func readWebKeyErr() (string, error) {
	b, err := getBytesErr(bkState, "web_key")
	return string(b), err
}

// readWebKey renvoie la clé de pilotage, ou "" si aucune n'est définie (ou
// illisible). Réservé à l'affichage ; toute décision d'accès passe par
// readWebKeyErr.
func readWebKey() string { k, _ := readWebKeyErr(); return k }

// cmdSetWebKey sets (or clears) the control-API key in $LOOM_HOME/.web_key.
//
//	loom set-web-key <clé>     définit la clé
//	loom set-web-key           génère une clé aléatoire
//	loom set-web-key ""        supprime la protection (API ouverte)
//
// Contrairement à set-api-key, aucun redémarrage n'est nécessaire : le serveur
// web relit la clé à chaque requête.
func cmdSetWebKey(args []string) error {
	var key string
	switch {
	case len(args) == 0:
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		key = "loom-web-" + hex.EncodeToString(buf)
		fmt.Printf("%s generated key: %s\n", green("[ok]"), bold(key))
	case args[0] == "" || args[0] == "off" || args[0] == "none":
		key = ""
	default:
		key = strings.TrimSpace(args[0])
	}
	if err := storeWebKey(key); err != nil {
		return err
	}
	if key == "" {
		fmt.Printf("%s control key removed — the web API is no longer protected\n", yellow("[info]"))
		return nil
	}
	fmt.Printf("%s control key saved\n", green("[ok]"))
	fmt.Printf("       clients must send: %s\n", dim("Authorization: Bearer "+key))
	fmt.Printf("       (restart 'loom web' if the web server is already running — not required, read dynamically)\n")
	return nil
}
