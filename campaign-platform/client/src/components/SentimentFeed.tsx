/**
 * Real-Time Sentiment Feed
 * Live approval trend ticker showing candidate sentiment by geopolitical zone.
 * Polls the campaign planning sentiment endpoint. If no backend is configured
 * or the endpoint is unreachable, an explicit "unavailable" state is shown —
 * sentiment figures are NEVER simulated.
 */
import { useState, useEffect, useCallback, useRef } from "react";
import { TrendingUp, TrendingDown, Minus, Radio, RefreshCw, AlertTriangle } from "lucide-react";

interface ZoneSentiment {
  zone: string;
  code: string;
  approval: number;
  delta: number;   // change from last poll
  trend: "up" | "down" | "flat";
  sampleSize: number;
  lastUpdated: Date;
}

function approvalColor(pct: number): string {
  if (pct >= 60) return "oklch(0.65 0.18 145)";  // green
  if (pct >= 45) return "oklch(0.75 0.18 80)";   // amber
  return "oklch(0.65 0.18 25)";                   // red
}

interface Props {
  candidateName: string;
  office: string;
  stateName: string;
  compact?: boolean;
}

export default function SentimentFeed({ candidateName, office, stateName, compact = false }: Props) {
  const [data, setData] = useState<ZoneSentiment[]>([]);
  const [loading, setLoading] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchSentiment = useCallback(async () => {
    setLoading(true);
    try {
      // Attempt to call the campaign planning backend
      const backendUrl = import.meta.env.VITE_CAMPAIGN_API_URL ?? "";
      if (backendUrl) {
        const res = await fetch(`${backendUrl}/api/campaign/sentiment`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ candidate_name: candidateName, office, state: stateName }),
          signal: AbortSignal.timeout(3000),
        });
        if (res.ok) {
          const json = await res.json();
          if (json.zones) {
            setData(json.zones);
            setUnavailable(false);
            setLastRefresh(new Date());
            setLoading(false);
            return;
          }
        }
      }
    } catch {
      // Backend unavailable — show the explicit unavailable state below.
    }
    // No live sentiment source is connected. Never fabricate sentiment figures.
    setData([]);
    setUnavailable(true);
    setLastRefresh(null);
    setLoading(false);
  }, [candidateName, office, stateName]);

  // Initial fetch
  useEffect(() => {
    fetchSentiment();
  }, [fetchSentiment]);

  // Auto-refresh every 90 seconds
  useEffect(() => {
    if (autoRefresh) {
      intervalRef.current = setInterval(fetchSentiment, 90000);
    } else {
      if (intervalRef.current) clearInterval(intervalRef.current);
    }
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [autoRefresh, fetchSentiment]);

  const nationalAvg = data.length > 0
    ? Math.round(data.reduce((s, z) => s + z.approval, 0) / data.length)
    : null;

  if (compact) {
    // Compact mode: single-line ticker for sidebar
    if (unavailable) {
      return (
        <div
          className="rounded border px-3 py-2 flex items-center gap-3"
          style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
        >
          <div className="flex items-center gap-1.5 flex-shrink-0">
            <AlertTriangle className="w-3 h-3" style={{ color: "oklch(0.75 0.18 80)" }} />
            <span className="text-xs font-bold" style={{ color: "oklch(0.55 0.01 240)" }}>SENTIMENT</span>
          </div>
          <span className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
            Unavailable — no live source connected
          </span>
        </div>
      );
    }
    return (
      <div
        className="rounded border px-3 py-2 flex items-center gap-3"
        style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
      >
        <div className="flex items-center gap-1.5 flex-shrink-0">
          <Radio className="w-3 h-3 animate-pulse" style={{ color: "oklch(0.65 0.18 145)" }} />
          <span className="text-xs font-bold" style={{ color: "oklch(0.55 0.01 240)" }}>SENTIMENT</span>
        </div>
        <div className="flex items-center gap-3 overflow-x-auto flex-1 min-w-0">
          {data.map(z => (
            <div key={z.code} className="flex items-center gap-1 flex-shrink-0">
              <span className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{z.code}</span>
              <span className="text-xs font-bold" style={{ color: approvalColor(z.approval) }}>{z.approval}%</span>
              {z.trend === "up" && <TrendingUp className="w-3 h-3" style={{ color: "oklch(0.65 0.18 145)" }} />}
              {z.trend === "down" && <TrendingDown className="w-3 h-3" style={{ color: "oklch(0.65 0.18 25)" }} />}
              {z.trend === "flat" && <Minus className="w-3 h-3" style={{ color: "oklch(0.55 0.01 240)" }} />}
            </div>
          ))}
        </div>
        {nationalAvg !== null && (
          <div className="flex-shrink-0 text-xs font-bold" style={{ color: approvalColor(nationalAvg) }}>
            NAT {nationalAvg}%
          </div>
        )}
      </div>
    );
  }

  // Full mode: detailed sentiment panel
  return (
    <div className="flex flex-col gap-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {unavailable ? (
            <AlertTriangle className="w-4 h-4" style={{ color: "oklch(0.75 0.18 80)" }} />
          ) : (
            <Radio className="w-4 h-4 animate-pulse" style={{ color: "oklch(0.65 0.18 145)" }} />
          )}
          <span className="text-sm font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>
            Live Sentiment Tracker
          </span>
          {loading && <RefreshCw className="w-3 h-3 animate-spin" style={{ color: "oklch(0.55 0.01 240)" }} />}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setAutoRefresh(a => !a)}
            className="text-xs px-2 py-1 rounded border transition-all"
            style={{
              background: autoRefresh ? "oklch(0.22 0.12 145)" : "oklch(0.18 0.008 240)",
              borderColor: autoRefresh ? "oklch(0.45 0.18 145)" : "oklch(0.28 0.01 240)",
              color: autoRefresh ? "oklch(0.70 0.18 145)" : "oklch(0.55 0.01 240)",
            }}
          >
            {autoRefresh ? "Auto ✓" : "Auto"}
          </button>
          <button
            onClick={fetchSentiment}
            className="text-xs px-2 py-1 rounded border transition-all"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.55 0.01 240)" }}
          >
            <RefreshCw className="w-3 h-3" />
          </button>
        </div>
      </div>

      {unavailable ? (
        <div
          className="rounded border p-4 text-center"
          style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.75 0.18 80)" }}
        >
          <AlertTriangle className="w-5 h-5 mx-auto mb-2" style={{ color: "oklch(0.75 0.18 80)" }} />
          <div className="text-sm font-bold mb-1" style={{ color: "oklch(0.88 0.005 240)" }}>
            Sentiment data unavailable — no live source connected
          </div>
          <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>
            No simulated sentiment is shown. Connect a sentiment backend (VITE_CAMPAIGN_API_URL) to populate this panel.
          </div>
        </div>
      ) : (
      <>
      {/* National average */}
      {nationalAvg !== null && (
        <div
          className="rounded border p-3 text-center"
          style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
        >
          <div className="text-xs tracking-wider mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>NATIONAL AVERAGE</div>
          <div className="text-3xl font-bold" style={{ color: approvalColor(nationalAvg) }}>{nationalAvg}%</div>
          <div className="text-xs mt-1" style={{ color: "oklch(0.45 0.01 240)" }}>Approval Rating</div>
        </div>
      )}

      {/* Zone breakdown */}
      <div className="flex flex-col gap-2">
        {data.map(z => (
          <div
            key={z.code}
            className="rounded border p-2.5 flex items-center gap-3"
            style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
          >
            <div className="w-8 text-center flex-shrink-0">
              <div className="text-xs font-bold" style={{ color: "oklch(0.55 0.01 240)" }}>{z.code}</div>
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between mb-1">
                <span className="text-xs truncate" style={{ color: "oklch(0.72 0.01 240)" }}>{z.zone}</span>
                <div className="flex items-center gap-1.5 flex-shrink-0">
                  {z.trend === "up" && <TrendingUp className="w-3 h-3" style={{ color: "oklch(0.65 0.18 145)" }} />}
                  {z.trend === "down" && <TrendingDown className="w-3 h-3" style={{ color: "oklch(0.65 0.18 25)" }} />}
                  {z.trend === "flat" && <Minus className="w-3 h-3" style={{ color: "oklch(0.55 0.01 240)" }} />}
                  <span className="text-xs font-bold" style={{ color: approvalColor(z.approval) }}>{z.approval}%</span>
                  <span className="text-xs" style={{ color: z.delta > 0 ? "oklch(0.65 0.18 145)" : z.delta < 0 ? "oklch(0.65 0.18 25)" : "oklch(0.45 0.01 240)" }}>
                    {z.delta > 0 ? "+" : ""}{z.delta}
                  </span>
                </div>
              </div>
              {/* Approval bar */}
              <div className="h-1.5 rounded-full overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
                <div
                  className="h-full rounded-full transition-all duration-700"
                  style={{ width: `${z.approval}%`, background: approvalColor(z.approval) }}
                />
              </div>
              <div className="text-xs mt-1" style={{ color: "oklch(0.35 0.01 240)" }}>n={z.sampleSize.toLocaleString()}</div>
            </div>
          </div>
        ))}
      </div>

      {lastRefresh && (
        <div className="text-xs text-center" style={{ color: "oklch(0.35 0.01 240)" }}>
          Last updated {lastRefresh.toLocaleTimeString("en-NG")} · {autoRefresh ? "Auto-refresh every 90s" : "Manual refresh"}
        </div>
      )}
      </>
      )}
    </div>
  );
}
