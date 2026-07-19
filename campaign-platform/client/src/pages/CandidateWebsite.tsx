/**
 * Candidate Website Builder
 * Auto-generates a shareable single-page campaign microsite from candidate profile, endorsements, and party branding.
 */
import { useState } from "react";
import { ArrowLeft, Globe, Copy, CheckCheck, Eye, Code2, Download, Palette, Type, Image, Star } from "lucide-react";
import { Link } from "wouter";

interface SiteConfig {
  candidateName: string;
  office: string;
  state: string;
  party: string;
  partyColor: string;
  tagline: string;
  bio: string;
  phone: string;
  email: string;
  twitter: string;
  facebook: string;
  manifesto: string[];
  showEndorsements: boolean;
  showTimeline: boolean;
  showDonation: boolean;
  theme: "dark" | "light" | "green";
}

const DEFAULT_CONFIG: SiteConfig = {
  candidateName: "Aminu Bello",
  office: "Governor",
  state: "Lagos",
  party: "APC",
  partyColor: "#006400",
  tagline: "A New Lagos for Every Nigerian",
  bio: "Former Commissioner for Finance with 15 years of public service. Committed to infrastructure, education, and economic empowerment for all Lagosians.",
  phone: "+234 800 000 0000",
  email: "contact@aminubello.ng",
  twitter: "@AminuBelloNG",
  facebook: "AminuBelloOfficial",
  manifesto: [
    "Rebuild 500km of roads in the first year",
    "Free primary and secondary education for all",
    "Create 200,000 jobs through SME grants",
    "24-hour electricity in all LGAs by 2028",
    "Universal health insurance for Lagos residents",
  ],
  showEndorsements: true,
  showTimeline: false,
  showDonation: true,
  theme: "dark",
};

const THEMES = {
  dark: { bg: "#0d1117", text: "#e2e8f0", card: "#161b22", border: "#30363d" },
  light: { bg: "#f8fafc", text: "#1e293b", card: "#ffffff", border: "#e2e8f0" },
  green: { bg: "#052e16", text: "#dcfce7", card: "#14532d", border: "#166534" },
};

function generateHTML(cfg: SiteConfig): string {
  const t = THEMES[cfg.theme];
  const accent = cfg.partyColor;
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>${cfg.candidateName} for ${cfg.office} — ${cfg.state} ${cfg.party}</title>
  <style>
    * { margin:0; padding:0; box-sizing:border-box; }
    body { font-family: 'Segoe UI', sans-serif; background:${t.bg}; color:${t.text}; }
    .hero { background: linear-gradient(135deg, ${accent}22, ${t.bg}); padding: 80px 24px; text-align:center; border-bottom: 1px solid ${t.border}; }
    .hero h1 { font-size: clamp(2rem,5vw,3.5rem); font-weight:900; letter-spacing:-1px; margin-bottom:12px; }
    .hero .party { display:inline-block; background:${accent}; color:#fff; font-size:12px; font-weight:700; padding:4px 14px; border-radius:20px; margin-bottom:16px; letter-spacing:2px; }
    .hero .tagline { font-size:1.2rem; opacity:0.75; max-width:600px; margin:0 auto 28px; }
    .cta { display:inline-block; background:${accent}; color:#fff; padding:14px 32px; border-radius:8px; font-weight:700; text-decoration:none; font-size:1rem; }
    .section { max-width:900px; margin:0 auto; padding:60px 24px; }
    .section h2 { font-size:1.5rem; font-weight:800; margin-bottom:28px; padding-bottom:12px; border-bottom:2px solid ${accent}; }
    .manifesto-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(250px,1fr)); gap:16px; }
    .manifesto-item { background:${t.card}; border:1px solid ${t.border}; border-left:4px solid ${accent}; padding:20px; border-radius:8px; font-size:0.95rem; }
    .bio-section { background:${t.card}; border:1px solid ${t.border}; padding:32px; border-radius:12px; font-size:1rem; line-height:1.7; }
    .contact-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:16px; }
    .contact-item { background:${t.card}; border:1px solid ${t.border}; padding:16px; border-radius:8px; font-size:0.9rem; }
    .contact-item strong { display:block; font-size:0.75rem; letter-spacing:1px; opacity:0.6; margin-bottom:4px; }
    .footer { text-align:center; padding:32px; font-size:0.8rem; opacity:0.5; border-top:1px solid ${t.border}; }
    ${cfg.showDonation ? `.donate { background:${accent}11; border:1px solid ${accent}44; padding:40px; text-align:center; border-radius:12px; margin:40px 0; }
    .donate h3 { font-size:1.4rem; font-weight:800; margin-bottom:8px; }
    .donate-btn { display:inline-block; background:${accent}; color:#fff; padding:12px 28px; border-radius:8px; font-weight:700; text-decoration:none; margin-top:16px; }` : ""}
  </style>
