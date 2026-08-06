export function runWhenDocumentIsLoaded(
  callback: () => void,
  readyState: DocumentReadyState = document.readyState,
  addLoadListener: (listener: () => void) => void = (listener) => {
    window.addEventListener('load', listener, { once: true });
  },
): void {
  if (readyState === 'complete') {
    callback();
    return;
  }
  addLoadListener(callback);
}

export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator) || !import.meta.env.PROD) return;

  runWhenDocumentIsLoaded(() => {
    void navigator.serviceWorker.register('/sw.js').then((registration) => {
      registration.addEventListener('updatefound', () => {
        const worker = registration.installing;
        worker?.addEventListener('statechange', () => {
          if (worker.state === 'installed' && navigator.serviceWorker.controller) {
            worker.postMessage({ type: 'SKIP_WAITING' });
          }
        });
      });
    }).catch(() => {
      // Installability is progressive enhancement; the local database still protects work.
    });
  });
}
