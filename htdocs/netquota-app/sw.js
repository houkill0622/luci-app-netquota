// netquota-app Service Worker
const CACHE = 'netquota-v2';
const ASSETS = [
  '/netquota-app/',
  '/netquota-app/index.html',
  '/netquota-app/manifest.json',
  '/netquota-app/icons/icon-192.png',
  '/netquota-app/icons/icon-512.png'
];

// Install: cache assets
self.addEventListener('install', e => {
  e.waitUntil(
    caches.open(CACHE).then(cache => cache.addAll(ASSETS)).then(() => self.skipWaiting())
  );
});

// Activate: clean old caches
self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(keys => Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k))))
  );
});

// Fetch: network first, fallback to cache
self.addEventListener('fetch', e => {
  // Only handle netquota-app requests
  if (!e.request.url.includes('/netquota-app/')) return;
  
  e.respondWith(
    fetch(e.request).catch(() => caches.match(e.request))
  );
});