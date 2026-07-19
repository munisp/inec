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
import { ArrowLeft, Search, Plus, Loader2 } from "lucide-react";

const THREAT_COLORS: Record<string,string> = { low:"#008751", medium:"#F59E0B", high:"#C0392B", critical:"#4A1525" };

export default function OppositionResearch() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: entries = [], isLoading } = trpc.opposition.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const upsertMut = trpc.opposition.upsert.useMutation({
    onSuccess: () => { utils.opposition.list.invalidate(); toast.success("Entry saved"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ opponentName:"", party:"", strength:"", weakness:"", threatLevel:"medium" as "low"|"medium"|"high"|"critical", notes:"" });

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Search size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Opposition Research</h1>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Opponent</Button></DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>Add Opponent Dossier</DialogTitle></DialogHeader>
            <div className="grid gap-3 py-2">
              <Input placeholder="Opponent Name *" value={form.opponentName} onChange={e=>setForm(f=>({...f,opponentName:e.target.value}))}/>
              <Input placeholder="Party" value={form.party} onChange={e=>setForm(f=>({...f,party:e.target.value}))}/>
              <Input placeholder="Key Strengths" value={form.strength} onChange={e=>setForm(f=>({...f,strength:e.target.value}))}/>
              <Input placeholder="Key Weaknesses" value={form.weakness} onChange={e=>setForm(f=>({...f,weakness:e.target.value}))}/>
              <Select value={form.threatLevel} onValueChange={v=>setForm(f=>({...f,threatLevel:v as any}))}>
                <SelectTrigger><SelectValue/></SelectTrigger>
                <SelectContent>
                  {["low","medium","high","critical"].map(t=><SelectItem key={t} value={t}>{t.charAt(0).toUpperCase()+t.slice(1)}</SelectItem>)}
                </SelectContent>
              </Select>
              <Input placeholder="Notes" value={form.notes} onChange={e=>setForm(f=>({...f,notes:e.target.value}))}/>
              <Button onClick={()=>{ if(!profileId||!form.opponentName) return toast.error("Name required"); upsertMut.mutate({profileId,...form}); }} disabled={upsertMut.isPending} style={{background:"#4A1525",color:"white"}}>
                {upsertMut.isPending?<Loader2 size={14} className="animate-spin"/>:"Save"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : entries.length === 0 ? <div className="text-center py-20 text-gray-500"><Search size={48} className="mx-auto mb-4 opacity-30"/><p>No opposition entries yet</p></div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {entries.map(e=>(
              <div key={e.id} className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: `3px solid ${THREAT_COLORS[e.threatLevel??"medium"]}` }}>
                <div className="flex items-start justify-between mb-3">
                  <div>
                    <p className="font-bold text-gray-900 text-lg">{e.opponentName}</p>
                    {e.party && <p className="text-sm text-gray-500">{e.party}</p>}
                  </div>
                  <Badge style={{ background: THREAT_COLORS[e.threatLevel??"medium"]+"22", color: THREAT_COLORS[e.threatLevel??"medium"] }}>
                    {e.threatLevel?.toUpperCase()} THREAT
                  </Badge>
                </div>
                {e.strength && <div className="mb-2"><p className="text-xs font-bold text-green-700 uppercase tracking-wider mb-1">Strengths</p><p className="text-sm text-gray-700">{e.strength}</p></div>}
                {e.weakness && <div className="mb-2"><p className="text-xs font-bold text-red-700 uppercase tracking-wider mb-1">Weaknesses</p><p className="text-sm text-gray-700">{e.weakness}</p></div>}
                {e.notes && <p className="text-xs text-gray-500 mt-2 italic border-t pt-2">{e.notes}</p>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
