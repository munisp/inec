import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Plus, UserCheck, Loader2, Search } from "lucide-react";

export default function VoterRegistration() {
  const { profileId , canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: voters = [], isLoading } = trpc.voters.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const addMut = trpc.voters.add.useMutation({
    onSuccess: () => { utils.voters.list.invalidate(); toast.success("Voter registered"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [form, setForm] = useState({ fullName: "", vin: "", lga: "", ward: "", pollingUnit: "", phone: "" });

  const filtered = voters.filter(v => v.fullName.toLowerCase().includes(search.toLowerCase()) || (v.vin ?? "").includes(search));

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <UserCheck size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Voter Registration</h1>
        </div>
        <div className="flex items-center gap-3">
          <div className="relative"><Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"/><Input placeholder="Search voters…" value={search} onChange={e=>setSearch(e.target.value)} className="pl-9 w-56 bg-white/10 text-white placeholder:text-white/60 border-white/20"/></div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Register Voter</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Register New Voter</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Full Name *" value={form.fullName} onChange={e=>setForm(f=>({...f,fullName:e.target.value}))}/>
                <Input placeholder="VIN (Voter ID Number)" value={form.vin} onChange={e=>setForm(f=>({...f,vin:e.target.value}))}/>
                <Input placeholder="LGA" value={form.lga} onChange={e=>setForm(f=>({...f,lga:e.target.value}))}/>
                <Input placeholder="Ward" value={form.ward} onChange={e=>setForm(f=>({...f,ward:e.target.value}))}/>
                <Input placeholder="Polling Unit" value={form.pollingUnit} onChange={e=>setForm(f=>({...f,pollingUnit:e.target.value}))}/>
                <Input placeholder="Phone" value={form.phone} onChange={e=>setForm(f=>({...f,phone:e.target.value}))}/>
                <Button onClick={() => { if (!profileId || !form.fullName) return toast.error("Name required"); addMut.mutate({ profileId, ...form }); }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Register"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        <div className="flex items-center justify-between mb-4">
          <p className="text-sm text-gray-600"><span className="font-bold text-gray-900">{voters.length}</span> registered voters</p>
        </div>
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : filtered.length === 0 ? <div className="text-center py-20 text-gray-500"><UserCheck size={48} className="mx-auto mb-4 opacity-30"/><p>No voters found</p></div>
        : (
          <div className="bg-white border border-gray-200 rounded overflow-hidden">
            <table className="w-full text-sm">
              <thead><tr className="bg-gray-50 border-b border-gray-200">{["Name","VIN","LGA","Ward","Polling Unit","Phone","Registered"].map(h=><th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>)}</tr></thead>
              <tbody>{filtered.map((v,i)=>(
                <tr key={v.id} className={i%2===0?"bg-white":"bg-gray-50/50"}>
                  <td className="px-4 py-3 font-medium text-gray-900">{v.fullName}</td>
                  <td className="px-4 py-3 font-mono text-gray-600">{v.vin ?? "—"}</td>
                  <td className="px-4 py-3 text-gray-600">{v.lga ?? "—"}</td>
                  <td className="px-4 py-3 text-gray-600">{v.ward ?? "—"}</td>
                  <td className="px-4 py-3 text-gray-600">{v.pollingUnit ?? "—"}</td>
                  <td className="px-4 py-3 text-gray-600">{v.phone ?? "—"}</td>
                  <td className="px-4 py-3 text-xs text-gray-400">{new Date(v.registeredAt).toLocaleDateString()}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
