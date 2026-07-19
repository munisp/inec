import { useState } from "react";
import { Link } from "wouter";
import { ArrowLeft, Download, Megaphone, Sparkles, Save, Loader2, FileText, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import jsPDF from "jspdf";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { Streamdown } from "streamdown";

const TEMPLATES = ["Policy Announcement","Campaign Rally","Response to Opponent","Endorsement Received","Election Result","General Statement"];
const TONES = ["professional and authoritative","urgent and passionate","calm and reassuring","bold and assertive","empathetic and community-focused"];

export default function PressReleaseGenerator() {
  const { profileId, profile } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: releases = [], isLoading } = trpc.pressRelease.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const saveMut = trpc.pressRelease.save.useMutation({
    onSuccess: () => { utils.pressRelease.list.invalidate(); toast.success("Press release saved"); setDraft(null); },
    onError: e => toast.error(e.message),
  });
  const aiDraftMut = trpc.pressRelease.aiDraft.useMutation({
    onSuccess: (data) => {
      setDraft({ title: data.title, body: data.content, template: selectedTemplate });
      toast.success("AI draft generated!");
    },
    onError: e => toast.error("AI draft failed: " + e.message),
  });

  const [draft, setDraft] = useState<{ title: string; body: string; template: string } | null>(null);
  const [selected, setSelected] = useState<typeof releases[0] | null>(null);
  const [selectedTemplate, setSelectedTemplate] = useState(TEMPLATES[0]);
  const [selectedTone, setSelectedTone] = useState(TONES[0]);
  const [aiHeadline, setAiHeadline] = useState("");
  const [aiKeyPoints, setAiKeyPoints] = useState("");
  const [showAiPanel, setShowAiPanel] = useState(false);

  const generateManualDraft = (template: string) => {
    const name = profile?.candidateName ?? "Our Candidate";
    const party = profile?.partyName ?? "Our Party";
    const state = profile?.stateName ?? "the State";
    const office = profile?.office ?? "Office";
    const bodies: Record<string, string> = {
      "Policy Announcement": `FOR IMMEDIATE RELEASE\n\n${name}, ${party} candidate for ${office} in ${state}, today announced a comprehensive policy initiative.\n\n"This policy represents our commitment to the people of ${state}," said ${name}.\n\nFor more information, contact the campaign media team.`,
      "Campaign Rally": `FOR IMMEDIATE RELEASE\n\n${party} Campaign Announces Major Rally in ${state}\n\n${name}'s campaign for ${office} will hold a major rally.\n\n"The momentum behind our campaign is unstoppable," said ${name}.\n\nFor more information, contact the campaign media team.`,
      "Response to Opponent": `FOR IMMEDIATE RELEASE\n\n${name} Campaign Responds to Opposition Claims\n\n"The claims made by our opponents are misleading," said ${name}.\n\nFor more information, contact the campaign media team.`,
      "Endorsement Received": `FOR IMMEDIATE RELEASE\n\n${name} Receives Major Endorsement\n\n"This endorsement reflects the broad coalition of support we have built," said ${name}.\n\nFor more information, contact the campaign media team.`,
      "Election Result": `FOR IMMEDIATE RELEASE\n\n${name} Campaign Statement on Election Results\n\nFor more information, contact the campaign media team.`,
      "General Statement": `FOR IMMEDIATE RELEASE\n\n${name} Issues Statement\n\n"[Insert statement here]"\n\nFor more information, contact the campaign media team.`,
    };
    setDraft({ title: `${template} — ${new Date().toLocaleDateString()}`, body: bodies[template] ?? "", template });
  };

  const handleAiDraft = () => {
    if (!profileId) return toast.error("No profile selected");
    if (!aiHeadline.trim()) return toast.error("Please enter a headline");
    if (!aiKeyPoints.trim()) return toast.error("Please enter key points");
    aiDraftMut.mutate({ profileId, template: selectedTemplate, headline: aiHeadline, keyPoints: aiKeyPoints, tone: selectedTone });
  };

  function handleExportPDF(release: { title: string; body: string; template: string; createdAt: Date | string }) {
    const doc = new jsPDF({ orientation: "portrait", unit: "mm", format: "a4" });
    const pageW = doc.internal.pageSize.getWidth();
    doc.setFillColor(74, 21, 37);
    doc.rect(0, 0, pageW, 22, "F");
    doc.setTextColor(255, 255, 255);
    doc.setFontSize(14);
    doc.setFont("helvetica", "bold");
    doc.text("PRESS RELEASE", 14, 10);
    doc.setFontSize(8);
    doc.setFont("helvetica", "normal");
    doc.text("INEC Campaign Platform", 14, 16);
    doc.text(new Date(release.createdAt).toLocaleDateString("en-NG", { year: "numeric", month: "long", day: "numeric" }), pageW - 14, 16, { align: "right" });
    doc.setFillColor(0, 135, 81);
    doc.rect(0, 22, pageW, 2, "F");
    doc.setTextColor(74, 21, 37);
    doc.setFontSize(16);
    doc.setFont("helvetica", "bold");
    const titleLines = doc.splitTextToSize(release.title, pageW - 28);
    doc.text(titleLines, 14, 34);
    doc.setFontSize(8);
    doc.setFont("helvetica", "normal");
    doc.setTextColor(100, 100, 100);
    doc.text(`Template: ${release.template}`, 14, 34 + titleLines.length * 7 + 4);
    const bodyY = 34 + titleLines.length * 7 + 12;
    doc.setDrawColor(200, 200, 200);
    doc.line(14, bodyY - 2, pageW - 14, bodyY - 2);
    doc.setTextColor(30, 30, 30);
    doc.setFontSize(10);
    doc.setFont("helvetica", "normal");
    const bodyLines = doc.splitTextToSize(release.body, pageW - 28);
    doc.text(bodyLines, 14, bodyY + 4);
    doc.setFillColor(245, 240, 235);
    doc.rect(0, 285, pageW, 12, "F");
    doc.setFontSize(7);
    doc.setTextColor(120, 120, 120);
    doc.text("Generated by INEC Campaign Platform — Confidential", 14, 292);
    doc.text(window.location.hostname, pageW - 14, 292, { align: "right" });
    doc.save(`${release.title.replace(/[^a-z0-9]/gi, "_").toLowerCase()}.pdf`);
    toast.success("PDF exported");
  }

  return (
    <div className="min-h-screen bg-[#F5F0EB]">
      <header className="bg-[#4A1525] px-4 sm:px-6 py-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Megaphone size={18} className="text-white"/>
          <h1 className="text-white font-bold text-base sm:text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Press Release Generator</h1>
        </div>
        <Badge variant="secondary" className="text-xs">{releases.length} saved</Badge>
      </header>

      <div className="max-w-6xl mx-auto px-4 sm:px-6 py-6 grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Left: Templates + AI Panel + Saved */}
        <div className="col-span-1 space-y-4">
          {/* Template Picker */}
          <div className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #4A1525" }}>
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Quick Templates</p>
            <div className="space-y-2">
              {TEMPLATES.map(t => (
                <button key={t} onClick={() => generateManualDraft(t)}
                  className="w-full text-left px-3 py-2 text-sm rounded hover:bg-gray-50 border border-gray-200 transition-colors">
                  {t}
                </button>
              ))}
            </div>
          </div>

          {/* AI Draft Panel */}
          <div className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #008751" }}>
            <button onClick={() => setShowAiPanel(v => !v)}
              className="w-full flex items-center justify-between text-xs font-bold uppercase tracking-widest text-gray-500 mb-1">
              <span className="flex items-center gap-1"><Sparkles size={12} className="text-[#008751]"/> AI Draft</span>
              <span className="text-gray-400">{showAiPanel ? "▲" : "▼"}</span>
            </button>
            {showAiPanel && (
              <div className="space-y-3 mt-3">
                <div>
                  <label className="text-xs text-gray-500 mb-1 block">Template Type</label>
                  <Select value={selectedTemplate} onValueChange={setSelectedTemplate}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue/></SelectTrigger>
                    <SelectContent>{TEMPLATES.map(t => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
                <div>
                  <label className="text-xs text-gray-500 mb-1 block">Tone</label>
                  <Select value={selectedTone} onValueChange={setSelectedTone}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue/></SelectTrigger>
                    <SelectContent>{TONES.map(t => <SelectItem key={t} value={t}>{t}</SelectItem>)}</SelectContent>
                  </Select>
                </div>
                <div>
                  <label className="text-xs text-gray-500 mb-1 block">Headline / Topic</label>
                  <Input value={aiHeadline} onChange={e => setAiHeadline(e.target.value)} placeholder="e.g. New Education Policy for Oyo State" className="h-8 text-xs"/>
                </div>
                <div>
                  <label className="text-xs text-gray-500 mb-1 block">Key Points (one per line)</label>
                  <Textarea value={aiKeyPoints} onChange={e => setAiKeyPoints(e.target.value)} placeholder="- Free school meals&#10;- 500 new classrooms&#10;- Teacher salary increase" className="text-xs min-h-[80px]"/>
                </div>
                <Button size="sm" className="w-full gap-1 bg-[#008751] hover:bg-[#006B40] text-white"
                  onClick={handleAiDraft} disabled={aiDraftMut.isPending}>
                  {aiDraftMut.isPending ? <><Loader2 size={12} className="animate-spin"/> Drafting…</> : <><Sparkles size={12}/> Generate with AI</>}
                </Button>
              </div>
            )}
          </div>

          {/* Saved Releases */}
          <div className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #1A3A5C" }}>
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Saved Releases</p>
            {isLoading ? <p className="text-xs text-gray-400">Loading…</p> : releases.length === 0 ? (
              <p className="text-xs text-gray-400">No saved releases yet.</p>
            ) : (
              <div className="space-y-2 max-h-64 overflow-y-auto">
                {releases.map(r => (
                  <button key={r.id} onClick={() => setSelected(r)}
                    className={`w-full text-left px-3 py-2 text-xs rounded border transition-colors ${selected?.id === r.id ? "border-[#4A1525] bg-[#F5E8EB]" : "border-gray-200 hover:bg-gray-50"}`}>
                    <p className="font-semibold truncate">{r.title}</p>
                    <p className="text-gray-400">{new Date(r.createdAt).toLocaleDateString()}</p>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Right: Editor / Viewer */}
        <div className="col-span-1 md:col-span-2">
          {selected && !draft ? (
            <div className="bg-white border border-gray-200 rounded p-6" style={{ borderTop: "3px solid #1A3A5C" }}>
              <div className="flex items-start justify-between mb-4">
                <div>
                  <h2 className="font-bold text-lg text-[#1A3A5C]">{selected.title}</h2>
                  <p className="text-xs text-gray-400">{selected.template} · {new Date(selected.createdAt).toLocaleDateString()}</p>
                </div>
                <div className="flex gap-1">
                  <Button variant="outline" size="sm" className="gap-1" onClick={() => handleExportPDF({ ...selected!, template: selected!.template ?? "" })}><Download size={12}/> PDF</Button>
                  <Button variant="ghost" size="sm" onClick={() => setSelected(null)}><Trash2 size={14}/></Button>
                </div>
              </div>
              <div className="prose prose-sm max-w-none text-gray-800 whitespace-pre-wrap font-mono text-xs leading-relaxed border border-gray-100 rounded p-4 bg-gray-50">
                {selected.body}
              </div>
            </div>
          ) : draft ? (
            <div className="bg-white border border-gray-200 rounded p-6" style={{ borderTop: "3px solid #008751" }}>
              <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 mb-4">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <Badge className="bg-[#008751] text-white text-xs">Draft</Badge>
                    {aiDraftMut.isSuccess && <Badge className="bg-purple-600 text-white text-xs gap-1"><Sparkles size={10}/> AI Generated</Badge>}
                  </div>
                  <Input value={draft.title} onChange={e => setDraft(d => d ? { ...d, title: e.target.value } : null)}
                    className="font-bold text-base border-0 border-b border-gray-200 rounded-none px-0 focus-visible:ring-0"/>
                </div>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setDraft(null)}>Discard</Button>
                  <Button size="sm" className="gap-1 bg-[#4A1525] hover:bg-[#3A0F1A] text-white"
                    onClick={() => profileId && saveMut.mutate({ profileId, title: draft.title, content: draft.body, template: draft.template })}
                    disabled={saveMut.isPending}>
                    {saveMut.isPending ? <Loader2 size={12} className="animate-spin"/> : <Save size={12}/>} Save
                  </Button>
                  <Button size="sm" variant="outline" className="gap-1"
                    onClick={() => handleExportPDF({ title: draft.title, body: draft.body, template: draft.template, createdAt: new Date() })}>
                    <Download size={12}/> PDF
                  </Button>
                </div>
              </div>
              {aiDraftMut.isSuccess ? (
                <div className="prose prose-sm max-w-none border border-gray-100 rounded p-4 bg-gray-50">
                  <Streamdown>{draft.body}</Streamdown>
                </div>
              ) : null}
              <Textarea value={draft.body} onChange={e => setDraft(d => d ? { ...d, body: e.target.value } : null)}
                className="font-mono text-xs leading-relaxed min-h-[400px] mt-3"/>
            </div>
          ) : (
            <div className="bg-white border border-gray-200 rounded p-12 flex flex-col items-center justify-center text-center">
              <FileText size={48} className="text-gray-200 mb-4"/>
              <p className="text-gray-500 font-medium mb-2">No release selected</p>
              <p className="text-sm text-gray-400">Pick a template from the left to start drafting, or use <span className="text-[#008751] font-semibold">AI Draft</span> to generate a full release automatically.</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
