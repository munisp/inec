/**
 * Data Protection — NDPA 2023 consent, provenance, DSAR and transparency.
 * Every figure on this page comes from the dataProtection tRPC router; empty
 * tables render as zeros/empty states, never estimates. Mutations are gated
 * behind canEdit (manager role).
 */
import { useState } from "react";
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import { ArrowLeft, ShieldCheck, Loader2, Search, FileCheck, Trash2, CheckCircle, XCircle } from "lucide-react";

const SUBJECT_TABLES = [
  "voter_registrations",
  "diaspora_contacts",
  "stakeholder_contacts",
  "volunteers",
  "petition_signatures",
] as const;
type SubjectTable = (typeof SUBJECT_TABLES)[number];

const LAWFUL_BASES = [
  "consent",
  "contract",
  "legal_obligation",
  "vital_interest",
  "public_interest",
  "legitimate_interest",
] as const;
type LawfulBasis = (typeof LAWFUL_BASES)[number];

const DSAR_TYPES = ["access", "rectification", "erasure", "restriction", "portability", "objection"] as const;
type DsarType = (typeof DSAR_TYPES)[number];

const DSAR_STATUSES = ["open", "in_progress", "fulfilled", "rejected"] as const;

const DSAR_STATUS_COLORS: Record<string, string> = {
  open: "#F59E0B",
  in_progress: "#1A3A5C",
  fulfilled: "#008751",
  rejected: "#C0392B",
};

