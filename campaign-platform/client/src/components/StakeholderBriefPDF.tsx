/**
 * Stakeholder Brief PDF Generator — v2.0
 * Supports: party logo, party color branding, 4-language export (EN/HA/YO/IG)
 */
import { useCallback, useState } from "react";
import { FileDown, ChevronDown } from "lucide-react";
import type { Stakeholder } from "./StakeholderTypes";

interface Props {
  stakeholders: Stakeholder[];
  candidateName: string;
  office: string;
  stateName: string;
  party?: string;
  partyLogo?: string;   // base64 data URL
  partyColor?: string;  // hex color e.g. "#006400"
}

type Lang = "en" | "ha" | "yo" | "ig";

const T: Record<Lang, Record<string, string>> = {
  en: {
    title: "Stakeholder Engagement Brief",
    generated: "Generated",
    priority_groups: "Priority Groups",
    critical_priority: "Critical Priority",
    high_priority: "High Priority",
    est_voter_reach: "Est. Voter Reach",
    top10_heading: "Top 10 Stakeholder Groups — Engagement Playbook",
    cultural_protocol: "Cultural Protocol",
    talking_points: "Talking Points",
    key_ask: "Key Ask",
    engagement: "Engagement",
    best_time: "Best Time",
    confidential: "CONFIDENTIAL — For Campaign Use Only",
    platform: "INEC Campaign Intelligence Platform · Stakeholder Engagement Engine v6.0",
  },
  ha: {
    title: "Takarda ta Hulɗa da Masu Ruwa da Tsaki",
    generated: "An Samar",
    priority_groups: "Ƙungiyoyin Da Suka Fi Muhimmanci",
    critical_priority: "Muhimmanci na Farko",
    high_priority: "Muhimmanci Mai Girma",
    est_voter_reach: "Adadin Masu Zaɓe da Za a Iya Kaiwa",
    top10_heading: "Manyan Ƙungiyoyi 10 — Tsarin Hulɗa",
    cultural_protocol: "Al'adar Hulɗa",
    talking_points: "Manyan Batutuwa",
    key_ask: "Babban Buƙata",
    engagement: "Hanyar Hulɗa",
    best_time: "Mafi Kyawun Lokaci",
    confidential: "SIRRI — Don Amfanin Kamfen Kawai",
    platform: "Tsarin Bayanan Kamfen na INEC · Injin Hulɗa da Masu Ruwa da Tsaki v6.0",
  },
  yo: {
    title: "Iwe Ìjọpọ̀ Àwọn Olùkópa",
    generated: "Ti Ṣẹda",
    priority_groups: "Àwọn Ẹgbẹ́ Pàtàkì",
    critical_priority: "Ìpele Pàtàkì Jùlọ",
    high_priority: "Ìpele Pàtàkì Gíga",
    est_voter_reach: "Iye Àwọn Oludibo Tí A Lè De",
    top10_heading: "Àwọn Ẹgbẹ́ Olùkópa 10 Tó Ṣe Pàtàkì — Ètò Ìjọpọ̀",
    cultural_protocol: "Àṣà Ìbáṣepọ̀",
    talking_points: "Àwọn Ọ̀rọ̀ Pàtàkì",
    key_ask: "Ìbéèrè Pàtàkì",
    engagement: "Ọ̀nà Ìjọpọ̀",
    best_time: "Àkókò Tó Dára Jùlọ",
    confidential: "AṢIRI — Fún Lílo Ìpolongo Nìkan",
    platform: "Ètò Ìmọ̀ Ìpolongo INEC · Ẹ̀rọ Ìjọpọ̀ Olùkópa v6.0",
  },
  ig: {
    title: "Akwụkwọ Mmekọrịta ndị Ọrụ",
    generated: "Emepụtara",
    priority_groups: "Otu ndị Dị Mkpa",
    critical_priority: "Ọkwa Mkpa Kachasị",
    high_priority: "Ọkwa Mkpa Dị Elu",
    est_voter_reach: "Ọnụọgụ ndị Ntuli Aka Enwere Ike Iru",
    top10_heading: "Otu ndị Ọrụ 10 Kacha Mkpa — Atụmatụ Mmekọrịta",
    cultural_protocol: "Omenaala Mmekọrịta",
    talking_points: "Isi Okwu Dị Mkpa",
    key_ask: "Arịọ Dị Mkpa",
    engagement: "Ụzọ Mmekọrịta",
    best_time: "Oge Kachasị Mma",
    confidential: "NZUZO — Maka Ojiji Mkpọsa Naanị",
    platform: "Ngwa Ozi Mkpọsa INEC · Igwe Mmekọrịta ndị Ọrụ v6.0",
  },
};

const LANG_LABELS: Record<Lang, string> = { en: "English", ha: "Hausa", yo: "Yoruba", ig: "Igbo" };

