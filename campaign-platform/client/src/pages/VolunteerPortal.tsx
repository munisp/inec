import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Users, Plus, Loader2, ClipboardList, CheckCircle2, Clock, XCircle, Trash2, BarChart2 } from "lucide-react";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell, PieChart, Pie, Legend } from "recharts";

type TaskStatus = "pending" | "in_progress" | "completed" | "cancelled";
type TaskType = "canvassing" | "polling_unit" | "data_entry" | "logistics" | "other" | "social_media" | "other";

const STATUS_COLORS: Record<TaskStatus, string> = {
  pending: "#F59E0B", in_progress: "#1A3A5C", completed: "#008751", cancelled: "#9CA3AF",
};
const STATUS_ICONS: Record<TaskStatus, React.ElementType> = {
  pending: Clock, in_progress: Loader2, completed: CheckCircle2, cancelled: XCircle,
};

export default function VolunteerPortal() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();

  const { data: volunteers = [], isLoading: vLoading } = trpc.volunteers.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const { data: allTasks = [], isLoading: tLoading } = trpc.volunteerTasks.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );

  const addVolunteerMut = trpc.volunteers.add.useMutation({
    onSuccess: () => { utils.volunteers.list.invalidate(); toast.success("Volunteer added"); setVOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const createTaskMut = trpc.volunteerTasks.create.useMutation({
    onSuccess: () => { utils.volunteerTasks.list.invalidate(); toast.success("Task created"); setTOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const updateStatusMut = trpc.volunteerTasks.updateStatus.useMutation({
    onSuccess: () => { utils.volunteerTasks.list.invalidate(); },
    onError: (e) => toast.error(e.message),
  });
  const deleteTaskMut = trpc.volunteerTasks.delete.useMutation({
    onSuccess: () => { utils.volunteerTasks.list.invalidate(); toast.success("Task deleted"); },
    onError: (e) => toast.error(e.message),
  });

  const [vOpen, setVOpen] = useState(false);
  const [tOpen, setTOpen] = useState(false);
  const [vForm, setVForm] = useState({ fullName: "", phone: "", email: "", lga: "", ward: "", role: "", skills: "" });
  const [tForm, setTForm] = useState({
    title: "", description: "", taskType: "canvassing" as TaskType,
    status: "pending" as TaskStatus, volunteerId: "", dueDate: "",
  });

  const pendingCount = allTasks.filter(t => t.status === "pending").length;
  const inProgressCount = allTasks.filter(t => t.status === "in_progress").length;
  const completedCount = allTasks.filter(t => t.status === "completed").length;

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-4 sm:px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14} /><span className="hidden sm:inline">Home</span></Button></Link>
          <Users size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Volunteer Portal</h1>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/30 hover:bg-white/10" onClick={() => setTOpen(true)} disabled={!canEdit}>
            <ClipboardList size={14} /><span className="hidden sm:inline">Assign Task</span>
          </Button>
          <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5" onClick={() => setVOpen(true)} disabled={!canEdit}>
            <Plus size={14} /><span className="hidden sm:inline">Add Volunteer</span>
          </Button>
        </div>
      </header>

      {/* KPI Bar */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 max-w-5xl mx-auto px-4 sm:px-6 py-4">
        {[
          { label: "Total Volunteers", value: volunteers.length, color: "#1A3A5C" },
          { label: "Pending Tasks", value: pendingCount, color: "#F59E0B" },
          { label: "In Progress", value: inProgressCount, color: "#1A3A5C" },
          { label: "Completed", value: completedCount, color: "#008751" },
        ].map(k => (
          <div key={k.label} className="bg-white border border-gray-200 rounded p-4 text-center" style={{ borderTop: `3px solid ${k.color}` }}>
            <p className="text-2xl font-bold font-mono" style={{ color: k.color }}>{k.value}</p>
            <p className="text-xs text-gray-500 mt-1">{k.label}</p>
          </div>
        ))}
      </div>

      <div className="max-w-5xl mx-auto px-4 sm:px-6 pb-8">
        <Tabs defaultValue="tasks">
          <TabsList className="mb-4">
            <TabsTrigger value="tasks">Task Board</TabsTrigger>
            <TabsTrigger value="volunteers">Volunteers ({volunteers.length})</TabsTrigger>
            <TabsTrigger value="analytics">Analytics</TabsTrigger>
          </TabsList>

          <TabsContent value="tasks">
            {tLoading ? (
              <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
            ) : allTasks.length === 0 ? (
              <div className="text-center py-20 text-gray-500">
                <ClipboardList size={48} className="mx-auto mb-4 opacity-30" />
                <p className="mb-4">No tasks assigned yet</p>
                <Button onClick={() => setTOpen(true)} style={{ background: "#4A1525", color: "white" }} disabled={!canEdit}>
                  <Plus size={14} className="mr-1" /> Assign First Task
                </Button>
              </div>
            ) : (
              <div className="space-y-3">
                {allTasks.map(task => {
                  const StatusIcon = STATUS_ICONS[task.status as TaskStatus] ?? Clock;
                  const volunteer = volunteers.find(v => v.id === task.volunteerId);
                  return (
                    <div key={task.id} className="bg-white border border-gray-200 rounded p-4 flex items-start justify-between gap-4">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 flex-wrap mb-1">
                          <StatusIcon size={14} style={{ color: STATUS_COLORS[task.status as TaskStatus] }} />
                          <span className="font-semibold text-gray-900 text-sm">{task.title}</span>
                          <Badge style={{ background: STATUS_COLORS[task.status as TaskStatus] + "22", color: STATUS_COLORS[task.status as TaskStatus], fontSize: "10px" }}>
                            {(task.status ?? "").replace("_", " ")}
                          </Badge>
                          {task.taskType && (
                            <Badge variant="outline" style={{ fontSize: "10px" }}>{task.taskType.replace("_", " ")}</Badge>
                          )}
                        </div>
                        {task.description && <p className="text-xs text-gray-500 mb-1">{task.description}</p>}
                        <div className="flex items-center gap-3 text-xs text-gray-400">
                          {volunteer && <span>Assigned to: <strong>{volunteer.fullName}</strong></span>}
                          {task.dueDate && <span>Due: {new Date(task.dueDate).toLocaleDateString()}</span>}
                        </div>
                      </div>
                      <div className="flex items-center gap-1 flex-shrink-0">
                        {task.status !== "completed" && (
                          <Select
                            value={task.status ?? undefined}
                            onValueChange={(v) => updateStatusMut.mutate({ id: task.id, status: v as TaskStatus })}
                            disabled={!canEdit}
                          >
                            <SelectTrigger className="h-7 text-xs w-28">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="pending">Pending</SelectItem>
                              <SelectItem value="in_progress">In Progress</SelectItem>
                              <SelectItem value="completed">Completed</SelectItem>
                              <SelectItem value="cancelled">Cancelled</SelectItem>
                            </SelectContent>
                          </Select>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-red-400 hover:text-red-600 h-7 w-7 p-0"
                          onClick={() => { if (confirm("Delete task?")) deleteTaskMut.mutate({ id: task.id }); }}
                          disabled={!canDelete}
                        >
                          <Trash2 size={12} />
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </TabsContent>

          <TabsContent value="volunteers">
            {vLoading ? (
              <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400" /></div>
            ) : volunteers.length === 0 ? (
              <div className="text-center py-20 text-gray-500">
                <Users size={48} className="mx-auto mb-4 opacity-30" />
                <p>No volunteers registered yet</p>
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {volunteers.map(v => {
                  const vTasks = allTasks.filter(t => t.volunteerId === v.id);
                  const done = vTasks.filter(t => t.status === "completed").length;
                  return (
                    <div key={v.id} className="bg-white border border-gray-200 rounded p-4">
                      <div className="flex items-start justify-between mb-2">
                        <div>
                          <p className="font-semibold text-gray-900">{v.fullName}</p>
                          <p className="text-xs text-gray-500">{v.role || "Volunteer"} · {v.lga || "—"}</p>
                        </div>
                        <Badge style={{ background: v.status === "active" ? "#E6F4EE" : "#F3F4F6", color: v.status === "active" ? "#008751" : "#6B7280" }}>
                          {v.status}
                        </Badge>
                      </div>
                      {v.phone && <p className="text-xs text-gray-400 mb-1">📞 {v.phone}</p>}
                      {v.skills && <p className="text-xs text-gray-400 mb-2">Skills: {v.skills}</p>}
                      <p className="text-xs text-gray-500">{vTasks.length} tasks · {done} completed</p>
                    </div>
                  );
                })}
              </div>
            )}
          </TabsContent>

          <TabsContent value="analytics">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              {/* Task status breakdown */}
              <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #4A1525" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Task Status Breakdown</p>
                <ResponsiveContainer width="100%" height={220}>
                  <PieChart>
                    <Pie
                      data={[
                        { name: "Pending", value: pendingCount },
                        { name: "In Progress", value: inProgressCount },
                        { name: "Completed", value: completedCount },
                        { name: "Cancelled", value: allTasks.filter(t => t.status === "cancelled").length },
                      ].filter(d => d.value > 0)}
                      dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={80}
                      label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`} labelLine={false}
                    >
                      {[STATUS_COLORS.pending, STATUS_COLORS.in_progress, STATUS_COLORS.completed, "#9CA3AF"].map((color, i) => (
                        <Cell key={i} fill={color} />
                      ))}
                    </Pie>
                    <Legend />
                    <Tooltip />
                  </PieChart>
                </ResponsiveContainer>
              </div>
              {/* Task type breakdown */}
              <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #1A3A5C" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Tasks by Type</p>
                <ResponsiveContainer width="100%" height={220}>
                  <BarChart
                    data={Object.entries(
                      allTasks.reduce<Record<string, number>>((acc, t) => {
                        const type = (t.taskType ?? "other").replace("_", " ");
                        acc[type] = (acc[type] ?? 0) + 1;
                        return acc;
                      }, {})
                    ).map(([name, count]) => ({ name, count }))}
                    margin={{ top: 5, right: 10, left: 0, bottom: 30 }}
                  >
                    <XAxis dataKey="name" tick={{ fontSize: 10 }} angle={-25} textAnchor="end" height={50} />
                    <YAxis tick={{ fontSize: 11 }} allowDecimals={false} />
                    <Tooltip />
                    <Bar dataKey="count" radius={[3, 3, 0, 0]} fill="#1A3A5C" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
              {/* Volunteer task load */}
              <div className="bg-white border border-gray-200 rounded p-5 md:col-span-2" style={{ borderTop: "3px solid #008751" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Task Load per Volunteer</p>
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart
                    data={volunteers.map(v => ({
                      name: v.fullName.split(" ")[0],
                      total: allTasks.filter(t => t.volunteerId === v.id).length,
                      done: allTasks.filter(t => t.volunteerId === v.id && t.status === "completed").length,
                    })).filter(v => v.total > 0).sort((a, b) => b.total - a.total).slice(0, 12)}
                    margin={{ top: 5, right: 10, left: 0, bottom: 30 }}
                  >
                    <XAxis dataKey="name" tick={{ fontSize: 10 }} angle={-25} textAnchor="end" height={50} />
                    <YAxis tick={{ fontSize: 11 }} allowDecimals={false} />
                    <Tooltip />
                    <Bar dataKey="total" name="Total" fill="#4A1525" radius={[3, 3, 0, 0]} />
                    <Bar dataKey="done" name="Completed" fill="#008751" radius={[3, 3, 0, 0]} />
                    <Legend />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>
          </TabsContent>
        </Tabs>
      </div>

      {/* Add Volunteer Dialog */}
      <Dialog open={vOpen} onOpenChange={setVOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>Add Volunteer</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <Input placeholder="Full Name *" value={vForm.fullName} onChange={e => setVForm(f => ({ ...f, fullName: e.target.value }))} />
            <div className="grid grid-cols-2 gap-2">
              <Input placeholder="Phone" value={vForm.phone} onChange={e => setVForm(f => ({ ...f, phone: e.target.value }))} />
              <Input placeholder="Email" value={vForm.email} onChange={e => setVForm(f => ({ ...f, email: e.target.value }))} />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <Input placeholder="LGA" value={vForm.lga} onChange={e => setVForm(f => ({ ...f, lga: e.target.value }))} />
              <Input placeholder="Ward" value={vForm.ward} onChange={e => setVForm(f => ({ ...f, ward: e.target.value }))} />
            </div>
            <Input placeholder="Role (e.g. Polling Agent)" value={vForm.role} onChange={e => setVForm(f => ({ ...f, role: e.target.value }))} />
            <Input placeholder="Skills (comma-separated)" value={vForm.skills} onChange={e => setVForm(f => ({ ...f, skills: e.target.value }))} />
            <Button
              onClick={() => {
                if (!profileId || !vForm.fullName) return toast.error("Full name required");
                addVolunteerMut.mutate({ profileId, ...vForm, skills: vForm.skills ? vForm.skills.split(',').map(s => s.trim()).filter(Boolean) : [] });
              }}
              disabled={addVolunteerMut.isPending}
              style={{ background: "#4A1525", color: "white" }}
            >
              {addVolunteerMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Add Volunteer"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Assign Task Dialog */}
      <Dialog open={tOpen} onOpenChange={setTOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>Assign Task</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <Input placeholder="Task Title *" value={tForm.title} onChange={e => setTForm(f => ({ ...f, title: e.target.value }))} />
            <textarea
              rows={3}
              placeholder="Description (optional)"
              value={tForm.description}
              onChange={e => setTForm(f => ({ ...f, description: e.target.value }))}
              className="w-full text-sm border border-gray-200 rounded p-3 resize-none outline-none focus:border-gray-400"
            />
            <div className="grid grid-cols-2 gap-2">
              <Select value={tForm.taskType} onValueChange={v => setTForm(f => ({ ...f, taskType: v as TaskType }))}>
                <SelectTrigger><SelectValue placeholder="Task Type" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="canvassing">Canvassing</SelectItem>
                  <SelectItem value="polling_unit">Polling Unit</SelectItem>
                  <SelectItem value="data_entry">Data Entry</SelectItem>
                  <SelectItem value="logistics">Logistics</SelectItem>
                  <SelectItem value="other">Security</SelectItem>
                  <SelectItem value="social_media">Media</SelectItem>
                  <SelectItem value="other">Other</SelectItem>
                </SelectContent>
              </Select>
              <Select value={tForm.volunteerId} onValueChange={v => setTForm(f => ({ ...f, volunteerId: v }))}>
                <SelectTrigger><SelectValue placeholder="Assign to…" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="">Unassigned</SelectItem>
                  {volunteers.map(v => (
                    <SelectItem key={v.id} value={String(v.id)}>{v.fullName}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <Input type="date" value={tForm.dueDate} onChange={e => setTForm(f => ({ ...f, dueDate: e.target.value }))} />
            <Button
              onClick={() => {
                if (!profileId || !tForm.title) return toast.error("Task title required");
                createTaskMut.mutate({
                  profileId,
                  title: tForm.title,
                  description: tForm.description || undefined,
                  taskType: tForm.taskType,
                  status: tForm.status,
                  volunteerId: tForm.volunteerId ? parseInt(tForm.volunteerId) : undefined,
                  dueDate: tForm.dueDate || undefined,
                });
              }}
              disabled={createTaskMut.isPending}
              style={{ background: "#4A1525", color: "white" }}
            >
              {createTaskMut.isPending ? <Loader2 size={14} className="animate-spin" /> : "Create Task"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
