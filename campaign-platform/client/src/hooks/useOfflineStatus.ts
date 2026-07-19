/**
 * useOfflineStatus hook
 * Detects browser connectivity state using the Navigator onLine API
 * and the online/offline events. Returns live connectivity status.
 */
import { useState, useEffect } from "react";

export interface OfflineStatus {
  isOnline: boolean;
  wasOffline: boolean;   // true if the session went offline at least once
  lastOnline: Date | null;
  lastOffline: Date | null;
}

export function useOfflineStatus(): OfflineStatus {
  const [isOnline, setIsOnline] = useState(() => navigator.onLine);
  const [wasOffline, setWasOffline] = useState(false);
  const [lastOnline, setLastOnline] = useState<Date | null>(navigator.onLine ? new Date() : null);
  const [lastOffline, setLastOffline] = useState<Date | null>(null);

  useEffect(() => {
    function handleOnline() {
      setIsOnline(true);
      setLastOnline(new Date());
    }
    function handleOffline() {
      setIsOnline(false);
      setWasOffline(true);
      setLastOffline(new Date());
    }

    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);

    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  return { isOnline, wasOffline, lastOnline, lastOffline };
}