export default function DataProtection() {
  const { profileId, canEdit } = useCandidateProfile();
  const utils = trpc.useUtils();

  // ── Consent: record / withdraw / status lookup ─────────────────────────────
  const [consentForm, setConsentForm] = useState({
    subjectTable: "voter_registrations" as SubjectTable,
    subjectId: "",
    lawfulBasis: "consent" as LawfulBasis,
    purpose: "",
    consentMethod: "digital" as "verbal" | "written" | "digital",
    consentGranted: true,
    retentionUntil: "",
    notes: "",
  });
  const [withdrawId, setWithdrawId] = useState("");
  const [statusLookup, setStatusLookup] = useState({ subjectTable: "voter_registrations" as SubjectTable, subjectId: "" });

  const recordMut = trpc.dataProtection.recordConsent.useMutation({
    onSuccess: () => {
      toast.success("Consent record saved");
      utils.dataProtection.consentStatus.invalidate();
      utils.dataProtection.transparencyReport.invalidate();
    },
    onError: (e) => toast.error(e.message),
  });
  const withdrawMut = trpc.dataProtection.withdrawConsent.useMutation({
    onSuccess: () => {
      toast.success("Consent withdrawn");
      utils.dataProtection.consentStatus.invalidate();
      utils.dataProtection.transparencyReport.invalidate();
    },
    onError: (e) => toast.error(e.message),
  });
  const statusQuery = trpc.dataProtection.consentStatus.useQuery(
    {
      profileId: profileId!,
      subjectTable: statusLookup.subjectTable,
      subjectId: parseInt(statusLookup.subjectId || "0", 10),
    },
    { enabled: !!profileId && /^\d+$/.test(statusLookup.subjectId) }
  );

  // ── Provenance lookup ───────────────────────────────────────────────────────
  const [provLookup, setProvLookup] = useState({ subjectTable: "voter_registrations" as SubjectTable, subjectId: "" });
  const provQuery = trpc.dataProtection.provenance.useQuery(
    {
      profileId: profileId!,
      subjectTable: provLookup.subjectTable,
      subjectId: parseInt(provLookup.subjectId || "0", 10),
    },
    { enabled: !!profileId && /^\d+$/.test(provLookup.subjectId) }
  );

  // ── DSAR ────────────────────────────────────────────────────────────────────
  const [dsarForm, setDsarForm] = useState({
    requestType: "access" as DsarType,
    subjectName: "",
    subjectContact: "",
    subjectTable: "" as "" | SubjectTable,
    subjectId: "",
    notes: "",
  });
  const [dsarFilter, setDsarFilter] = useState<string>("all");
  const [rejectingId, setRejectingId] = useState<number | null>(null);
  const [rejectReason, setRejectReason] = useState("");

  const dsarsQuery = trpc.dataProtection.listDsars.useQuery(
    { profileId: profileId!, status: dsarFilter === "all" ? undefined : dsarFilter },
    { enabled: !!profileId }
  );
  const fileMut = trpc.dataProtection.fileDsar.useMutation({
    onSuccess: () => {
      toast.success("DSAR filed — 30-day response deadline tracked");
      utils.dataProtection.listDsars.invalidate();
      utils.dataProtection.transparencyReport.invalidate();
      setDsarForm({ requestType: "access", subjectName: "", subjectContact: "", subjectTable: "", subjectId: "", notes: "" });
    },
    onError: (e) => toast.error(e.message),
  });
  const resolveMut = trpc.dataProtection.resolveDsar.useMutation({
    onSuccess: (_r, vars) => {
      toast.success(vars.action === "fulfill" ? "DSAR fulfilled" : "DSAR rejected");
      utils.dataProtection.listDsars.invalidate();
      utils.dataProtection.transparencyReport.invalidate();
      setRejectingId(null);
      setRejectReason("");
    },
    onError: (e) => toast.error(e.message),
  });

  // ── Transparency report ─────────────────────────────────────────────────────
  const reportQuery = trpc.dataProtection.transparencyReport.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId }
  );
  const report = reportQuery.data;

  const cardCls = "bg-white border border-gray-200 rounded p-5";
  const labelCls = "text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1 block";

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <ShieldCheck size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Data Protection</h1>
        </div>
        {!canEdit && (
          <Badge variant="outline" className="text-white/70 border-white/40">Read-only — manager role required to make changes</Badge>
        )}
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8">
        <Tabs defaultValue="consent">
          <TabsList className="mb-6">
            <TabsTrigger value="consent">Consent</TabsTrigger>
            <TabsTrigger value="provenance">Provenance</TabsTrigger>
            <TabsTrigger value="dsar">DSAR</TabsTrigger>
            <TabsTrigger value="transparency">Transparency</TabsTrigger>
          </TabsList>

          {/* ── Consent ──────────────────────────────────────────────────── */}
          <TabsContent value="consent">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              <div className={cardCls} style={{ borderTop: "3px solid #008751" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Record Consent</p>
                <div className="grid gap-3">
                  <div>
                    <span className={labelCls}>Subject Table *</span>
                    <Select value={consentForm.subjectTable} onValueChange={(v) => setConsentForm((f) => ({ ...f, subjectTable: v as SubjectTable }))}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>{SUBJECT_TABLES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                    </Select>
                  </div>
                  <div>
                    <span className={labelCls}>Subject ID *</span>
                    <Input type="number" min={1} placeholder="Row id in the subject table" value={consentForm.subjectId}
                      onChange={(e) => setConsentForm((f) => ({ ...f, subjectId: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Lawful Basis *</span>
                    <Select value={consentForm.lawfulBasis} onValueChange={(v) => setConsentForm((f) => ({ ...f, lawfulBasis: v as LawfulBasis }))}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>{LAWFUL_BASES.map((b) => <SelectItem key={b} value={b}>{b.replace(/_/g, " ")}</SelectItem>)}</SelectContent>
                    </Select>
                  </div>
                  <div>
                    <span className={labelCls}>Purpose *</span>
                    <Input placeholder="e.g. Voter outreach calls" maxLength={120} value={consentForm.purpose}
                      onChange={(e) => setConsentForm((f) => ({ ...f, purpose: e.target.value }))} />
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <span className={labelCls}>Method</span>
                      <Select value={consentForm.consentMethod} onValueChange={(v) => setConsentForm((f) => ({ ...f, consentMethod: v as "verbal" | "written" | "digital" }))}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="verbal">Verbal</SelectItem>
                          <SelectItem value="written">Written</SelectItem>
                          <SelectItem value="digital">Digital</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div>
                      <span className={labelCls}>Retention Until</span>
                      <Input type="date" value={consentForm.retentionUntil}
                        onChange={(e) => setConsentForm((f) => ({ ...f, retentionUntil: e.target.value }))} />
                    </div>
                  </div>
                  <label className="flex items-center gap-2 text-sm text-gray-700">
                    <input type="checkbox" checked={consentForm.consentGranted}
                      onChange={(e) => setConsentForm((f) => ({ ...f, consentGranted: e.target.checked }))} />
                    Consent granted
                  </label>
                  <div>
                    <span className={labelCls}>Notes</span>
                    <Input placeholder="Optional" value={consentForm.notes}
                      onChange={(e) => setConsentForm((f) => ({ ...f, notes: e.target.value }))} />
                  </div>
                  <Button
                    disabled={!canEdit || recordMut.isPending}
                    style={{ background: "#008751", color: "white" }}
                    onClick={() => {
                      if (!profileId) return;
                      if (!/^\d+$/.test(consentForm.subjectId)) return toast.error("Subject ID must be a number");
                      if (!consentForm.purpose.trim()) return toast.error("Purpose is required");
                      recordMut.mutate({
                        profileId,
                        subjectTable: consentForm.subjectTable,
                        subjectId: parseInt(consentForm.subjectId, 10),
                        lawfulBasis: consentForm.lawfulBasis,
                        purpose: consentForm.purpose.trim(),
                        consentMethod: consentForm.consentMethod,
                        consentGranted: consentForm.consentGranted,
                        retentionUntil: consentForm.retentionUntil || undefined,
                        notes: consentForm.notes || undefined,
                      });
                    }}
                  >
                    {recordMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Save Consent Record"}
                  </Button>
                </div>

                <div className="mt-6 pt-4 border-t border-gray-100">
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Withdraw Consent</p>
                  <div className="flex gap-2">
                    <Input type="number" min={1} placeholder="Consent record ID" value={withdrawId}
                      onChange={(e) => setWithdrawId(e.target.value)} />
                    <Button
                      variant="outline"
                      className="gap-1.5 text-red-700 border-red-300 hover:bg-red-50"
                      disabled={!canEdit || withdrawMut.isPending || !/^\d+$/.test(withdrawId)}
                      onClick={() => profileId && withdrawMut.mutate({ profileId, consentId: parseInt(withdrawId, 10) })}
                    >
                      {withdrawMut.isPending ? <Loader2 size={14} className="animate-spin" /> : <><Trash2 size={13} /> Withdraw</>}
                    </Button>
                  </div>
                </div>
              </div>

              <div className={cardCls} style={{ borderTop: "3px solid #1A3A5C" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Consent Status Lookup</p>
                <div className="flex gap-2 mb-4">
                  <Select value={statusLookup.subjectTable} onValueChange={(v) => setStatusLookup((l) => ({ ...l, subjectTable: v as SubjectTable }))}>
                    <SelectTrigger className="w-56"><SelectValue /></SelectTrigger>
                    <SelectContent>{SUBJECT_TABLES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                  </Select>
                  <Input type="number" min={1} placeholder="Subject ID" value={statusLookup.subjectId}
                    onChange={(e) => setStatusLookup((l) => ({ ...l, subjectId: e.target.value }))} />
                </div>
                {!/^\d+$/.test(statusLookup.subjectId) ? (
                  <p className="text-sm text-gray-400 flex items-center gap-2"><Search size={14} /> Enter a subject ID to look up consent records.</p>
                ) : statusQuery.isLoading ? (
                  <div className="flex justify-center py-8"><Loader2 size={24} className="animate-spin text-gray-400" /></div>
                ) : (statusQuery.data ?? []).length === 0 ? (
                  <p className="text-sm text-gray-500">No consent records found for this subject.</p>
                ) : (
                  <div className="space-y-2">
                    {(statusQuery.data ?? []).map((c) => (
                      <div key={c.id} className="border border-gray-200 rounded p-3 text-sm">
                        <div className="flex items-center gap-2 flex-wrap mb-1">
                          <Badge style={{
                            background: (c.withdrawnAt ? "#C0392B" : c.consentGranted ? "#008751" : "#6b7280") + "22",
                            color: c.withdrawnAt ? "#C0392B" : c.consentGranted ? "#008751" : "#6b7280",
                          }}>
                            {c.withdrawnAt ? "WITHDRAWN" : c.consentGranted ? "ACTIVE" : "NOT GRANTED"}
                          </Badge>
                          <span className="text-xs text-gray-500">Record #{c.id}</span>
                          <span className="text-xs text-gray-500">{c.lawfulBasis.replace(/_/g, " ")}</span>
                        </div>
                        <p className="text-gray-800">{c.purpose}</p>
                        <p className="text-xs text-gray-500 mt-1">
                          {c.consentMethod ? `Method: ${c.consentMethod} · ` : ""}
                          {c.consentedAt ? `Consented: ${new Date(c.consentedAt).toLocaleDateString("en-NG")} · ` : ""}
                          {c.withdrawnAt ? `Withdrawn: ${new Date(c.withdrawnAt).toLocaleDateString("en-NG")} · ` : ""}
                          {c.retentionUntil ? `Retention until: ${c.retentionUntil}` : ""}
                        </p>
                        {c.notes && <p className="text-xs text-gray-500 mt-1">{c.notes}</p>}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </TabsContent>

          {/* ── Provenance ───────────────────────────────────────────────── */}
          <TabsContent value="provenance">
            <div className={cardCls} style={{ borderTop: "3px solid #4A1525" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Data Provenance Lookup</p>
              <p className="text-xs text-gray-400 mb-4">Append-only origin records: where each personal-data row came from, who collected it, and under which lawful basis.</p>
              <div className="flex gap-2 mb-4 flex-wrap">
                <Select value={provLookup.subjectTable} onValueChange={(v) => setProvLookup((l) => ({ ...l, subjectTable: v as SubjectTable }))}>
                  <SelectTrigger className="w-56"><SelectValue /></SelectTrigger>
                  <SelectContent>{SUBJECT_TABLES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                </Select>
                <Input type="number" min={1} placeholder="Subject ID" className="w-40" value={provLookup.subjectId}
                  onChange={(e) => setProvLookup((l) => ({ ...l, subjectId: e.target.value }))} />
              </div>
              {!/^\d+$/.test(provLookup.subjectId) ? (
                <p className="text-sm text-gray-400 flex items-center gap-2"><Search size={14} /> Enter a subject ID to view provenance.</p>
              ) : provQuery.isLoading ? (
                <div className="flex justify-center py-8"><Loader2 size={24} className="animate-spin text-gray-400" /></div>
              ) : (provQuery.data ?? []).length === 0 ? (
                <p className="text-sm text-gray-500">No provenance entries recorded for this subject.</p>
              ) : (
                <div className="space-y-2">
                  {(provQuery.data ?? []).map((p) => (
                    <div key={p.id} className="border border-gray-200 rounded p-3 text-sm flex items-start gap-3">
                      <FileCheck size={16} className="text-gray-400 mt-0.5 flex-shrink-0" />
                      <div>
                        <div className="flex items-center gap-2 flex-wrap">
                          <Badge variant="outline">{p.source}</Badge>
                          <span className="text-xs text-gray-500">{p.lawfulBasis.replace(/_/g, " ")}</span>
                        </div>
                        <p className="text-xs text-gray-500 mt-1">
                          Collected {new Date(p.collectedAt).toLocaleString("en-NG")}
                          {p.collectedBy ? ` by ${p.collectedBy}` : ""}
                        </p>
                        {p.notes && <p className="text-xs text-gray-500 mt-1">{p.notes}</p>}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          {/* ── DSAR ─────────────────────────────────────────────────────── */}
          <TabsContent value="dsar">
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
              <div className={cardCls} style={{ borderTop: "3px solid #C0392B" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">File a DSAR</p>
                <p className="text-xs text-gray-400 mb-4">NDPA data-subject request. A 30-day response deadline is tracked automatically.</p>
                <div className="grid gap-3">
                  <div>
                    <span className={labelCls}>Request Type *</span>
                    <Select value={dsarForm.requestType} onValueChange={(v) => setDsarForm((f) => ({ ...f, requestType: v as DsarType }))}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>{DSAR_TYPES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                    </Select>
                  </div>
                  <div>
                    <span className={labelCls}>Subject Name *</span>
                    <Input placeholder="Full name of the data subject" value={dsarForm.subjectName}
                      onChange={(e) => setDsarForm((f) => ({ ...f, subjectName: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Subject Contact</span>
                    <Input placeholder="Phone or email (optional)" value={dsarForm.subjectContact}
                      onChange={(e) => setDsarForm((f) => ({ ...f, subjectContact: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Subject Table</span>
                    <Select value={dsarForm.subjectTable || "none"} onValueChange={(v) => setDsarForm((f) => ({ ...f, subjectTable: v === "none" ? "" : v as SubjectTable }))}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">Not specified</SelectItem>
                        {SUBJECT_TABLES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </div>
                  {dsarForm.subjectTable && (
                    <div>
                      <span className={labelCls}>Subject ID</span>
                      <Input type="number" min={1} placeholder="Row id (optional)" value={dsarForm.subjectId}
                        onChange={(e) => setDsarForm((f) => ({ ...f, subjectId: e.target.value }))} />
                    </div>
                  )}
                  <div>
                    <span className={labelCls}>Notes</span>
                    <Input placeholder="Optional" value={dsarForm.notes}
                      onChange={(e) => setDsarForm((f) => ({ ...f, notes: e.target.value }))} />
                  </div>
                  <Button
                    disabled={!canEdit || fileMut.isPending}
                    style={{ background: "#C0392B", color: "white" }}
                    onClick={() => {
                      if (!profileId) return;
                      if (!dsarForm.subjectName.trim()) return toast.error("Subject name is required");
                      if (dsarForm.subjectId && !/^\d+$/.test(dsarForm.subjectId)) return toast.error("Subject ID must be a number");
                      fileMut.mutate({
                        profileId,
                        requestType: dsarForm.requestType,
                        subjectName: dsarForm.subjectName.trim(),
                        subjectContact: dsarForm.subjectContact || undefined,
                        subjectTable: dsarForm.subjectTable || undefined,
                        subjectId: dsarForm.subjectId ? parseInt(dsarForm.subjectId, 10) : undefined,
                        notes: dsarForm.notes || undefined,
                      });
                    }}
                  >
                    {fileMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "File DSAR"}
                  </Button>
                </div>
              </div>

              <div className={`${cardCls} lg:col-span-2`} style={{ borderTop: "3px solid #1A3A5C" }}>
                <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500">DSAR Register</p>
                  <Select value={dsarFilter} onValueChange={setDsarFilter}>
                    <SelectTrigger className="h-8 text-xs w-36"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All statuses</SelectItem>
                      {DSAR_STATUSES.map((s) => <SelectItem key={s} value={s}>{s.replace(/_/g, " ")}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                {dsarsQuery.isLoading ? (
                  <div className="flex justify-center py-12"><Loader2 size={24} className="animate-spin text-gray-400" /></div>
                ) : (dsarsQuery.data ?? []).length === 0 ? (
                  <p className="text-sm text-gray-500 py-8 text-center">No data-subject requests{dsarFilter !== "all" ? ` with status "${dsarFilter.replace(/_/g, " ")}"` : ""} on record.</p>
                ) : (
                  <div className="space-y-2">
                    {(dsarsQuery.data ?? []).map((d) => (
                      <div key={d.id} className="border border-gray-200 rounded p-3 text-sm">
                        <div className="flex items-center gap-2 flex-wrap mb-1">
                          <Badge style={{ background: (DSAR_STATUS_COLORS[d.status] ?? "#6b7280") + "22", color: DSAR_STATUS_COLORS[d.status] ?? "#6b7280" }}>
                            {d.status.replace(/_/g, " ").toUpperCase()}
                          </Badge>
                          <span className="font-medium text-gray-900">{d.subjectName}</span>
                          <span className="text-xs text-gray-500 capitalize">{d.requestType}</span>
                          <span className="text-xs text-gray-400 ml-auto">#{d.id}</span>
                        </div>
                        <p className="text-xs text-gray-500">
                          Received {new Date(d.receivedAt).toLocaleDateString("en-NG")} · Due {d.dueAt}
                          {d.fulfilledAt ? ` · Fulfilled ${new Date(d.fulfilledAt).toLocaleDateString("en-NG")}` : ""}
                          {d.subjectTable ? ` · ${d.subjectTable}${d.subjectId != null ? ` #${d.subjectId}` : ""}` : ""}
                        </p>
                        {d.rejectionReason && <p className="text-xs text-red-700 mt-1">Rejection reason: {d.rejectionReason}</p>}
                        {d.notes && <p className="text-xs text-gray-500 mt-1">{d.notes}</p>}
                        {canEdit && (d.status === "open" || d.status === "in_progress") && (
                          <div className="mt-2 pt-2 border-t border-gray-100">
                            {rejectingId === d.id ? (
                              <div className="flex gap-2 items-center flex-wrap">
                                <Input
                                  placeholder="Rejection reason (required)"
                                  value={rejectReason}
                                  onChange={(e) => setRejectReason(e.target.value)}
                                  className="flex-1 min-w-48 h-8 text-xs"
                                />
                                <Button
                                  size="sm"
                                  variant="outline"
                                  className="text-red-700 border-red-300 h-8"
                                  disabled={resolveMut.isPending || !rejectReason.trim()}
                                  onClick={() => profileId && resolveMut.mutate({ profileId, dsarId: d.id, action: "reject", rejectionReason: rejectReason.trim() })}
                                >
                                  Confirm Reject
                                </Button>
                                <Button size="sm" variant="ghost" className="h-8" onClick={() => { setRejectingId(null); setRejectReason(""); }}>Cancel</Button>
                              </div>
                            ) : (
                              <div className="flex gap-2">
                                <Button
                                  size="sm"
                                  variant="outline"
                                  className="gap-1 text-green-700 border-green-300 h-8"
                                  disabled={resolveMut.isPending}
                                  onClick={() => {
                                    if (!profileId) return;
                                    if (d.requestType === "erasure" && !window.confirm("Fulfilling an erasure request permanently deletes the subject row. Continue?")) return;
                                    resolveMut.mutate({ profileId, dsarId: d.id, action: "fulfill" });
                                  }}
                                >
                                  <CheckCircle size={13} /> Fulfill
                                </Button>
                                <Button
                                  size="sm"
                                  variant="outline"
                                  className="gap-1 text-red-700 border-red-300 h-8"
                                  onClick={() => { setRejectingId(d.id); setRejectReason(""); }}
                                >
                                  <XCircle size={13} /> Reject
                                </Button>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </TabsContent>

          {/* ── Transparency ─────────────────────────────────────────────── */}
          <TabsContent value="transparency">
            {reportQuery.isLoading ? (
              <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
            ) : !report ? (
              <div className={`${cardCls} text-center text-gray-500 py-12`}>
                Transparency report unavailable — the compliance tables are not reachable.
              </div>
            ) : (
              <div className="space-y-6">
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                  {[
                    { label: "Voter Registrations", value: report.records.voterRegistrations, color: "#1A3A5C" },
                    { label: "Active Consents", value: report.consent.active, color: "#008751" },
                    { label: "Withdrawn Consents", value: report.consent.withdrawn, color: "#C0392B" },
                    { label: "Access Audit Entries", value: report.accessAuditEntries, color: "#4A1525" },
                  ].map((k) => (
                    <div key={k.label} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: `3px solid ${k.color}` }}>
                      <p className="text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1">{k.label}</p>
                      <p className="font-mono text-2xl font-bold" style={{ color: k.color }}>{k.value}</p>
                    </div>
                  ))}
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
                  <div className={cardCls} style={{ borderTop: "3px solid #008751" }}>
                    <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Consent Coverage</p>
                    <div className="space-y-1.5 text-sm">
                      <div className="flex justify-between"><span className="text-gray-600">Total consent records</span><span className="font-mono font-bold">{report.consent.totalRecords}</span></div>
                      <div className="flex justify-between"><span className="text-gray-600">Active</span><span className="font-mono font-bold text-green-700">{report.consent.active}</span></div>
                      <div className="flex justify-between"><span className="text-gray-600">Withdrawn</span><span className="font-mono font-bold text-red-700">{report.consent.withdrawn}</span></div>
                      <div className="flex justify-between">
                        <span className="text-gray-600">Voter consent coverage</span>
                        <span className="font-mono font-bold">
                          {report.consent.voterConsentCoveragePct != null ? `${report.consent.voterConsentCoveragePct}%` : "—"}
                        </span>
                      </div>
                      {report.consent.voterConsentCoveragePct == null && (
                        <p className="text-xs text-gray-400">Coverage unavailable — no voter registrations on record.</p>
                      )}
                    </div>
                  </div>

                  <div className={cardCls} style={{ borderTop: "3px solid #C0392B" }}>
                    <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">DSAR Summary</p>
                    {report.dsar.byStatus.length === 0 ? (
                      <p className="text-sm text-gray-500">No DSARs filed.</p>
                    ) : (
                      <div className="space-y-1.5 text-sm">
                        {report.dsar.byStatus.map((s) => (
                          <div key={s.status} className="flex justify-between">
                            <span className="text-gray-600 capitalize">{s.status.replace(/_/g, " ")}</span>
                            <span className="font-mono font-bold">{s.count}</span>
                          </div>
                        ))}
                      </div>
                    )}
                    <div className="flex justify-between text-sm mt-2 pt-2 border-t border-gray-100">
                      <span className="text-gray-600">Overdue open requests</span>
                      <span className={`font-mono font-bold ${report.dsar.overdueOpen > 0 ? "text-red-700" : "text-green-700"}`}>{report.dsar.overdueOpen}</span>
                    </div>
                  </div>
                </div>

                <div className={cardCls} style={{ borderTop: "3px solid #1A3A5C" }}>
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Provenance by Source</p>
                  {report.provenanceBySource.length === 0 ? (
                    <p className="text-sm text-gray-500">No provenance entries recorded yet.</p>
                  ) : (
                    <div className="space-y-1.5 text-sm">
                      {report.provenanceBySource.map((p) => (
                        <div key={p.source} className="flex justify-between">
                          <span className="text-gray-600">{p.source}</span>
                          <span className="font-mono font-bold">{p.count}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <p className="text-xs text-gray-400">
                  {report.note} Generated {new Date(report.generatedAt).toLocaleString("en-NG")}.
                </p>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
