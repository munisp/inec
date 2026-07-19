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
import { PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer } from "recharts";

export default function FundraisingTracker() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const [viewMode, setViewMode] = useState<"table" | "chart">("table");
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
  // Category breakdown for pie chart
  const categoryBreakdown = Object.entries(
    donations.reduce<Record<string, number>>((acc, d) => {
      const cat = d.category ?? "Other";
      acc[cat] = (acc[cat] ?? 0) + (d.amount ?? 0);
      return acc;
    }, {})
  ).map(([name, value]) => ({ name, value })).sort((a, b) => b.value - a.value);
  const PIE_COLORS = ["#4A1525", "#008751", "#1A3A5C", "#C0392B", "#E67E22", "#8E44AD"];

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
        {/* Tab switcher */}
        <div className="flex gap-2 mb-4">
          {(["table", "chart"] as const).map(t => (
            <button key={t} onClick={() => setViewMode(t)}
              className={`px-4 py-1.5 text-xs font-semibold uppercase tracking-wide rounded transition-all ${viewMode === t ? "text-white" : "bg-white text-gray-600 border border-gray-200"}`}
              style={viewMode === t ? { background: "#4A1525" } : {}}>
              {t === "table" ? "Donation Table" : "Donor Breakdown"}
            </button>
          ))}
        </div>

        {/* Donor Breakdown Chart */}
        {viewMode === "chart" && donations.length > 0 && (
          <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #4A1525" }}>
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Donations by Category</p>
            <p className="text-xs text-gray-400 mb-4">All amounts in ₦</p>
            <div className="grid grid-cols-2 gap-6">
              <ResponsiveContainer width="100%" height={260}>
                <PieChart>
                  <Pie data={categoryBreakdown} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={100}
                    label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`} labelLine={false}>
                    {categoryBreakdown.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
                  </Pie>
                  <Tooltip formatter={(v: number) => [`₦${v.toLocaleString()}`, ""]} />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
              <div className="flex flex-col justify-center gap-3">
                {categoryBreakdown.map((cat, i) => (
                  <div key={cat.name} className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-3 h-3 rounded-full" style={{ background: PIE_COLORS[i % PIE_COLORS.length] }} />
                      <span className="text-sm text-gray-700">{cat.name}</span>
                    </div>
                    <span className="font-mono font-bold text-sm" style={{ color: PIE_COLORS[i % PIE_COLORS.length] }}>
                      ₦{cat.value.toLocaleString()}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}

        {/* Donation Table */}
        {viewMode === "table" && (isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
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
        ))}
      </div>
    </div>
  );
}
