import { useState } from "react";
import { Link } from "wouter";
import { ArrowLeft, Users, UserPlus, Trash2, Shield, Eye, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";

const ROLE_META = {
  owner:   { label: "Owner",   color: "bg-[#4A1525] text-white", icon: Shield },
  manager: { label: "Manager", color: "bg-[#1A3A5C] text-white", icon: Shield },
  viewer:  { label: "Viewer",  color: "bg-gray-200 text-gray-700", icon: Eye },
};

export default function CampaignTeam() {
  const { profileId } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: members = [], isLoading } = trpc.team.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const inviteMut = trpc.team.invite.useMutation({
    onSuccess: () => { utils.team.list.invalidate(); toast.success("Member invited"); setForm({ name: "", email: "", role: "viewer" }); },
    onError: e => toast.error(e.message),
  });
  const updateRoleMut = trpc.team.updateRole.useMutation({
    onSuccess: () => { utils.team.list.invalidate(); toast.success("Role updated"); },
    onError: e => toast.error(e.message),
  });
  const removeMut = trpc.team.remove.useMutation({
    onSuccess: () => { utils.team.list.invalidate(); toast.success("Member removed"); },
    onError: e => toast.error(e.message),
  });

  const [form, setForm] = useState({ name: "", email: "", role: "viewer" as "manager" | "viewer" });

  const handleInvite = () => {
    if (!profileId) return toast.error("No profile selected");
    if (!form.name.trim()) return toast.error("Name is required");
    if (!form.email.trim()) return toast.error("Email is required");
    inviteMut.mutate({ profileId, ...form });
  };

  return (
    <div className="min-h-screen bg-[#F5F0EB]">
      <header className="bg-[#4A1525] px-4 sm:px-6 py-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Users size={18} className="text-white"/>
          <h1 className="text-white font-bold text-base sm:text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Campaign Team</h1>
        </div>
        <Badge variant="secondary">{members.length} member{members.length !== 1 ? "s" : ""}</Badge>
      </header>

      <div className="max-w-4xl mx-auto px-4 sm:px-6 py-8 space-y-6">
        {/* Invite Form */}
        <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #008751" }}>
          <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4 flex items-center gap-1"><UserPlus size={12}/> Invite Team Member</p>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <Input placeholder="Full name" value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} className="text-sm"/>
            <Input placeholder="Email address" type="email" value={form.email} onChange={e => setForm(f => ({ ...f, email: e.target.value }))} className="text-sm"/>
            <Select value={form.role} onValueChange={v => setForm(f => ({ ...f, role: v as "manager" | "viewer" }))}>
              <SelectTrigger className="text-sm"><SelectValue/></SelectTrigger>
              <SelectContent>
                <SelectItem value="manager">Manager — can add/edit records</SelectItem>
                <SelectItem value="viewer">Viewer — read-only access</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button className="mt-3 gap-1 bg-[#008751] hover:bg-[#006B40] text-white" onClick={handleInvite} disabled={inviteMut.isPending}>
            {inviteMut.isPending ? <Loader2 size={14} className="animate-spin"/> : <UserPlus size={14}/>} Invite Member
          </Button>
        </div>

        {/* Role Permissions Reference */}
        <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #1A3A5C" }}>
          <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Role Permissions</p>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm">
            {[
              { role: "Owner", perms: ["Full access", "Invite/remove members", "Delete profile", "All modules"], color: "#4A1525" },
              { role: "Manager", perms: ["Add/edit all records", "View all modules", "Cannot delete profile", "Cannot manage team"], color: "#1A3A5C" },
              { role: "Viewer", perms: ["Read-only access", "View all modules", "Cannot add/edit/delete", "Cannot manage team"], color: "#666" },
            ].map(({ role, perms, color }) => (
              <div key={role} className="border border-gray-100 rounded p-3">
                <p className="font-bold text-sm mb-2" style={{ color }}>{role}</p>
                <ul className="space-y-1">
                  {perms.map(p => <li key={p} className="text-xs text-gray-600 flex items-center gap-1"><span className="w-1 h-1 rounded-full bg-gray-400 flex-shrink-0"/>{p}</li>)}
                </ul>
              </div>
            ))}
          </div>
        </div>

        {/* Members List */}
        <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #4A1525" }}>
          <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Team Members</p>
          {isLoading ? (
            <div className="flex items-center gap-2 text-gray-400 text-sm"><Loader2 size={14} className="animate-spin"/> Loading…</div>
          ) : members.length === 0 ? (
            <p className="text-sm text-gray-400 text-center py-8">No team members yet. Invite someone above to get started.</p>
          ) : (
            <div className="space-y-3">
              {members.map(m => {
                const meta = ROLE_META[m.role as keyof typeof ROLE_META] ?? ROLE_META.viewer;
                const Icon = meta.icon;
                return (
                  <div key={m.id} className="flex flex-wrap items-center justify-between gap-3 p-3 border border-gray-100 rounded hover:bg-gray-50">
                    <div className="flex items-center gap-3">
                      <div className="w-9 h-9 rounded-full bg-[#4A1525] flex items-center justify-center text-white font-bold text-sm flex-shrink-0">
                        {m.name.charAt(0).toUpperCase()}
                      </div>
                      <div>
                        <p className="font-semibold text-sm text-gray-800">{m.name}</p>
                        <p className="text-xs text-gray-400">{m.email}</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge className={`${meta.color} text-xs gap-1`}><Icon size={10}/>{meta.label}</Badge>
                      {m.role !== "owner" && (
                        <>
                          <Select value={m.role} onValueChange={v => updateRoleMut.mutate({ memberId: m.id, role: v as "manager" | "viewer" })}>
                            <SelectTrigger className="h-7 text-xs w-28"><SelectValue/></SelectTrigger>
                            <SelectContent>
                              <SelectItem value="manager">Manager</SelectItem>
                              <SelectItem value="viewer">Viewer</SelectItem>
                            </SelectContent>
                          </Select>
                          <Button variant="ghost" size="sm" className="text-red-500 hover:text-red-700 h-7 w-7 p-0"
                            onClick={() => removeMut.mutate({ memberId: m.id })}>
                            <Trash2 size={12}/>
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
