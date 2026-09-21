// Loom service worker — UNIQUEMENT les notifications Web Push.
//
// ⚠️ VOLONTAIREMENT sans cache ni handler `fetch` : mettre l'app en cache
// casserait l'anti-cache du portail E2E (app.ajean.link sert app.html?v=<build>
// avec un bootstrap qui doit toujours repartir du réseau — voir ajean-app). Un
// service worker qui interposerait une réponse en cache pourrait servir une UI
// périmée et bloquer la récupération de la clé E2E. Ce worker ne fait donc QUE
// recevoir les push du serveur et afficher la notification.
//
// Le SERVEUR (push.go) pousse à la fin d'un tour utilisateur, même app fermée /
// iPhone verrouillé — c'est tout l'intérêt par rapport à une notif côté page.

self.addEventListener('install', function(){ self.skipWaiting(); });
self.addEventListener('activate', function(e){ e.waitUntil(self.clients.claim()); });

self.addEventListener('push', function(e){
  var data = { title: 'Loom', body: 'Réponse prête' };
  try { if (e.data) data = Object.assign(data, e.data.json()); } catch (_){}
  e.waitUntil(self.registration.showNotification(data.title, {
    body: data.body,
    // tag + renotify : une nouvelle réponse REMPLACE l'ancienne notif (pas
    // d'empilement), mais re-sonne/vibre pour signaler qu'elle est fraîche.
    tag: data.tag || 'ajean-turn',
    renotify: true,
    // Icône = le carré « J » de la marque, en data-URI (aucun fichier externe,
    // compatible avec l'origine GitHub Pages du portail E2E).
    icon: "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'><rect width='16' height='16' rx='3' fill='%230d0d0d'/><g fill='none' stroke='%23f4f1ea' stroke-width='1.35' stroke-linecap='round'><path d='M3 5.5h10M3 8h10M3 10.5h10'/><path d='M5.5 3v10M8 3v10M10.5 3v10'/></g></svg>"
  }));
});

// Clic sur la notif : ramène l'onglet AJEAN au premier plan s'il est déjà ouvert,
// sinon en ouvre un. `includeUncontrolled` : les onglets ouverts AVANT que ce
// worker prenne le contrôle comptent aussi.
self.addEventListener('notificationclick', function(e){
  e.notification.close();
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function(cl){
    for (var i = 0; i < cl.length; i++){ if ('focus' in cl[i]) return cl[i].focus(); }
    if (self.clients.openWindow) return self.clients.openWindow('/');
  }));
});
