// Minimal service worker — its only job is to exist so the browser treats
// this as an installable PWA. This app is inherently online-first (chat +
// live session state live on the server), so there's no offline cache
// strategy here beyond a pass-through fetch handler.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
self.addEventListener("fetch", () => {
    // Intentionally not calling event.respondWith — let the network handle
    // every request normally.
});
