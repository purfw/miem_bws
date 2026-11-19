const SHELL_CACHE = "confhub-shell-v2";
const STATIC_CACHE = "confhub-static-v2";
const STATIC_CACHE_LIMIT = 60;
const APP_SHELL = ["/", "/manifest.webmanifest", "/favicon.svg", "/icons/icon-192.png", "/icons/icon-512.png"];

// Hashed build assets accumulate across deploys; drop the oldest entries (keys() is insertion-ordered).
async function trimCache(name, limit) {
  const cache = await caches.open(name);
  const keys = await cache.keys();
  await Promise.all(keys.slice(0, Math.max(0, keys.length - limit)).map((key) => cache.delete(key)));
}

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.addAll(APP_SHELL)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  const current = new Set([SHELL_CACHE, STATIC_CACHE]);
  event.waitUntil(
    caches.keys()
      .then((names) => Promise.all(names.filter((name) => !current.has(name)).map((name) => caches.delete(name))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;

  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() => caches.match("/")),
    );
    return;
  }

  if (!["script", "style", "image", "font", "manifest"].includes(request.destination)) return;
  event.respondWith(
    caches.match(request).then((cached) => {
      const network = fetch(request).then((response) => {
        if (response.ok && response.type === "basic") {
          const copy = response.clone();
          void caches.open(STATIC_CACHE)
            .then((cache) => cache.put(request, copy))
            .then(() => trimCache(STATIC_CACHE, STATIC_CACHE_LIMIT));
        }
        return response;
      });
      if (cached) {
        network.catch(() => undefined);
        return cached;
      }
      return network;
    }),
  );
});
