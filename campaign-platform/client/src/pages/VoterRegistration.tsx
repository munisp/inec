import { useState, useRef } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Plus, UserCheck, Loader2, Search, Upload, Download } from "lucide-react";

function parseVoterCSV(text: string): Array<{ fullName: string; vin?: string; lga?: string; ward?: string; pollingUnit?: string; phone?: string }> {
  const lines = text.trim().split("\n").filter(l => l.trim());
  if (lines.length < 2) return [];
  const headers = lines[0].split(",").map(h => h.trim().toLowerCase().replace(/\s+/g, "_"));
  const colIdx = (names: string[]) => names.map(n => headers.indexOf(n)).find(i => i >= 0) ?? -1;
  const nameIdx = colIdx(["full_name","fullname","name","voter_name"]);
  const vinIdx = colIdx(["vin","voter_id","voter_identification_number"]);
  const lgaIdx = colIdx(["lga","local_government","local_government_area"]);
  const wardIdx = colIdx(["ward"]);
  const puIdx = colIdx(["polling_unit","pollingunit","pu","polling_unit_name"]);
  const phoneIdx = colIdx(["phone","mobile","phone_number"]);
  if (nameIdx < 0) throw new Error("CSV must have a 'full_name' or 'name' column");
  return lines.slice(1).map(line => {
    const cols = line.split(",").map(c => c.trim().replace(/^"|"$/g, ""));
    return {
      fullName: cols[nameIdx] ?? "",
      vin: vinIdx >= 0 ? cols[vinIdx] : undefined,
      lga: lgaIdx >= 0 ? cols[lgaIdx] : undefined,
      ward: wardIdx >= 0 ? cols[wardIdx] : undefined,
      pollingUnit: puIdx >= 0 ? cols[puIdx] : undefined,
      phone: phoneIdx >= 0 ? cols[phoneIdx] : undefined,
    };
  }).filter(v => v.fullName);
}

export default function VoterRegistration() {
  const { profileId } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: voters = [], isLoading } = trpc.voters.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const addMut = trpc.voters.add.useMutation({
    onSuccess: () => { utils.voters.list.invalidate(); toast.success("Voter registered"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const bulkMut = trpc.voters.bulkImport.useMutation({
    onSuccess: d => { utils.voters.list.invalidate(); toast.success(`Imported ${d.inserted} voters`); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [form, setForm] = useState({ fullName: "", vin: "", lga: "", ward: "", pollingUnit: "", phone: "" });
  const fileRef = useRef<HTMLInputElement>(null);

  const filtered = voters.filter(v =>
    v.fullName.toLowerCase().includes(search.toLowerCase()) || (v.vin ?? "").includes(search)
  );

  const handleCSVUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file || !profileId) return;
    const reader = new FileReader();
    reader.onload = ev => {
      try {
        const rows = parseVoterCSV(ev.target?.result as string);
        if (rows.length === 0) return toast.error("No valid rows found in CSV");
        bulkMut.mutate({ profileId, rows });
      } catch (err: any) {
        toast.error(err.message ?? "CSV parse error");
      }
    };
    reader.readAsText(file);
    e.target.value = "";
  };

  const handleExportCSV = () => {
    const header = "Full Name,VIN,LGA,Ward,Polling Unit,Phone,Registered At";
    const rows = voters.map(v =>
      [v.fullName, v.vin ?? "", v.lga ?? "", v.ward ?? "", v.pollingUnit ?? "", v.phone ?? "", new Date(v.registeredAt).toLocaleDateString()].join(",")
    );
    const blob = new Blob([header + "\n" + rows.join("\n")], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a"); a.href = url; a.download = "voters.csv"; a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <UserCheck size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Voter Registration</h1>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <div className="relative">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"/>
            <Input placeholder="Search voters…" value={search} onChange={e=>setSearch(e.target.value)} className="pl-9 w-48 bg-white/10 text-white placeholder:text-white/60 border-white/20"/>
          </div>
          {/* CSV bulk import */}
          <input ref={fileRef} type="file" accept=".csv" className="hidden" onChange={handleCSVUpload}/>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10"
            disabled={bulkMut.isPending} onClick={() => fileRef.current?.click()}>
            {bulkMut.isPending ? <Loader2 size={13} className="animate-spin"/> : <Upload size={13}/>} Import CSV
          </Button>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10"
            disabled={voters.length === 0} onClick={handleExportCSV}>
            <Download size={13}/> Export CSV
          </Button>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5">
                <Plus size={14}/> Register Voter
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Register New Voter</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Full Name *" value={form.fullName} onChange={e=>setForm(f=>({...f,fullName:e.target.value}))}/>
                <Input placeholder="VIN (Voter ID Number)" value={form.vin} onChange={e=>setForm(f=>({...f,vin:e.target.value}))}/>
                <Input placeholder="LGA" value={form.lga} onChange={e=>setForm(f=>({...f,lga:e.target.value}))}/>
                <Input placeholder="Ward" value={form.ward} onChange={e=>setForm(f=>({...f,ward:e.target.value}))}/>
                <Input placeholder="Polling Unit" value={form.pollingUnit} onChange={e=>setForm(f=>({...f,pollingUnit:e.target.value}))}/>
                <Input placeholder="Phone" value={form.phone} onChange={e=>setForm(f=>({...f,phone:e.target.value}))}/>
                <Button onClick={() => { if (!profileId || !form.fullName) return toast.error("Name required"); addMut.mutate({ profileId, ...form }); }}
                  disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Register"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8">
        {/* CSV format hint */}
        <div className="mb-4 p-3 rounded text-xs text-gray-600" style={{ background: "#EBF2F8", border: "1px solid #BDD4E8" }}>
          <strong>CSV Import Format:</strong> Columns: <code>full_name, vin, lga, ward, polling_unit, phone</code>. First row must be headers. Only <code>full_name</code> is required.
        </div>
        <div className="flex items-center justify-between mb-4">
          <p className="text-sm text-gray-600"><span className="font-bold text-gray-900">{voters.length}</span> registered voters</p>
        </div>
        {isLoading
          ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
          : filtered.length === 0
            ? <div className="text-center py-20 text-gray-500"><UserCheck size={48} className="mx-auto mb-4 opacity-30"/><p>No voters found. Register manually or import a CSV file.</p></div>
            : (
              <div className="bg-white border border-gray-200 rounded overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="bg-gray-50 border-b border-gray-200">
                      {["Name","VIN","LGA","Ward","Polling Unit","Phone","Registered"].map(h=>(
                        <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {filtered.map((v,i)=>(
                      <tr key={v.id} className={i%2===0?"bg-white":"bg-gray-50/50"}>
                        <td className="px-4 py-3 font-medium text-gray-900">{v.fullName}</td>
                        <td className="px-4 py-3 font-mono text-gray-600">{v.vin ?? "—"}</td>
                        <td className="px-4 py-3 text-gray-600">{v.lga ?? "—"}</td>
                        <td className="px-4 py-3 text-gray-600">{v.ward ?? "—"}</td>
                        <td className="px-4 py-3 text-gray-600">{v.pollingUnit ?? "—"}</td>
                        <td className="px-4 py-3 text-gray-600">{v.phone ?? "—"}</td>
                        <td className="px-4 py-3 text-xs text-gray-400">{new Date(v.registeredAt).toLocaleDateString()}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
      </div>
    </div>
  );
}
