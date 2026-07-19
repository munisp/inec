import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Share2, Loader2, Plus, Sparkles, Calendar, Clock } from "lucide-react";

const PLATFORMS = ["Twitter", "Facebook", "WhatsApp", "Instagram"];
const PLATFORM_COLORS: Record<string, string> = {
  Twitter: "#1DA1F2", Facebook: "#1877F2", WhatsApp: "#25D366", Instagram: "#E1306C",
};
const CHAR_LIMITS: Record<string, number> = { Twitter: 280, Facebook: 63206, WhatsApp: 65536, Instagram: 2200 };

export default function SocialMediaCenter() {
  const { profileId, profile, canEdit } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: posts = [], isLoading } = trpc.socialMedia.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const saveMut = trpc.socialMedia.save.useMutation({
    onSuccess: () => {
      utils.socialMedia.list.invalidate();
      toast.success("Post saved");
      setContent("");
      setScheduledDate("");
      setScheduledTime("");
    },
    onError: e => toast.error(e.message),
  });
  const aiMut = trpc.socialMedia.aiGenerate.useMutation({
    onSuccess: d => { setContent(d.content); toast.success("AI content generated"); },
    onError: e => toast.error(e.message),
  });

  const [platform, setPlatform] = useState("Twitter");
  const [content, setContent] = useState("");
  const [filter, setFilter] = useState("All");
  const [aiTopic, setAiTopic] = useState("");
  const [aiTone, setAiTone] = useState("inspiring and relatable");
  const [showAiPanel, setShowAiPanel] = useState(false);
  const [scheduledDate, setScheduledDate] = useState("");
  const [scheduledTime, setScheduledTime] = useState("");

  const limit = CHAR_LIMITS[platform] ?? 280;
  const charPct = Math.min(100, (content.length / limit) * 100);
  const charColor = charPct > 90 ? "#C0392B" : charPct > 70 ? "#E67E22" : "#008751";
  const filtered = filter === "All" ? posts : posts.filter(p => p.platform === filter);

  const handleSave = () => {
    if (!profileId || !content.trim()) return toast.error("Content required");
    let scheduledAt: string | undefined;
    if (scheduledDate) {
      scheduledAt = scheduledTime
        ? new Date(`${scheduledDate}T${scheduledTime}`).toISOString()
        : new Date(`${scheduledDate}T09:00`).toISOString();
    }
    saveMut.mutate({ profileId, platform, content, scheduledAt, status: scheduledAt ? "scheduled" : "pending" });
  };

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <Share2 size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Social Media Center</h1>
        </div>
        <div className="flex gap-2 flex-wrap">
          {["All", ...PLATFORMS].map(p => (
            <button key={p} onClick={() => setFilter(p)}
              className="px-3 py-1 text-xs font-semibold rounded-full transition-all"
              style={{ background: filter === p ? "white" : "transparent", color: filter === p ? "#4A1525" : "white", border: "1px solid white" }}>
              {p}
            </button>
          ))}
        </div>
      </header>

      <div className="max-w-4xl mx-auto px-6 py-8">
        {/* Composer */}
        <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #4A1525" }}>
          <div className="flex items-center gap-3 mb-3 flex-wrap">
            <Select value={platform} onValueChange={setPlatform}>
              <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
              <SelectContent>{PLATFORMS.map(p => <SelectItem key={p} value={p}>{p}</SelectItem>)}</SelectContent>
            </Select>
            {/* Character count ring */}
            <div className="flex items-center gap-1.5">
              <svg width="28" height="28" viewBox="0 0 28 28">
                <circle cx="14" cy="14" r="11" fill="none" stroke="#e5e7eb" strokeWidth="3" />
                <circle cx="14" cy="14" r="11" fill="none" stroke={charColor} strokeWidth="3"
                  strokeDasharray={`${charPct * 0.691} 69.1`} strokeLinecap="round"
                  transform="rotate(-90 14 14)" style={{ transition: "stroke-dasharray 0.2s" }} />
              </svg>
              <span className="text-xs text-gray-400 font-mono">{content.length}/{limit}</span>
            </div>
            <button onClick={() => setShowAiPanel(v => !v)}
              className="ml-auto flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded transition-all"
              style={{ background: showAiPanel ? "#4A1525" : "#F5F0EB", color: showAiPanel ? "white" : "#4A1525", border: "1px solid #4A1525" }}>
              <Sparkles size={12} /> AI Generate
            </button>
          </div>

          {/* AI panel */}
          {showAiPanel && (
            <div className="mb-3 p-3 rounded" style={{ background: "#F5F0EB", border: "1px solid #e5e0db" }}>
              <p className="text-xs font-semibold uppercase tracking-wider mb-2" style={{ color: "#4A1525" }}>AI Content Generator</p>
              <div className="flex gap-2 flex-wrap">
                <input value={aiTopic} onChange={e => setAiTopic(e.target.value)}
                  placeholder="Topic (e.g. healthcare policy, rally announcement…)"
                  className="flex-1 min-w-48 text-sm border border-gray-200 rounded px-3 py-2 outline-none focus:border-gray-400" />
                <Select value={aiTone} onValueChange={setAiTone}>
                  <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {["inspiring and relatable", "formal and authoritative", "urgent and direct", "warm and community-focused", "bold and confrontational"].map(t => (
                      <SelectItem key={t} value={t}>{t}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button onClick={() => { if (!aiTopic.trim()) return toast.error("Enter a topic"); aiMut.mutate({ platform, topic: aiTopic, tone: aiTone }); }}
                  disabled={aiMut.isPending} size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5">
                  {aiMut.isPending ? <Loader2 size={12} className="animate-spin" /> : <Sparkles size={12} />} Generate
                </Button>
              </div>
            </div>
          )}

          <textarea value={content} onChange={e => setContent(e.target.value)} rows={4}
            placeholder={`Write a ${platform} post for ${profile?.candidateName ?? "the campaign"}…`}
            className="w-full text-sm text-gray-700 border border-gray-200 rounded p-3 resize-none outline-none focus:border-gray-400"
            maxLength={limit} />

          {/* Schedule row */}
          <div className="flex items-center gap-3 mt-3 flex-wrap">
            <div className="flex items-center gap-1.5 text-xs text-gray-500">
              <Calendar size={13} />
              <span>Schedule:</span>
            </div>
            <input type="date" value={scheduledDate} onChange={e => setScheduledDate(e.target.value)}
              className="text-xs border border-gray-200 rounded px-2 py-1.5 outline-none focus:border-gray-400" />
            {scheduledDate && (
              <div className="flex items-center gap-1.5">
                <Clock size={12} className="text-gray-400" />
                <input type="time" value={scheduledTime} onChange={e => setScheduledTime(e.target.value)}
                  className="text-xs border border-gray-200 rounded px-2 py-1.5 outline-none focus:border-gray-400" />
              </div>
            )}
            <Button onClick={handleSave} disabled={saveMut.isPending || !content.trim()}
              style={{ background: "#4A1525", color: "white" }} className="ml-auto gap-1.5" size="sm">
              {saveMut.isPending ? <Loader2 size={13} className="animate-spin" /> : <Plus size={13} />}
              {scheduledDate ? "Schedule Post" : "Save Post"}
            </Button>
          </div>
        </div>

        {/* Feed */}
        {isLoading
          ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
          : filtered.length === 0
            ? <div className="text-center py-20 text-gray-500"><Share2 size={48} className="mx-auto mb-4 opacity-30" /><p>No posts yet. Use AI Generate or write your first post above.</p></div>
            : (
              <div className="space-y-3">
                {filtered.map(p => (
                  <div key={p.id} className="bg-white border border-gray-200 rounded p-4">
                    <div className="flex items-center gap-2 mb-2 flex-wrap">
                      <Badge style={{ background: PLATFORM_COLORS[p.platform] + "22", color: PLATFORM_COLORS[p.platform] }}>{p.platform}</Badge>
                      <span className="text-xs text-gray-400">{new Date(p.createdAt).toLocaleString()}</span>
                      {p.scheduledAt && (
                        <span className="flex items-center gap-1 text-xs text-blue-600">
                          <Calendar size={11} /> Scheduled: {new Date(p.scheduledAt).toLocaleString()}
                        </span>
                      )}
                      <Badge variant="outline" className="ml-auto capitalize">{p.status}</Badge>
                    </div>
                    <p className="text-sm text-gray-700 whitespace-pre-wrap">{p.content}</p>
                  </div>
                ))}
              </div>
            )}
      </div>
    </div>
  );
}
