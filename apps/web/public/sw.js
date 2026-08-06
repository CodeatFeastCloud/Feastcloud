const CACHE_NAME = 'feastcloud-shell-v5';
const RUNTIME_CACHE = 'feastcloud-runtime-v5';
// Language resources live in a separate cache populated only after application-level
// schema, origin and SHA-256 verification. Preserve it across shell upgrades.
const LANGUAGE_PACK_CACHE = 'feastcloud-language-packs-v1';
const APP_SHELL = ['/', '/index.html', '/manifest.webmanifest', '/icons/feastcloud.svg'];

function isApiRequest(url) {
  return url.pathname === '/api' || url.pathname.startsWith('/api/');
}

function isCacheableAsset(url) {
  return (
    APP_SHELL.includes(url.pathname) ||
    url.pathname.startsWith('/assets/')
  );
}

function responseAllowsStorage(response) {
  const cacheControl = response.headers.get('Cache-Control')?.toLowerCase() ?? '';
  return response.ok && !cacheControl.includes('no-store') && !cacheControl.includes('private');
}

async function precacheAppShell() {
  const cache = await caches.open(CACHE_NAME);
  await cache.addAll(APP_SHELL);

  // Vite fingerprints production bundles. Discover those URLs from the built HTML
  // during installation so the very first successful visit is safe to reopen offline.
  const response = await fetch('/index.html', { cache: 'reload' });
  if (!response.ok) return;
  const html = await response.clone().text();
  await cache.put('/index.html', response);
  const buildAssets = Array.from(
    html.matchAll(/(?:src|href)=["'](\/assets\/[^"']+)["']/g),
    (match) => match[1],
  );
  if (buildAssets.length > 0) await cache.addAll([...new Set(buildAssets)]);
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    precacheAppShell().then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) =>
        Promise.all(
          names
            .filter(
              (name) =>
                name !== CACHE_NAME &&
                name !== RUNTIME_CACHE &&
                name !== LANGUAGE_PACK_CACHE,
            )
            .map((name) => caches.delete(name)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const request = event.request;

  if (request.method !== 'GET') return;

  const url = new URL(request.url);
  if (
    url.origin !== self.location.origin ||
    isApiRequest(url) ||
    request.headers.has('Authorization') ||
    request.cache === 'no-store'
  ) return;

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (responseAllowsStorage(response)) {
            const copy = response.clone();
            event.waitUntil(caches.open(RUNTIME_CACHE).then((cache) => cache.put('/index.html', copy)));
          }
          return response;
        })
        .catch(async () => (await caches.match(request)) || caches.match('/index.html')),
    );
    return;
  }

  if (!isCacheableAsset(url)) return;

  event.respondWith(
    caches.match(request).then((cached) => {
      const network = fetch(request)
        .then((response) => {
          if (responseAllowsStorage(response)) {
            const copy = response.clone();
            event.waitUntil(caches.open(RUNTIME_CACHE).then((cache) => cache.put(request, copy)));
          }
          return response;
        })
        .catch(() => cached);

      return cached || network;
    }),
  );
});

self.addEventListener('message', (event) => {
  if (event.data?.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('sync', (event) => {
  if (event.tag !== 'feastcloud-outbox') return;
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      clients.forEach((client) => client.postMessage({ type: 'FLUSH_OUTBOX' }));
    }),
  );
});
