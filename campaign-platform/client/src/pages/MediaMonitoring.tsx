/**
 * Media Monitoring Dashboard — DB-backed with tRPC
 * Real media mentions, sentiment tracking, and competitor coverage analysis.
 */
import { useState } from "react";
import { ArrowLeft, TrendingUp, TrendingDown, Minus, Radio, Newspaper, Tv, Globe, Plus, Trash2, Loader2, Database, FileText, Download} from "lucide-react";
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { exportToCSV, exportToPDF } from "@/hooks/useExport";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell, PieChart, Pie, Legend } from "recharts";

const SOURCES = ["Channels TV", "NTA", "AIT", "Punch", "Vanguard", "Guardian", "Arise TV", "TVC", "Daily Trust", "Premium Times", "Sahara Reporters", "Silverbird FM", "BBC Hausa", "VOA Hausa"];
const ZONES = ["South-West", "South-East", "South-South", "North-West", "North-East", "North-Central", "National"];
const SENTIMENT_COLORS: Record<string, string> = { positive: "#22c55e", neutral: "#94a3b8", negative: "#ef4444" };
const SOURCE_ICONS: Record<string, React.ReactNode> = {
  tv: <Tv className="w-3.5 h-3.5" />,
  broadcast: <Tv className="w-3.5 h-3.5" />,
  radio: <Radio className="w-3.5 h-3.5" />,
  print: <Newspaper className="w-3.5 h-3.5" />,
  online: <Globe className="w-3.5 h-3.5" />,
};

function SentimentIcon({ s }: { s: string }) {
  if (s === "positive") return <TrendingUp className="w-3.5 h-3.5 text-green-500" />;
  if (s === "negative") return <TrendingDown className="w-3.5 h-3.5 text-red-500" />;
  return <Minus className="w-3.5 h-3.5 text-gray-400" />;
}

