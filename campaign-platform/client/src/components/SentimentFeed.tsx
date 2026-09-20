/**
 * Real-Time Sentiment Feed
 * Approval trend panel by geopolitical zone, backed by the campaign-planning
 * FastAPI service: POST {VITE_CAMPAIGN_API_URL}/api/v1/campaign/sentiment
 *   body:   { candidate_id: string, period: "7d" | "30d" | "90d" }
 *   result: { items_analysed, by_sentiment, reach_by_sentiment,
 *             by_zone: { zone: { label: count } }, by_source_type, note,
 *             computed_at }
 * Positive share per zone = positive / (positive + negative + neutral).
 * Zones with no labelled items are shown as "insufficient data" — a number is
 * never invented. If no backend is configured, the fetch fails, or
 * items_analysed is 0, an explicit "unavailable" state is shown.
 */
import { useState, useEffect, useCallback, useRef } from "react";
import { TrendingUp, TrendingDown, Minus, Radio, RefreshCw, AlertTriangle } from "lucide-react";

interface ZoneSentiment {
  zone: string;
  code: string;
  approval: number | null; // null = insufficient labelled data
  delta: number;   // change from last successful poll (0 on first poll)
  trend: "up" | "down" | "flat";
  sampleSize: number;
}

interface SentimentApiResponse {
  items_analysed: number;
  by_sentiment: Record<string, number>;
  reach_by_sentiment: Record<string, number>;
  by_zone: Record<string, Record<string, number>>;
  by_source_type: Record<string, Record<string, number>>;
  note?: string;
  computed_at?: string;
}

const ZONE_CODES: Record<string, string> = {
  "south-west": "SW",
  "south-east": "SE",
  "south-south": "SS",
  "north-west": "NW",
  "north-east": "NE",
  "north-central": "NC",
};

function zoneCode(zone: string): string {
  const key = zone.trim().toLowerCase();
  if (ZONE_CODES[key]) return ZONE_CODES[key];
  // Fallback: initials of the zone name (e.g. "unspecified" -> "UN")
  const initials = key.split(/[\s-]+/).map(w => w[0] ?? "").join("").toUpperCase();
  return initials.slice(0, 2) || "??";
}

function approvalColor(pct: number): string {
  if (pct >= 60) return "oklch(0.65 0.18 145)";  // green
  if (pct >= 45) return "oklch(0.75 0.18 80)";   // amber
  return "oklch(0.65 0.18 25)";                   // red
}

const INSUFFICIENT_COLOR = "oklch(0.45 0.01 240)";

interface Props {
  profileId: number; // numeric campaign profile id — sent as candidate_id
  compact?: boolean;
}

