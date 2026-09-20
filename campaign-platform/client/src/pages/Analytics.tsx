/**
 * Analytics — consented survey panel + message experiments.
 * Lawful analogue of psychographic micro-targeting: data is captured ONLY
 * from consented panelists and the campaign's own audiences. Nothing here is
 * simulated — created tests/variants and recorded counts are exactly what the
 * analytics tRPC router returns. There is no server-side list endpoint, so
 * only tests created in this session are shown, clearly labelled as such.
 */
import { useState } from "react";
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { ArrowLeft, BarChart2, Loader2, Plus, Trash2, FlaskConical, Users } from "lucide-react";

interface CreatedVariant { id: number; testId: number; label: string; body: string }
interface CreatedTest { id: number; name: string; channel: string | null }
interface CreatedEntry { test: CreatedTest; variants: CreatedVariant[] }

const EVENT_TYPES = ["impression", "response", "conversion"] as const;
type EventType = (typeof EVENT_TYPES)[number];

const cardCls = "bg-white border border-gray-200 rounded p-5";
const labelCls = "text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1 block";

export default function Analytics() {
  const { profileId, canEdit } = useCandidateProfile();

  // ── Panel enrollment ────────────────────────────────────────────────────────
  const [enrollForm, setEnrollForm] = useState({
    consentId: "", fullName: "", stateCode: "", lga: "", ward: "", ageBand: "", gender: "",
  });
  const [enrolled, setEnrolled] = useState<Array<{ id: number; fullName: string }>>([]);
  const enrollMut = trpc.analytics.enrollPanelist.useMutation({
    onSuccess: (row) => {
      if (row) {
        setEnrolled((prev) => [{ id: row.id, fullName: row.fullName }, ...prev]);
        toast.success(`Panelist enrolled (id #${row.id})`);
        setEnrollForm({ consentId: "", fullName: "", stateCode: "", lga: "", ward: "", ageBand: "", gender: "" });
      } else {
        toast.success("Panelist enrolled");
      }
    },
    onError: (e) => toast.error(e.message),
  });

  // ── Instrument responses ────────────────────────────────────────────────────
  const [respPanelistId, setRespPanelistId] = useState("");
  const [instrument, setInstrument] = useState("OCEAN20");
  const [rows, setRows] = useState<Array<{ itemKey: string; score: number }>>([{ itemKey: "", score: 3 }]);
  const respMut = trpc.analytics.recordResponses.useMutation({
    onSuccess: (r) => {
      toast.success(`${r?.inserted ?? 0} response(s) recorded`);
      setRows([{ itemKey: "", score: 3 }]);
    },
    onError: (e) => toast.error(e.message),
  });

  // ── Message tests ───────────────────────────────────────────────────────────
  const [testName, setTestName] = useState("");
  const [testChannel, setTestChannel] = useState("");
  const [variants, setVariants] = useState<Array<{ label: string; body: string }>>([
    { label: "A", body: "" },
    { label: "B", body: "" },
  ]);
  const [created, setCreated] = useState<CreatedEntry[]>([]);
  const createMut = trpc.analytics.createMessageTest.useMutation({
    onSuccess: (r) => {
      if (r) {
        setCreated((prev) => [{ test: r.test as CreatedTest, variants: r.variants as CreatedVariant[] }, ...prev]);
        toast.success(`Message test "${r.test.name}" created with ${r.variants.length} variants`);
        setTestName("");
        setTestChannel("");
        setVariants([{ label: "A", body: "" }, { label: "B", body: "" }]);
      }
    },
    onError: (e) => toast.error(e.message),
  });
  const [eventForm, setEventForm] = useState<{ variantId: string; eventType: EventType }>({ variantId: "", eventType: "impression" });
  const eventMut = trpc.analytics.recordEvent.useMutation({
    onSuccess: () => toast.success("Event recorded"),
    onError: (e) => toast.error(e.message),
  });

  const allVariants = created.flatMap((c) => c.variants.map((v) => ({ ...v, testName: c.test.name })));

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <BarChart2 size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Campaign Analytics</h1>
        </div>
        {!canEdit && (
          <Badge variant="outline" className="text-white/70 border-white/40">Read-only — manager role required to make changes</Badge>
        )}
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8 space-y-8">
        {/* ── Consented Panel ─────────────────────────────────────────────── */}
        <section>
          <h2 className="text-sm font-bold uppercase tracking-widest text-gray-500 mb-3 flex items-center gap-2">
            <Users size={15} /> Consented Survey Panel
          </h2>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className={cardCls} style={{ borderTop: "3px solid #008751" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Enroll Panelist</p>
              <p className="text-xs text-gray-400 mb-4">
                An <strong>active consent record id</strong> is required (see Data Protection → Consent).
                Enrollment is refused if consent is missing, not granted, or withdrawn — NDPA 2023.
              </p>
              <div className="grid gap-3">
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <span className={labelCls}>Consent Record ID *</span>
                    <Input type="number" min={1} placeholder="e.g. 12" value={enrollForm.consentId}
                      onChange={(e) => setEnrollForm((f) => ({ ...f, consentId: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Full Name *</span>
                    <Input placeholder="Panelist name" value={enrollForm.fullName}
                      onChange={(e) => setEnrollForm((f) => ({ ...f, fullName: e.target.value }))} />
                  </div>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <div>
                    <span className={labelCls}>State Code</span>
                    <Input placeholder="e.g. LAG" maxLength={10} value={enrollForm.stateCode}
                      onChange={(e) => setEnrollForm((f) => ({ ...f, stateCode: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>LGA</span>
                    <Input value={enrollForm.lga} onChange={(e) => setEnrollForm((f) => ({ ...f, lga: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Ward</span>
                    <Input value={enrollForm.ward} onChange={(e) => setEnrollForm((f) => ({ ...f, ward: e.target.value }))} />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <span className={labelCls}>Age Band</span>
                    <Input placeholder="e.g. 25-34" maxLength={10} value={enrollForm.ageBand}
                      onChange={(e) => setEnrollForm((f) => ({ ...f, ageBand: e.target.value }))} />
                  </div>
                  <div>
                    <span className={labelCls}>Gender</span>
                    <Input maxLength={20} value={enrollForm.gender}
                      onChange={(e) => setEnrollForm((f) => ({ ...f, gender: e.target.value }))} />
                  </div>
                </div>
                <Button
                  disabled={!canEdit || enrollMut.isPending}
                  style={{ background: "#008751", color: "white" }}
                  onClick={() => {
                    if (!profileId) return;
                    if (!/^\d+$/.test(enrollForm.consentId)) return toast.error("A numeric consent record id is required");
                    if (!enrollForm.fullName.trim()) return toast.error("Full name is required");
                    enrollMut.mutate({
                      profileId,
                      consentId: parseInt(enrollForm.consentId, 10),
                      fullName: enrollForm.fullName.trim(),
                      stateCode: enrollForm.stateCode || undefined,
                      lga: enrollForm.lga || undefined,
                      ward: enrollForm.ward || undefined,
                      ageBand: enrollForm.ageBand || undefined,
                      gender: enrollForm.gender || undefined,
                    });
                  }}
                >
                  {enrollMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Enroll Panelist"}
                </Button>
                {enrolled.length > 0 && (
                  <div className="mt-2 pt-3 border-t border-gray-100">
                    <p className="text-xs font-semibold text-gray-500 mb-2">Enrolled this session (from API responses):</p>
                    <div className="space-y-1">
                      {enrolled.map((p) => (
                        <p key={p.id} className="text-sm text-gray-700">#{p.id} — {p.fullName}</p>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </div>

            <div className={cardCls} style={{ borderTop: "3px solid #1A3A5C" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Record Instrument Responses</p>
              <p className="text-xs text-gray-400 mb-4">
                Likert 1–5 item responses (e.g. OCEAN20 items such as E1, N3r). These are the only lawful input to trait scoring.
              </p>
              <div className="grid gap-3">
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <span className={labelCls}>Panelist ID *</span>
                    <Input type="number" min={1} placeholder="Panelist id" value={respPanelistId}
                      onChange={(e) => setRespPanelistId(e.target.value)} />
                  </div>
                  <div>
                    <span className={labelCls}>Instrument *</span>
                    <Input maxLength={40} value={instrument} onChange={(e) => setInstrument(e.target.value)} />
                  </div>
                </div>
                <div className="space-y-2">
                  {rows.map((r, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Input
                        placeholder="Item key (e.g. E1)"
                        maxLength={20}
                        className="w-40"
                        value={r.itemKey}
                        onChange={(e) => setRows((prev) => prev.map((x, j) => (j === i ? { ...x, itemKey: e.target.value } : x)))}
                      />
                      <Select
                        value={String(r.score)}
                        onValueChange={(v) => setRows((prev) => prev.map((x, j) => (j === i ? { ...x, score: parseInt(v, 10) } : x)))}
                      >
                        <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
                        <SelectContent>{[1, 2, 3, 4, 5].map((s) => <SelectItem key={s} value={String(s)}>{s}</SelectItem>)}</SelectContent>
                      </Select>
                      <button
                        type="button"
                        className="text-gray-300 hover:text-red-500 transition-colors disabled:opacity-30"
                        disabled={rows.length <= 1}
                        onClick={() => setRows((prev) => prev.filter((_, j) => j !== i))}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="gap-1"
                    onClick={() => setRows((prev) => [...prev, { itemKey: "", score: 3 }])}
                  >
                    <Plus size={13} /> Add Row
                  </Button>
                  <Button
                    disabled={!canEdit || respMut.isPending}
                    style={{ background: "#1A3A5C", color: "white" }}
                    onClick={() => {
                      if (!profileId) return;
                      if (!/^\d+$/.test(respPanelistId)) return toast.error("A numeric panelist id is required");
                      if (!instrument.trim()) return toast.error("Instrument is required");
                      const valid = rows.filter((r) => r.itemKey.trim());
                      if (valid.length === 0) return toast.error("At least one item key is required");
                      respMut.mutate({
                        profileId,
                        panelistId: parseInt(respPanelistId, 10),
                        instrument: instrument.trim(),
                        responses: valid.map((r) => ({ itemKey: r.itemKey.trim(), score: r.score })),
                      });
                    }}
                  >
                    {respMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Record Responses"}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* ── Message Tests ───────────────────────────────────────────────── */}
        <section>
          <h2 className="text-sm font-bold uppercase tracking-widest text-gray-500 mb-3 flex items-center gap-2">
            <FlaskConical size={15} /> Message Tests
          </h2>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className={cardCls} style={{ borderTop: "3px solid #4A1525" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Create Message Test</p>
              <p className="text-xs text-gray-400 mb-4">A/B experiment on the campaign's own consented audiences — 2 to 8 variants.</p>
              <div className="grid gap-3">
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <span className={labelCls}>Test Name *</span>
                    <Input placeholder="e.g. Economy message v1" value={testName} onChange={(e) => setTestName(e.target.value)} />
                  </div>
                  <div>
                    <span className={labelCls}>Channel</span>
                    <Input placeholder="e.g. sms, whatsapp" maxLength={40} value={testChannel} onChange={(e) => setTestChannel(e.target.value)} />
                  </div>
                </div>
                <div className="space-y-2">
                  {variants.map((v, i) => (
                    <div key={i} className="border border-gray-200 rounded p-2.5 grid gap-2">
                      <div className="flex items-center gap-2">
                        <Input
                          placeholder="Label *"
                          maxLength={40}
                          className="w-32"
                          value={v.label}
                          onChange={(e) => setVariants((prev) => prev.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))}
                        />
                        <button
                          type="button"
                          className="ml-auto text-gray-300 hover:text-red-500 transition-colors disabled:opacity-30"
                          disabled={variants.length <= 2}
                          onClick={() => setVariants((prev) => prev.filter((_, j) => j !== i))}
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                      <Input
                        placeholder="Message body *"
                        value={v.body}
                        onChange={(e) => setVariants((prev) => prev.map((x, j) => (j === i ? { ...x, body: e.target.value } : x)))}
                      />
                    </div>
                  ))}
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="gap-1"
                    disabled={variants.length >= 8}
                    onClick={() => setVariants((prev) => [...prev, { label: String.fromCharCode(65 + prev.length), body: "" }])}
                  >
                    <Plus size={13} /> Add Variant
                  </Button>
                  <Button
                    disabled={!canEdit || createMut.isPending}
                    style={{ background: "#4A1525", color: "white" }}
                    onClick={() => {
                      if (!profileId) return;
                      if (!testName.trim()) return toast.error("Test name is required");
                      if (variants.some((v) => !v.label.trim() || !v.body.trim())) return toast.error("Every variant needs a label and a body");
                      createMut.mutate({
                        profileId,
                        name: testName.trim(),
                        channel: testChannel || undefined,
                        variants: variants.map((v) => ({ label: v.label.trim(), body: v.body.trim() })),
                      });
                    }}
                  >
                    {createMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Create Test"}
                  </Button>
                </div>
              </div>
            </div>

            <div className={cardCls} style={{ borderTop: "3px solid #C0392B" }}>
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">Record Events</p>
              <p className="text-xs text-gray-400 mb-4">
                Log a real impression / response / conversion against a variant id.
                The API exposes no list endpoint, so only tests created in this session appear below — no result charts are shown because no aggregate results endpoint exists.
              </p>
              <div className="flex gap-2 mb-4 flex-wrap">
                <Select value={eventForm.variantId} onValueChange={(v) => setEventForm((f) => ({ ...f, variantId: v }))}>
                  <SelectTrigger className="w-64"><SelectValue placeholder="Variant" /></SelectTrigger>
                  <SelectContent>
                    {allVariants.length === 0 ? (
                      <SelectItem value="none" disabled>No variants created this session</SelectItem>
                    ) : (
                      allVariants.map((v) => (
                        <SelectItem key={v.id} value={String(v.id)}>#{v.id} — {v.testName} / {v.label}</SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
                <Select value={eventForm.eventType} onValueChange={(v) => setEventForm((f) => ({ ...f, eventType: v as EventType }))}>
                  <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
                  <SelectContent>{EVENT_TYPES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                </Select>
                <Button
                  disabled={!canEdit || eventMut.isPending || !/^\d+$/.test(eventForm.variantId)}
                  style={{ background: "#C0392B", color: "white" }}
                  onClick={() => profileId && eventMut.mutate({ profileId, variantId: parseInt(eventForm.variantId, 10), eventType: eventForm.eventType })}
                >
                  {eventMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Record Event"}
                </Button>
              </div>

              {created.length === 0 ? (
                <p className="text-sm text-gray-500 py-6 text-center">No message tests created this session.</p>
              ) : (
                <div className="space-y-3">
                  {created.map((c) => (
                    <div key={c.test.id} className="border border-gray-200 rounded p-3">
                      <div className="flex items-center gap-2 mb-2 flex-wrap">
                        <span className="font-semibold text-sm text-gray-900">{c.test.name}</span>
                        {c.test.channel && <Badge variant="outline">{c.test.channel}</Badge>}
                        <span className="text-xs text-gray-400 ml-auto">Test #{c.test.id}</span>
                      </div>
                      <div className="space-y-1.5">
                        {c.variants.map((v) => (
                          <div key={v.id} className="text-sm flex items-start gap-2">
                            <Badge variant="outline" className="flex-shrink-0">{v.label}</Badge>
                            <div className="min-w-0">
                              <p className="text-gray-700 break-words">{v.body}</p>
                              <p className="text-xs text-gray-400">Variant #{v.id}</p>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