const EXPORT_COLS_M = [
  { header: "Source", key: "source" },
  { header: "Type", key: "sourceType" },
  { header: "Sentiment", key: "sentiment" },
  { header: "Reach", key: "reach" },
  { header: "Zone", key: "zone" },
  { header: "Headline", key: "headline" },
];
export default function MediaMonitoring() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();

  const { data: mentions = [], isLoading } = trpc.media.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.media.add.useMutation({
    onSuccess: () => { utils.media.list.invalidate(); toast.success("Mention added"); setOpen(false); resetForm(); },
    onError: e => toast.error(e.message),
  });
  const deleteMut = trpc.media.delete.useMutation({
    onSuccess: () => { utils.media.list.invalidate(); toast.success("Mention removed"); },
    onError: e => toast.error(e.message),
  });
  const seedMut = trpc.seed.all.useMutation({
    onSuccess: () => { utils.media.list.invalidate(); toast.success("Sample media mentions seeded!"); },
    onError: e => toast.error(e.message),
  });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ source: "Channels TV", headline: "", sentiment: "positive", sourceType: "tv", reach: "", zone: "National" });
  const [sentimentFilter, setSentimentFilter] = useState<"all" | "positive" | "neutral" | "negative">("all");
  const [sourceFilter, setSourceFilter] = useState<string>("all");

  function resetForm() {
    setForm({ source: "Channels TV", headline: "", sentiment: "positive", sourceType: "tv", reach: "", zone: "National" });
  }

  const filtered = mentions.filter(m =>
    (sentimentFilter === "all" || m.sentiment === sentimentFilter) &&
    (sourceFilter === "all" || m.sourceType === sourceFilter)
  );

  const positive = mentions.filter(m => m.sentiment === "positive").length;
  const negative = mentions.filter(m => m.sentiment === "negative").length;
  const neutral = mentions.filter(m => m.sentiment === "neutral").length;
  const totalReach = mentions.reduce((s, m) => s + (m.reach ?? 0), 0);

  const sentimentData = [
    { name: "Positive", value: positive, fill: "#22c55e" },
    { name: "Neutral", value: neutral, fill: "#94a3b8" },
    { name: "Negative", value: negative, fill: "#ef4444" },
  ].filter(d => d.value > 0);

  const zoneData = Object.entries(
    mentions.reduce<Record<string, number>>((acc, m) => {
      const z = m.zone ?? "Unknown";
      acc[z] = (acc[z] ?? 0) + 1;
      return acc;
    }, {})
  ).map(([zone, count]) => ({ zone, count })).sort((a, b) => b.count - a.count);

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <Radio size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Media Monitoring</h1>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {mentions.length === 0 && (
            <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10"
              disabled={!profileId || seedMut.isPending}
              onClick={() => profileId && seedMut.mutate({ profileId })}>
              {seedMut.isPending ? <Loader2 size={13} className="animate-spin" /> : <Database size={13} />} Seed Sample Data
            </Button>
          )}
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToCSV("media-monitoring", EXPORT_COLS_M, (mentions ?? []) as Record<string, unknown>[])}><Download size={13}/> CSV</Button>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToPDF("media-monitoring", "Media Monitoring Report", `Total mentions: ${(mentions ?? []).length}`, EXPORT_COLS_M, (mentions ?? []) as Record<string, unknown>[])}><FileText size={13}/> PDF</Button>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5" disabled={!canEdit}>
                <Plus size={14} /> Add Mention
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Media Mention</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Select value={form.source} onValueChange={v => setForm(f => ({ ...f, source: v }))}>
                  <SelectTrigger><SelectValue placeholder="Source" /></SelectTrigger>
                  <SelectContent>{SOURCES.map(s => <SelectItem key={s} value={s}>{s}</SelectItem>)}</SelectContent>
                </Select>
                <Input placeholder="Headline *" value={form.headline} onChange={e => setForm(f => ({ ...f, headline: e.target.value }))} />
                <Select value={form.sentiment} onValueChange={v => setForm(f => ({ ...f, sentiment: v }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="positive">Positive</SelectItem>
                    <SelectItem value="neutral">Neutral</SelectItem>
                    <SelectItem value="negative">Negative</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={form.sourceType} onValueChange={v => setForm(f => ({ ...f, sourceType: v }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="tv">TV / Broadcast</SelectItem>
                    <SelectItem value="radio">Radio</SelectItem>
                    <SelectItem value="print">Print</SelectItem>
                    <SelectItem value="online">Online</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={form.zone} onValueChange={v => setForm(f => ({ ...f, zone: v }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>{ZONES.map(z => <SelectItem key={z} value={z}>{z}</SelectItem>)}</SelectContent>
                </Select>
                <Input type="number" placeholder="Estimated Reach (audience size)" value={form.reach} onChange={e => setForm(f => ({ ...f, reach: e.target.value }))} />
                <Button onClick={() => {
                  if (!profileId || !form.headline) return toast.error("Headline required");
                  addMut.mutate({ profileId, source: form.source, headline: form.headline, sentiment: form.sentiment, sourceType: form.sourceType, zone: form.zone, reach: form.reach ? parseInt(form.reach) : undefined });
                }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Add Mention"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>

      <div className="max-w-6xl mx-auto px-6 py-8">
        {/* KPI Row */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
          {[
            { label: "Total Mentions", value: mentions.length, color: "#4A1525" },
            { label: "Positive", value: positive, color: "#22c55e" },
            { label: "Negative", value: negative, color: "#ef4444" },
            { label: "Est. Total Reach", value: totalReach >= 1_000_000 ? `${(totalReach / 1_000_000).toFixed(1)}M` : totalReach >= 1000 ? `${(totalReach / 1000).toFixed(0)}K` : String(totalReach), color: "#1A3A5C" },
          ].map(k => (
            <div key={k.label} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: `3px solid ${k.color}` }}>
              <p className="text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1">{k.label}</p>
              <p className="font-mono text-2xl font-bold" style={{ color: k.color }}>{k.value}</p>
            </div>
          ))}
        </div>

        {/* Charts */}
        {mentions.length > 0 && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 mb-6">
            <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #4A1525" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Sentiment Breakdown</p>
              <ResponsiveContainer width="100%" height={200}>
                <PieChart>
                  <Pie data={sentimentData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={75}
                    label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`} labelLine={false}>
                    {sentimentData.map((d, i) => <Cell key={i} fill={d.fill} />)}
                  </Pie>
                  <Legend />
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #1A3A5C" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Mentions by Zone</p>
              <ResponsiveContainer width="100%" height={200}>
                <BarChart data={zoneData} margin={{ top: 5, right: 10, left: 0, bottom: 30 }}>
                  <XAxis dataKey="zone" tick={{ fontSize: 10 }} angle={-25} textAnchor="end" height={50} />
                  <YAxis tick={{ fontSize: 11 }} allowDecimals={false} />
                  <Tooltip />
                  <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                    {zoneData.map((_, i) => <Cell key={i} fill={["#4A1525", "#008751", "#1A3A5C", "#C0392B", "#F59E0B", "#6366F1", "#0891b2"][i % 7]} />)}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        )}

        {/* Filters */}
        <div className="flex gap-2 mb-4 flex-wrap items-center">
          {(["all", "positive", "neutral", "negative"] as const).map(s => (
            <button key={s} onClick={() => setSentimentFilter(s)}
              className={`px-3 py-1.5 text-xs font-semibold uppercase tracking-wide rounded-full transition-all ${sentimentFilter === s ? "text-white" : "bg-white text-gray-600 border border-gray-200"}`}
              style={sentimentFilter === s ? { background: s === "all" ? "#4A1525" : SENTIMENT_COLORS[s] } : {}}>
              {s}
            </button>
          ))}
          <div className="ml-auto">
            <Select value={sourceFilter} onValueChange={v => setSourceFilter(v)}>
              <SelectTrigger className="h-8 text-xs w-40"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Sources</SelectItem>
                <SelectItem value="tv">TV / Broadcast</SelectItem>
                <SelectItem value="radio">Radio</SelectItem>
                <SelectItem value="print">Print</SelectItem>
                <SelectItem value="online">Online</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {/* Mentions Feed */}
        {isLoading ? (
          <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
        ) : filtered.length === 0 ? (
          <div className="text-center py-20 text-gray-500">
            <Radio size={48} className="mx-auto mb-4 opacity-30" />
            <p className="font-semibold mb-2">No media mentions yet</p>
            <p className="text-sm">Add mentions manually or use "Seed Sample Data" to populate with realistic Nigerian media coverage.</p>
          </div>
        ) : (
          <div className="space-y-3">
            {filtered.map(m => (
              <div key={m.id} className="bg-white border border-gray-200 rounded p-4 flex items-start gap-3"
                style={{ borderLeft: `3px solid ${SENTIMENT_COLORS[m.sentiment ?? "neutral"]}` }}>
                <div className="mt-0.5 flex-shrink-0 text-gray-500">{SOURCE_ICONS[m.sourceType ?? "online"] ?? <Globe className="w-3.5 h-3.5" />}</div>
                <div className="flex-1 min-w-0">
                  <p className="font-semibold text-gray-900 text-sm leading-snug">{m.headline}</p>
                  <div className="flex items-center gap-3 mt-1 text-xs text-gray-500 flex-wrap">
                    <span className="font-medium text-gray-700">{m.source}</span>
                    {m.zone && <span>{m.zone}</span>}
                    {m.reach && <span>{m.reach >= 1_000_000 ? `${(m.reach / 1_000_000).toFixed(1)}M reach` : `${(m.reach / 1000).toFixed(0)}K reach`}</span>}
                    <span>{new Date(m.createdAt ?? Date.now()).toLocaleDateString("en-NG", { day: "numeric", month: "short" })}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  <SentimentIcon s={m.sentiment ?? "neutral"} />
                  {canDelete && (
                    <button onClick={() => deleteMut.mutate({ id: m.id })} className="text-gray-300 hover:text-red-500 transition-colors">
                      <Trash2 size={14} />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
