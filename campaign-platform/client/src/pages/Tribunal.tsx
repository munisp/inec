/**
 * Election Petition Tribunal Tracker — DB-backed with tRPC.
 * Tracks pre/post-election petitions: parties, counsel, hearing dates,
 * status and outcomes. Only real records from the tribunal router are shown.
 */
import { useState } from "react";
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { ArrowLeft, Gavel, Plus, Loader2, Pencil, Trash2 } from "lucide-react";

const PETITION_TYPES = ["pre_election", "post_election"] as const;
type PetitionType = (typeof PETITION_TYPES)[number];
const STATUSES = ["filed", "hearing", "judgment", "appealed", "closed"] as const;
type Status = (typeof STATUSES)[number];

const STATUS_COLORS: Record<string, string> = {
  filed: "#1A3A5C",
  hearing: "#F59E0B",
  judgment: "#008751",
  appealed: "#C0392B",
  closed: "#6b7280",
};

interface PetitionForm {
  electionName: string;
  petitionType: PetitionType;
  court: string;
  caseNumber: string;
  petitioner: string;
  respondent: string;
  counsel: string;
  filedAt: string;
  hearingDate: string;
  status: Status;
  outcome: string;
  notes: string;
}

const EMPTY_FORM: PetitionForm = {
  electionName: "",
  petitionType: "post_election",
  court: "",
  caseNumber: "",
  petitioner: "",
  respondent: "",
  counsel: "",
  filedAt: "",
  hearingDate: "",
  status: "filed",
  outcome: "",
  notes: "",
};

function toDateInput(v: unknown): string {
  if (!v) return "";
  const d = new Date(v as string);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10);
}

