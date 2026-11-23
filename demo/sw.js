// Loom Public Demo service worker — intentionally no-op.
// Never caches API routes, conversations, or simulated state.

self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});
