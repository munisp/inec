/**
 * Public Petition Signing Page — unauthenticated, shareable link
 * Route: /sign/:petitionId
 */
import { useState } from "react";
import { useParams } from "wouter";
import { trpc } from "@/lib/trpc";
import { toast } from "sonner";
import { CheckCircle, FileText, Users, AlertCircle } from "lucide-react";

const NIGERIAN_LGAS = [
  "Abuja Municipal", "Gwagwalada", "Kuje", "Bwari", "Abaji", "Kwali",
  "Lagos Island", "Lagos Mainland", "Ikeja", "Surulere", "Alimosho",
  "Kano Municipal", "Fagge", "Dala", "Gwale", "Tarauni",
  "Other",
];

export default function PetitionSignPage() {
  const params = useParams<{ petitionId: string }>();
  const petitionId = Number(params.petitionId);

  const { data: petition, isLoading, error } = trpc.petitions.getPublic.useQuery(
    { petitionId },
    { enabled: !!petitionId && !isNaN(petitionId) }
  );

  const signMut = trpc.petitions.publicSign.useMutation({
    onSuccess: () => {
      setSigned(true);
      toast.success("Your signature has been recorded!");
    },
    onError: (e) => toast.error(e.message || "Failed to sign — please try again"),
  });

  const [form, setForm] = useState({ signerName: "", signerPhone: "", signerLga: "", signerEmail: "" });
  const [signed, setSigned] = useState(false);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.signerName.trim() || form.signerName.trim().length < 2) {
      return toast.error("Please enter your full name");
    }
    signMut.mutate({ petitionId, ...form });
  };

  if (isNaN(petitionId)) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: "#F5F0EB" }}>
        <div className="text-center p-8">
          <AlertCircle size={48} className="mx-auto mb-4 text-red-500" />
          <h1 className="text-xl font-bold text-gray-800">Invalid petition link</h1>
          <p className="text-gray-500 mt-2">This link does not point to a valid petition.</p>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: "#F5F0EB" }}>
        <div className="w-10 h-10 border-4 border-gray-200 rounded-full animate-spin" style={{ borderTopColor: "#008751" }} />
      </div>
    );
  }

  if (error || !petition) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: "#F5F0EB" }}>
        <div className="text-center p-8">
          <AlertCircle size={48} className="mx-auto mb-4 text-red-500" />
          <h1 className="text-xl font-bold text-gray-800">Petition not found</h1>
          <p className="text-gray-500 mt-2">This petition may have been removed or the link is incorrect.</p>
        </div>
      </div>
    );
  }

  const progress = petition.targetSignatures
    ? Math.min(100, Math.round(((petition.signatureCount ?? 0) / petition.targetSignatures) * 100))
    : null;

  return (
    <div className="min-h-screen py-8 px-4" style={{ background: "#F5F0EB", fontFamily: "'Inter', sans-serif" }}>
      <div className="max-w-lg mx-auto">
        {/* Header */}
        <div className="text-center mb-6">
          <div className="w-12 h-12 rounded-full flex items-center justify-center mx-auto mb-3" style={{ background: "#4A1525" }}>
            <FileText size={22} className="text-white" />
          </div>
          <p className="text-xs font-bold uppercase tracking-widest mb-1" style={{ color: "#4A1525" }}>INEC Campaign Petition</p>
        </div>

        {/* Petition Card */}
        <div className="bg-white rounded-lg border border-gray-200 p-6 mb-6" style={{ borderTop: "4px solid #4A1525" }}>
          <h1 className="text-2xl font-bold text-gray-900 mb-3">{petition.title}</h1>
          {petition.description && (
            <p className="text-gray-600 text-sm leading-relaxed mb-4">{petition.description}</p>
          )}

          {/* Signature count */}
          <div className="flex items-center gap-3 mb-3">
            <Users size={16} style={{ color: "#008751" }} />
            <span className="font-bold text-lg" style={{ color: "#008751" }}>{(petition.signatureCount ?? 0).toLocaleString()}</span>
            <span className="text-gray-500 text-sm">
              {petition.targetSignatures ? `of ${petition.targetSignatures.toLocaleString()} signatures` : "signatures collected"}
            </span>
          </div>

          {progress !== null && (
            <div className="w-full h-3 rounded-full overflow-hidden mb-1" style={{ background: "#F0EBE8" }}>
              <div
                className="h-full rounded-full transition-all"
                style={{ width: `${progress}%`, background: progress >= 100 ? "#008751" : "#4A1525" }}
              />
            </div>
          )}
          {progress !== null && (
            <p className="text-xs text-gray-400 text-right">{progress}% of goal reached</p>
          )}
        </div>

        {/* Sign Form or Success */}
        {signed ? (
          <div className="bg-white rounded-lg border border-green-200 p-8 text-center" style={{ borderTop: "4px solid #008751" }}>
            <CheckCircle size={48} className="mx-auto mb-4" style={{ color: "#008751" }} />
            <h2 className="text-xl font-bold text-gray-900 mb-2">Thank you for signing!</h2>
            <p className="text-gray-600 text-sm">Your signature has been recorded. Share this petition to help reach the goal.</p>
            <button
              onClick={() => {
                navigator.clipboard.writeText(window.location.href);
                toast.success("Link copied to clipboard!");
              }}
              className="mt-5 px-6 py-2.5 rounded font-semibold text-sm text-white"
              style={{ background: "#4A1525" }}
            >
              Copy Petition Link
            </button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="bg-white rounded-lg border border-gray-200 p-6" style={{ borderTop: "4px solid #008751" }}>
            <h2 className="font-bold text-gray-900 mb-4">Add Your Signature</h2>

            <div className="space-y-3">
              <div>
                <label className="text-xs font-semibold uppercase tracking-wider text-gray-500 block mb-1">Full Name *</label>
                <input
                  type="text"
                  value={form.signerName}
                  onChange={e => setForm(f => ({ ...f, signerName: e.target.value }))}
                  placeholder="Enter your full name"
                  className="w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:border-gray-500"
                  required
                />
              </div>

              <div>
                <label className="text-xs font-semibold uppercase tracking-wider text-gray-500 block mb-1">Phone Number</label>
                <input
                  type="tel"
                  value={form.signerPhone}
                  onChange={e => setForm(f => ({ ...f, signerPhone: e.target.value }))}
                  placeholder="+234 800 000 0000"
                  className="w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:border-gray-500"
                />
              </div>

              <div>
                <label className="text-xs font-semibold uppercase tracking-wider text-gray-500 block mb-1">LGA</label>
                <select
                  value={form.signerLga}
                  onChange={e => setForm(f => ({ ...f, signerLga: e.target.value }))}
                  className="w-full border border-gray-300 rounded px-3 py-2 text-sm bg-white focus:outline-none focus:border-gray-500"
                >
                  <option value="">— Select your LGA —</option>
                  {NIGERIAN_LGAS.map(lga => <option key={lga} value={lga}>{lga}</option>)}
                </select>
              </div>

              <div>
                <label className="text-xs font-semibold uppercase tracking-wider text-gray-500 block mb-1">Email (optional)</label>
                <input
                  type="email"
                  value={form.signerEmail}
                  onChange={e => setForm(f => ({ ...f, signerEmail: e.target.value }))}
                  placeholder="you@example.com"
                  className="w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:border-gray-500"
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={signMut.isPending}
              className="w-full mt-5 py-3 rounded font-bold text-sm text-white uppercase tracking-widest transition-all active:scale-95"
              style={{ background: signMut.isPending ? "#ccc" : "#4A1525", cursor: signMut.isPending ? "wait" : "pointer" }}
            >
              {signMut.isPending ? "Submitting…" : "Sign This Petition"}
            </button>

            <p className="text-center text-xs text-gray-400 mt-3">
              Your information is kept private and will only be used for this petition.
            </p>
          </form>
        )}

        <p className="text-center text-xs text-gray-400 mt-6">
          Powered by INEC Campaign Intelligence Platform
        </p>
      </div>
    </div>
  );
}
