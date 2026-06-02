import { useState, useEffect } from 'react';
import { WifiOff, Wifi } from 'lucide-react';

export function OfflineBanner() {
  const [online, setOnline] = useState(navigator.onLine);
  const [showReconnect, setShowReconnect] = useState(false);

  useEffect(() => {
    const onOnline = () => {
      setOnline(true);
      setShowReconnect(true);
      setTimeout(() => setShowReconnect(false), 3000);
    };
    const onOffline = () => { setOnline(false); setShowReconnect(false); };

    window.addEventListener('online', onOnline);
    window.addEventListener('offline', onOffline);
    return () => {
      window.removeEventListener('online', onOnline);
      window.removeEventListener('offline', onOffline);
    };
  }, []);

  if (online && !showReconnect) return null;

  return (
    <div
      className={`fixed top-0 left-0 right-0 z-[200] flex items-center justify-center gap-2 py-2 px-4 text-sm font-medium transition-all duration-500 ${
        online
          ? 'bg-green-500 text-white translate-y-0'
          : 'bg-zinc-800 text-white translate-y-0'
      }`}
      role="status"
      aria-live="assertive"
    >
      {online ? (
        <>
          <Wifi className="w-4 h-4" />
          <span>Back online — syncing data</span>
        </>
      ) : (
        <>
          <WifiOff className="w-4 h-4" />
          <span>You are offline — changes will sync when reconnected</span>
        </>
      )}
    </div>
  );
}
