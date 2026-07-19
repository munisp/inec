import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, ArrowUp, ArrowDown, BookOpen, Plus, Loader2, Pencil, Sparkles, Trash2 } from "lucide-react";

type Priority = "low" | "medium" | "high" | "critical";
const PRIORITY_COLORS: Record<Priority, string> = {
  low: "#1A3A5C", medium: "#F59E0B", high: "#C0392B", critical: "#7B0000",
};

export default function ManifestoBuilder() {
  const { profile, profileId, canEdit, canDelete } = useCandidateProfile();
  const candidateName = profile?.candidateName;
  const partyName = profile?.partyName;
  const stateName = profile?.stateName;
  const utils = trpc.useUtils();

  const { data: sections = [], isLoading } = trpc.manifesto.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );

  const upsertMut = trpc.manifesto.upsert.useMutation({
    onSuccess: () => {
      utils.manifesto.list.invalidate();
      toast.success("Section saved");
      setOpen(false);
      setEditing(null);
    },
    onError: (e) => toast.error(e.message),
  });

  const deleteMut = trpc.manifesto.delete.useMutation({
    onSuccess: () => { utils.manifesto.list.invalidate(); toast.success("Section deleted"); },
    onError: (e) => toast.error(e.message),
  });

  const aiDraftMut = trpc.manifestoAI.draft.useMutation({
    onSuccess: (data) => {
      setForm(f => ({ ...f, summary: data.content }));
      setAiOpen(false);
      toast.success("AI draft generated — review and save");
    },
    onError: (e) => toast.error("AI draft failed: " + e.message),
  });

  const [open, setOpen] = useState(false);
  const [aiOpen, setAiOpen] = useState(false);
  const [editing, setEditing] = useState<typeof sections[0] | null>(null);
  const [form, setForm] = useState({ sectionTitle: "", summary: "", priority: "medium" as Priority, sortOrder: 1 });
  const [aiBrief, setAiBrief] = useState("");

  const openNew = () => {
    setEditing(null);
    setForm({ sectionTitle: "", summary: "", priority: "medium", sortOrder: sections.length + 1 });
    setOpen(true);
  };

  const openEdit = (s: typeof sections[0]) => {
    setEditing(s);
    setForm({
      sectionTitle: s.sectionTitle,
      summary: s.summary ?? "",
      priority: (s.priority ?? "medium") as Priority,
      sortOrder: s.sortOrder ?? 1,
    });
    setOpen(true);
  };

  const handleAIDraft = () => {
    if (!form.sectionTitle) return toast.error("Enter a section title first");
    setAiBrief("");
    setAiOpen(true);
  };

  const runAIDraft = () => {
    if (!aiBrief.trim()) return toast.error("Enter key points for the AI to expand on");
    aiDraftMut.mutate({
      profileId: profileId!,
      policyArea: form.sectionTitle,
      brief: aiBrief,
    });
  };


  const reorderMut = trpc.manifesto.upsert.useMutation({
    onSuccess: () => utils.manifesto.list.invalidate(),
  });
  function moveSection(idx: number, dir: -1 | 1) {
    const sorted = [...sections].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0));
    const target = sorted[idx];
    const swap = sorted[idx + dir];
    if (!target || !swap) return;
    reorderMut.mutate({ profileId: profileId!, id: target.id, sectionTitle: target.sectionTitle, sortOrder: swap.sortOrder ?? idx + dir + 1 });
    reorderMut.mutate({ profileId: profileId!, id: swap.id, sectionTitle: swap.sectionTitle, sortOrder: target.sortOrder ?? idx + 1 });
  }
  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-4 sm:px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/">
            <Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10">
              <ArrowLeft size={14} /> <span className="hidden sm:inline">Home</span>
            </Button>
          </Link>
          <BookOpen size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>
            Manifesto Builder
          </h1>
        </div>
        <Button
          size="sm"
          style={{ background: "#008751", color: "white" }}
          className="gap-1.5"
          onClick={openNew}
          disabled={!canEdit}
        >
          <Plus size={14} /> <span className="hidden sm:inline">Add Section</span>
        </Button>
      </header>

      <div className="max-w-4xl mx-auto px-4 sm:px-6 py-8">
        {isLoading ? (
          <div className="flex justify-center py-20">
            <Loader2 size={32} className="animate-spin text-gray-400" />
          </div>
        ) : sections.length === 0 ? (
          <div className="text-center py-20 text-gray-500">
            <BookOpen size={48} className="mx-auto mb-4 opacity-30" />
            <p className="mb-4">No manifesto sections yet</p>
            <Button onClick={openNew} style={{ background: "#4A1525", color: "white" }} disabled={!canEdit}>
              <Plus size={14} className="mr-1" /> Create First Section
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            {(() => {
              const sortedSections = [...sections].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0));
              return sortedSections.map((s, i) => (
              <div
                key={s.id}
                className="bg-white border border-gray-200 rounded p-5"
                style={{ borderLeft: `4px solid ${PRIORITY_COLORS[(s.priority ?? "medium") as Priority]}` }}
              >
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-mono text-xs text-gray-400 w-6">{String(i + 1).padStart(2, "0")}</span>
                    <h3 className="font-bold text-gray-900">{s.sectionTitle}</h3>
                    {s.priority && (
                      <Badge
                        style={{
                          background: PRIORITY_COLORS[(s.priority ?? "medium") as Priority] + "22",
                          color: PRIORITY_COLORS[(s.priority ?? "medium") as Priority],
                        }}
                      >
                        {s.priority}
                      </Badge>
                    )}
                  </div>
                  <div className="flex gap-1">
                    {canEdit && (
                      <>
                        <Button variant="ghost" size="sm" onClick={() => moveSection(i, -1)} disabled={i === 0} title="Move up">
                          <ArrowUp size={14} />
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => moveSection(i, 1)} disabled={i === sortedSections.length - 1} title="Move down">
                          <ArrowDown size={14} />
                        </Button>
                      </>
                    )}
                    <Button variant="ghost" size="sm" onClick={() => openEdit(s)} disabled={!canEdit}>
                      <Pencil size={14} />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-red-500 hover:text-red-700"
                      onClick={() => {
                        if (confirm("Delete this section?")) deleteMut.mutate({ id: s.id });
                      }}
                      disabled={!canDelete || deleteMut.isPending}
                    >
                      <Trash2 size={14} />
                    </Button>
                  </div>
                </div>
                <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap ml-8">{s.summary}</p>
              </div>
            ));
            })()}
          </div>
        )}
      </div>

      {/* Section Edit Dialog */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit Section" : "New Manifesto Section"}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <Input
              placeholder="Section Title *"
              value={form.sectionTitle}
              onChange={e => setForm(f => ({ ...f, sectionTitle: e.target.value }))}
            />
            <div className="flex gap-2">
              <Select value={form.priority} onValueChange={v => setForm(f => ({ ...f, priority: v as Priority }))}>
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="Priority" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="low">Low</SelectItem>
                  <SelectItem value="medium">Medium</SelectItem>
                  <SelectItem value="high">High</SelectItem>
                  <SelectItem value="critical">Critical</SelectItem>
                </SelectContent>
              </Select>
              <Input
                type="number"
                placeholder="Sort order"
                value={form.sortOrder}
                onChange={e => setForm(f => ({ ...f, sortOrder: parseInt(e.target.value) || 1 }))}
                min={1}
                className="w-32"
              />
            </div>

            {/* AI Draft Button */}
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="gap-1.5 text-purple-700 border-purple-300 hover:bg-purple-50"
                onClick={handleAIDraft}
                disabled={aiDraftMut.isPending}
              >
                <Sparkles size={14} />
                AI Draft
              </Button>
              <span className="text-xs text-gray-400">Let AI write a full policy section from your key points</span>
            </div>

            <textarea
              rows={10}
              placeholder="Policy content / summary… (or use AI Draft above)"
              value={form.summary}
              onChange={e => setForm(f => ({ ...f, summary: e.target.value }))}
              className="w-full text-sm border border-gray-200 rounded p-3 resize-none outline-none focus:border-gray-400"
            />

            <Button
              onClick={() => {
                if (!profileId || !form.sectionTitle) return toast.error("Title required");
                upsertMut.mutate({
                  id: editing?.id,
                  profileId,
                  sectionTitle: form.sectionTitle,
                  summary: form.summary,
                  priority: form.priority,
                  sortOrder: form.sortOrder,
                });
              }}
              disabled={upsertMut.isPending || !canEdit}
              style={{ background: "#4A1525", color: "white" }}
            >
              {upsertMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Save Section"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* AI Brief Dialog */}
      <Dialog open={aiOpen} onOpenChange={setAiOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Sparkles size={16} className="text-purple-600" />
              AI Draft — {form.sectionTitle}
            </DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <p className="text-sm text-gray-600">
              Enter 3–5 key points or policy commitments. The AI will expand them into a full manifesto section.
            </p>
            <textarea
              rows={6}
              placeholder="e.g.&#10;- Build 500 new classrooms in rural LGAs&#10;- Increase teacher salaries by 40%&#10;- Free school meals for primary pupils&#10;- Digital skills programme for secondary schools"
              value={aiBrief}
              onChange={e => setAiBrief(e.target.value)}
              className="w-full text-sm border border-gray-200 rounded p-3 resize-none outline-none focus:border-gray-400"
            />
            <Button
              onClick={runAIDraft}
              disabled={aiDraftMut.isPending || !aiBrief.trim()}
              style={{ background: "#7C3AED", color: "white" }}
              className="gap-2"
            >
              {aiDraftMut.isPending ? (
                <><Loader2 size={14} className="animate-spin" /> Generating…</>
              ) : (
                <><Sparkles size={14} /> Generate Draft</>
              )}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
