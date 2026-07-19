import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, DollarSign, Plus, Loader2, Download, FileText } from "lucide-react";
import { exportToCSV, exportToPDF } from "@/hooks/useExport";

export default function FundraisingTracker() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: donations = [], isLoading } = trpc.fundraising.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.fundraising.add.useMutation({
    onSuccess: () => { utils.fundraising.list.invalidate(); toast.success("Donation recorded"); setOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ donorName: "", amount: "", currency: "NGN", source: "Cash", category: "Individual", notes: "" });

  const total = donations.reduce((s, d) => s + (d.amount ?? 0), 0);

  const EXPORT_COLS_F = [
    { header: "Donor Name", key: "donorName" },
    { header: "Amount (NGN)", key: "amount" },
    { header: "Source", key: "source" },
    { header: "Category", key: "category" },
    { header: "Notes", key: "notes" },
    { header: "Date", key: "transactedAt" },
  ];
  const totalRaised = donations.reduce((s, d) => s + (d.amount ?? 0), 0);
  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <DollarSign size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Fundraising Tracker</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToCSV("fundraising", EXPORT_COLS_F, donations as Record<string, unknown>[])}><Download size={13}/> CSV</Button>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToPDF("fundraising", "Fundraising Tracker", `Total Raised: NGN${totalRaised.toLocaleString()}`, EXPORT_COLS_F, donations as Record<string, unknown>[])}><FileText size={13}/> PDF</Button>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right"><p className="text-xs text-white/60">TOTAL RAISED</p><p className="font-mono font-bold text-green-300">₦{total.toLocaleString()}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">DONORS</p><p className="font-mono font-bold text-white">{donations.length}</p></div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Donation</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Record Donation</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Donor Name" value={form.donorName} onChange={e => setForm(f => ({ ...f, donorName: e.target.value }))}/>
                <Input type="number" placeholder="Amount *" value={form.amount} onChange={e => setForm(f => ({ ...f, amount: e.target.value }))}/>
                <Select value={form.currency} onValueChange={v => setForm(f => ({ ...f, currency: v }))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>{["NGN","USD","GBP","EUR"].map(c => <SelectItem key={c} value={c}>{c}</SelectItem>)}</SelectContent>
                </Select>
                <Select value={form.source} onValueChange={v => setForm(f => ({ ...f, source: v }))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>{["Cash","Bank Transfer","Online","Cheque","Crypto","Event"].map(m => <SelectItem key={m} value={m}>{m}</SelectItem>)}</SelectContent>
                </Select>
                <Select value={form.category} onValueChange={v => setForm(f => ({ ...f, category: v }))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>{["Individual","Corporate","Party","Diaspora","Event","Other"].map(c => <SelectItem key={c} value={c}>{c}</SelectItem>)}</SelectContent>
                </Select>
                <Input placeholder="Notes" value={form.notes} onChange={e => setForm(f => ({ ...f, notes: e.target.value }))}/>
                <Button onClick={() => {
                  if (!profileId || !form.amount) return toast.error("Amount required");
                  addMut.mutate({ profileId, donorName: form.donorName || undefined, amount: parseFloat(form.amount), currency: form.currency, source: form.source, category: form.category, notes: form.notes || undefined });
                }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Record"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : donations.length === 0 ? <div className="text-center py-20 text-gray-500"><DollarSign size={48} className="mx-auto mb-4 opacity-30"/><p>No donations recorded yet</p></div>
        : (
          <div className="bg-white border border-gray-200 rounded overflow-hidden">
            <table className="w-full text-sm">
              <thead><tr className="bg-gray-50 border-b">
                {["Donor","Amount","Currency","Source","Category","Notes","Date"].map(h => <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>)}
              </tr></thead>
              <tbody>{donations.map((d, i) => (
                <tr key={d.id} className={i % 2 === 0 ? "bg-white" : "bg-gray-50/50"}>
                  <td className="px-4 py-3 font-medium text-gray-900">{d.donorName ?? "Anonymous"}</td>
                  <td className="px-4 py-3 font-mono font-bold">{(d.amount ?? 0).toLocaleString()}</td>
                  <td className="px-4 py-3 text-gray-600">{d.currency ?? "NGN"}</td>
                  <td className="px-4 py-3 text-gray-600">{d.source ?? "—"}</td>
                  <td className="px-4 py-3 text-gray-600">{d.category ?? "—"}</td>
                  <td className="px-4 py-3 text-xs text-gray-500">{d.notes ?? "—"}</td>
                  <td className="px-4 py-3 text-xs text-gray-400">{new Date(d.transactedAt).toLocaleDateString()}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