export default function SentimentFeed({ profileId, compact = false }: Props) {
  const [data, setData] = useState<ZoneSentiment[]>([]);
  const [loading, setLoading] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // Approvals from the previous successful poll, for delta/trend computation.
  const prevApprovalsRef = useRef<Record<string, number | null>>({});

  const fetchSentiment = useCallback(async () => {
    setLoading(true);
    try {
      const backendUrl = import.meta.env.VITE_CAMPAIGN_API_URL ?? "";
      if (!backendUrl) {
        setData([]);
        setUnavailable(true);
        setLastRefresh(null);
        setLoading(false);
        return;
      }
      const res = await fetch(`${backendUrl}/api/v1/campaign/sentiment`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ candidate_id: String(profileId), period: "30d" }),
        signal: AbortSignal.timeout(5000),
      });
      if (!res.ok) throw new Error(`sentiment endpoint returned ${res.status}`);
      const json = (await res.json()) as SentimentApiResponse;
      if (!json || typeof json.items_analysed !== "number" || json.items_analysed === 0 || !json.by_zone) {
        // Backend reachable but no items analysed — honest unavailable state.
        setData([]);
        setUnavailable(true);
        setLastRefresh(null);
        setLoading(false);
        return;
      }
      const prev = prevApprovalsRef.current;
      const next: Record<string, number | null> = {};
      const zones: ZoneSentiment[] = Object.entries(json.by_zone).map(([zone, labels]) => {
        const counts = labels ?? {};
        const sampleSize = Object.values(counts).reduce((s, n) => s + (typeof n === "number" ? n : 0), 0);
        const labelled = (counts["positive"] ?? 0) + (counts["negative"] ?? 0) + (counts["neutral"] ?? 0);
        const approval = labelled > 0 ? Math.round(((counts["positive"] ?? 0) / labelled) * 100) : null;
        next[zone] = approval;
        const prevApproval = prev[zone];
        const delta = approval !== null && prevApproval !== null && prevApproval !== undefined
          ? approval - prevApproval
          : 0;
        return {
          zone,
          code: zoneCode(zone),
          approval,
          delta,
          trend: delta > 0 ? "up" as const : delta < 0 ? "down" as const : "flat" as const,
          sampleSize,
        };
      }).sort((a, b) => b.sampleSize - a.sampleSize);
      prevApprovalsRef.current = next;
      setData(zones);
      setUnavailable(false);
      setLastRefresh(new Date());
      setLoading(false);
    } catch {
      // Backend unavailable — show the explicit unavailable state below.
      setData([]);
      setUnavailable(true);
      setLastRefresh(null);
      setLoading(false);
    }
  }, [profileId]);

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

  const scored = data.filter(z => z.approval !== null);
  const nationalAvg = scored.length > 0
    ? Math.round(scored.reduce((s, z) => s + (z.approval ?? 0), 0) / scored.length)
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
            <div key={z.zone} className="flex items-center gap-1 flex-shrink-0">
              <span className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{z.code}</span>
              {z.approval === null ? (
                <span className="text-xs" style={{ color: INSUFFICIENT_COLOR }} title="Insufficient labelled data">n/a</span>
              ) : (
                <>
                  <span className="text-xs font-bold" style={{ color: approvalColor(z.approval) }}>{z.approval}%</span>
                  {z.trend === "up" && <TrendingUp className="w-3 h-3" style={{ color: "oklch(0.65 0.18 145)" }} />}
                  {z.trend === "down" && <TrendingDown className="w-3 h-3" style={{ color: "oklch(0.65 0.18 25)" }} />}
                  {z.trend === "flat" && <Minus className="w-3 h-3" style={{ color: "oklch(0.55 0.01 240)" }} />}
                </>
              )}
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
            key={z.zone}
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
                  {z.approval === null ? (
                    <span className="text-xs" style={{ color: INSUFFICIENT_COLOR }}>insufficient data</span>
                  ) : (
                    <>
                      {z.trend === "up" && <TrendingUp className="w-3 h-3" style={{ color: "oklch(0.65 0.18 145)" }} />}
                      {z.trend === "down" && <TrendingDown className="w-3 h-3" style={{ color: "oklch(0.65 0.18 25)" }} />}
                      {z.trend === "flat" && <Minus className="w-3 h-3" style={{ color: "oklch(0.55 0.01 240)" }} />}
                      <span className="text-xs font-bold" style={{ color: approvalColor(z.approval) }}>{z.approval}%</span>
                      <span className="text-xs" style={{ color: z.delta > 0 ? "oklch(0.65 0.18 145)" : z.delta < 0 ? "oklch(0.65 0.18 25)" : "oklch(0.45 0.01 240)" }}>
                        {z.delta > 0 ? "+" : ""}{z.delta}
                      </span>
                    </>
                  )}
                </div>
              </div>
              {/* Approval bar */}
              <div className="h-1.5 rounded-full overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
                <div
                  className="h-full rounded-full transition-all duration-700"
                  style={{ width: `${z.approval ?? 0}%`, background: z.approval === null ? INSUFFICIENT_COLOR : approvalColor(z.approval) }}
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
