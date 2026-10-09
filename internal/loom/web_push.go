// web_push.go — endpoints d'inscription aux notifications Web Push (voir push.go).
// Tous passent par /api/* (donc par jfetch/E2E sur app.loom.local, comme le reste)
// en simple req/resp : le service worker et le manifest, eux, sont servis en clair
// à la racine (web_server.go), car un service worker doit venir de l'origine même.
package loom

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// pushEndpointHost extrait l'hôte d'un endpoint de push pour les logs (ex.
// web.push.apple.com) sans divulguer le jeton complet.
func pushEndpointHost(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return "?"
}

// handlePushKey (GET) : remet la clé publique VAPID dont l'UI a besoin pour
// s'abonner (PushManager.subscribe applicationServerKey). Générée à la volée au
// premier appel, puis stable.
func handlePushKey(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	if secure, reason := browserSecure(r); !secure {
		sendJSON(w, 400, map[string]any{"ok": false, "secure": false, "reason": reason})
		return
	}
	_, pub, err := vapidKeys()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "key": pub})
}

// handlePushSubscribe (POST) : enregistre l'abonnement PushSubscription du
// navigateur. Corps = l'objet renvoyé par subscription.toJSON() côté JS.
func handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	if secure, _ := browserSecure(r); !secure {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "Web Push requires a secure browser context"})
		return
	}
	var s pushSub
	if !workspaceDecode(w, r, &s) {
		return
	}
	u, err := url.Parse(s.Endpoint)
	ip := net.ParseIP(uHostname(u))
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(s.Endpoint) > 4096 || len(s.Keys.Auth) > 1024 || len(s.Keys.P256dh) > 1024 || ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate()) || u.Hostname() == "localhost" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "public HTTPS push endpoint required"})
		return
	}
	if err := addSub(s); err != nil {
		fmt.Printf("[push] subscription REJECTED (storage): %v\n", err)
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	fmt.Printf("[push] subscription saved (%s) — %d total\n", pushEndpointHost(s.Endpoint), len(loadSubs()))
	sendJSON(w, 200, map[string]any{"ok": true})
}

// handlePushUnsubscribe (POST {endpoint}) : retire un abonnement (l'utilisateur a
// coupé les notifs dans les réglages).
func handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !workspaceDecode(w, r, &body) {
		return
	}
	if err := removeSub(body.Endpoint); err != nil {
		webAuthUnavailable(w)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func uHostname(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Hostname()
}
