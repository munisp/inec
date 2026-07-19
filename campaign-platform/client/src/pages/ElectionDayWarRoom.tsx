import { useState, useEffect, useRef } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Radio, AlertTriangle, CheckCircle, Plus, Loader2, Bell, BellOff } from "lucide-react";

const SEV_COLORS: Record<string, string> = {
  low: "#1A3A5C",
  medium: "#F59E0B",
  high: "#C0392B",
  critical: "#7B0000",
  resolved: "#008751",
  escalated: "#D97706",
};

type FeedFilter = "all" | "unresolved" | "resolved";

export default function ElectionDayWarRoom() {
  const { profileId } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: incidents = [], isLoading } = trpc.warRoom.incidents.useQuery(
    { profileId: profileId! }, { enabled: !!profileId, refetchInterval: 10000 }
  );
  const addMut = trpc.warRoom.addIncident.useMutation({
    onSuccess: () => {
      utils.warRoom.incidents.invalidate();
      toast.success("Incident logged");
      setDesc("");
      setLga("");
    },
    onError: (e) => toast.error(e.message),
  });
  const resolveMut = trpc.warRoom.updateIncidentStatus.useMutation({
    onSuccess: () => utils.warRoom.incidents.invalidate(),
  });

  const [desc, setDesc] = useState("");
  const [severity, setSeverity] = useState<"low" | "medium" | "high" | "critical">("low");
  const [lga, setLga] = useState("");
  const [feedFilter, setFeedFilter] = useState<FeedFilter>("all");
  const [newIds, setNewIds] = useState<Set<number>>(new Set());
  const prevCountRef = useRef(0);
  const feedRef = useRef<HTMLDivElement>(null);

  // Flash new incidents when count increases
  useEffect(() => {
    if (incidents.length > prevCountRef.current && prevCountRef.current > 0) {
      const latestNew = incidents.slice(0, incidents.length - prevCountRef.current).map((i: any) => i.id);
      setNewIds(prev => new Set(Array.from(prev).concat(latestNew)));
      // Clear flash after 3 seconds
      setTimeout(() => setNewIds(prev => {
        const next = new Set(Array.from(prev));
        latestNew.forEach((id: number) => next.delete(id));
        return next;
      }), 3000);
    }
    prevCountRef.current = incidents.length;
  }, [incidents.length]);

  // Auto-scroll to bottom when new incidents arrive
  useEffect(() => {
    if (feedRef.current) feedRef.current.scrollTop = feedRef.current.scrollHeight;
  }, [incidents.length]);

  // SSE for real-time updates
  useEffect(() => {
    if (!profileId) return;
    const es = new EventSource(`/api/war-room/stream?profileId=${profileId}`);
    es.onmessage = () => utils.warRoom.incidents.invalidate();
    es.onerror = () => es.close();
    return () => es.close();
  }, [profileId, utils.warRoom.incidents]);

  const active = incidents.filter((i: any) => i.status !== "resolved").length;
  const critical = incidents.filter((i: any) => (i.severity === "critical" || i.severity === "high") && i.status !== "resolved").length;
  const resolved = incidents.filter((i: any) => i.status === "resolved").length;

  const filteredIncidents = incidents.filter((i: any) => {
    if (feedFilter === "unresolved") return i.status !== "resolved";
    if (feedFilter === "resolved") return i.status === "resolved";
    return true;
  });

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "#1A0A10" }}>
      {/* Flash animation style */}
      <style>{`
        @keyframes incidentFlash {
          0%, 100% { opacity: 1; }
          25% { opacity: 0.5; background-color: rgba(192, 57, 43, 0.35); }
          50% { opacity: 1; }
          75% { opacity: 0.7; background-color: rgba(192, 57, 43, 0.25); }
        }
        .incident-new { animation: incidentFlash 0.8s ease-in-out 3; }
      `}</style>

      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /> Home</Button></Link>
          <Radio size={18} className="text-white animate-pulse" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Election Day War Room</h1>
          <Badge style={{ background: "#008751", color: "white" }} className="ml-2">LIVE</Badge>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right">
            <p className="text-xs text-white/60">ACTIVE</p>
            <p className="font-mono font-bold text-white">{active}</p>
          </div>
          <div className="text-right">
            <p className="text-xs text-red-300">CRITICAL/HIGH</p>
            <p className="font-mono font-bold text-red-300">{critical}</p>
          </div>
          <div className="text-right">
            <p className="text-xs text-green-400">RESOLVED</p>
            <p className="font-mono font-bold text-green-400">{resolved}</p>
          </div>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex-1 flex flex-col">
          {/* Filter tabs */}
          <div className="flex items-center gap-1 px-4 pt-3 pb-0 border-b border-gray-800">
            {(["all", "unresolved", "resolved"] as FeedFilter[]).map(f => (
              <button
                key={f}
                onClick={() => setFeedFilter(f)}
                className="px-4 py-2 text-xs font-semibold uppercase tracking-widest border-b-2 transition-colors capitalize"
                style={{
                  borderColor: feedFilter === f ? "#C0392B" : "transparent",
                  color: feedFilter === f ? "#C0392B" : "#6b7280",
                  background: "transparent",
                }}
              >
                {f === "all" && `All (${incidents.length})`}
                {f === "unresolved" && (
                  <span className="flex items-center gap-1.5">
                    <Bell size={11} />
                    Unresolved ({active})
                  </span>
                )}
                {f === "resolved" && (
                  <span className="flex items-center gap-1.5">
                    <BellOff size={11} />
                    Resolved ({resolved})
                  </span>
                )}
              </button>
            ))}
          </div>

          <div ref={feedRef} className="flex-1 overflow-y-auto p-4 space-y-2" style={{ maxHeight: "calc(100vh - 220px)" }}>
            {isLoading ? (
              <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-500" /></div>
            ) : filteredIncidents.length === 0 ? (
              <div className="text-center py-20 text-gray-600">
                <Radio size={48} className="mx-auto mb-4 opacity-20" />
                <p className="font-semibold text-gray-400">
                  {feedFilter === "resolved" ? "No resolved incidents yet" : feedFilter === "unresolved" ? "No active incidents — all clear" : "War Room Standing By"}
                </p>
                <p className="text-sm text-gray-600">Log incidents as they occur on election day.</p>
              </div>
            ) : filteredIncidents.map((inc: any) => (
              <div
                key={inc.id}
                className={`flex items-start gap-3 p-3 rounded transition-all ${newIds.has(inc.id) ? "incident-new" : ""}`}
                style={{
                  background: inc.status === "resolved"
                    ? SEV_COLORS.resolved + "18"
                    : (SEV_COLORS[inc.severity ?? "low"] ?? SEV_COLORS.low) + "18",
                  borderLeft: `3px solid ${inc.status === "resolved" ? SEV_COLORS.resolved : (SEV_COLORS[inc.severity ?? "low"] ?? SEV_COLORS.low)}`,
                }}
              >
                {(inc.severity === "critical" || inc.severity === "high") && inc.status !== "resolved"
                  ? <AlertTriangle size={16} style={{ color: SEV_COLORS[inc.severity] }} className="mt-0.5 flex-shrink-0" />
                  : <CheckCircle size={16} style={{ color: inc.status === "resolved" ? SEV_COLORS.resolved : (SEV_COLORS[inc.severity ?? "low"] ?? SEV_COLORS.low) }} className="mt-0.5 flex-shrink-0" />}
                <div className="flex-1">
                  <div className="flex items-center gap-2 mb-0.5 flex-wrap">
                    {inc.status === "resolved" ? (
                      <Badge style={{ background: SEV_COLORS.resolved + "33", color: SEV_COLORS.resolved }} className="text-xs">RESOLVED</Badge>
                    ) : inc.status === "escalated" ? (
                      <Badge style={{ background: SEV_COLORS.escalated + "33", color: SEV_COLORS.escalated }} className="text-xs">ESCALATED</Badge>
                    ) : (
                      <Badge style={{ background: (SEV_COLORS[inc.severity ?? "low"] ?? SEV_COLORS.low) + "33", color: SEV_COLORS[inc.severity ?? "low"] ?? SEV_COLORS.low }} className="text-xs">{inc.severity?.toUpperCase()}</Badge>
                    )}
                    {inc.lga && <span className="text-xs text-gray-400">{inc.lga}</span>}
                    {inc.incidentType && <span className="text-xs text-gray-500">{inc.incidentType}</span>}
                    <span className="text-xs text-gray-500 ml-auto">{new Date(inc.reportedAt).toLocaleTimeString()}</span>
                  </div>
                  <p className="text-sm text-gray-200">{inc.description}</p>
                </div>
                {inc.status !== "resolved" && (
                  <div className="flex flex-col gap-1">
                    {inc.status !== "escalated" && (
                      <Button size="sm" variant="ghost" className="text-yellow-400 text-xs hover:bg-yellow-900/20"
                        onClick={() => resolveMut.mutate({ id: inc.id, status: "escalated" })}>
                        Escalate
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" className="text-green-400 text-xs hover:bg-green-900/20"
                      onClick={() => resolveMut.mutate({ id: inc.id, status: "resolved" })}>
                      Resolve
                    </Button>
                  </div>
                )}
              </div>
            ))}
          </div>

          <div className="p-4 border-t border-gray-800 flex gap-2">
            <Select value={severity} onValueChange={v => setSeverity(v as "low" | "medium" | "high" | "critical")}>
              <SelectTrigger className="w-32 bg-gray-800 border-gray-700 text-white"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="low">Low</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="high">High</SelectItem>
                <SelectItem value="critical">Critical</SelectItem>
              </SelectContent>
            </Select>
            <Input
              placeholder="LGA (optional)"
              value={lga}
              onChange={e => setLga(e.target.value)}
              className="w-40 bg-gray-800 border-gray-700 text-white placeholder:text-gray-500"
            />
            <Input
              placeholder="Describe the incident…"
              value={desc}
              onChange={e => setDesc(e.target.value)}
              onKeyDown={e => {
                if (e.key === "Enter" && desc.trim() && profileId)
                  addMut.mutate({ profileId, description: desc, severity, lga });
              }}
              className="flex-1 bg-gray-800 border-gray-700 text-white placeholder:text-gray-500"
            />
            <Button
              onClick={() => { if (!profileId || !desc.trim()) return; addMut.mutate({ profileId, description: desc, severity, lga }); }}
              disabled={addMut.isPending}
              style={{ background: "#C0392B", color: "white" }}
              className="gap-1.5"
            >
              {addMut.isPending ? <Loader2 size={14} className="animate-spin" /> : <><Plus size={14} /> Log</>}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