</head>
<body>
  <div class="hero">
    <div class="party">${cfg.party} · ${cfg.state}</div>
    <h1>${cfg.candidateName}</h1>
    <p class="tagline">${cfg.tagline}</p>
    <a href="#contact" class="cta">Join the Movement →</a>
  </div>
  <div class="section">
    <h2>About ${cfg.candidateName.split(" ")[0]}</h2>
    <div class="bio-section">${cfg.bio}</div>
  </div>
  <div class="section">
    <h2>Our Manifesto</h2>
    <div class="manifesto-grid">
      ${cfg.manifesto.map((p, i) => `<div class="manifesto-item"><strong style="color:${accent};font-size:1.2rem;">${i + 1}.</strong> ${p}</div>`).join("\n      ")}
    </div>
  </div>
  ${cfg.showDonation ? `<div class="section"><div class="donate"><h3>Support the Campaign</h3><p>Your contribution powers grassroots mobilisation across all 36 LGAs.</p><a href="#" class="donate-btn">Donate Now</a></div></div>` : ""}
  <div class="section" id="contact">
    <h2>Get in Touch</h2>
    <div class="contact-grid">
      <div class="contact-item"><strong>PHONE</strong>${cfg.phone}</div>
      <div class="contact-item"><strong>EMAIL</strong>${cfg.email}</div>
      <div class="contact-item"><strong>TWITTER</strong>${cfg.twitter}</div>
      <div class="contact-item"><strong>FACEBOOK</strong>${cfg.facebook}</div>
    </div>
  </div>
  <div class="footer">Authorised by ${cfg.candidateName} Campaign Organisation · ${cfg.party} ${cfg.state} · Powered by INEC Campaign Intelligence Platform</div>
