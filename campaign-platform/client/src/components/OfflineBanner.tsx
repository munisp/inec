/**
 * OfflineBanner component
 * Shows a persistent banner when the device loses internet connectivity.
 * Informs field teams that the app is still usable with cached data.
 * Automatically dismisses when connectivity is restored.
 */
import { useEffect, useState } from "react";
import { WifiOff, Wifi, X, Database } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useOfflineStatus } from "../hooks/useOfflineStatus";

export default function OfflineBanner() {
  const { isOnline, wasOffline, lastOffline, lastOnline } = useOfflineStatus();
  const [showRestored, setShowRestored] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  // When connectivity is restored, show a brief "restored" toast
  useEffect(() => {
    if (isOnline && wasOffline) {
      setShowRestored(true);
      setDismissed(false);
      const t = setTimeout(() => setShowRestored(false), 4000);
      return () => clearTimeout(t);
    }
  }, [isOnline, wasOffline]);

  // Reset dismissed state when going offline again
  useEffect(() => {
    if (!isOnline) setDismissed(false);
  }, [isOnline]);

  const offlineDuration = lastOffline
    ? Math.round((Date.now() - lastOffline.getTime()) / 60000)
    : 0;

  return (
    <AnimatePresence>
      {/* Offline banner */}
      {!isOnline && !dismissed && (
        <motion.div
          key="offline"
          initial={{ y: -48, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -48, opacity: 0 }}
          transition={{ duration: 0.25, ease: [0.23, 1, 0.32, 1] }}
          className="fixed top-0 left-0 right-0 z-50 flex items-center gap-3 px-4 py-2.5"
          style={{ background: "oklch(0.35 0.12 50)", borderBottom: "1px solid oklch(0.50 0.18 50)" }}
        >
          <WifiOff className="w-4 h-4 flex-shrink-0" style={{ color: "oklch(0.92 0.08 50)" }} />
          <div className="flex-1 min-w-0">
            <span className="text-xs font-bold" style={{ color: "oklch(0.97 0.02 50)" }}>
              Offline — Cached Data Active
            </span>
            <span className="text-xs ml-2" style={{ color: "oklch(0.80 0.06 50)" }}>
              {offlineDuration > 0 ? `${offlineDuration}m ago` : "Just now"} · All stakeholder data is still available
            </span>
          </div>
          <div className="flex items-center gap-1.5 flex-shrink-0">
            <Database className="w-3.5 h-3.5" style={{ color: "oklch(0.80 0.06 50)" }} />
            <span className="text-xs" style={{ color: "oklch(0.80 0.06 50)" }}>Local cache</span>
          </div>
          <button
            onClick={() => setDismissed(true)}
            className="flex-shrink-0 p-1 rounded transition-colors"
            style={{ color: "oklch(0.80 0.06 50)" }}
            title="Dismiss"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </motion.div>
      )}

      {/* Connection restored toast */}
      {isOnline && showRestored && (
        <motion.div
          key="restored"
          initial={{ y: -48, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          exit={{ y: -48, opacity: 0 }}
          transition={{ duration: 0.25, ease: [0.23, 1, 0.32, 1] }}
          className="fixed top-0 left-0 right-0 z-50 flex items-center gap-3 px-4 py-2.5"
          style={{ background: "oklch(0.30 0.12 145)", borderBottom: "1px solid oklch(0.45 0.18 145)" }}
        >
          <Wifi className="w-4 h-4 flex-shrink-0" style={{ color: "oklch(0.85 0.12 145)" }} />
          <span className="text-xs font-bold" style={{ color: "oklch(0.97 0.02 145)" }}>
            Connection Restored
          </span>
          {lastOnline && (
            <span className="text-xs" style={{ color: "oklch(0.72 0.08 145)" }}>
              · Back online at {lastOnline.toLocaleTimeString("en-NG")}
            </span>
          )}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

