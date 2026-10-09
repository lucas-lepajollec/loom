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

// A payload-free push wakes the worker even when every tab is closed. Normal
// same-origin authentication protects pending notices; never cache this reply.
self.addEventListener('push', function(e){
  e.waitUntil((async function(){
    let notices = [];
    try {
      const response = await fetch('/api/notify/pending', {credentials:'same-origin',cache:'no-store'});
      if (response.ok) {
        const data = await response.json();
        if (Array.isArray(data.notifications)) notices = data.notifications;
      }
    } catch (_) {}
    if (!notices.length) notices = [{title:'Loom',body:'Open Loom to view task updates',url:'/#/tasks',tag:'loom-tasks'}];
    for (const notice of notices.slice(-64)) {
      let url = '/#/tasks';
      try { if (typeof notice.url === 'string' && notice.url) { const target = new URL(notice.url, self.location.origin); if (target.origin === self.location.origin) url = target.href; } } catch (_) {}
      await self.registration.showNotification(notice.title || 'Loom', {
        body: notice.body || 'Task update', tag: notice.tag || 'loom-tasks', renotify:false,
        icon:'/icons/loom-192.png', data:{url}, actions:[{action:'open',title:'Open'}]
      });
    }
  })());
});
self.addEventListener('notificationclick', function(e){
  e.notification.close();
  e.waitUntil((async function(){
    let url = new URL('/#/tasks', self.location.origin).href;
    try { const raw = e.notification.data?.url; if (typeof raw === 'string' && raw) { const target = new URL(raw, self.location.origin); if (target.origin === self.location.origin) url = target.href; } } catch (_) {}
    const windows = await self.clients.matchAll({type:'window',includeUncontrolled:true});
    for (const client of windows) {
      if (new URL(client.url).origin !== self.location.origin) continue;
      if ('navigate' in client) await client.navigate(url);
      if ('focus' in client) return client.focus();
    }
    if (self.clients.openWindow) return self.clients.openWindow(url);
  })());
});