</body>
</html>`;
}

export default function CandidateWebsite() {
  const [cfg, setCfg] = useState<SiteConfig>(DEFAULT_CONFIG);
  const [tab, setTab] = useState<"preview" | "code">("preview");
  const [copied, setCopied] = useState(false);
  const [newPoint, setNewPoint] = useState("");

  const html = generateHTML(cfg);

  function copyHTML() {
    navigator.clipboard.writeText(html);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  function downloadHTML() {
    const blob = new Blob([html], { type: "text/html" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${cfg.candidateName.replace(/\s+/g, "_")}_campaign_site.html`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function addManifestoPoint() {
    if (newPoint.trim()) {
      setCfg(c => ({ ...c, manifesto: [...c.manifesto, newPoint.trim()] }));
      setNewPoint("");
    }
  }

  const field = (label: string, key: keyof SiteConfig, type = "text") => (
    <div className="mb-3">
      <label className="block text-xs font-bold tracking-widest uppercase mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>{label}</label>
      <input
        type={type}
        value={String(cfg[key])}
        onChange={e => setCfg(c => ({ ...c, [key]: e.target.value }))}
        className="w-full text-xs px-3 py-2 rounded border outline-none"
        style={{ background: "oklch(0.16 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.85 0.005 240)" }}
      />
    </div>
  );

  return (
    <div className="min-h-screen flex flex-col" style={{ background: "#0d1117", fontFamily: "'IBM Plex Mono', monospace", color: "#e2e8f0" }}>
      {/* Header */}
      <div className="border-b px-6 py-4 flex items-center justify-between flex-shrink-0" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.12 0.008 240)" }}>
        <div className="flex items-center gap-4">
          <Link href="/stakeholders">
            <button className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border" style={{ borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}>
              <ArrowLeft className="w-3.5 h-3.5" /> Back
            </button>
          </Link>
          <div>
            <div className="text-xs tracking-widest uppercase mb-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>INEC Campaign Intelligence</div>
            <div className="font-bold text-sm">Candidate Website Builder</div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={() => setTab("preview")} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border transition-all" style={{ borderColor: tab === "preview" ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)", color: tab === "preview" ? "oklch(0.65 0.18 145)" : "oklch(0.55 0.01 240)", background: tab === "preview" ? "oklch(0.16 0.04 145)" : "transparent" }}>
            <Eye className="w-3.5 h-3.5" /> Preview
          </button>
          <button onClick={() => setTab("code")} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border transition-all" style={{ borderColor: tab === "code" ? "oklch(0.55 0.18 200)" : "oklch(0.28 0.01 240)", color: tab === "code" ? "oklch(0.65 0.18 200)" : "oklch(0.55 0.01 240)", background: tab === "code" ? "oklch(0.16 0.04 200)" : "transparent" }}>
            <Code2 className="w-3.5 h-3.5" /> HTML
          </button>
          <button onClick={copyHTML} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border font-bold transition-all" style={{ borderColor: "oklch(0.55 0.18 280)", color: "oklch(0.65 0.18 280)", background: "oklch(0.16 0.04 280)" }}>
            {copied ? <CheckCheck className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
            {copied ? "Copied!" : "Copy HTML"}
          </button>
          <button onClick={downloadHTML} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded border font-bold transition-all" style={{ borderColor: "oklch(0.55 0.18 145)", color: "oklch(0.65 0.18 145)", background: "oklch(0.16 0.04 145)" }}>
            <Download className="w-3.5 h-3.5" /> Download
          </button>
        </div>
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* Config sidebar */}
        <div className="w-72 flex-shrink-0 border-r overflow-y-auto p-4" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.11 0.008 240)" }}>
          <div className="text-xs font-bold tracking-widest uppercase mb-4 flex items-center gap-2" style={{ color: "oklch(0.55 0.01 240)" }}>
            <Type className="w-3.5 h-3.5" /> Candidate Info
          </div>
          {field("Candidate Name", "candidateName")}
          {field("Office Sought", "office")}
          {field("State", "state")}
          {field("Party", "party")}
          {field("Tagline", "tagline")}
          <div className="mb-3">
            <label className="block text-xs font-bold tracking-widest uppercase mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>Bio</label>
            <textarea
              value={cfg.bio}
              onChange={e => setCfg(c => ({ ...c, bio: e.target.value }))}
              rows={4}
              className="w-full text-xs px-3 py-2 rounded border outline-none resize-none"
              style={{ background: "oklch(0.16 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.85 0.005 240)" }}
            />
          </div>

          <div className="text-xs font-bold tracking-widest uppercase mb-3 mt-5 flex items-center gap-2" style={{ color: "oklch(0.55 0.01 240)" }}>
            <Palette className="w-3.5 h-3.5" /> Branding
          </div>
          <div className="mb-3">
            <label className="block text-xs font-bold tracking-widest uppercase mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>Party Colour</label>
            <div className="flex items-center gap-2">
              <input type="color" value={cfg.partyColor} onChange={e => setCfg(c => ({ ...c, partyColor: e.target.value }))} className="w-8 h-8 rounded cursor-pointer border-0" />
              <span className="text-xs" style={{ color: "oklch(0.65 0.01 240)" }}>{cfg.partyColor}</span>
            </div>
          </div>
          <div className="mb-3">
            <label className="block text-xs font-bold tracking-widest uppercase mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>Theme</label>
            <div className="flex gap-2">
              {(["dark", "light", "green"] as const).map(t => (
                <button key={t} onClick={() => setCfg(c => ({ ...c, theme: t }))} className="text-xs px-3 py-1.5 rounded border capitalize transition-all" style={{ borderColor: cfg.theme === t ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)", color: cfg.theme === t ? "oklch(0.65 0.18 145)" : "oklch(0.55 0.01 240)", background: cfg.theme === t ? "oklch(0.16 0.04 145)" : "transparent" }}>{t}</button>
              ))}
            </div>
          </div>

          <div className="text-xs font-bold tracking-widest uppercase mb-3 mt-5 flex items-center gap-2" style={{ color: "oklch(0.55 0.01 240)" }}>
            <Star className="w-3.5 h-3.5" /> Manifesto Points
          </div>
          <div className="space-y-1.5 mb-3">
            {cfg.manifesto.map((p, i) => (
              <div key={i} className="flex items-start gap-2 text-xs p-2 rounded border" style={{ borderColor: "oklch(0.22 0.01 240)", background: "oklch(0.15 0.008 240)" }}>
                <span className="flex-1" style={{ color: "oklch(0.75 0.01 240)" }}>{p}</span>
                <button onClick={() => setCfg(c => ({ ...c, manifesto: c.manifesto.filter((_, j) => j !== i) }))} style={{ color: "oklch(0.55 0.18 25)" }} className="text-xs">✕</button>
              </div>
            ))}
          </div>
          <div className="flex gap-2">
            <input value={newPoint} onChange={e => setNewPoint(e.target.value)} onKeyDown={e => e.key === "Enter" && addManifestoPoint()} placeholder="Add manifesto point…" className="flex-1 text-xs px-2 py-1.5 rounded border outline-none" style={{ background: "oklch(0.16 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.85 0.005 240)" }} />
            <button onClick={addManifestoPoint} className="text-xs px-2 py-1.5 rounded border font-bold" style={{ borderColor: "oklch(0.55 0.18 145)", color: "oklch(0.65 0.18 145)", background: "oklch(0.16 0.04 145)" }}>+</button>
          </div>

          <div className="text-xs font-bold tracking-widest uppercase mb-3 mt-5 flex items-center gap-2" style={{ color: "oklch(0.55 0.01 240)" }}>
            <Globe className="w-3.5 h-3.5" /> Contact & Social
          </div>
          {field("Phone", "phone")}
          {field("Email", "email")}
          {field("Twitter Handle", "twitter")}
          {field("Facebook Page", "facebook")}

          <div className="text-xs font-bold tracking-widest uppercase mb-3 mt-5" style={{ color: "oklch(0.55 0.01 240)" }}>Sections</div>
          {([["showEndorsements", "Show Endorsements"], ["showDonation", "Show Donation CTA"]] as const).map(([key, label]) => (
            <label key={key} className="flex items-center gap-2 mb-2 cursor-pointer">
              <div onClick={() => setCfg(c => ({ ...c, [key]: !c[key] }))} className="w-8 h-4 rounded-full relative transition-all" style={{ background: cfg[key] ? "oklch(0.55 0.18 145)" : "oklch(0.28 0.01 240)" }}>
                <div className="absolute top-0.5 w-3 h-3 rounded-full bg-white transition-all" style={{ left: cfg[key] ? "17px" : "2px" }} />
              </div>
              <span className="text-xs" style={{ color: "oklch(0.65 0.01 240)" }}>{label}</span>
            </label>
          ))}
        </div>

        {/* Preview / Code panel */}
        <div className="flex-1 overflow-hidden">
          {tab === "preview" ? (
            <iframe
              srcDoc={html}
              className="w-full h-full border-0"
              title="Campaign Website Preview"
              sandbox="allow-same-origin"
            />
          ) : (
            <pre className="w-full h-full overflow-auto p-6 text-xs leading-relaxed" style={{ background: "oklch(0.10 0.008 240)", color: "oklch(0.70 0.01 240)", fontFamily: "'IBM Plex Mono', monospace" }}>
              {html}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}
