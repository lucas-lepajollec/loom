// Cache only the public offline fallback and brand assets, NEVER the app HTML,
// API, conversations, credentials, provider requests or SSE.
const PWA_CACHE='loom-public-offline-v1';
const PWA_ASSETS=['/offline.html','/icons/loom-192.png','/icons/loom-512.png'];
self.addEventListener('install', function(e){e.waitUntil(caches.open(PWA_CACHE).then(c=>c.addAll(PWA_ASSETS)).then(()=>self.skipWaiting()));});
self.addEventListener('activate', function(e){e.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k.startsWith('loom-public-offline-')&&k!==PWA_CACHE).map(k=>caches.delete(k)))).then(()=>self.clients.claim()));});
self.addEventListener('fetch',function(e){
  const url=new URL(e.request.url);
  if(e.request.method!=='GET'||url.origin!==self.location.origin||url.search)return;
  if(e.request.mode==='navigate'&&(url.pathname==='/'||url.pathname==='/index.html')){
    e.respondWith(fetch(e.request).catch(()=>caches.match('/offline.html')));
  }else if(PWA_ASSETS.includes(url.pathname)){
    e.respondWith(caches.match(e.request).then(hit=>hit||fetch(e.request)));
  }
});

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
    icon: "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'><rect width='16' height='16' rx='3' fill='%230d0d0d'/><g fill='none' stroke='%23f4f1ea' stroke-width='1.35' stroke-linecap='round'><path d='M3 5.5h10M3 8h10M3 10.5h10'/><path d='M5.5 3v10M8 3v10M10.5 3v10'/></g></svg>"
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
