/**
 * Public-facing Endorsement Tracker
 * Displays confirmed endorsements from the CRM (status = "Endorsed")
 * Includes party branding, shareable links, embed code, and live stats
 */
import { useState, useEffect } from "react";
import { Share2, Code2, Copy, CheckCheck, Trophy, Users, MapPin, Calendar, ExternalLink, BadgeCheck } from "lucide-react";
import type { CRMContact } from "../components/StakeholderTypes";

// Demo endorsements that simulate what would come from the CRM
const DEMO_ENDORSEMENTS: CRMContact[] = [
  { id: "e1", stakeholderId: "s1", stakeholderName: "Traditional Leaders", contactName: "HRH Oba Adewale Ogundimu", role: "Paramount Ruler, Ibadan North", phone: "", email: "", status: "Endorsed", lastContact: "2025-11-15", nextAction: "", notes: "Publicly endorsed at palace ceremony. Committed to mobilising 12 ward heads.", createdAt: "2025-11-01" },
  { id: "e2", stakeholderId: "s2", stakeholderName: "Youth Groups", contactName: "Comrade Fatima Musa", role: "NANS Chairperson, Oyo State", phone: "", email: "", status: "Endorsed", lastContact: "2025-11-20", nextAction: "", notes: "Endorsed at student union rally. 3,000 student members mobilised.", createdAt: "2025-11-05" },
  { id: "e3", stakeholderId: "s3", stakeholderName: "Women Associations", contactName: "Chief Mrs. Ngozi Okafor", role: "NCWS State Chairperson", phone: "", email: "", status: "Endorsed", lastContact: "2025-11-22", nextAction: "", notes: "Endorsed after policy briefing on women's economic empowerment.", createdAt: "2025-11-10" },
  { id: "e4", stakeholderId: "s4", stakeholderName: "Religious Bodies", contactName: "Rev. Dr. Emmanuel Adeyemi", role: "CAN State Chairman", phone: "", email: "", status: "Endorsed", lastContact: "2025-11-25", nextAction: "", notes: "Endorsed after candidate signed peace accord. Agreed to announce from pulpits.", createdAt: "2025-11-12" },
  { id: "e5", stakeholderId: "s5", stakeholderName: "Labour", contactName: "Comrade Bello Suleiman", role: "NLC State Chairman", phone: "", email: "", status: "Endorsed", lastContact: "2025-11-28", nextAction: "", notes: "Endorsed after candidate committed to minimum wage review.", createdAt: "2025-11-15" },
  { id: "e6", stakeholderId: "s6", stakeholderName: "Professional Bodies", contactName: "Barrister Aisha Garba", role: "NBA Branch Chairman, Abuja", phone: "", email: "", status: "Endorsed", lastContact: "2025-12-01", nextAction: "", notes: "Endorsed at NBA Annual Dinner. Committed to legal community outreach.", createdAt: "2025-11-20" },
];

const CATEGORY_COLORS: Record<string, string> = {
  "Traditional Leaders": "#b45309",
  "Youth Groups": "#0891b2",
  "Women Associations": "#be185d",
  "Religious Bodies": "#7c3aed",
  "Labour": "#dc2626",
  "Professional Bodies": "#1d4ed8",
  "Civil Society": "#059669",
  "Agriculture": "#65a30d",
  "Commerce": "#d97706",
};

function getColor(category: string): string {
  return CATEGORY_COLORS[category] ?? "#374151";
}

function formatDate(dateStr: string): string {
  try {
    return new Date(dateStr).toLocaleDateString("en-NG", { day: "numeric", month: "short", year: "numeric" });
  } catch {
    return dateStr;
  }
}

