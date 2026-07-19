import { useState, useEffect, useRef } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Radio, AlertTriangle, CheckCircle, Plus, Loader2 } from "lucide-react";

const SEV_COLORS: Record<string,string> = { low:"#1A3A5C", medium:"#F59E0B", high:"#C0392B", critical:"#7B0000", resolved:"#008751" };

export default function ElectionDayWarRoom() {
  const { profileId } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: incidents = [], isLoading } = trpc.warRoom.incidents.useQuery(
    { profileId: profileId! }, { enabled: !!profileId, refetchInterval: 10000 }
  );
  const addMut = trpc.warRoom.addIncident.useMutation({
    onSuccess: () => { utils.warRoom.incidents.invalidate(); toast.success("Incident logged"); setDesc(""); setLga(""); },
    onError: (e) => toast.error(e.message),
  });
  const resolveMut = trpc.warRoom.updateIncidentStatus.useMutation({
    onSuccess: () => utils.warRoom.incidents.invalidate(),
  });

  const [desc, setDesc] = useState("");
  const [severity, setSeverity] = useState<"low"|"medium"|"high"|"critical">("low");
  const [lga, setLga] = useState("");
  const feedRef = useRef<HTMLDivElement>(null);

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

  const active = incidents.filter(i => i.status !== "resolved").length;
  const critical = incidents.filter(i => (i.severity === "critical" || i.severity === "high") && i.status !== "resolved").length;

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "#1A0A10" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Radio size={18} className="text-white animate-pulse"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Election Day War Room</h1>
          <Badge style={{ background: "#008751", color: "white" }} className="ml-2">LIVE</Badge>
        </div>
        <div className="flex items-center gap-6">
          <div className="text-right"><p className="text-xs text-white/60">ACTIVE</p><p className="font-mono font-bold text-white">{active}</p></div>
          <div className="text-right"><p className="text-xs text-red-300">CRITICAL/HIGH</p><p className="font-mono font-bold text-red-300">{critical}</p></div>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex-1 flex flex-col">
          <div ref={feedRef} className="flex-1 overflow-y-auto p-4 space-y-2" style={{ maxHeight: "calc(100vh - 180px)" }}>
            {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-500"/></div>
            : incidents.length === 0 ? (
              <div className="text-center py-20 text-gray-600">
                <Radio size={48} className="mx-auto mb-4 opacity-20"/>
                <p className="font-semibold text-gray-400">War Room Standing By</p>
                <p className="text-sm text-gray-600">Log incidents as they occur on election day.</p>
              </div>
            ) : incidents.map(inc => (
              <div key={inc.id} className="flex items-start gap-3 p-3 rounded" style={{ background: SEV_COLORS[inc.severity ?? "low"] + "18", borderLeft: `3px solid ${SEV_COLORS[inc.severity ?? "low"]}` }}>
                {(inc.severity === "critical" || inc.severity === "high")
                  ? <AlertTriangle size={16} style={{ color: SEV_COLORS[inc.severity] }} className="mt-0.5 flex-shrink-0"/>
                  : <CheckCircle size={16} style={{ color: SEV_COLORS[inc.severity ?? "low"] }} className="mt-0.5 flex-shrink-0"/>}
                <div className="flex-1">
                  <div className="flex items-center gap-2 mb-0.5">
                    <Badge style={{ background: SEV_COLORS[inc.severity ?? "low"] + "33", color: SEV_COLORS[inc.severity ?? "low"] }} className="text-xs">{inc.severity?.toUpperCase()}</Badge>
                    {inc.lga && <span className="text-xs text-gray-400">{inc.lga}</span>}
                    {inc.incidentType && <span className="text-xs text-gray-500">{inc.incidentType}</span>}
                    <span className="text-xs text-gray-500 ml-auto">{new Date(inc.reportedAt).toLocaleTimeString()}</span>
                  </div>
                  <p className="text-sm text-gray-200">{inc.description}</p>
                </div>
                {inc.status !== "resolved" && (
                  <Button size="sm" variant="ghost" className="text-green-400 text-xs hover:bg-green-900/20"
                    onClick={() => resolveMut.mutate({ id: inc.id, status: "resolved" })}>
                    Resolve
                  </Button>
                )}
              </div>
            ))}
          </div>
          <div className="p-4 border-t border-gray-800 flex gap-2">
            <Select value={severity} onValueChange={v => setSeverity(v as "low"|"medium"|"high"|"critical")}>
              <SelectTrigger className="w-32 bg-gray-800 border-gray-700 text-white"><SelectValue/></SelectTrigger>
              <SelectContent>
                <SelectItem value="low">Low</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="high">High</SelectItem>
                <SelectItem value="critical">Critical</SelectItem>
              </SelectContent>
            </Select>
            <Input placeholder="LGA (optional)" value={lga} onChange={e => setLga(e.target.value)} className="w-40 bg-gray-800 border-gray-700 text-white placeholder:text-gray-500"/>
            <Input placeholder="Describe the incident…" value={desc} onChange={e => setDesc(e.target.value)}
              onKeyDown={e => { if (e.key === "Enter" && desc.trim() && profileId) addMut.mutate({ profileId, description: desc, severity, lga }); }}
              className="flex-1 bg-gray-800 border-gray-700 text-white placeholder:text-gray-500"/>
            <Button onClick={() => { if (!profileId || !desc.trim()) return; addMut.mutate({ profileId, description: desc, severity, lga }); }}
              disabled={addMut.isPending} style={{ background: "#C0392B", color: "white" }} className="gap-1.5">
              {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : <><Plus size={14}/> Log</>}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
