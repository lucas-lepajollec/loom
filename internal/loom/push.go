// push.go retains the installation's existing Web Push subscriptions. Phase 4
// delivery is opt-in through notification rules and uses payload-free VAPID
// requests; the worker fetches notices through normal Loom authentication.
package loom

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/notify"
)

// fmtDurFR : durée → « 42s », « 3 mn 05s », « 1 h 12 mn » pour le corps de la
// notification. Miroir serveur de fmtElapsed() côté UI (09-stream.js).
func fmtDurFR(d time.Duration) string {
	secs := int(d.Round(time.Second).Seconds())
	if secs < 0 {
		secs = 0
	}
	h, m, s := secs/3600, (secs%3600)/60, secs%60
	switch {
	case h > 0:
		return fmt.Sprintf("%d h %02d mn", h, m)
	case m > 0:
		return fmt.Sprintf("%d mn %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Stockées dans bkState (une seule base bbolt, voir store.go).
const (
	stPushVAPIDPriv = "push_vapid_priv" // clé privée VAPID (base64url)
	stPushVAPIDPub  = "push_vapid_pub"  // clé publique VAPID (base64url), servie à l'UI
	stPushSubs      = "push_subs"       // liste JSON des abonnements
)

type pushKeys struct {
	Auth   string `json:"auth"`
	P256dh string `json:"p256dh"`
}
type pushSub struct {
	Endpoint string   `json:"endpoint"`
	Keys     pushKeys `json:"keys"`
}

// vapidMu sérialise la génération paresseuse des clés : deux requêtes /api/push/key
// concurrentes sur une base neuve ne doivent pas produire deux paires différentes
// (la seconde écraserait la première, invalidant les abonnements de la première).
var vapidMu sync.Mutex

// vapidKeys renvoie la paire VAPID, la générant et la persistant au premier appel.
// La paire est STABLE pour la vie de l'installation : la changer invaliderait tous
// les abonnements déjà pris (le navigateur signe l'abonnement avec la clé publique).
func vapidKeys() (priv, pub string, err error) {
	vapidMu.Lock()
	defer vapidMu.Unlock()
	pair, err := persistentNotificationSecret("notify-vapid", func() (string, error) {
		priv, pub := getStr(bkState, stPushVAPIDPriv), getStr(bkState, stPushVAPIDPub)
		if priv == "" || pub == "" {
			var err error
			priv, pub, err = notify.GenerateVAPID()
			if err != nil {
				return "", err
			}
		}
		return priv + "." + pub, nil
	})
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(pair, ".")
	if len(parts) != 2 {
		return "", "", errors.New("invalid stored VAPID keys")
	}
	if err := putStr(bkState, stPushVAPIDPriv, ""); err != nil {
		return "", "", err
	}
	return parts[0], parts[1], nil
}

// subsMu sérialise les mutations de la liste d'abonnements (lecture-modif-écriture) :
// une inscription et une purge concurrentes ne doivent pas s'écraser l'une l'autre.
var subsMu sync.Mutex

func loadSubs() []pushSub { subs, _ := loadSubsErr(); return subs }
func loadSubsErr() ([]pushSub, error) {
	raw, err := getBytesErr(bkState, stPushSubs)
	if err != nil {
		return nil, errors.New("push subscription storage unavailable")
	}
	subs := []pushSub{}
	if len(raw) > 0 && json.Unmarshal(raw, &subs) != nil {
		return nil, errors.New("push subscription storage invalid")
	}
	return subs, nil
}

func saveSubs(subs []pushSub) error { return putJSON(bkState, stPushSubs, subs) }

// addSub enregistre un abonnement (idempotent : ré-inscrire le même endpoint met
// simplement à jour ses clés au lieu de le dupliquer — un navigateur ré-abonné
// garde le même endpoint mais peut renouveler ses clés).
func addSub(s pushSub) error {
	if s.Endpoint == "" {
		return nil
	}
	subsMu.Lock()
	defer subsMu.Unlock()
	subs, err := loadSubsErr()
	if err != nil {
		return err
	}
	for i, x := range subs {
		if x.Endpoint == s.Endpoint {
			subs[i] = s
			return saveSubs(subs)
		}
	}
	if len(subs) >= 64 {
		return errors.New("maximum 64 push subscriptions")
	}
	subs = append(subs, s)
	return saveSubs(subs)
}

// removeSub retire un abonnement par son endpoint (désinscription explicite, ou
// purge après un 404/410 « gone » renvoyé par le service de push).
func removeSub(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	subsMu.Lock()
	defer subsMu.Unlock()
	subs, err := loadSubsErr()
	if err != nil {
		return err
	}
	out := subs[:0]
	for _, x := range subs {
		if x.Endpoint != endpoint {
			out = append(out, x)
		}
	}
	return saveSubs(out)
}

// Legacy subscriptions remain usable through the opt-in notification rules.
func hasPushSubs() bool { return len(loadSubs()) > 0 }
