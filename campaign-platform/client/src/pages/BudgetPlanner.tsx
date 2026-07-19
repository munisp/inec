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
import { ArrowLeft, Calculator, Plus, Loader2, Download, FileText } from "lucide-react";
import { exportToCSV, exportToPDF } from "@/hooks/useExport";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Legend } from "recharts";

const CATEGORIES = ["Advertising","Events & Rallies","Staff & Salaries","Travel & Logistics","Legal & Compliance","Technology","Printing","Security","Miscellaneous"];
type Priority = "low"|"medium"|"high"|"critical";
const PRIORITY_COLORS: Record<Priority,string> = { low:"#1A3A5C", medium:"#F59E0B", high:"#C0392B", critical:"#7B0000" };

export default function BudgetPlanner() {
  const { profileId , canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: items = [], isLoading } = trpc.budget.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.budget.upsert.useMutation({
    onSuccess: () => { utils.budget.list.invalidate(); toast.success("Budget item added"); setOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<"table" | "chart">("table");
  const [form, setForm] = useState({ description: "", category: "Advertising", budgetedAmount: "", spentAmount: "", priority: "medium" as Priority, notes: "" });

  const totalBudgeted = items.reduce((s, i) => s + (i.budgetedAmount ?? 0), 0);
  const totalSpent = items.reduce((s, i) => s + (i.spentAmount ?? 0), 0);
  const variance = totalBudgeted - totalSpent;

  // Reconciliation chart data: budgeted vs spent by category
  const categoryData = Object.entries(
    items.reduce<Record<string, { budgeted: number; spent: number }>>((acc, item) => {
      const cat = item.category ?? "Other";
      if (!acc[cat]) acc[cat] = { budgeted: 0, spent: 0 };
      acc[cat].budgeted += item.budgetedAmount ?? 0;
      acc[cat].spent += item.spentAmount ?? 0;
      return acc;
    }, {})
  ).map(([category, vals]) => ({ category, ...vals }));

  const EXPORT_COLS = [
    { header: "Category", key: "category" },
    { header: "Description", key: "description" },
    { header: "Budgeted (NGN)", key: "budgetedAmount" },
    { header: "Spent (NGN)", key: "spentAmount" },
    { header: "Variance (NGN)", key: "_variance" },
    { header: "Priority", key: "priority" },
    { header: "Notes", key: "notes" },
  ];
  const exportRows = items.map(i => ({
    ...i,
    _variance: (i.budgetedAmount ?? 0) - (i.spentAmount ?? 0),
  })) as Record<string, unknown>[];

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Calculator size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Budget Planner</h1>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right"><p className="text-xs text-white/60">BUDGETED</p><p className="font-mono font-bold text-white">₦{totalBudgeted.toLocaleString()}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">SPENT</p><p className="font-mono font-bold text-yellow-300">₦{totalSpent.toLocaleString()}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">VARIANCE</p><p className={`font-mono font-bold ${variance >= 0 ? "text-green-300" : "text-red-300"}`}>₦{Math.abs(variance).toLocaleString()}</p></div>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToCSV("budget-plan", EXPORT_COLS, exportRows)}><Download size={13}/> CSV</Button>
            <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToPDF("budget-plan", "Campaign Budget Plan", `Budgeted: NGN${totalBudgeted.toLocaleString()} | Spent: NGN${totalSpent.toLocaleString()}`, EXPORT_COLS, exportRows)}><FileText size={13}/> PDF</Button>
          </div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Item</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Budget Item</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Description *" value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))}/>
                <Select value={form.category} onValueChange={v => setForm(f => ({ ...f, category: v }))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>{CATEGORIES.map(c => <SelectItem key={c} value={c}>{c}</SelectItem>)}</SelectContent>
                </Select>
                <Input type="number" placeholder="Budgeted Amount (₦) *" value={form.budgetedAmount} onChange={e => setForm(f => ({ ...f, budgetedAmount: e.target.value }))}/>
                <Input type="number" placeholder="Spent Amount (₦)" value={form.spentAmount} onChange={e => setForm(f => ({ ...f, spentAmount: e.target.value }))}/>
                <Select value={form.priority} onValueChange={v => setForm(f => ({ ...f, priority: v as Priority }))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="low">Low</SelectItem>
                    <SelectItem value="medium">Medium</SelectItem>
                    <SelectItem value="high">High</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                  </SelectContent>
                </Select>
                <Input placeholder="Notes" value={form.notes} onChange={e => setForm(f => ({ ...f, notes: e.target.value }))}/>
                <Button onClick={() => {
                  if (!profileId || !form.description || !form.budgetedAmount) return toast.error("Description and budget required");
                  addMut.mutate({ profileId, description: form.description, category: form.category, budgetedAmount: parseFloat(form.budgetedAmount), spentAmount: form.spentAmount ? parseFloat(form.spentAmount) : undefined, priority: form.priority, notes: form.notes || undefined });
                }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Add Item"}
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
            <button key={t} onClick={() => setActiveTab(t)}
              className={`px-4 py-1.5 text-xs font-semibold uppercase tracking-wide rounded transition-all ${activeTab === t ? "text-white" : "bg-white text-gray-600 border border-gray-200"}`}
              style={activeTab === t ? { background: "#4A1525" } : {}}>
              {t === "table" ? "Budget Table" : "Reconciliation Chart"}
            </button>
          ))}
        </div>

        {/* Reconciliation Chart */}
        {activeTab === "chart" && (
          <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #1A3A5C" }}>
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Budgeted vs Spent by Category</p>
            <p className="text-xs text-gray-400 mb-4">All amounts in ₦</p>
            {categoryData.length === 0 ? (
              <p className="text-center text-gray-400 py-8">No budget items yet</p>
            ) : (
              <ResponsiveContainer width="100%" height={280}>
                <BarChart data={categoryData} margin={{ top: 5, right: 10, left: 10, bottom: 50 }}>
                  <XAxis dataKey="category" tick={{ fontSize: 10 }} angle={-25} textAnchor="end" height={60} />
                  <YAxis tickFormatter={v => `₦${(v / 1000).toFixed(0)}K`} tick={{ fontSize: 10 }} />
                  <Tooltip formatter={(v: number) => [`₦${v.toLocaleString()}`, ""]} />
                  <Legend />
                  <Bar dataKey="budgeted" name="Budgeted" fill="#1A3A5C" radius={[3, 3, 0, 0]} />
                  <Bar dataKey="spent" name="Spent" fill="#008751" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
            <div className="grid grid-cols-3 gap-4 mt-4 pt-4 border-t border-gray-100">
              {[
                { label: "Total Budgeted", value: `₦${totalBudgeted.toLocaleString()}`, color: "#1A3A5C" },
                { label: "Total Spent", value: `₦${totalSpent.toLocaleString()}`, color: "#008751" },
                { label: "Variance", value: `${variance >= 0 ? "+" : ""}₦${Math.abs(variance).toLocaleString()}`, color: variance >= 0 ? "#008751" : "#C0392B" },
              ].map(s => (
                <div key={s.label} className="text-center">
                  <p className="text-xs text-gray-500 mb-1">{s.label}</p>
                  <p className="font-mono font-bold text-lg" style={{ color: s.color }}>{s.value}</p>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Budget Table */}
        {activeTab === "table" && (
          isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
          : items.length === 0 ? <div className="text-center py-20 text-gray-500"><Calculator size={48} className="mx-auto mb-4 opacity-30"/><p>No budget items yet</p></div>
          : (
            <div className="bg-white border border-gray-200 rounded overflow-hidden">
              <table className="w-full text-sm">
                <thead><tr className="bg-gray-50 border-b">
                  {["Description","Category","Budgeted","Spent","Variance","Priority","Notes"].map(h => <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>)}
                </tr></thead>
                <tbody>{items.map((item, i) => {
                  const spent = item.spentAmount ?? 0;
                  const v = (item.budgetedAmount ?? 0) - spent;
                  return (
                    <tr key={item.id} className={i % 2 === 0 ? "bg-white" : "bg-gray-50/50"}>
                      <td className="px-4 py-3 font-medium text-gray-900">{item.description}</td>
                      <td className="px-4 py-3 text-gray-600">{item.category}</td>
                      <td className="px-4 py-3 font-mono">₦{(item.budgetedAmount ?? 0).toLocaleString()}</td>
                      <td className="px-4 py-3 font-mono">{item.spentAmount ? `₦${item.spentAmount.toLocaleString()}` : "—"}</td>
                      <td className={`px-4 py-3 font-mono font-bold ${v >= 0 ? "text-green-700" : "text-red-700"}`}>{v >= 0 ? "+" : ""}{v.toLocaleString()}</td>
                      <td className="px-4 py-3"><Badge style={{ background: PRIORITY_COLORS[(item.priority ?? "medium") as Priority] + "22", color: PRIORITY_COLORS[(item.priority ?? "medium") as Priority] }}>{item.priority ?? "medium"}</Badge></td>
                      <td className="px-4 py-3 text-xs text-gray-500">{item.notes ?? "—"}</td>
                    </tr>
                  );
                })}</tbody>
              </table>
            </div>
          )
        )}
      </div>
    </div>
  );
}
