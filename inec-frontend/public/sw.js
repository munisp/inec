// INEC Platform Service Worker — Offline Support + Cache Busting + Push Notifications
//
// IMPORTANT: Bump CACHE_VERSION on every deployment to force cache refresh.
// The build script injects the current timestamp automatically.
const CACHE_VERSION = '__BUILD_TIMESTAMP__';
const CACHE_NAME = `inec-platform-${CACHE_VERSION}`;
const OFFLINE_QUEUE_KEY = 'inec-offline-queue';

// Static assets to cache — do NOT cache index.html (always fetch fresh)
const STATIC_ASSETS = [];

// Install — skip waiting to activate immediately
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => cache.addAll(PRECACHE_URLS))
  );
  self.skipWaiting();
});

// Activate — delete ALL old caches (forces fresh content on new deploy)
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k))
      )
    ).then(() => {
      // Notify all clients to reload with fresh content
      return self.clients.matchAll({ type: 'window' }).then((clients) => {
        clients.forEach((client) => {
          client.postMessage({ type: 'SW_UPDATED', version: CACHE_VERSION });
        });
      });
    })
  );
  self.clients.claim();
});

// Fetch strategy:
// - index.html / navigation: ALWAYS network-first, never serve stale HTML
// - Hashed assets (/assets/*): cache-first (filename contains content hash)
// - API requests: pass through (no interception)
self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // API requests — pass through without interception
  const isAPI = url.pathname.startsWith('/api') || 
                url.pathname.startsWith('/auth') ||
                url.pathname.startsWith('/ws');
  if (isAPI) return;

  // For write requests to known observer endpoints — queue if offline
  if ((url.pathname.startsWith('/observer/') || url.pathname.startsWith('/results/')) &&
      (request.method === 'POST' || request.method === 'PUT' || request.method === 'PATCH')) {
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

  const isNavigation = request.mode === 'navigate';
  const isHTML = request.headers.get('accept')?.includes('text/html');
  const isHashedAsset = url.pathname.startsWith('/assets/');

  // Hashed assets (e.g. /assets/TVDashboardPage-C3nJroOo.js)
  // These have content hashes in the filename — safe to cache forever
  if (isHashedAsset) {
    event.respondWith(
      caches.match(request).then((cached) => {
        if (cached) return cached;
        return fetch(request).then((response) => {
          if (response.ok) {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
          }
          return response;
        });
      })
    );
    return;
  }

  // Navigation / HTML — ALWAYS network-first, NEVER serve stale index.html
  if (isNavigation || isHTML) {
    event.respondWith(
      fetch(request).then((response) => {
        return response;
      }).catch(() => {
        // Only fall back to cache if truly offline
        return caches.match(request).then(cached => cached || caches.match('/'));
      })
    );
    return;
  }

  // Other static assets (icons, fonts, etc.) — network first with cache fallback
  const staticExtensions = ['.png', '.jpg', '.svg', '.ico', '.woff', '.woff2', '.ttf', '.css'];
  const isStaticAsset = staticExtensions.some(ext => url.pathname.endsWith(ext));
  if (isStaticAsset) {
    event.respondWith(
      fetch(request).then((response) => {
        if (response.ok) {
          const clone = response.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
        }
        return response;
      }).catch(() => caches.match(request))
    );
  }
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

// Listen for skip-waiting message from the app
self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') {
    self.skipWaiting();
  }
  if (event.data === 'CLEAR_CACHE') {
    caches.keys().then((keys) => Promise.all(keys.map((k) => caches.delete(k))));
  }
});

// ── Offline Queue Helpers ──

async function queueOfflineRequest(request) {
  const db = await openIndexedDB();
  const tx = db.transaction('offline-queue', 'readwrite');
  tx.objectStore('offline-queue').add(request);
  await tx.complete;

  if (self.registration.sync) {
    await self.registration.sync.register('offline-sync');
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
