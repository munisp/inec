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
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
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
  const { request } = event;
  const url = new URL(request.url);

  // API requests — pass through without interception
  const isAPI = url.pathname.startsWith('/api') || 
                url.pathname.startsWith('/auth') ||
                url.pathname.startsWith('/ws');
  if (isAPI) return;

  // For write requests to known observer endpoints — queue if offline
  if ((url.pathname.startsWith('/observer/') || url.pathname.startsWith('/results/')) &&
      (request.method === 'POST' || request.method === 'PUT' || request.method === 'PATCH')) {
    event.respondWith(
      fetch(request.clone()).catch(async () => {
        const body = await request.clone().text();
        await queueOfflineRequest({
          url: request.url,
          method: request.method,
          headers: Object.fromEntries(request.headers.entries()),
          body,
          timestamp: Date.now(),
        });
        return new Response(JSON.stringify({
          queued: true,
          message: 'Request queued — will sync when online',
        }), {
          status: 202,
          headers: { 'Content-Type': 'application/json' },
        });
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

// Background Sync — replay queued requests when back online
self.addEventListener('sync', (event) => {
  if (event.tag === 'offline-sync') {
    event.waitUntil(replayOfflineQueue());
  }
});

// Push Notifications — display result updates
self.addEventListener('push', (event) => {
  const data = event.data ? event.data.json() : {};
  const title = data.title || 'INEC Election Update';
  const options = {
    body: data.body || 'New result submitted',
    icon: '/favicon.ico',
    badge: '/favicon.ico',
    data: data.url || '/',
    actions: [
      { action: 'view', title: 'View Result' },
      { action: 'dismiss', title: 'Dismiss' },
    ],
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  if (event.action === 'view') {
    event.waitUntil(self.clients.openWindow(event.notification.data));
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

async function replayOfflineQueue() {
  const db = await openIndexedDB();
  const tx = db.transaction('offline-queue', 'readwrite');
  const store = tx.objectStore('offline-queue');
  const requests = await getAllFromStore(store);

  for (const req of requests) {
    try {
      await fetch(req.url, {
        method: req.method,
        headers: req.headers,
        body: req.body,
      });
      store.delete(req.id);
    } catch {
      break;
    }
  }
}

function openIndexedDB() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('inec-offline', 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore('offline-queue', { keyPath: 'id', autoIncrement: true });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function getAllFromStore(store) {
  return new Promise((resolve) => {
    const request = store.getAll();
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => resolve([]);
  });
}
