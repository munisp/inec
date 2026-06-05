// INEC Platform Service Worker — Offline Support + Push Notifications
const CACHE_NAME = 'inec-platform-v1';
const OFFLINE_QUEUE_KEY = 'inec-offline-queue';

// Static assets to cache for offline access
const STATIC_ASSETS = [
  '/',
  '/index.html',
];

// Install — pre-cache static shell
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
  );
  self.skipWaiting();
});

// Activate — clean old caches
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    )
  );
  self.clients.claim();
});

// Fetch — network-first with offline fallback
self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // Only intercept navigation and static asset requests
  // Let ALL API requests pass through to the network (Vite proxy handles them)
  const staticExtensions = ['.js', '.css', '.png', '.jpg', '.svg', '.ico', '.woff', '.woff2', '.ttf'];
  const isStaticAsset = staticExtensions.some(ext => url.pathname.endsWith(ext));
  const isNavigation = request.mode === 'navigate';
  const isHTML = request.headers.get('accept')?.includes('text/html');

  // API requests — pass through without interception
  if (!isStaticAsset && !isNavigation && !isHTML) {
    return; // Don't call event.respondWith — let browser handle normally
  }

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

  // Static assets — cache first, network fallback
  if (isStaticAsset) {
    event.respondWith(
      caches.match(request).then((cached) => cached || fetch(request))
    );
    return;
  }

  // Navigation — network first, cache fallback (for offline shell)
  if (isNavigation || isHTML) {
    event.respondWith(
      fetch(request).then((response) => {
        const clone = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
        return response;
      }).catch(() => caches.match(request).then(cached => cached || caches.match('/')))
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

// ── Offline Queue Helpers ──

async function queueOfflineRequest(request) {
  const db = await openIndexedDB();
  const tx = db.transaction('offline-queue', 'readwrite');
  tx.objectStore('offline-queue').add(request);
  await tx.complete;

  // Register for background sync
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
      // Will retry on next sync
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
