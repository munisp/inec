import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Save, User } from "lucide-react";

const STATES = ["Abia","Adamawa","Akwa Ibom","Anambra","Bauchi","Bayelsa","Benue","Borno","Cross River","Delta","Ebonyi","Edo","Ekiti","Enugu","FCT","Gombe","Imo","Jigawa","Kaduna","Kano","Katsina","Kebbi","Kogi","Kwara","Lagos","Nasarawa","Niger","Ogun","Ondo","Osun","Oyo","Plateau","Rivers","Sokoto","Taraba","Yobe","Zamfara"];
const ZONES = ["North-Central","North-East","North-West","South-East","South-South","South-West"];

export default function CandidateProfilePage() {
  const { profile, refetch } = useCandidateProfile();
  const utils = trpc.useUtils();

  const [form, setForm] = useState({
    candidateName: profile?.candidateName ?? "",
    partyName: profile?.partyName ?? "",
    partyColor: profile?.partyColor ?? "#006400",
    stateCode: profile?.stateCode ?? "",
    stateName: profile?.stateName ?? "",
    office: (profile?.office ?? "Governor") as "President"|"Governor"|"Senator"|"House"|"LGA",
    religion: profile?.religion ?? "",
    gender: profile?.gender ?? "",
    geopoliticalZone: profile?.geopoliticalZone ?? "",
  });

  const updateMut = trpc.profile.update.useMutation({
    onSuccess: () => {
      utils.profile.get.invalidate();
      refetch();
      toast.success("Profile saved successfully");
    },
    onError: (e) => toast.error("Save failed: " + e.message),
  });

  const handleSave = () => {
    if (!profile) return;
    updateMut.mutate({ id: profile.id, ...form });
  };

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center gap-4">
        <Link href="/">
          <Button variant="ghost" size="sm" className="text-white gap-1.5 hover:bg-white/10">
            <ArrowLeft size={14} /> Back
          </Button>
        </Link>
        <div className="flex items-center gap-2">
          <User size={18} className="text-white" />
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>
            Candidate Profile
          </h1>
        </div>
      </header>

      <div className="max-w-2xl mx-auto px-6 py-8">
        <div className="bg-white border border-gray-200 rounded p-6" style={{ borderTop: "3px solid #4A1525" }}>
          <h2 className="font-bold text-lg text-gray-900 mb-6">Campaign Identity</h2>
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <Label>Candidate Full Name</Label>
              <Input value={form.candidateName} onChange={e => setForm(f => ({ ...f, candidateName: e.target.value }))} className="mt-1" />
            </div>
            <div>
              <Label>Party Name</Label>
              <Input value={form.partyName} onChange={e => setForm(f => ({ ...f, partyName: e.target.value }))} className="mt-1" />
            </div>
            <div>
              <Label>Party Colour</Label>
              <div className="flex gap-2 mt-1">
                <input type="color" value={form.partyColor} onChange={e => setForm(f => ({ ...f, partyColor: e.target.value }))} className="w-10 h-10 rounded border cursor-pointer" />
                <Input value={form.partyColor} onChange={e => setForm(f => ({ ...f, partyColor: e.target.value }))} className="flex-1" />
              </div>
            </div>
            <div>
              <Label>State</Label>
              <Select value={form.stateName} onValueChange={v => setForm(f => ({ ...f, stateName: v, stateCode: v.toUpperCase().slice(0,3) }))}>
                <SelectTrigger className="mt-1"><SelectValue placeholder="Select state" /></SelectTrigger>
                <SelectContent>{STATES.map(s => <SelectItem key={s} value={s}>{s}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div>
              <Label>Office Sought</Label>
              <Select value={form.office} onValueChange={v => setForm(f => ({ ...f, office: v as any }))}>
                <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {["President","Governor","Senator","House","LGA"].map(o => <SelectItem key={o} value={o}>{o}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label>Geopolitical Zone</Label>
              <Select value={form.geopoliticalZone} onValueChange={v => setForm(f => ({ ...f, geopoliticalZone: v }))}>
                <SelectTrigger className="mt-1"><SelectValue placeholder="Select zone" /></SelectTrigger>
                <SelectContent>{ZONES.map(z => <SelectItem key={z} value={z}>{z}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div>
              <Label>Gender</Label>
              <Select value={form.gender} onValueChange={v => setForm(f => ({ ...f, gender: v }))}>
                <SelectTrigger className="mt-1"><SelectValue placeholder="Select" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="Male">Male</SelectItem>
                  <SelectItem value="Female">Female</SelectItem>
                  <SelectItem value="Prefer not to say">Prefer not to say</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label>Religion</Label>
              <Input value={form.religion} onChange={e => setForm(f => ({ ...f, religion: e.target.value }))} className="mt-1" placeholder="e.g. Islam, Christianity, Mixed" />
            </div>
          </div>
          <div className="mt-6 flex justify-end">
            <Button onClick={handleSave} disabled={updateMut.isPending} className="gap-2" style={{ background: "#4A1525", color: "white" }}>
              <Save size={14} /> {updateMut.isPending ? "Saving…" : "Save Profile"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

