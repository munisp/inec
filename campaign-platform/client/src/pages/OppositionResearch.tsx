import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Search, Plus, Loader2, Sparkles, ChevronDown, ChevronUp } from "lucide-react";

const THREAT_COLORS: Record<string, string> = {
  low: "#008751", medium: "#F59E0B", high: "#C0392B", critical: "#4A1525",
};

function OpponentCard({ entry }: { entry: any }) {
  const [analysis, setAnalysis] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const aiMut = trpc.opposition.aiAnalyze.useMutation({
    onSuccess: d => { setAnalysis(d.analysis); setExpanded(true); },
    onError: e => toast.error(e.message),
  });

  return (
    <div className="bg-white border border-gray-200 rounded p-5"
      style={{ borderTop: `3px solid ${THREAT_COLORS[entry.threatLevel ?? "medium"]}` }}>
      <div className="flex items-start justify-between mb-3">
        <div>
          <p className="font-bold text-gray-900 text-lg">{entry.opponentName}</p>
          {entry.party && <p className="text-sm text-gray-500">{entry.party}</p>}
        </div>
        <Badge style={{ background: THREAT_COLORS[entry.threatLevel ?? "medium"] + "22", color: THREAT_COLORS[entry.threatLevel ?? "medium"] }}>
          {entry.threatLevel?.toUpperCase()} THREAT
        </Badge>
      </div>
      {entry.strength && (
        <div className="mb-2">
          <p className="text-xs font-bold text-green-700 uppercase tracking-wider mb-1">Strengths</p>
          <p className="text-sm text-gray-700">{entry.strength}</p>
        </div>
      )}
      {entry.weakness && (
        <div className="mb-2">
          <p className="text-xs font-bold text-red-700 uppercase tracking-wider mb-1">Weaknesses</p>
          <p className="text-sm text-gray-700">{entry.weakness}</p>
        </div>
      )}
      {entry.notes && <p className="text-xs text-gray-500 mt-2 italic border-t pt-2">{entry.notes}</p>}

      {/* AI Analysis */}
      <div className="mt-3 pt-3 border-t border-gray-100">
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline"
            onClick={() => {
              if (analysis) { setExpanded(v => !v); return; }
              aiMut.mutate({
                opponentName: entry.opponentName,
                party: entry.party ?? undefined,
                strength: entry.strength ?? undefined,
                weakness: entry.weakness ?? undefined,
                notes: entry.notes ?? undefined,
                threatLevel: entry.threatLevel ?? undefined,
              });
            }}
            disabled={aiMut.isPending}
            className="gap-1.5 text-xs"
            style={{ borderColor: "#4A1525", color: "#4A1525" }}>
            {aiMut.isPending
              ? <><Loader2 size={11} className="animate-spin" /> Analysing…</>
              : analysis
                ? <>{expanded ? <ChevronUp size={11} /> : <ChevronDown size={11} />} {expanded ? "Hide" : "Show"} AI Strategy</>
                : <><Sparkles size={11} /> AI Counter-Strategy</>}
          </Button>
        </div>
        {analysis && expanded && (
          <div className="mt-3 p-3 rounded text-sm text-gray-700 leading-relaxed whitespace-pre-wrap"
            style={{ background: "#F5F0EB", borderLeft: "3px solid #4A1525" }}>
            {analysis}
          </div>
        )}
      </div>
    </div>
  );
}

export default function OppositionResearch() {
  const { profileId, canEdit } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: entries = [], isLoading } = trpc.opposition.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const upsertMut = trpc.opposition.upsert.useMutation({
    onSuccess: () => { utils.opposition.list.invalidate(); toast.success("Entry saved"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    opponentName: "", party: "", strength: "", weakness: "",
    threatLevel: "medium" as "low" | "medium" | "high" | "critical", notes: "",
  });

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <Search size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Opposition Research</h1>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5">
              <Plus size={14} /> Add Opponent
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>Add Opponent Dossier</DialogTitle></DialogHeader>
            <div className="grid gap-3 py-2">
              <Input placeholder="Opponent Name *" value={form.opponentName} onChange={e => setForm(f => ({ ...f, opponentName: e.target.value }))} />
              <Input placeholder="Party" value={form.party} onChange={e => setForm(f => ({ ...f, party: e.target.value }))} />
              <Input placeholder="Key Strengths" value={form.strength} onChange={e => setForm(f => ({ ...f, strength: e.target.value }))} />
              <Input placeholder="Key Weaknesses" value={form.weakness} onChange={e => setForm(f => ({ ...f, weakness: e.target.value }))} />
              <Select value={form.threatLevel} onValueChange={v => setForm(f => ({ ...f, threatLevel: v as any }))}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {["low", "medium", "high", "critical"].map(t => (
                    <SelectItem key={t} value={t}>{t.charAt(0).toUpperCase() + t.slice(1)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input placeholder="Notes" value={form.notes} onChange={e => setForm(f => ({ ...f, notes: e.target.value }))} />
              <Button
                onClick={() => { if (!profileId || !form.opponentName) return toast.error("Name required"); upsertMut.mutate({ profileId, ...form }); }}
                disabled={upsertMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                {upsertMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Save"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8">
        {isLoading
          ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
          : entries.length === 0
            ? (
              <div className="text-center py-20 text-gray-500">
                <Search size={48} className="mx-auto mb-4 opacity-30" />
                <p>No opposition entries yet. Add an opponent to begin AI-powered strategic analysis.</p>
              </div>
            )
            : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {entries.map(e => <OpponentCard key={e.id} entry={e} />)}
              </div>
            )}
      </div>
    </div>
  );
}
