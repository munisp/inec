/**
 * Media Monitoring Dashboard
 * Real-time media mentions, sentiment tracking, and competitor coverage analysis.
 */
import { useState, useEffect, useRef } from "react";
import { ArrowLeft, TrendingUp, TrendingDown, Minus, Radio, Newspaper, Tv, Globe, RefreshCw, AlertCircle } from "lucide-react";
import { Link } from "wouter";
import { BarChart, Bar, LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell, PieChart, Pie, Legend } from "recharts";

interface Mention {
  id: string;
  source: string;
  sourceType: "tv" | "radio" | "print" | "online";
  headline: string;
  sentiment: "positive" | "neutral" | "negative";
  reach: number;
  time: string;
  zone: string;
}

const SOURCES = ["Channels TV", "NTA", "AIT", "Punch", "Vanguard", "Guardian", "Arise TV", "TVC", "Daily Trust", "Premium Times", "Sahara Reporters", "Silverbird FM"];
const SOURCE_TYPES: Record<string, "tv" | "radio" | "print" | "online"> = {
  "Channels TV": "tv", "NTA": "tv", "AIT": "tv", "Arise TV": "tv", "TVC": "tv", "Silverbird FM": "radio",
  "Punch": "print", "Vanguard": "print", "Guardian": "print", "Daily Trust": "print",
  "Premium Times": "online", "Sahara Reporters": "online",
};
const ZONES = ["South-West", "South-East", "South-South", "North-West", "North-East", "North-Central"];
const HEADLINES = [
  "Candidate pledges infrastructure overhaul for Lagos",
  "Campaign rally draws thousands in Ibadan",
  "Opponent questions candidate's economic record",
  "Youth groups endorse candidate's education plan",
  "Candidate visits flood-affected communities",
  "New poll shows candidate leading by 8 points",
  "Campaign team denies vote-buying allegations",
  "Candidate signs women's empowerment charter",
  "Debate performance praised by political analysts",
  "Grassroots mobilisation intensifies ahead of election",
];

function randomMention(id: number): Mention {
  const source = SOURCES[Math.floor(Math.random() * SOURCES.length)];
  const sentiments: Mention["sentiment"][] = ["positive", "positive", "positive", "neutral", "neutral", "negative"];
  return {
    id: String(id),
    source,
    sourceType: SOURCE_TYPES[source],
    headline: HEADLINES[Math.floor(Math.random() * HEADLINES.length)],
    sentiment: sentiments[Math.floor(Math.random() * sentiments.length)],
    reach: Math.floor(Math.random() * 2000000) + 50000,
    time: new Date(Date.now() - Math.floor(Math.random() * 3600000)).toLocaleTimeString("en-NG", { hour: "2-digit", minute: "2-digit" }),
    zone: ZONES[Math.floor(Math.random() * ZONES.length)],
  };
}

const INITIAL_MENTIONS: Mention[] = Array.from({ length: 18 }, (_, i) => randomMention(i));

const SENTIMENT_COLORS = { positive: "#22c55e", neutral: "#94a3b8", negative: "#ef4444" };
const SOURCE_ICONS = { tv: <Tv className="w-3.5 h-3.5" />, radio: <Radio className="w-3.5 h-3.5" />, print: <Newspaper className="w-3.5 h-3.5" />, online: <Globe className="w-3.5 h-3.5" /> };

