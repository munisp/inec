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
import { ArrowLeft, Scale, Plus, Loader2, CheckCircle, AlertTriangle, XCircle, FileText, Download, Bell, BellOff, Send } from "lucide-react";
import { exportToCSV, exportToPDF } from "@/hooks/useExport";

const STATUS_META = {
  compliant: { color: "#008751", icon: CheckCircle, label: "Compliant" },
  warning: { color: "#F59E0B", icon: AlertTriangle, label: "Warning" },
  non_compliant: { color: "#C0392B", icon: XCircle, label: "Non-Compliant" },
  pending: { color: "#1A3A5C", icon: AlertTriangle, label: "Pending" },
};
const EXPORT_COLS_C = [
  { header: "Title", key: "title" },
  { header: "Category", key: "category" },
  { header: "Status", key: "status" },
  { header: "Deadline", key: "deadline" },
  { header: "Description", key: "description" },
  { header: "Notes", key: "notes" },
];
const CATEGORIES = ["Financial","Campaign Materials","Rallies & Events","Digital Media","Staff & Agents","Voter Registration","Reporting","Other"];

export default function LegalCompliance() {
  const { profileId , canEdit, canDelete } = useCandidateProfile();
  const { data: notifStatus, refetch: refetchNotif } = trpc.notifications.status.useQuery(undefined, { refetchInterval: 30000 });
  const enableNotifMut = trpc.notifications.enable.useMutation({ onSuccess: () => { refetchNotif(); toast.success("Deadline alerts enabled — daily at 08:00 UTC"); } });
  const disableNotifMut = trpc.notifications.disable.useMutation({ onSuccess: () => { refetchNotif(); toast.success("Deadline alerts disabled"); } });
  const testAlertMut = trpc.notifications.testAlert.useMutation({ onSuccess: (d) => toast.success(d.count > 0 ? `Test alert sent — ${d.count} upcoming deadline(s)` : "Test alert sent — no upcoming deadlines") });
  const utils = trpc.useUtils();
  const { data: items = [], isLoading } = trpc.compliance.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const upsertMut = trpc.compliance.upsert.useMutation({
    onSuccess: () => { utils.compliance.list.invalidate(); toast.success("Item saved"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ title:"", category:"Financial", description:"", status:"pending" as "compliant"|"warning"|"non_compliant"|"pending", deadline:"", notes:"" });

  const compliant = items.filter(i=>i.status==="compliant").length;
  const nonCompliant = items.filter(i=>i.status==="non_compliant").length;
  const score = items.length ? Math.round((compliant/items.length)*100) : 0;

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Scale size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Legal Compliance</h1>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right"><p className="text-xs text-white/60">SCORE</p><p className="font-mono font-bold text-white">{score}%</p></div>
          <div className="text-right"><p className="text-xs text-white/60">ISSUES</p><p className="font-mono font-bold text-red-300">{nonCompliant}</p></div>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToCSV("legal-compliance", EXPORT_COLS_C, items as Record<string, unknown>[])}><Download size={13}/> CSV</Button>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToPDF("legal-compliance", "Legal Compliance Report", `Score: ${score}% | Issues: ${nonCompliant}`, EXPORT_COLS_C, items as Record<string, unknown>[])}><FileText size={13}/> PDF</Button>
        {/* Notification Settings Panel */}
        <div className="mb-6 p-4 rounded-lg border" style={{ borderColor: "#1A3A5C33", background: "#EBF2F8" }}>
          <div className="flex items-center justify-between flex-wrap gap-3">
            <div className="flex items-center gap-2">
              <Bell size={16} style={{ color: "#1A3A5C" }} />
              <div>
                <p className="text-sm font-bold" style={{ color: "#1A3A5C" }}>Deadline Reminder Notifications</p>
                <p className="text-xs text-gray-500">Daily push notifications 7 days and 1 day before each compliance deadline</p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <button onClick={() => testAlertMut.mutate({ profileId: profileId! })} disabled={!profileId || testAlertMut.isPending}
                className="text-xs px-3 py-1.5 rounded border flex items-center gap-1" style={{ borderColor: "#1A3A5C44", color: "#1A3A5C", background: "white" }}>
                {testAlertMut.isPending ? <Loader2 size={12} className="animate-spin" /> : <Send size={12} />} Test Alert
              </button>
              {notifStatus?.enabled ? (
                <button onClick={() => profileId && disableNotifMut.mutate({ profileId })} disabled={disableNotifMut.isPending}
                  className="text-xs px-3 py-1.5 rounded border flex items-center gap-1" style={{ borderColor: "#C0392B44", color: "#C0392B", background: "white" }}>
                  {disableNotifMut.isPending ? <Loader2 size={12} className="animate-spin" /> : <BellOff size={12} />} Disable Alerts
                </button>
              ) : (
                <button onClick={() => profileId && enableNotifMut.mutate({ profileId })} disabled={enableNotifMut.isPending}
                  className="text-xs px-3 py-1.5 rounded text-white flex items-center gap-1" style={{ background: "#008751" }}>
                  {enableNotifMut.isPending ? <Loader2 size={12} className="animate-spin" /> : <Bell size={12} />} Enable Alerts
                </button>
              )}
            </div>
          </div>
          {notifStatus?.enabled && (
            <div className="mt-2 flex items-center gap-1.5 text-xs" style={{ color: "#008751" }}>
              <CheckCircle size={12} />
              <span>Daily alerts active — next run: {notifStatus.job?.nextExecutionAt ? new Date(notifStatus.job.nextExecutionAt).toLocaleString("en-NG") : "scheduled"}</span>
            </div>
          )}
        </div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Item</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Compliance Item</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Title *" value={form.title} onChange={e=>setForm(f=>({...f,title:e.target.value}))}/>
                <Select value={form.category} onValueChange={v=>setForm(f=>({...f,category:v}))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>{CATEGORIES.map(c=><SelectItem key={c} value={c}>{c}</SelectItem>)}</SelectContent>
                </Select>
                <Input placeholder="Description" value={form.description} onChange={e=>setForm(f=>({...f,description:e.target.value}))}/>
                <Select value={form.status} onValueChange={v=>setForm(f=>({...f,status:v as any}))}>
                  <SelectTrigger><SelectValue/></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="compliant">Compliant</SelectItem>
                    <SelectItem value="warning">Warning</SelectItem>
                    <SelectItem value="non_compliant">Non-Compliant</SelectItem>
                    <SelectItem value="pending">Pending</SelectItem>
                  </SelectContent>
                </Select>
                <Input type="date" value={form.deadline} onChange={e=>setForm(f=>({...f,deadline:e.target.value}))} placeholder="Deadline"/>
                <Input placeholder="Notes" value={form.notes} onChange={e=>setForm(f=>({...f,notes:e.target.value}))}/>
                <Button onClick={()=>{ if(!profileId||!form.title) return toast.error("Title required"); upsertMut.mutate({profileId,...form}); }} disabled={upsertMut.isPending} style={{background:"#4A1525",color:"white"}}>
                  {upsertMut.isPending?<Loader2 size={14} className="animate-spin"/>:"Save"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {/* Score bar */}
        <div className="bg-white border border-gray-200 rounded p-4 mb-6">
          <div className="flex items-center justify-between mb-2">
            <p className="text-sm font-semibold text-gray-700">Overall Compliance Score</p>
            <p className="font-mono font-bold text-lg" style={{ color: score>=80?"#008751":score>=60?"#F59E0B":"#C0392B" }}>{score}%</p>
          </div>
          <div className="h-3 bg-gray-100 rounded-full overflow-hidden">
            <div className="h-full rounded-full transition-all" style={{ width:`${score}%`, background: score>=80?"#008751":score>=60?"#F59E0B":"#C0392B" }}/>
          </div>
          <div className="flex gap-4 mt-3 text-xs text-gray-500">
            <span className="text-green-700 font-semibold">{compliant} compliant</span>
            <span className="text-yellow-600 font-semibold">{items.filter(i=>i.status==="warning").length} warnings</span>
            <span className="text-red-700 font-semibold">{nonCompliant} non-compliant</span>
            <span className="text-blue-700 font-semibold">{items.filter(i=>i.status==="pending").length} pending</span>
          </div>
        </div>
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : items.length === 0 ? <div className="text-center py-20 text-gray-500"><Scale size={48} className="mx-auto mb-4 opacity-30"/><p>No compliance items yet</p></div>
        : (
          <div className="space-y-3">
            {items.map(item=>{
              const meta = STATUS_META[item.status as keyof typeof STATUS_META] ?? STATUS_META.pending;
              const Icon = meta.icon;
              return (
                <div key={item.id} className="bg-white border border-gray-200 rounded p-4 flex items-start gap-4">
                  <Icon size={20} style={{ color: meta.color }} className="mt-0.5 flex-shrink-0"/>
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <p className="font-semibold text-gray-900">{item.title}</p>
                      <Badge style={{ background: meta.color+"22", color: meta.color }}>{meta.label}</Badge>
                      {item.category && <Badge variant="outline">{item.category}</Badge>}
                    </div>
                    {item.description && <p className="text-sm text-gray-600">{item.description}</p>}
                    {item.deadline && (() => {
                      const daysLeft = Math.ceil((new Date(item.deadline).getTime() - Date.now()) / 86400000);
                      const isOverdue = daysLeft < 0;
                      const isUrgent = daysLeft >= 0 && daysLeft <= 7;
                      return (
                        <div className="flex items-center gap-2 mt-1">
                          <p className="text-xs text-gray-400">Deadline: {new Date(item.deadline).toLocaleDateString("en-NG", { day: "numeric", month: "short", year: "numeric" })}</p>
                          <span className={`text-xs font-bold px-1.5 py-0.5 rounded ${isOverdue ? "bg-red-100 text-red-700" : isUrgent ? "bg-amber-100 text-amber-700" : "bg-gray-100 text-gray-600"}`}>
                            {isOverdue ? `${Math.abs(daysLeft)}d overdue` : daysLeft === 0 ? "Due today!" : `${daysLeft}d left`}
                          </span>
                        </div>
                      );
                    })()}
                    {item.notes && <p className="text-xs text-gray-500 mt-1 italic">{item.notes}</p>}
                  </div>
                  <Button variant="outline" size="sm" onClick={()=>upsertMut.mutate({id:item.id,profileId:profileId!,title:item.title,status:"compliant"})}>
                    Mark Compliant
                  </Button>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
