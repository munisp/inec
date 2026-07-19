/**
 * Public-facing Endorsement Tracker
 * Uses tRPC DB-backed endorsements data
 * Palette: #4A1525 (burgundy), #008751 (green), #1A3A5C (navy), #F5F0EB (paper)
 */
import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { Link } from "wouter";
import { Share2, Copy, CheckCheck, Trophy, Users, MapPin, BadgeCheck, Plus, Loader2, ArrowLeft } from "lucide-react";

const CATEGORY_COLORS: Record<string, string> = {
  "Traditional Leaders": "#b45309",
  "Youth Groups": "#0891b2",
  "Women Associations": "#be185d",
  "Religious Bodies": "#7c3aed",
  "Labour": "#dc2626",
  "Professional Bodies": "#1d4ed8",
  "Civil Society": "#059669",
  "Agriculture": "#65a30d",
  "Commerce": "#d97706",
};
function getColor(category: string): string {
  return CATEGORY_COLORS[category] ?? "#374151";
}

export default function EndorsementTracker() {
  const { profileId, profile, canEdit } = useCandidateProfile();
  const utils = trpc.useUtils();

  const { data: endorsements = [], isLoading } = trpc.endorsements.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.endorsements.add.useMutation({
    onSuccess: () => {
      utils.endorsements.list.invalidate();
      toast.success("Endorsement added");
      setOpen(false);
      setForm({ endorserName: "", title: "", organization: "", category: "Traditional Leaders", statement: "" });
    },
    onError: e => toast.error(e.message),
  });

  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("All");
  const [copied, setCopied] = useState(false);
  const [form, setForm] = useState({ endorserName: "", title: "", organization: "", category: "Traditional Leaders", statement: "" });

  const categories = ["All", ...Array.from(new Set(endorsements.map(e => e.category ?? "Other")))];
  const filtered = filter === "All" ? endorsements : endorsements.filter(e => e.category === filter);

  const shareUrl = typeof window !== "undefined" ? window.location.href : "";
  function copyLink() {
    navigator.clipboard.writeText(shareUrl).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
      toast.success("Link copied!");
    });
  }

  const categoryStats = endorsements.reduce<Record<string, number>>((acc, e) => {
    const cat = e.category ?? "Other";
    acc[cat] = (acc[cat] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <BadgeCheck size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Endorsement Tracker</h1>
        </div>
        <div className="flex items-center gap-4 flex-wrap">
          <div className="text-right"><p className="text-xs text-white/60">ENDORSEMENTS</p><p className="font-mono font-bold text-white">{endorsements.length}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">CATEGORIES</p><p className="font-mono font-bold text-white">{Object.keys(categoryStats).length}</p></div>
          <button onClick={copyLink} className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-white rounded border border-white/30 hover:bg-white/10 transition-colors">
            {copied ? <><CheckCheck size={12}/> Copied!</> : <><Copy size={12}/> Share Link</>}
          </button>
          {canEdit && (
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Endorsement</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader><DialogTitle>Add Endorsement</DialogTitle></DialogHeader>
                <div className="grid gap-3 py-2">
                  <Input placeholder="Endorser Name *" value={form.endorserName} onChange={e => setForm(f => ({ ...f, endorserName: e.target.value }))}/>
                  <Input placeholder="Title / Role" value={form.title} onChange={e => setForm(f => ({ ...f, title: e.target.value }))}/>
                  <Input placeholder="Organization" value={form.organization} onChange={e => setForm(f => ({ ...f, organization: e.target.value }))}/>
                  <select className="border border-gray-200 rounded px-3 py-2 text-sm" value={form.category} onChange={e => setForm(f => ({ ...f, category: e.target.value }))}>
                    {Object.keys(CATEGORY_COLORS).map(c => <option key={c} value={c}>{c}</option>)}
                  </select>
                  <textarea className="border border-gray-200 rounded px-3 py-2 text-sm resize-none" rows={3} placeholder="Endorsement statement" value={form.statement} onChange={e => setForm(f => ({ ...f, statement: e.target.value }))}/>
                  <Button onClick={() => {
                    if (!profileId || !form.endorserName) return toast.error("Endorser name required");
                    addMut.mutate({ profileId, endorserName: form.endorserName, title: form.title || undefined, organization: form.organization || undefined, category: form.category || undefined, statement: form.statement || undefined });
                  }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                    {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Add Endorsement"}
                  </Button>
                </div>
              </DialogContent>
            </Dialog>
          )}
        </div>
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8">
        {/* Stats row */}
        <div className="grid grid-cols-4 gap-4 mb-6">
          {[
            { label: "Total Endorsements", value: endorsements.length, icon: Trophy, color: "#4A1525" },
            { label: "Public Endorsements", value: endorsements.filter(e => e.isPublic !== false).length, icon: Share2, color: "#008751" },
            { label: "Categories Covered", value: Object.keys(categoryStats).length, icon: Users, color: "#1A3A5C" },
            { label: "Latest", value: endorsements.length > 0 ? new Date(endorsements[endorsements.length - 1].endorsedAt).toLocaleDateString("en-NG", { month: "short", day: "numeric" }) : "—", icon: MapPin, color: "#C0392B" },
          ].map(s => (
            <div key={s.label} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: `3px solid ${s.color}` }}>
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1">{s.label}</p>
                  <p className="font-mono text-2xl font-bold" style={{ color: s.color }}>{s.value}</p>
                </div>
                <s.icon size={18} style={{ color: s.color }} className="mt-1 opacity-60"/>
              </div>
            </div>
          ))}
        </div>

        {/* Category filter */}
        <div className="flex gap-2 flex-wrap mb-4">
          {categories.map(cat => (
            <button key={cat} onClick={() => setFilter(cat)}
              className={`px-3 py-1 text-xs font-semibold rounded transition-all ${filter === cat ? "text-white" : "bg-white text-gray-600 border border-gray-200 hover:bg-gray-50"}`}
              style={filter === cat ? { background: cat === "All" ? "#4A1525" : getColor(cat) } : {}}>
              {cat} {cat !== "All" && `(${categoryStats[cat] ?? 0})`}
            </button>
          ))}
        </div>

        {/* Endorsement cards */}
        {isLoading ? (
          <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        ) : filtered.length === 0 ? (
          <div className="text-center py-20 text-gray-500">
            <BadgeCheck size={48} className="mx-auto mb-4 opacity-30"/>
            <p className="font-semibold">No endorsements yet</p>
            <p className="text-sm mt-1">Add endorsements from the button above, or seed sample data from the Home page.</p>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-4">
            {filtered.map(e => {
              const color = getColor(e.category ?? "Other");
              return (
                <div key={e.id} className="bg-white border border-gray-200 rounded p-4" style={{ borderLeft: `3px solid ${color}` }}>
                  <div className="flex items-start justify-between mb-2">
                    <div>
                      <p className="font-bold text-gray-900">{e.endorserName}</p>
                      {e.title && <p className="text-xs text-gray-500">{e.title}</p>}
                      {e.organization && <p className="text-xs text-gray-400">{e.organization}</p>}
                    </div>
                    <Badge style={{ background: color + "22", color }}>{e.category ?? "Other"}</Badge>
                  </div>
                  {e.statement && (
                    <p className="text-sm text-gray-600 italic border-l-2 pl-3 mt-2" style={{ borderColor: color + "66" }}>
                      "{e.statement}"
                    </p>
                  )}
                  <p className="text-xs text-gray-400 mt-2">{new Date(e.endorsedAt).toLocaleDateString("en-NG", { day: "numeric", month: "short", year: "numeric" })}</p>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
