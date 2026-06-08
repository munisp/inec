import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// SW registration deferred to avoid fetch interception during dev
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', async () => {
    try {
      const reg = await navigator.serviceWorker.register('/sw.js');
      // Register background sync for offline queue
      if ('sync' in reg) {
        await (reg as unknown as { sync: { register: (tag: string) => Promise<void> } }).sync.register('inec-offline-sync');
      }
      // Register periodic sync to keep election data fresh
      if ('periodicSync' in reg) {
        const periodicSync = reg as unknown as { periodicSync: { register: (tag: string, opts: { minInterval: number }) => Promise<void> } };
        await periodicSync.periodicSync.register('inec-data-refresh', { minInterval: 60 * 60 * 1000 }); // hourly
      }
      // Listen for new service worker available
      reg.addEventListener('updatefound', () => {
        const newWorker = reg.installing;
        if (newWorker) {
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              console.info('New SW version available — will activate on next reload');
            }
          });
        }
      });
    } catch (err) {
      console.error('SW registration failed:', err);
    }
  });
}
