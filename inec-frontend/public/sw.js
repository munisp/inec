// INEC + GOTV Platform — Service Worker for Offline Support
const CACHE_VERSION = 'inec-v3';
const STATIC_CACHE = `${CACHE_VERSION}-static`;
const API_CACHE = `${CACHE_VERSION}-api`;
const OFFLINE_QUEUE_KEY = 'inec-offline-queue';

// Static assets to precache
const PRECACHE_URLS = [
  '/',
  '/index.html',
  '/manifest.json',
  '/offline.html',
];

// Critical API paths to cache aggressively for election day
const CRITICAL_API_PATHS = [
  // INEC
  '/elections', '/collation', '/results', '/geo/', '/dashboard',
  '/healthz', '/readiness', '/polling-units', '/bvas',
  // GOTV
  '/gotv/dashboard', '/gotv/campaigns', '/gotv/contacts', '/gotv/volunteers',
  '/gotv/pledges', '/gotv/rides', '/gotv/leaderboard', '/gotv/segments',
  '/gotv/warroom', '/gotv/roi/', '/gotv/scoring/',
  // KOH Indicators
  '/gotv/koh/cpi/', '/gotv/koh/surveys', '/gotv/koh/endorsements',
  '/gotv/koh/social/', '/gotv/koh/lga/', '/gotv/koh/analytics/',
  // Party Primaries
  '/gotv/primaries/aspirants', '/gotv/primaries/delegates',
  '/gotv/primaries/elections/', '/gotv/primaries/remote/',
];

// Install: precache static assets
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => cache.addAll(PRECACHE_URLS))
  );
  self.skipWaiting();
});

// Activate: clean old caches
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((key) => key.startsWith('inec-') && key !== STATIC_CACHE && key !== API_CACHE)
          .map((key) => caches.delete(key))
      )
    )
  );
  self.clients.claim();
});

// Fetch: network-first for API, cache-first for static
self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Skip non-GET and cross-origin
  if (event.request.method !== 'GET' && event.request.method !== 'POST') return;

  // POST requests: queue for offline retry
  if (event.request.method === 'POST') {
    event.respondWith(
      fetch(event.request.clone()).catch(async () => {
        // Queue the request for later
        const body = await event.request.clone().text();
        const queueItem = {
          url: event.request.url,
          method: 'POST',
          headers: Object.fromEntries(event.request.headers.entries()),
          body,
          timestamp: Date.now(),
        };
        const db = await openOfflineDB();
        const tx = db.transaction('queue', 'readwrite');
        tx.objectStore('queue').add(queueItem);
        await tx.complete;

        return new Response(
          JSON.stringify({ queued: true, message: 'Request queued for when online' }),
          { status: 202, headers: { 'Content-Type': 'application/json' } }
        );
      })
    );
    return;
  }

  // API requests: network-first, cache fallback
  const isApiPath = CRITICAL_API_PATHS.some(p => url.pathname.startsWith(p)) ||
    url.pathname.startsWith('/api/') || url.pathname.startsWith('/auth/me') ||
    url.pathname.startsWith('/observer/') || url.pathname.startsWith('/biometric/') ||
    url.pathname.startsWith('/command-center/') || url.pathname.startsWith('/anomaly/') ||
    url.pathname.startsWith('/blockchain/') || url.pathname.startsWith('/admin/') ||
    url.pathname.startsWith('/gotv/');
  if (isApiPath) {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response.ok) {
            const clone = response.clone();
            caches.open(API_CACHE).then((cache) => cache.put(event.request, clone));
          }
          return response;
        })
        .catch(() => caches.match(event.request).then(cached =>
          cached || new Response(JSON.stringify({ offline: true, cached_at: null }), {
            status: 503, headers: { 'Content-Type': 'application/json' }
          })
        ))
    );
    return;
  }

  // Static assets: cache-first
  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached;
      return fetch(event.request).then((response) => {
        if (response.ok && (url.pathname.endsWith('.js') || url.pathname.endsWith('.css') || url.pathname.endsWith('.html'))) {
          const clone = response.clone();
          caches.open(STATIC_CACHE).then((cache) => cache.put(event.request, clone));
        }
        return response;
      });
    })
  );
});

// Background sync: replay queued requests when online
self.addEventListener('sync', (event) => {
  if (event.tag === 'inec-offline-sync') {
    event.waitUntil(replayQueue());
  }
});

// Periodic sync: keep critical election data fresh
self.addEventListener('periodicsync', (event) => {
  if (event.tag === 'inec-data-refresh') {
    event.waitUntil(refreshCriticalData());
  }
});

async function refreshCriticalData() {
  const cache = await caches.open(API_CACHE);
  const criticalEndpoints = ['/elections', '/healthz', '/dashboard/stats?election_id=1'];
  for (const endpoint of criticalEndpoints) {
    try {
      const resp = await fetch(endpoint, { credentials: 'include' });
      if (resp.ok) await cache.put(new Request(endpoint), resp);
    } catch { /* offline */ }
  }
}

async function replayQueue() {
  const db = await openOfflineDB();
  const tx = db.transaction('queue', 'readonly');
  const items = await tx.objectStore('queue').getAll();

  for (const item of items) {
    try {
      const response = await fetch(item.url, {
        method: item.method,
        headers: item.headers,
        body: item.body,
        credentials: 'include',
      });
      if (response.ok) {
        const delTx = db.transaction('queue', 'readwrite');
        delTx.objectStore('queue').delete(item.id);
      }
    } catch {
      // Still offline, will retry next sync
      break;
    }
  }
}

function openOfflineDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('inec-offline', 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains('queue')) {
        db.createObjectStore('queue', { keyPath: 'id', autoIncrement: true });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

// Listen for messages from main thread
self.addEventListener('message', (event) => {
  if (event.data === 'skipWaiting') {
    self.skipWaiting();
  }
  if (event.data === 'getQueueCount') {
    openOfflineDB().then(db => {
      const tx = db.transaction('queue', 'readonly');
      const req = tx.objectStore('queue').count();
      req.onsuccess = () => {
        event.source.postMessage({ type: 'queueCount', count: req.result });
      };
    });
  }
});

// Push notification support for election alerts
self.addEventListener('push', (event) => {
  const data = event.data ? event.data.json() : { title: 'INEC Alert', body: 'New election update' };
  event.waitUntil(
    self.registration.showNotification(data.title || 'INEC Alert', {
      body: data.body || 'New update available',
      icon: '/favicon.ico',
      badge: '/favicon.ico',
      tag: data.tag || 'inec-alert',
      data: { url: data.url || '/' },
    })
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = event.notification.data?.url || '/';
  event.waitUntil(clients.openWindow(url));
});
