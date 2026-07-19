import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Globe, Plus, Loader2 } from "lucide-react";

export default function DiasporaOutreach() {
  const { profileId , canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: contacts = [], isLoading } = trpc.diaspora.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.diaspora.add.useMutation({
    onSuccess: () => { utils.diaspora.list.invalidate(); toast.success("Contact added"); setOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", country: "", city: "", email: "", phone: "", organization: "", pledgedAmount: "", notes: "" });

  const byCountry = contacts.reduce<Record<string, number>>((acc, c) => {
    const key = c.country ?? "Unknown";
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Globe size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Diaspora Outreach</h1>
        </div>
        <div className="flex items-center gap-4">
          <div className="text-right"><p className="text-xs text-white/60">CONTACTS</p><p className="font-mono font-bold text-white">{contacts.length}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">COUNTRIES</p><p className="font-mono font-bold text-white">{Object.keys(byCountry).length}</p></div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Contact</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Diaspora Contact</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Full Name *" value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}/>
                <Input placeholder="Country *" value={form.country} onChange={e => setForm(f => ({ ...f, country: e.target.value }))}/>
                <Input placeholder="City" value={form.city} onChange={e => setForm(f => ({ ...f, city: e.target.value }))}/>
                <Input placeholder="Email" value={form.email} onChange={e => setForm(f => ({ ...f, email: e.target.value }))}/>
                <Input placeholder="Phone / WhatsApp" value={form.phone} onChange={e => setForm(f => ({ ...f, phone: e.target.value }))}/>
                <Input placeholder="Organization / Role" value={form.organization} onChange={e => setForm(f => ({ ...f, organization: e.target.value }))}/>
                <Input type="number" placeholder="Pledged Amount (₦)" value={form.pledgedAmount} onChange={e => setForm(f => ({ ...f, pledgedAmount: e.target.value }))}/>
                <Input placeholder="Notes" value={form.notes} onChange={e => setForm(f => ({ ...f, notes: e.target.value }))}/>
                <Button onClick={() => {
                  if (!profileId || !form.name || !form.country) return toast.error("Name and country required");
                  addMut.mutate({ profileId, fullName: form.name, country: form.country, city: form.city || undefined, email: form.email || undefined, phone: form.phone || undefined, organization: form.organization || undefined, notes: form.notes || undefined });
                }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Add Contact"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {Object.keys(byCountry).length > 0 && (
          <div className="flex flex-wrap gap-2 mb-6">
            {Object.entries(byCountry).sort((a, b) => b[1] - a[1]).map(([country, count]) => (
              <div key={country} className="bg-white border border-gray-200 rounded px-3 py-2 flex items-center gap-2">
                <Globe size={12} className="text-gray-400"/>
                <span className="text-sm font-medium text-gray-700">{country}</span>
                <Badge variant="outline">{count}</Badge>
              </div>
            ))}
          </div>
        )}
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : contacts.length === 0 ? <div className="text-center py-20 text-gray-500"><Globe size={48} className="mx-auto mb-4 opacity-30"/><p>No diaspora contacts yet</p></div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {contacts.map(c => (
              <div key={c.id} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #4A1525" }}>
                <p className="font-semibold text-gray-900 mb-1">{c.name}</p>
                <p className="text-xs text-gray-500">{c.city ? `${c.city}, ` : ""}{c.country}</p>
                {c.organization && <p className="text-xs text-gray-400 mt-1">{c.organization}</p>}
                {c.pledgedAmount && <p className="text-xs font-mono text-green-700 mt-1">Pledged: ₦{c.pledgedAmount.toLocaleString()}</p>}
                <div className="mt-2 text-xs text-gray-500 space-y-0.5">
                  {c.email && <p>{c.email}</p>}
                  {c.phone && <p>{c.phone}</p>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
