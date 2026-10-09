// Loom service worker — UNIQUEMENT les notifications Web Push.
//
// ⚠️ VOLONTAIREMENT sans cache ni handler `fetch` : ce worker ne fait QUE
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
    tag: data.tag || 'loom-turn',
    renotify: true,
    // Icône = le logo de la marque Loom, en data-URI.
    icon: "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 256 256'><rect x='8.5' y='8.5' width='239' height='239' rx='55.5' fill='%230d0d0d'/><path transform='translate(42.67 42.67) scale(.6667)' fill='%23f4f1ea' d='t'/></svg>"
  }));
});

// Clic sur la notif : ramène l'onglet Loom au premier plan s'il est déjà ouvert,
// sinon en ouvre un. `includeUncontrolled` : les onglets ouverts AVANT que ce
// worker prenne le contrôle comptent aussi.
self.addEventListener('notificationclick', function(e){
  e.notification.close();
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function(cl){
    for (var i = 0; i < cl.length; i++){ if ('focus' in cl[i]) return cl[i].focus(); }
    if (self.clients.openWindow) return self.clients.openWindow('/');
  }));
});