const CATEGORY_ICONS: Record<string, string> = {
  "Traditional Leaders": "👑", "Religious": "🕌", "Women": "👩", "Youth": "🎓",
  "Labour": "⚒️", "Agriculture": "🌾", "Commerce": "🏪", "Professional": "⚖️",
  "Civil Society": "🤝", "Diaspora": "✈️", "Inclusion": "♿", "Ethnic/Regional": "🏛️",
  "Pastoral": "🐄", "Media & Influencers": "📡",
};

function generateBriefHTML(
  stakeholders: Stakeholder[], candidateName: string, office: string,
  stateName: string, party: string, lang: Lang, partyLogo: string, partyColor: string
): string {
  const t = T[lang];
  const accentColor = partyColor || "#111827";
  const top10 = [...stakeholders]
    .sort((a, b) => a.priority - b.priority || (b.estimated_voter_reach ?? b.reach_pct * 50000) - (a.estimated_voter_reach ?? a.reach_pct * 50000))
    .slice(0, 10);

  const rows = top10.map((s, i) => {
    const icon = CATEGORY_ICONS[s.category] ?? "📌";
    const talkingPoints = s.talking_points?.slice(0, 3) ?? [s.key_ask];
    const priorityColor = s.priority === 1 ? "#dc2626" : s.priority === 2 ? "#d97706" : "#2563eb";
    const priorityLabel = s.priority === 1 ? "CRITICAL" : s.priority === 2 ? "HIGH" : "MEDIUM";
    const reach = s.estimated_voter_reach !== undefined ? s.estimated_voter_reach : Math.round(s.reach_pct * 50000);
    return `
    <div style="margin-bottom:14px;padding:12px;border:1px solid #e5e7eb;border-left:4px solid ${accentColor};border-radius:6px;page-break-inside:avoid;">
      <div style="display:flex;align-items:flex-start;gap:10px;">
        <div style="width:26px;height:26px;border-radius:50%;background:${accentColor};color:white;display:flex;align-items:center;justify-content:center;font-size:12px;font-weight:700;flex-shrink:0;">${i + 1}</div>
        <div style="flex:1;min-width:0;">
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:6px;">
            <span style="font-size:14px;font-weight:700;color:#111827;">${icon} ${s.name}</span>
            <span style="font-size:10px;font-weight:700;padding:2px 7px;border-radius:10px;background:${priorityColor}22;color:${priorityColor};border:1px solid ${priorityColor}44;">${priorityLabel}</span>
            <span style="font-size:10px;color:#6b7280;margin-left:auto;">${s.category} · ~${(reach / 1000).toFixed(0)}K voters</span>
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:6px;">
            <div>
              <div style="font-size:10px;font-weight:700;color:#374151;text-transform:uppercase;letter-spacing:0.05em;margin-bottom:3px;">${t.key_ask}</div>
              <div style="font-size:11px;color:#374151;line-height:1.5;">${s.key_ask}</div>
            </div>
            <div>
              <div style="font-size:10px;font-weight:700;color:#374151;text-transform:uppercase;letter-spacing:0.05em;margin-bottom:3px;">${t.cultural_protocol}</div>
              <div style="font-size:11px;color:#374151;line-height:1.5;">${s.cultural_protocol.split(".")[0]}.</div>
            </div>
          </div>
          <div style="margin-top:7px;">
            <div style="font-size:10px;font-weight:700;color:#374151;text-transform:uppercase;letter-spacing:0.05em;margin-bottom:3px;">${t.talking_points}</div>
            <ul style="margin:0;padding-left:16px;">
              ${talkingPoints.map(tp => `<li style="font-size:11px;color:#374151;line-height:1.6;margin-bottom:2px;">${tp}</li>`).join("")}
            </ul>
          </div>
          <div style="margin-top:7px;display:flex;gap:16px;flex-wrap:wrap;">
            <span style="font-size:10px;color:#6b7280;"><strong>${t.engagement}:</strong> ${Array.isArray(s.engagement_method) ? s.engagement_method[0] : s.engagement_method}</span>
            <span style="font-size:10px;color:#6b7280;"><strong>${t.best_time}:</strong> ${s.best_engagement_time}</span>
          </div>
        </div>
      </div>
    </div>`;
  }).join("");

  const totalReach = top10.reduce((sum, s) => sum + (s.estimated_voter_reach !== undefined ? s.estimated_voter_reach : Math.round(s.reach_pct * 50000)), 0);
  const critical = top10.filter(s => s.priority === 1).length;
  const high = top10.filter(s => s.priority === 2).length;
  const dateStr = new Date().toLocaleDateString("en-NG", { weekday: "long", year: "numeric", month: "long", day: "numeric" });
  const logoHTML = partyLogo ? `<img src="${partyLogo}" style="height:52px;width:auto;object-fit:contain;margin-right:14px;flex-shrink:0;" alt="Party Logo" />` : "";

  return `<!DOCTYPE html>
<html lang="${lang}">
<head>
  <meta charset="UTF-8">
  <title>${candidateName} — ${t.title}</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: Georgia, "Times New Roman", serif; margin: 0; padding: 32px 40px; color: #111827; background: white; }
    @media print { body { padding: 20px 24px; } @page { margin: 1cm; size: A4; } }
    .header { border-bottom: 3px solid ${accentColor}; padding-bottom: 16px; margin-bottom: 20px; display: flex; align-items: center; }
    .header h1 { font-size: 21px; margin: 0 0 4px 0; color: ${accentColor}; }
    .header .meta { font-size: 12px; color: #6b7280; }
    .stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 20px; }
    .stat { background: #f9fafb; border: 1px solid #e5e7eb; border-left: 4px solid ${accentColor}; border-radius: 8px; padding: 10px 14px; text-align: center; }
    .stat .val { font-size: 20px; font-weight: 700; color: ${accentColor}; }
    .stat .lbl { font-size: 10px; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; margin-top: 2px; }
    .section-title { font-size: 13px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; color: #374151; border-bottom: 2px solid ${accentColor}; padding-bottom: 6px; margin-bottom: 14px; }
    .footer { margin-top: 20px; padding-top: 12px; border-top: 1px solid #e5e7eb; font-size: 10px; color: #9ca3af; text-align: center; }
  </style>
</head>
<body>
  <div class="header">
    ${logoHTML}
    <div>
      <h1>${candidateName} — ${t.title}</h1>
      <div class="meta">${office} · ${stateName}${party ? " · " + party : ""} · ${t.generated} ${dateStr}</div>
    </div>
  </div>
  <div class="stats">
    <div class="stat"><div class="val">${top10.length}</div><div class="lbl">${t.priority_groups}</div></div>
    <div class="stat"><div class="val">${critical}</div><div class="lbl">${t.critical_priority}</div></div>
    <div class="stat"><div class="val">${high}</div><div class="lbl">${t.high_priority}</div></div>
    <div class="stat"><div class="val">${(totalReach / 1000000).toFixed(1)}M</div><div class="lbl">${t.est_voter_reach}</div></div>
  </div>
  <div class="section-title">${t.top10_heading}</div>
  ${rows}
  <div class="footer">${t.platform} · ${t.confidential}</div>
</body>
</html>`;
}