export default function MediaMonitoring() {
  const [mentions, setMentions] = useState<Mention[]>(INITIAL_MENTIONS);
  const [filter, setFilter] = useState<"all" | "positive" | "neutral" | "negative">("all");
  const [sourceFilter, setSourceFilter] = useState<"all" | "tv" | "radio" | "print" | "online">("all");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const counterRef = useRef(18);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => {
      counterRef.current++;
      setMentions(prev => [randomMention(counterRef.current), ...prev.slice(0, 29)]);
    }, 4000);
    return () => clearInterval(interval);
  }, [autoRefresh]);

  const filtered = mentions.filter(m =>
    (filter === "all" || m.sentiment === filter) &&
    (sourceFilter === "all" || m.sourceType === sourceFilter)
  );

  const positive = mentions.filter(m => m.sentiment === "positive").length;
  const neutral = mentions.filter(m => m.sentiment === "neutral").length;
  const negative = mentions.filter(m => m.sentiment === "negative").length;
  const total = mentions.length;
  const sentimentScore = Math.round(((positive - negative) / total) * 100);

  const byZone = ZONES.map(z => ({
    zone: z.split("-")[0],
    positive: mentions.filter(m => m.zone === z && m.sentiment === "positive").length,
    negative: mentions.filter(m => m.zone === z && m.sentiment === "negative").length,
    neutral: mentions.filter(m => m.zone === z && m.sentiment === "neutral").length,
  }));

  const pieData = [
    { name: "Positive", value: positive, color: "#22c55e" },
    { name: "Neutral", value: neutral, color: "#94a3b8" },
    { name: "Negative", value: negative, color: "#ef4444" },
  ];

  const totalReach = mentions.reduce((s, m) => s + m.reach, 0);

  return (
    <div className="min-h-screen" style={{ background: "#0d1117", fontFamily: "'IBM Plex Mono', monospace", color: "#e2e8f0" }}>
      {/* Header */}
      <div className="border-b px-6 py-4 flex items-center justify-between" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
        <div className="flex items-center gap-4">
          <Link href="/stakeholders">
            <button className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}>
              <ArrowLeft className="w-3.5 h-3.5" /> Back
            </button>
          </Link>
          <div>
            <div className="text-xs tracking-widest uppercase mb-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>INEC Campaign Intelligence</div>
            <div className="font-bold text-sm">Media Monitoring Dashboard</div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5 text-xs" style={{ color: autoRefresh ? "oklch(0.65 0.18 145)" : "oklch(0.45 0.01 240)" }}>
            <div className="w-2 h-2 rounded-full animate-pulse" style={{ background: autoRefresh ? "oklch(0.65 0.18 145)" : "oklch(0.35 0.01 240)" }} />
            {autoRefresh ? "LIVE" : "PAUSED"}
          </div>
          <button onClick={() => setAutoRefresh(a => !a)} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border transition-all" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.55 0.01 240)" }}>
            <RefreshCw className="w-3.5 h-3.5" /> {autoRefresh ? "Pause" : "Resume"}
          </button>
        </div>
      </div>

      <div className="p-6 space-y-6">
        {/* KPI row */}
        <div className="grid grid-cols-2 gap-4" style={{ gridTemplateColumns: "repeat(4, 1fr)" }}>
          {[
            { label: "Total Mentions", value: total, color: "oklch(0.65 0.18 200)", icon: <Globe className="w-4 h-4" /> },
            { label: "Positive", value: positive, color: "#22c55e", icon: <TrendingUp className="w-4 h-4" /> },
            { label: "Negative", value: negative, color: "#ef4444", icon: <TrendingDown className="w-4 h-4" /> },
            { label: "Sentiment Score", value: `${sentimentScore > 0 ? "+" : ""}${sentimentScore}`, color: sentimentScore > 0 ? "#22c55e" : sentimentScore < 0 ? "#ef4444" : "#94a3b8", icon: <Minus className="w-4 h-4" /> },
          ].map(kpi => (
            <div key={kpi.label} className="rounded-xl border p-4" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs tracking-widest uppercase" style={{ color: "oklch(0.45 0.01 240)" }}>{kpi.label}</span>
                <span style={{ color: kpi.color }}>{kpi.icon}</span>
              </div>
              <div className="text-2xl font-bold" style={{ color: kpi.color }}>{kpi.value}</div>
            </div>
          ))}
        </div>

        {/* Reach KPI */}
        <div className="rounded-xl border p-4 flex items-center justify-between" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
          <span className="text-xs tracking-widest uppercase" style={{ color: "oklch(0.45 0.01 240)" }}>Total Audience Reach</span>
          <span className="text-xl font-bold" style={{ color: "oklch(0.65 0.18 145)" }}>{(totalReach / 1000000).toFixed(1)}M</span>
        </div>

        {/* Charts row */}
        <div className="grid gap-4" style={{ gridTemplateColumns: "1fr 1fr" }}>
          <div className="rounded-xl border p-4" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
            <div className="text-xs font-bold tracking-widest uppercase mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>Sentiment by Zone</div>
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={byZone} barSize={12}>
                <XAxis dataKey="zone" tick={{ fontSize: 10, fill: "oklch(0.45 0.01 240)" }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 10, fill: "oklch(0.45 0.01 240)" }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={{ background: "oklch(0.16 0.008 240)", border: "1px solid oklch(0.28 0.01 240)", borderRadius: "8px", fontSize: "11px" }} />
                <Bar dataKey="positive" fill="#22c55e" radius={[2, 2, 0, 0]} />
                <Bar dataKey="neutral" fill="#94a3b8" radius={[2, 2, 0, 0]} />
                <Bar dataKey="negative" fill="#ef4444" radius={[2, 2, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
          <div className="rounded-xl border p-4" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
            <div className="text-xs font-bold tracking-widest uppercase mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>Sentiment Distribution</div>
            <ResponsiveContainer width="100%" height={200}>
              <PieChart>
                <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={75} label={({ name, percent }) => `${name} ${Math.round(percent * 100)}%`} labelLine={false} style={{ fontSize: "10px" }}>
                  {pieData.map((entry, i) => <Cell key={i} fill={entry.color} />)}
                </Pie>
                <Tooltip contentStyle={{ background: "oklch(0.16 0.008 240)", border: "1px solid oklch(0.28 0.01 240)", borderRadius: "8px", fontSize: "11px" }} />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Filters + Feed */}
        <div className="rounded-xl border overflow-hidden" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.14 0.008 240)" }}>
          <div className="px-4 py-3 border-b flex items-center gap-3 flex-wrap" style={{ borderColor: "oklch(0.20 0.01 240)" }}>
            <span className="text-xs font-bold tracking-widest uppercase" style={{ color: "oklch(0.55 0.01 240)" }}>Live Feed</span>
            <div className="flex gap-1.5 flex-wrap">
              {(["all", "positive", "neutral", "negative"] as const).map(s => (
                <button key={s} onClick={() => setFilter(s)} className="text-xs px-2.5 py-1 rounded-full border capitalize transition-all" style={{ borderColor: filter === s ? SENTIMENT_COLORS[s as keyof typeof SENTIMENT_COLORS] || "oklch(0.55 0.18 200)" : "oklch(0.28 0.01 240)", color: filter === s ? SENTIMENT_COLORS[s as keyof typeof SENTIMENT_COLORS] || "oklch(0.65 0.18 200)" : "oklch(0.50 0.01 240)", background: filter === s ? (SENTIMENT_COLORS[s as keyof typeof SENTIMENT_COLORS] || "oklch(0.55 0.18 200)") + "22" : "transparent" }}>{s}</button>
              ))}
            </div>
            <div className="flex gap-1.5 flex-wrap ml-auto">
              {(["all", "tv", "radio", "print", "online"] as const).map(s => (
                <button key={s} onClick={() => setSourceFilter(s)} className="text-xs px-2.5 py-1 rounded-full border capitalize transition-all" style={{ borderColor: sourceFilter === s ? "oklch(0.55 0.18 200)" : "oklch(0.28 0.01 240)", color: sourceFilter === s ? "oklch(0.65 0.18 200)" : "oklch(0.50 0.01 240)", background: sourceFilter === s ? "oklch(0.16 0.04 200)" : "transparent" }}>{s}</button>
              ))}
            </div>
          </div>
          <div className="divide-y max-h-96 overflow-y-auto" style={{ borderColor: "oklch(0.18 0.008 240)" }}>
            {filtered.map(m => (
              <div key={m.id} className="px-4 py-3 flex items-start gap-3 hover:bg-white/5 transition-colors">
                <div className="flex-shrink-0 mt-0.5" style={{ color: "oklch(0.50 0.01 240)" }}>
                  {SOURCE_ICONS[m.sourceType]}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-bold mb-0.5" style={{ color: "oklch(0.80 0.01 240)" }}>{m.headline}</div>
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>{m.source}</span>
                    <span className="text-xs" style={{ color: "oklch(0.40 0.01 240)" }}>·</span>
                    <span className="text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>{m.zone}</span>
                    <span className="text-xs" style={{ color: "oklch(0.40 0.01 240)" }}>·</span>
                    <span className="text-xs" style={{ color: "oklch(0.50 0.01 240)" }}>{(m.reach / 1000).toFixed(0)}K reach</span>
                  </div>
                </div>
                <div className="flex-shrink-0 flex items-center gap-2">
                  <span className="text-xs" style={{ color: "oklch(0.40 0.01 240)" }}>{m.time}</span>
                  <div className="w-2 h-2 rounded-full" style={{ background: SENTIMENT_COLORS[m.sentiment] }} title={m.sentiment} />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

