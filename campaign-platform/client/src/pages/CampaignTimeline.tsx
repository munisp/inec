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
import { ArrowLeft, Plus, Calendar, Trash2, Loader2, BarChart2 } from "lucide-react";

const STATUS_COLORS: Record<string, string> = {
  active: "#008751", pending: "#1A3A5C", completed: "#666",
  cancelled: "#C0392B", inactive: "#999",
};
const PRIORITIES = ["low","medium","high","critical"] as const;
const CATEGORIES = ["Rally","Debate","Media","Legal","Logistics","Finance","Outreach","Other"];

export default function CampaignTimeline() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: events = [], isLoading } = trpc.timeline.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const upsertMut = trpc.timeline.upsert.useMutation({
    onSuccess: () => { utils.timeline.list.invalidate(); toast.success("Event saved"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const deleteMut = trpc.timeline.delete.useMutation({
    onSuccess: () => { utils.timeline.list.invalidate(); toast.success("Event deleted"); },
  });

  const [open, setOpen] = useState(false);
  const [viewMode, setViewMode] = useState<"timeline" | "gantt">("timeline");
  const [form, setForm] = useState({ title: "", description: "", eventDate: "", category: "Rally", status: "pending" as const, priority: "medium" as const, location: "" });

  const handleSave = () => {
    if (!profileId || !form.title || !form.eventDate) return toast.error("Title and date required");
    upsertMut.mutate({ profileId, ...form });
  };

  // Gantt data: find date range
  const sortedByDate = [...events].filter(e => e.eventDate).sort((a, b) => new Date(a.eventDate).getTime() - new Date(b.eventDate).getTime());
  const minDate = sortedByDate.length > 0 ? new Date(sortedByDate[0].eventDate) : new Date();
  const maxDate = sortedByDate.length > 0 ? new Date(sortedByDate[sortedByDate.length - 1].eventDate) : new Date();
  const totalDays = Math.max(1, Math.ceil((maxDate.getTime() - minDate.getTime()) / 86400000) + 1);

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Calendar size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Campaign Timeline</h1>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Event</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>New Campaign Event</DialogTitle></DialogHeader>
            <div className="grid gap-3 py-2">
              <Input placeholder="Event title *" value={form.title} onChange={e => setForm(f=>({...f,title:e.target.value}))} />
              <Input type="date" value={form.eventDate} onChange={e => setForm(f=>({...f,eventDate:e.target.value}))} />
              <Input placeholder="Location" value={form.location} onChange={e => setForm(f=>({...f,location:e.target.value}))} />
              <Input placeholder="Description" value={form.description} onChange={e => setForm(f=>({...f,description:e.target.value}))} />
              <Select value={form.category} onValueChange={v=>setForm(f=>({...f,category:v}))}>
                <SelectTrigger><SelectValue/></SelectTrigger>
                <SelectContent>{CATEGORIES.map(c=><SelectItem key={c} value={c}>{c}</SelectItem>)}</SelectContent>
              </Select>
              <Select value={form.priority} onValueChange={v=>setForm(f=>({...f,priority:v as any}))}>
                <SelectTrigger><SelectValue/></SelectTrigger>
                <SelectContent>{PRIORITIES.map(p=><SelectItem key={p} value={p}>{p}</SelectItem>)}</SelectContent>
              </Select>
              <Button onClick={handleSave} disabled={!canEdit || upsertMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                {upsertMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Save Event"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </header>
      <div className="max-w-4xl mx-auto px-6 py-8">
        {/* View toggle */}
        <div className="flex gap-2 mb-4">
          {(["timeline", "gantt"] as const).map(v => (
            <button key={v} onClick={() => setViewMode(v)}
              className={`px-4 py-1.5 text-xs font-semibold uppercase tracking-wide rounded transition-all ${viewMode === v ? "text-white" : "bg-white text-gray-600 border border-gray-200"}`}
              style={viewMode === v ? { background: "#4A1525" } : {}}>
              {v === "timeline" ? "Timeline View" : "Gantt View"}
            </button>
          ))}
        </div>

        {/* Gantt View */}
        {viewMode === "gantt" && events.length > 0 && (
          <div className="bg-white border border-gray-200 rounded p-5 mb-4 overflow-x-auto" style={{ borderTop: "3px solid #1A3A5C" }}>
            <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Gantt Chart — Campaign Events</p>
            <div className="min-w-[600px]">
              {sortedByDate.map(ev => {
                const start = Math.max(0, Math.ceil((new Date(ev.eventDate).getTime() - minDate.getTime()) / 86400000));
                const barLeft = (start / totalDays) * 100;
                const barWidth = Math.max(1.5, (1 / totalDays) * 100);
                const color = STATUS_COLORS[ev.status ?? "pending"];
                return (
                  <div key={ev.id} className="flex items-center gap-3 mb-2">
                    <div className="w-32 shrink-0 text-xs text-gray-700 truncate font-medium">{ev.title}</div>
                    <div className="flex-1 relative h-7 bg-gray-100 rounded">
                      <div
                        className="absolute top-0.5 bottom-0.5 rounded flex items-center px-1.5"
                        style={{ left: `${barLeft}%`, width: `${barWidth}%`, background: color, minWidth: "8px" }}
                        title={`${ev.title} — ${new Date(ev.eventDate).toLocaleDateString("en-NG")}`}
                      >
                        <span className="text-white text-xs font-mono truncate hidden sm:block">{new Date(ev.eventDate).toLocaleDateString("en-NG", { day: "numeric", month: "short" })}</span>
                      </div>
                    </div>
                    <div className="w-16 shrink-0 text-xs text-gray-400 text-right">{ev.category ?? ""}</div>
                  </div>
                );
              })}
              <div className="flex mt-3 pt-3 border-t border-gray-100">
                <div className="w-32 shrink-0" />
                <div className="flex-1 flex justify-between text-xs text-gray-400">
                  <span>{minDate.toLocaleDateString("en-NG", { month: "short", day: "numeric" })}</span>
                  <span>{maxDate.toLocaleDateString("en-NG", { month: "short", day: "numeric" })}</span>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Timeline View */}
        {viewMode === "timeline" && (isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : events.length === 0 ? (
          <div className="text-center py-20 text-gray-500">
            <Calendar size={48} className="mx-auto mb-4 opacity-30"/>
            <p className="font-semibold">No events yet</p>
            <p className="text-sm">Add your first campaign event above.</p>
          </div>
        ) : (
          <div className="relative">
            <div className="absolute left-6 top-0 bottom-0 w-0.5 bg-gray-300"/>
            <div className="space-y-4">
              {events.map(ev => (
                <div key={ev.id} className="relative flex gap-4 pl-14">
                  <div className="absolute left-4 w-4 h-4 rounded-full border-2 border-white mt-1" style={{ background: STATUS_COLORS[ev.status ?? "pending"] }}/>
                  <div className="bg-white border border-gray-200 rounded p-4 flex-1" style={{ borderLeft: `3px solid ${STATUS_COLORS[ev.status ?? "pending"]}` }}>
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="font-semibold text-gray-900">{ev.title}</p>
                        <p className="text-xs text-gray-500 mt-0.5">{new Date(ev.eventDate).toLocaleDateString("en-NG", { weekday:"short", year:"numeric", month:"short", day:"numeric" })} {ev.location && `· ${ev.location}`}</p>
                        {ev.description && <p className="text-sm text-gray-600 mt-1">{ev.description}</p>}
                        <div className="flex gap-2 mt-2">
                          <Badge style={{ background: STATUS_COLORS[ev.status ?? "pending"] + "22", color: STATUS_COLORS[ev.status ?? "pending"] }}>{ev.status}</Badge>
                          {ev.category && <Badge variant="outline">{ev.category}</Badge>}
                          {ev.priority && <Badge variant="outline">{ev.priority}</Badge>}
                        </div>
                      </div>
                      <Button variant="ghost" size="sm" onClick={() => deleteMut.mutate({ id: ev.id })} className="text-red-400 hover:text-red-600">
                        <Trash2 size={14}/>
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