export default function StakeholderBriefPDF({
  stakeholders, candidateName, office, stateName,
  party = "", partyLogo = "", partyColor = ""
}: Props) {
  const [lang, setLang] = useState<Lang>("en");
  const [showMenu, setShowMenu] = useState(false);

  const handlePrint = useCallback(() => {
    if (stakeholders.length === 0) return;
    const html = generateBriefHTML(stakeholders, candidateName, office, stateName, party, lang, partyLogo, partyColor);
    const win = window.open("", "_blank");
    if (win) {
      win.document.write(html);
      win.document.close();
      win.focus();
      setTimeout(() => win.print(), 600);
    }
  }, [stakeholders, candidateName, office, stateName, party, lang, partyLogo, partyColor]);

  return (
    <div className="relative flex items-center">
      <button
        onClick={handlePrint}
        disabled={stakeholders.length === 0}
        className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-l border-y border-l transition-all disabled:opacity-40 disabled:cursor-not-allowed"
        style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.75 0.18 50)" }}
        title={`Print brief in ${LANG_LABELS[lang]}`}
      >
        <FileDown className="w-3.5 h-3.5" />
        Print ({LANG_LABELS[lang]})
      </button>
      <div className="relative">
        <button
          onClick={() => setShowMenu(v => !v)}
          disabled={stakeholders.length === 0}
          className="flex items-center px-1.5 py-1.5 text-xs rounded-r border transition-all disabled:opacity-40 disabled:cursor-not-allowed"
          style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.75 0.18 50)" }}
          title="Select language"
        >
          <ChevronDown className="w-3 h-3" />
        </button>
        {showMenu && (
          <div
            className="absolute right-0 top-full mt-1 rounded border shadow-lg z-50 min-w-[110px]"
            style={{ background: "oklch(0.15 0.008 240)", borderColor: "oklch(0.28 0.01 240)" }}
          >
            {(Object.entries(LANG_LABELS) as [Lang, string][]).map(([code, label]) => (
              <button
                key={code}
                onClick={() => { setLang(code); setShowMenu(false); }}
                className="w-full text-left px-3 py-1.5 text-xs transition-all"
                style={{
                  color: lang === code ? "oklch(0.75 0.18 50)" : "oklch(0.65 0.01 240)",
                  background: lang === code ? "oklch(0.22 0.01 240)" : "transparent",
                }}
              >
                {label}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