export default function EndorsementTracker() {
  const [endorsements, setEndorsements] = useState<CRMContact[]>([]);
  const [candidateName, setCandidateName] = useState("Aminu Bello");
  const [partyName, setPartyName] = useState("APC");
  const [partyColor, setPartyColor] = useState("#006400");
  const [partyLogo, setPartyLogo] = useState("");
  const [copied, setCopied] = useState<"link" | "embed" | null>(null);
  const [showEmbed, setShowEmbed] = useState(false);
  const [filter, setFilter] = useState("All");

  useEffect(() => {
    // Load from localStorage (CRM data) or fall back to demo data
    try {
      const stored = localStorage.getItem("inec_crm_contacts");
      if (stored) {
        const all: CRMContact[] = JSON.parse(stored);
        const endorsed = all.filter(c => c.status === "Endorsed");
        setEndorsements(endorsed.length > 0 ? endorsed : DEMO_ENDORSEMENTS);
      } else {
        setEndorsements(DEMO_ENDORSEMENTS);
      }
      const storedLogo = localStorage.getItem("inec_party_logo");
      const storedColor = localStorage.getItem("inec_party_color");
      const storedName = localStorage.getItem("inec_candidate_name");
      const storedParty = localStorage.getItem("inec_party_name");
      if (storedLogo) setPartyLogo(storedLogo);
      if (storedColor) setPartyColor(storedColor);
      if (storedName) setCandidateName(storedName);
      if (storedParty) setPartyName(storedParty);
    } catch {
      setEndorsements(DEMO_ENDORSEMENTS);
    }
  }, []);

  const categories = ["All", ...Array.from(new Set(endorsements.map(e => e.stakeholderName)))];
  const filtered = filter === "All" ? endorsements : endorsements.filter(e => e.stakeholderName === filter);

  const shareUrl = window.location.href;
  const embedCode = `<iframe src="${shareUrl}" width="100%" height="600" frameborder="0" style="border-radius:8px;"></iframe>`;

  function copyToClipboard(text: string, type: "link" | "embed") {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(type);
      setTimeout(() => setCopied(null), 2000);
    });
  }

  return (
    <div className="min-h-screen" style={{ background: "#f8fafc", fontFamily: "'IBM Plex Mono', monospace" }}>
      {/* Header Banner */}
      <div className="w-full py-8 px-6" style={{ background: partyColor, color: "white" }}>
        <div className="max-w-4xl mx-auto">
          <div className="flex items-center gap-4 mb-4">
            {partyLogo && (
              <img src={partyLogo} alt="Party Logo" className="h-16 w-auto object-contain rounded" style={{ background: "white", padding: "4px" }} />
            )}
            <div>
              <div className="text-xs tracking-widest uppercase opacity-80 mb-1">{partyName} · Official Campaign</div>
              <h1 className="text-2xl font-bold tracking-tight">{candidateName}</h1>
              <div className="text-sm opacity-80 mt-1">Stakeholder Endorsement Tracker</div>
            </div>
          </div>
          {/* Stats bar */}
          <div className="grid grid-cols-3 gap-4 mt-6">
            <div className="rounded-lg p-3 text-center" style={{ background: "rgba(255,255,255,0.15)" }}>
              <div className="text-2xl font-bold">{endorsements.length}</div>
              <div className="text-xs opacity-80 uppercase tracking-wider mt-1">Total Endorsements</div>
            </div>
            <div className="rounded-lg p-3 text-center" style={{ background: "rgba(255,255,255,0.15)" }}>
              <div className="text-2xl font-bold">{new Set(endorsements.map(e => e.stakeholderName)).size}</div>
              <div className="text-xs opacity-80 uppercase tracking-wider mt-1">Stakeholder Categories</div>
            </div>
            <div className="rounded-lg p-3 text-center" style={{ background: "rgba(255,255,255,0.15)" }}>
              <div className="text-2xl font-bold">{endorsements.filter(e => {
                const d = new Date(e.lastContact);
                const now = new Date();
                return (now.getTime() - d.getTime()) < 30 * 24 * 60 * 60 * 1000;
              }).length}</div>
              <div className="text-xs opacity-80 uppercase tracking-wider mt-1">Last 30 Days</div>
            </div>
          </div>
        </div>
      </div>

      {/* Toolbar */}
      <div className="max-w-4xl mx-auto px-6 py-4 flex items-center justify-between gap-4 flex-wrap">
        {/* Category filter */}
        <div className="flex items-center gap-2 flex-wrap">
          {categories.map(cat => (
            <button
              key={cat}
              onClick={() => setFilter(cat)}
              className="px-3 py-1 text-xs rounded-full border transition-all"
              style={{
                background: filter === cat ? partyColor : "white",
                borderColor: filter === cat ? partyColor : "#e2e8f0",
                color: filter === cat ? "white" : "#374151",
              }}
            >
              {cat}
            </button>
          ))}
        </div>
        {/* Share & Embed */}
        <div className="flex items-center gap-2">
          <button
            onClick={() => copyToClipboard(shareUrl, "link")}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border transition-all"
            style={{ background: "white", borderColor: "#e2e8f0", color: "#374151" }}
          >
            {copied === "link" ? <CheckCheck className="w-3.5 h-3.5 text-green-600" /> : <Share2 className="w-3.5 h-3.5" />}
            {copied === "link" ? "Copied!" : "Share Link"}
          </button>
          <button
            onClick={() => setShowEmbed(v => !v)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border transition-all"
            style={{ background: "white", borderColor: "#e2e8f0", color: "#374151" }}
          >
            <Code2 className="w-3.5 h-3.5" />
            Embed
          </button>
        </div>
      </div>

      {/* Embed code panel */}
      {showEmbed && (
        <div className="max-w-4xl mx-auto px-6 pb-4">
          <div className="rounded-lg border p-4" style={{ background: "white", borderColor: "#e2e8f0" }}>
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-bold uppercase tracking-wider text-gray-500">Embed Code</span>
              <button
                onClick={() => copyToClipboard(embedCode, "embed")}
                className="flex items-center gap-1 text-xs px-2 py-1 rounded border"
                style={{ borderColor: "#e2e8f0", color: "#374151" }}
              >
                {copied === "embed" ? <CheckCheck className="w-3 h-3 text-green-600" /> : <Copy className="w-3 h-3" />}
                {copied === "embed" ? "Copied!" : "Copy"}
              </button>
            </div>
            <pre className="text-xs text-gray-600 bg-gray-50 rounded p-3 overflow-x-auto whitespace-pre-wrap break-all">{embedCode}</pre>
            <p className="text-xs text-gray-400 mt-2">Paste this code into your campaign website to embed the live endorsement tracker.</p>
          </div>
        </div>
      )}

      {/* Endorsement Cards */}
      <div className="max-w-4xl mx-auto px-6 pb-12">
        {filtered.length === 0 ? (
          <div className="text-center py-16 text-gray-400">
            <Trophy className="w-10 h-10 mx-auto mb-3 opacity-30" />
            <div className="text-sm">No endorsements yet in this category.</div>
            <div className="text-xs mt-1">Add contacts to the CRM and mark them as "Endorsed" to see them here.</div>
          </div>
        ) : (
          <div className="grid gap-4">
            {filtered.map((e, idx) => (
              <div
                key={e.id}
                className="rounded-xl border p-5 transition-all"
                style={{
                  background: "white",
                  borderColor: "#e2e8f0",
                  borderLeft: `4px solid ${getColor(e.stakeholderName)}`,
                  animationDelay: `${idx * 60}ms`,
                }}
              >
                <div className="flex items-start gap-4">
                  {/* Avatar */}
                  <div
                    className="w-12 h-12 rounded-full flex items-center justify-center text-white font-bold text-lg flex-shrink-0"
                    style={{ background: getColor(e.stakeholderName) }}
                  >
                    {e.contactName.charAt(0)}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-start justify-between gap-2 flex-wrap">
                      <div>
                        <div className="font-bold text-gray-900 text-sm">{e.contactName}</div>
                        <div className="text-xs text-gray-500 mt-0.5 flex items-center gap-1">
                          <Users className="w-3 h-3" />
                          {e.role}
                        </div>
                      </div>
                      <div className="flex items-center gap-2 flex-wrap">
                        {e.verified && (
                          <span className="flex items-center gap-1 text-xs font-bold px-2 py-0.5 rounded-full" style={{ background: "#dbeafe", color: "#1d4ed8" }}>
                            <BadgeCheck className="w-3.5 h-3.5" />
                            VERIFIED
                          </span>
                        )}
                        <span
                          className="text-xs px-2 py-0.5 rounded-full font-medium"
                          style={{ background: `${getColor(e.stakeholderName)}18`, color: getColor(e.stakeholderName) }}
                        >
                          {e.stakeholderName}
                        </span>
                        <span className="flex items-center gap-1 text-xs text-gray-400">
                          <Calendar className="w-3 h-3" />
                          {formatDate(e.lastContact)}
                        </span>
                      </div>
                    </div>
                    {e.notes && (
                      <p className="text-sm text-gray-600 mt-2 leading-relaxed italic">"{e.notes}"</p>
                    )}
                    <div className="flex items-center gap-3 mt-3">
                      <span
                        className="flex items-center gap-1 text-xs font-bold px-2 py-0.5 rounded"
                        style={{ background: "#dcfce7", color: "#16a34a" }}
                      >
                        <CheckCheck className="w-3 h-3" />
                        ENDORSED
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="border-t py-4 text-center text-xs text-gray-400" style={{ borderColor: "#e2e8f0" }}>
        <div className="flex items-center justify-center gap-2">
          <ExternalLink className="w-3 h-3" />
          Powered by INEC Campaign Intelligence Platform · Stakeholder Engagement Engine v8.0
        </div>
      </div>
    </div>
  );
}
