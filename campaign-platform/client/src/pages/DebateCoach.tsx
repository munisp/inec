/**
 * Debate Coach — AI-powered debate preparation with opponent-aware talking points
 * Palette: #4A1525 (burgundy), #008751 (green), #1A3A5C (navy), #F5F0EB (paper)
 */
import { useState } from "react";
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { toast } from "sonner";
import { Streamdown } from "streamdown";
import { Cpu, ChevronLeft, Mic, BookOpen, Zap, Clock, RotateCcw, Save } from "lucide-react";

const TOPICS = [
  "Economy & Job Creation", "Security & Public Safety", "Education Reform",
  "Healthcare Infrastructure", "Roads & Infrastructure", "Anti-Corruption",
  "Youth & Women Inclusion", "Agriculture & Food Security",
];

export default function DebateCoach() {
  const { profileId, profile } = useCandidateProfile();
  const candidateName = profile?.candidateName;
  const partyName = profile?.partyName;
  const utils = trpc.useUtils();

  // Fetch saved prep notes
  const { data: notes = [], isLoading } = trpc.debate.list.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId }
  );

  // Fetch opponents from opposition research
  const { data: opponents = [] } = trpc.opposition.list.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId }
  );

  const upsertMut = trpc.debate.upsert.useMutation({
    onSuccess: () => { utils.debate.list.invalidate(); toast.success("Note saved"); },
  });

  const aiPrepMut = trpc.debate.aiPrep.useMutation({
    onError: () => toast.error("AI prep failed — please try again"),
  });

  const [selectedTopic, setSelectedTopic] = useState(TOPICS[0]);
  const [selectedOpponentId, setSelectedOpponentId] = useState<number | null>(null);
  const [timer, setTimer] = useState(0);
  const [timerActive, setTimerActive] = useState(false);
  const [activeTab, setActiveTab] = useState<"ai" | "saved" | "timer">("ai");

  // Timer logic
  useState(() => {
    let interval: ReturnType<typeof setInterval>;
    if (timerActive) {
      interval = setInterval(() => setTimer(t => t + 1), 1000);
    }
    return () => clearInterval(interval);
  });

  const selectedOpponent = opponents.find(o => o.id === selectedOpponentId);

  const handleAIPrep = () => {
    if (!profileId) return toast.error("Profile required");
    aiPrepMut.mutate({
      profileId,
      topic: selectedTopic,
      opponentName: selectedOpponent?.opponentName,
      opponentWeaknesses: selectedOpponent?.weakness ? [selectedOpponent.weakness] : [],
      opponentStrengths: selectedOpponent?.strength ? [selectedOpponent.strength] : [],
      candidateName: candidateName ?? undefined,
      partyName: partyName ?? undefined,
    });
  };

  const handleSaveAI = () => {
    if (!profileId || !aiPrepMut.data?.content) return;
    upsertMut.mutate({
      profileId,
      topic: selectedTopic,
      keyMessage: String(aiPrepMut.data.content).slice(0, 200),
      notes: String(aiPrepMut.data.content),
    });
  };

  const formatTime = (s: number) => `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB", fontFamily: "'Inter', sans-serif" }}>
      {/* Header */}
      <header className="flex items-center justify-between px-4 sm:px-6 py-3 border-b border-gray-300" style={{ background: "#4A1525" }}>
        <div className="flex items-center gap-3">
          <Link href="/" className="text-white opacity-70 hover:opacity-100 transition-opacity">
            <ChevronLeft size={20} />
          </Link>
          <div className="w-7 h-7 flex items-center justify-center rounded-sm" style={{ background: "#008751" }}>
            <Mic size={14} className="text-white" />
          </div>
          <div>
            <h1 className="text-white font-bold text-base sm:text-lg leading-none">Debate Coach</h1>
            <p className="text-xs" style={{ color: "#C9B8BE" }}>AI-POWERED DEBATE PREPARATION</p>
          </div>
        </div>
        <div className="text-xs font-mono px-2 py-1 rounded" style={{ background: "#3D1520", color: "#C9B8BE" }}>
          {notes.length} saved notes
        </div>
      </header>

      <div className="flex flex-col lg:flex-row h-[calc(100vh-56px)]">
        {/* Left: Controls */}
        <aside className="w-full lg:w-72 flex-shrink-0 border-b lg:border-b-0 lg:border-r border-gray-300 bg-white p-4 overflow-y-auto">
          {/* Topic Selector */}
          <div className="mb-5">
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-2">Select Topic</p>
            <div className="space-y-1">
              {TOPICS.map(t => (
                <button
                  key={t}
                  onClick={() => setSelectedTopic(t)}
                  className="w-full text-left px-3 py-2 text-sm rounded transition-all"
                  style={{
                    background: selectedTopic === t ? "#4A1525" : "transparent",
                    color: selectedTopic === t ? "white" : "#333",
                    fontWeight: selectedTopic === t ? 600 : 400,
                  }}
                >
                  {t}
                </button>
              ))}
            </div>
          </div>

          {/* Opponent Selector */}
          <div className="mb-5">
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-2">Opponent (Optional)</p>
            <select
              value={selectedOpponentId ?? ""}
              onChange={e => setSelectedOpponentId(e.target.value ? Number(e.target.value) : null)}
              className="w-full border border-gray-300 rounded px-2 py-1.5 text-sm bg-white"
            >
              <option value="">— No specific opponent —</option>
              {opponents.map(o => (
                <option key={o.id} value={o.id}>{o.opponentName} {o.party ? `(${o.party})` : ""}</option>
              ))}
            </select>
            {selectedOpponent && (
              <div className="mt-2 p-2 rounded text-xs" style={{ background: "#FEF3F2", border: "1px solid #FECACA" }}>
                <p className="font-semibold text-red-700">Threat: {selectedOpponent.threatLevel?.toUpperCase()}</p>
                {selectedOpponent.notes && <p className="text-gray-600 mt-1 line-clamp-2">{selectedOpponent.notes}</p>}
              </div>
            )}
          </div>

          {/* Generate Button */}
          <button
            onClick={handleAIPrep}
            disabled={aiPrepMut.isPending || !profileId}
            className="w-full py-3 font-semibold text-sm uppercase tracking-widest flex items-center justify-center gap-2 transition-all active:scale-95 rounded"
            style={{ background: aiPrepMut.isPending ? "#ccc" : "#008751", color: "white", cursor: aiPrepMut.isPending ? "wait" : "pointer" }}
          >
            {aiPrepMut.isPending ? (
              <><Cpu size={14} className="animate-pulse" /> Generating…</>
            ) : (
              <><Zap size={14} /> AI Prep for This Topic</>
            )}
          </button>
          <p className="text-center text-xs text-gray-400 mt-1">Powered by built-in LLM</p>
        </aside>

        {/* Right: Main Panel */}
        <main className="flex-1 overflow-y-auto p-4 sm:p-6">
          {/* Tabs */}
          <div className="flex gap-4 border-b border-gray-200 mb-5">
            {([["ai", "AI Draft", Zap], ["saved", "Saved Notes", BookOpen], ["timer", "Practice Timer", Clock]] as const).map(([tab, label, Icon]) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className="flex items-center gap-1.5 pb-2 text-sm font-semibold transition-all"
                style={{
                  color: activeTab === tab ? "#4A1525" : "#999",
                  borderBottom: activeTab === tab ? "2px solid #4A1525" : "2px solid transparent",
                }}
              >
                <Icon size={14} /> {label}
              </button>
            ))}
          </div>

          {/* AI Draft Tab */}
          {activeTab === "ai" && (
            <div>
              {!aiPrepMut.data && !aiPrepMut.isPending && (
                <div className="flex flex-col items-center justify-center py-20 text-center">
                  <Zap size={40} className="mb-4 opacity-20" style={{ color: "#4A1525" }} />
                  <p className="text-gray-500 text-sm">Select a topic and click <strong>AI Prep</strong> to generate<br />talking points, rebuttals, and an opening statement.</p>
                </div>
              )}
              {aiPrepMut.isPending && (
                <div className="flex flex-col items-center justify-center py-20">
                  <div className="w-10 h-10 border-4 border-gray-200 rounded-full animate-spin mb-4" style={{ borderTopColor: "#008751" }} />
                  <p className="text-gray-500 text-sm">Generating debate prep for <strong>{selectedTopic}</strong>…</p>
                </div>
              )}
              {aiPrepMut.data && (
                <div>
                  <div className="flex items-center justify-between mb-3">
                    <h2 className="font-bold text-lg" style={{ color: "#4A1525" }}>{selectedTopic}</h2>
                    <div className="flex gap-2">
                      <button
                        onClick={() => aiPrepMut.reset()}
                        className="flex items-center gap-1 px-3 py-1.5 text-xs border border-gray-300 rounded hover:bg-gray-50"
                      >
                        <RotateCcw size={12} /> Reset
                      </button>
                      <button
                        onClick={handleSaveAI}
                        disabled={upsertMut.isPending}
                        className="flex items-center gap-1 px-3 py-1.5 text-xs text-white rounded"
                        style={{ background: "#008751" }}
                      >
                        <Save size={12} /> Save Note
                      </button>
                    </div>
                  </div>
                  <div
                    className="prose prose-sm max-w-none p-5 rounded border border-gray-200 bg-white"
                    style={{ fontFamily: "'Inter', sans-serif", lineHeight: 1.7 }}
                  >
                    <Streamdown>{String(aiPrepMut.data.content)}</Streamdown>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Saved Notes Tab */}
          {activeTab === "saved" && (
            <div>
              {isLoading && <p className="text-gray-400 text-sm">Loading…</p>}
              {!isLoading && notes.length === 0 && (
                <div className="text-center py-16 text-gray-400">
                  <BookOpen size={36} className="mx-auto mb-3 opacity-30" />
                  <p className="text-sm">No saved notes yet. Generate an AI prep and save it.</p>
                </div>
              )}
              <div className="space-y-4">
                {notes.map(n => (
                  <div key={n.id} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #1A3A5C" }}>
                    <div className="flex items-start justify-between mb-2">
                      <h3 className="font-bold text-gray-900">{n.topic}</h3>
                      {n.practiceScore && (
                        <span className="text-xs font-mono px-2 py-0.5 rounded" style={{ background: "#E6F4EE", color: "#008751" }}>
                          Score: {n.practiceScore}/10
                        </span>
                      )}
                    </div>
                    {n.keyMessage && <p className="text-sm text-gray-600 mb-2 italic">"{n.keyMessage}"</p>}
                    {n.notes && (
                      <div className="prose prose-sm max-w-none text-gray-700">
                        <Streamdown>{n.notes ?? ''}</Streamdown>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Practice Timer Tab */}
          {activeTab === "timer" && (
            <div className="flex flex-col items-center justify-center py-10">
              <div
                className="w-48 h-48 rounded-full flex items-center justify-center mb-8 border-8"
                style={{ borderColor: timerActive ? "#008751" : "#4A1525", background: "#fff" }}
              >
                <span className="font-mono text-4xl font-bold" style={{ color: timerActive ? "#008751" : "#4A1525" }}>
                  {formatTime(timer)}
                </span>
              </div>
              <div className="flex gap-3">
                <button
                  onClick={() => setTimerActive(a => !a)}
                  className="px-6 py-3 rounded font-semibold text-white"
                  style={{ background: timerActive ? "#C0392B" : "#008751" }}
                >
                  {timerActive ? "Pause" : "Start"}
                </button>
                <button
                  onClick={() => { setTimer(0); setTimerActive(false); }}
                  className="px-6 py-3 rounded font-semibold border border-gray-300 text-gray-700 hover:bg-gray-50"
                >
                  Reset
                </button>
              </div>
              <div className="mt-8 grid grid-cols-3 gap-3 w-full max-w-sm">
                {[["Opening", 120], ["Response", 180], ["Closing", 60]].map(([label, secs]) => (
                  <button
                    key={label}
                    onClick={() => { setTimer(Number(secs)); setTimerActive(false); }}
                    className="text-center py-3 border border-gray-200 rounded hover:bg-gray-50"
                  >
                    <p className="text-xs text-gray-500">{label}</p>
                    <p className="font-mono font-bold text-sm">{formatTime(Number(secs))}</p>
                  </button>
                ))}
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
