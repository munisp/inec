import { useState, useEffect } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, FileSignature, Plus, Loader2, Copy, Share2, QrCode } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";

export default function PetitionDrive() {
  const { profileId, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const [showQR, setShowQR] = useState(false);

  // Get or create the campaign petition
  const { data: petitions = [], isLoading: loadingPetitions } = trpc.petitions.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const createPetitionMut = trpc.petitions.create.useMutation({
    onSuccess: () => utils.petitions.list.invalidate(),
  });

  // Auto-create petition if none exists
  useEffect(() => {
    if (!profileId || loadingPetitions || petitions.length > 0) return;
    createPetitionMut.mutate({ profileId, title: "Campaign Support Petition", description: "Show your support for this campaign.", targetSignatures: 10000 });
  }, [profileId, loadingPetitions, petitions.length]);

  const petition = petitions[0];

  const { data: signatures = [], isLoading: loadingSigs } = trpc.petitions.signatures.useQuery(
    { petitionId: petition?.id ?? 0 }, { enabled: !!petition?.id }
  );
  const signMut = trpc.petitions.sign.useMutation({
    onSuccess: () => { utils.petitions.signatures.invalidate(); toast.success("Signature added"); setForm({ name: "", phone: "", lga: "" }); },
    onError: (e) => toast.error(e.message),
  });

  const [form, setForm] = useState({ name: "", phone: "", lga: "" });
  const goal = petition?.targetSignatures ?? 10000;
  const pct = Math.min(100, Math.round((signatures.length / goal) * 100));
  const shareUrl = petition ? `${window.location.origin}/sign/${petition.id}` : null;
  const copyShareLink = () => {
    if (!shareUrl) return;
    navigator.clipboard.writeText(shareUrl).then(() => toast.success("Shareable link copied to clipboard!"));
  };

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <FileSignature size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Petition & Signature Drive</h1>
        </div>
        <div className="text-right">
          <p className="text-xs text-white/60">SIGNATURES</p>
          <p className="font-mono font-bold text-white">{signatures.length.toLocaleString()} / {goal.toLocaleString()}</p>
        </div>
      </header>
      <div className="max-w-4xl mx-auto px-6 py-8">
        {petition && (
          <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #4A1525" }}>
            <h2 className="font-bold text-gray-900 mb-1">{petition.title}</h2>
            {petition.description && <p className="text-sm text-gray-600 mb-3">{petition.description}</p>}
            <div className="flex justify-between mb-2">
              <p className="text-sm font-semibold text-gray-700">Progress to Goal</p>
              <p className="font-mono font-bold" style={{ color: pct >= 100 ? "#008751" : "#4A1525" }}>{pct}%</p>
            </div>
            <div className="h-4 bg-gray-100 rounded-full overflow-hidden">
              <div className="h-full rounded-full transition-all" style={{ width: `${pct}%`, background: pct >= 100 ? "#008751" : "#4A1525" }}/>
            </div>
          </div>
        )}
        {/* Shareable Public Link */}
        {petition && shareUrl && (
          <div className="bg-white border border-gray-200 rounded p-4 mb-4">
            <div className="flex items-center gap-2 mb-2">
              <Share2 size={14} style={{ color: "#008751" }} />
              <span className="text-xs font-bold uppercase tracking-widest text-gray-500">Public Signing Link</span>
            </div>
            <p className="text-xs text-gray-500 mb-3">Share this link so anyone can sign without needing an account.</p>
            <div className="flex items-center gap-2">
              <input
                readOnly
                value={shareUrl}
                className="flex-1 text-xs border border-gray-200 rounded px-3 py-2 bg-gray-50 font-mono text-gray-700 select-all"
                onClick={e => (e.target as HTMLInputElement).select()}
              />
              <button
                onClick={copyShareLink}
                className="flex items-center gap-1.5 px-3 py-2 text-xs font-semibold text-white rounded transition-all active:scale-95"
                style={{ background: "#008751" }}
              >
                <Copy size={12} /> Copy
              </button>
              <button
                onClick={() => setShowQR(q => !q)}
                className="flex items-center gap-1.5 px-3 py-2 text-xs font-semibold rounded border border-gray-200 hover:bg-gray-50 transition-all"
              >
                <QrCode size={12} /> QR
              </button>
            </div>
            {showQR && (
              <div className="mt-4 flex flex-col items-center gap-2">
                <p className="text-xs text-gray-500">Scan to sign the petition</p>
                <div className="p-3 border border-gray-200 rounded bg-white inline-block">
                  <QRCodeSVG value={shareUrl} size={160} fgColor="#4A1525" />
                </div>
                <p className="text-xs text-gray-400 font-mono">{petition?.title}</p>
              </div>
            )}
          </div>
        )}
        {/* Add signature form */}
        <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #008751" }}>
          <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Add Signature</p>
          <div className="grid grid-cols-3 gap-3">
            <Input placeholder="Full Name *" value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}/>
            <Input placeholder="Phone" value={form.phone} onChange={e => setForm(f => ({ ...f, phone: e.target.value }))}/>
            <Input placeholder="LGA" value={form.lga} onChange={e => setForm(f => ({ ...f, lga: e.target.value }))}/>
          </div>
          <Button className="mt-3 gap-1.5" onClick={() => {
            if (!petition?.id || !form.name) return toast.error("Name required");
            signMut.mutate({ petitionId: petition.id, signerName: form.name, signerPhone: form.phone, signerLga: form.lga });
          }} disabled={signMut.isPending || !petition} style={{ background: "#4A1525", color: "white" }}>
            {signMut.isPending ? <Loader2 size={14} className="animate-spin"/> : <><Plus size={14}/> Add Signature</>}
          </Button>
        </div>
        {/* Table */}
        {(loadingSigs || loadingPetitions) ? <div className="flex justify-center py-10"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : signatures.length > 0 && (
          <div className="bg-white border border-gray-200 rounded overflow-hidden">
            <table className="w-full text-sm">
              <thead><tr className="bg-gray-50 border-b">
                {["#","Name","Phone","LGA","Date"].map(h => <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>)}
              </tr></thead>
              <tbody>{signatures.map((s, i) => (
                <tr key={s.id} className={i % 2 === 0 ? "bg-white" : "bg-gray-50/50"}>
                  <td className="px-4 py-2 font-mono text-gray-400 text-xs">{i + 1}</td>
                  <td className="px-4 py-2 font-medium text-gray-900">{s.signerName}</td>
                  <td className="px-4 py-2 text-gray-600">{s.phone ?? "—"}</td>
                  <td className="px-4 py-2 text-gray-600">{s.lga ?? "—"}</td>
                  <td className="px-4 py-2 text-xs text-gray-400">{new Date(s.signedAt).toLocaleDateString()}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