export default function Tribunal() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: petitions = [], isLoading } = trpc.tribunal.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const upsertMut = trpc.tribunal.upsert.useMutation({
    onSuccess: () => {
      utils.tribunal.list.invalidate();
      toast.success(editingId ? "Petition updated" : "Petition recorded");
      setOpen(false);
      setEditingId(null);
      setForm(EMPTY_FORM);
    },
    onError: (e) => toast.error(e.message),
  });
  const deleteMut = trpc.tribunal.delete.useMutation({
    onSuccess: () => { utils.tribunal.list.invalidate(); toast.success("Petition deleted"); },
    onError: (e) => toast.error(e.message),
  });

  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState<PetitionForm>(EMPTY_FORM);

  function openEdit(p: any) {
    setEditingId(p.id);
    setForm({
      electionName: p.electionName ?? "",
      petitionType: (p.petitionType ?? "post_election") as PetitionType,
      court: p.court ?? "",
      caseNumber: p.caseNumber ?? "",
      petitioner: p.petitioner ?? "",
      respondent: p.respondent ?? "",
      counsel: p.counsel ?? "",
      filedAt: toDateInput(p.filedAt),
      hearingDate: toDateInput(p.hearingDate),
      status: (p.status ?? "filed") as Status,
      outcome: p.outcome ?? "",
      notes: p.notes ?? "",
    });
    setOpen(true);
  }

  function submit() {
    if (!profileId) return;
    if (!form.electionName.trim()) return toast.error("Election name is required");
    upsertMut.mutate({
      id: editingId ?? undefined,
      profileId,
      electionName: form.electionName.trim(),
      petitionType: form.petitionType,
      court: form.court || undefined,
      caseNumber: form.caseNumber || undefined,
      petitioner: form.petitioner || undefined,
      respondent: form.respondent || undefined,
      counsel: form.counsel || undefined,
      filedAt: form.filedAt || undefined,
      hearingDate: form.hearingDate || undefined,
      status: form.status,
      outcome: form.outcome || undefined,
      notes: form.notes || undefined,
    });
  }

  const labelCls = "text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1 block";

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <Gavel size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Election Petition Tribunal</h1>
        </div>
        <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) { setEditingId(null); setForm(EMPTY_FORM); } }}>
          <DialogTrigger asChild>
            <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5" disabled={!canEdit}>
              <Plus size={14} /> Add Petition
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
            <DialogHeader><DialogTitle>{editingId ? "Edit Petition" : "Record Election Petition"}</DialogTitle></DialogHeader>
            <div className="grid gap-3 py-2">
              <div>
                <span className={labelCls}>Election Name *</span>
                <Input placeholder="e.g. 2027 Governorship Election — Lagos" value={form.electionName}
                  onChange={(e) => setForm((f) => ({ ...f, electionName: e.target.value }))} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <span className={labelCls}>Petition Type</span>
                  <Select value={form.petitionType} onValueChange={(v) => setForm((f) => ({ ...f, petitionType: v as PetitionType }))}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>{PETITION_TYPES.map((t) => <SelectItem key={t} value={t}>{t.replace(/_/g, "-")}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
                <div>
                  <span className={labelCls}>Status</span>
                  <Select value={form.status} onValueChange={(v) => setForm((f) => ({ ...f, status: v as Status }))}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>{STATUSES.map((s) => <SelectItem key={s} value={s}>{s}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <span className={labelCls}>Court / Tribunal</span>
                  <Input value={form.court} onChange={(e) => setForm((f) => ({ ...f, court: e.target.value }))} />
                </div>
                <div>
                  <span className={labelCls}>Case Number</span>
                  <Input value={form.caseNumber} onChange={(e) => setForm((f) => ({ ...f, caseNumber: e.target.value }))} />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <span className={labelCls}>Petitioner</span>
                  <Input value={form.petitioner} onChange={(e) => setForm((f) => ({ ...f, petitioner: e.target.value }))} />
                </div>
                <div>
                  <span className={labelCls}>Respondent</span>
                  <Input value={form.respondent} onChange={(e) => setForm((f) => ({ ...f, respondent: e.target.value }))} />
                </div>
              </div>
              <div>
                <span className={labelCls}>Counsel</span>
                <Input value={form.counsel} onChange={(e) => setForm((f) => ({ ...f, counsel: e.target.value }))} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <span className={labelCls}>Filed On</span>
                  <Input type="date" value={form.filedAt} onChange={(e) => setForm((f) => ({ ...f, filedAt: e.target.value }))} />
                </div>
                <div>
                  <span className={labelCls}>Next Hearing Date</span>
                  <Input type="date" value={form.hearingDate} onChange={(e) => setForm((f) => ({ ...f, hearingDate: e.target.value }))} />
                </div>
              </div>
              <div>
                <span className={labelCls}>Outcome</span>
                <Input placeholder="e.g. Judgment for petitioner (once decided)" value={form.outcome}
                  onChange={(e) => setForm((f) => ({ ...f, outcome: e.target.value }))} />
              </div>
              <div>
                <span className={labelCls}>Notes</span>
                <Input value={form.notes} onChange={(e) => setForm((f) => ({ ...f, notes: e.target.value }))} />
              </div>
              <Button onClick={submit} disabled={upsertMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                {upsertMut.isPending ? <Loader2 size={14} className="animate-spin" /> : editingId ? "Save Changes" : "Record Petition"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </header>

      <div className="max-w-6xl mx-auto px-6 py-8">
        {isLoading ? (
          <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
        ) : petitions.length === 0 ? (
          <div className="text-center py-20 text-gray-500">
            <Gavel size={48} className="mx-auto mb-4 opacity-30" />
            <p className="font-semibold mb-2">No election petitions on record</p>
            <p className="text-sm">Record a petition to begin tracking tribunal proceedings.</p>
          </div>
        ) : (
          <div className="bg-white border border-gray-200 rounded overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-gray-50 border-b">
                  {["Election", "Type", "Court / Case No.", "Petitioner", "Respondent", "Counsel", "Filed", "Hearing", "Status", "Outcome", ""].map((h) => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 whitespace-nowrap">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {petitions.map((p: any, i: number) => (
                  <tr key={p.id} className={i % 2 === 0 ? "bg-white" : "bg-gray-50/50"}>
                    <td className="px-4 py-3 font-medium text-gray-900">{p.electionName}</td>
                    <td className="px-4 py-3 text-gray-600 whitespace-nowrap">{(p.petitionType ?? "").replace(/_/g, "-")}</td>
                    <td className="px-4 py-3 text-gray-600">
                      {p.court ?? "—"}
                      {p.caseNumber && <span className="block text-xs text-gray-400">{p.caseNumber}</span>}
                    </td>
                    <td className="px-4 py-3 text-gray-600">{p.petitioner ?? "—"}</td>
                    <td className="px-4 py-3 text-gray-600">{p.respondent ?? "—"}</td>
                    <td className="px-4 py-3 text-gray-600">{p.counsel ?? "—"}</td>
                    <td className="px-4 py-3 text-gray-600 whitespace-nowrap">{p.filedAt ? new Date(p.filedAt).toLocaleDateString("en-NG") : "—"}</td>
                    <td className="px-4 py-3 text-gray-600 whitespace-nowrap">{p.hearingDate ? new Date(p.hearingDate).toLocaleDateString("en-NG") : "—"}</td>
                    <td className="px-4 py-3">
                      <Badge style={{ background: (STATUS_COLORS[p.status] ?? "#6b7280") + "22", color: STATUS_COLORS[p.status] ?? "#6b7280" }}>
                        {(p.status ?? "filed").toUpperCase()}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-gray-600 max-w-48">{p.outcome ?? "—"}</td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      <div className="flex items-center gap-1">
                        {canEdit && (
                          <button onClick={() => openEdit(p)} className="text-gray-400 hover:text-gray-700 transition-colors" title="Edit">
                            <Pencil size={14} />
                          </button>
                        )}
                        {canDelete && (
                          <button
                            onClick={() => {
                              if (!profileId) return;
                              if (window.confirm(`Delete petition "${p.electionName}"? This cannot be undone.`))
                                deleteMut.mutate({ profileId, id: p.id });
                            }}
                            className="text-gray-300 hover:text-red-500 transition-colors"
                            title="Delete"
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
