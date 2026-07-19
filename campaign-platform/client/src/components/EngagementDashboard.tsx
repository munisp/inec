/**
 * Engagement Progress Dashboard
 * Shows CRM pipeline status, endorsement breakdown by category,
 * election countdown, and phase progress tracker.
 */
import { useState, useMemo } from "react";
import { motion } from "framer-motion";
import {
  PieChart, Pie, Cell, BarChart, Bar, XAxis, YAxis, Tooltip,
  ResponsiveContainer, Legend
} from "recharts";
import { Trophy, Clock, Target, TrendingUp, Calendar, CheckCircle2 } from "lucide-react";
import type { CRMContact, Stakeholder } from "./StakeholderTypes";

const STATUS_COLORS: Record<string, string> = {
  "Not Started":       "oklch(0.40 0.01 240)",
  "Contacted":         "oklch(0.65 0.18 280)",
  "Meeting Scheduled": "oklch(0.72 0.18 50)",
  "Met":               "oklch(0.65 0.18 200)",
  "Endorsed":          "oklch(0.65 0.18 145)",
  "Declined":          "oklch(0.60 0.18 25)",
};

const CATEGORY_COLORS_CHART: Record<string, string> = {
  "Traditional Leaders": "#d97706",
  "Women":               "#e11d48",
  "Religious":           "#7c3aed",
  "Youth":               "#059669",
  "Labour":              "#ea580c",
  "Agriculture":         "#65a30d",
  "Professional":        "#0284c7",
  "Civil Society":       "#0d9488",
  "Commerce":            "#0891b2",
  "Diaspora":            "#4f46e5",
  "Inclusion":           "#db2777",
  "Ethnic/Regional":     "#ca8a04",
  "Pastoral":            "#78716c",
};

interface Props {
  contacts: CRMContact[];
  stakeholders: Stakeholder[];
  candidateName: string;
  office: string;
  stateName: string;
}

function CountdownTimer({ electionDate }: { electionDate: Date }) {
  const now = new Date();
  const diff = electionDate.getTime() - now.getTime();
  const days = Math.max(0, Math.floor(diff / (1000 * 60 * 60 * 24)));
  const weeks = Math.floor(days / 7);
  const months = Math.floor(days / 30);

  const phase = days > 60 ? "Legitimacy" : days > 30 ? "Mobilisation" : "Consolidation";
  const phaseColor = days > 60 ? "oklch(0.65 0.18 280)" : days > 30 ? "oklch(0.65 0.18 145)" : "oklch(0.72 0.18 50)";

  return (
    <div className="rounded border p-4" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
      <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>ELECTION COUNTDOWN</div>
      <div className="flex items-end gap-4 mb-3">
        <div>
          <div className="text-4xl font-bold leading-none" style={{ color: "oklch(0.88 0.005 240)" }}>{days}</div>
          <div className="text-xs mt-1" style={{ color: "oklch(0.55 0.01 240)" }}>days remaining</div>
        </div>
        <div className="flex gap-4 pb-1">
          <div className="text-center">
            <div className="text-lg font-bold" style={{ color: "oklch(0.72 0.01 240)" }}>{weeks}</div>
            <div className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>weeks</div>
          </div>
          <div className="text-center">
            <div className="text-lg font-bold" style={{ color: "oklch(0.72 0.01 240)" }}>{months}</div>
            <div className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>months</div>
          </div>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-xs font-bold" style={{ color: phaseColor }}>Current Phase: {phase}</span>
        <span className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>·</span>
        <span className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
          {phase === "Legitimacy" ? "Establish credibility with traditional & religious leaders" :
           phase === "Mobilisation" ? "Activate ground networks: labour, youth, farmers" :
           "Lock in professional, civil society & diaspora endorsements"}
        </span>
      </div>
      {/* Phase progress bar */}
      <div className="mt-3">
        <div className="flex justify-between text-xs mb-1" style={{ color: "oklch(0.45 0.01 240)" }}>
          <span>Campaign Start</span>
          <span>Election Day</span>
        </div>
        <div className="h-2 rounded-full overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
          <motion.div
            className="h-full rounded-full"
            style={{ background: `linear-gradient(to right, oklch(0.65 0.18 280), ${phaseColor})` }}
            initial={{ width: 0 }}
            animate={{ width: `${Math.min(100, Math.max(5, 100 - (days / 180) * 100))}%` }}
            transition={{ duration: 1, ease: "easeOut" }}
          />
        </div>
        <div className="flex justify-between text-xs mt-1" style={{ color: "oklch(0.45 0.01 240)" }}>
          <span style={{ color: "oklch(0.65 0.18 280)" }}>Legitimacy</span>
          <span style={{ color: "oklch(0.65 0.18 145)" }}>Mobilisation</span>
          <span style={{ color: "oklch(0.72 0.18 50)" }}>Consolidation</span>
        </div>
      </div>
    </div>
  );
}

export default function EngagementDashboard({ contacts, stakeholders, candidateName, office, stateName }: Props) {
  const [electionDate, setElectionDate] = useState(() => {
    const d = new Date();
    d.setDate(d.getDate() + 120); // Default: 120 days from now
    return d;
  });
  const [electionDateStr, setElectionDateStr] = useState(
    electionDate.toISOString().slice(0, 10)
  );

  // ── KPI metrics ───────────────────────────────────────────────────────────────
  const totalStakeholders = stakeholders.length;
  const contactedCount = contacts.length;
  const endorsedCount = contacts.filter(c => c.status === "Endorsed").length;
  const declinedCount = contacts.filter(c => c.status === "Declined").length;
  const coveragePct = totalStakeholders > 0 ? Math.round((contactedCount / totalStakeholders) * 100) : 0;
  const endorsementRate = contactedCount > 0 ? Math.round((endorsedCount / contactedCount) * 100) : 0;

  // ── Donut chart data (CRM pipeline status) ────────────────────────────────────
  const statusData = useMemo(() => {
    const counts: Record<string, number> = {};
    contacts.forEach(c => { counts[c.status] = (counts[c.status] ?? 0) + 1; });
    // Add "Not Started" for uncovered stakeholders
    const notStarted = Math.max(0, totalStakeholders - contactedCount);
    if (notStarted > 0) counts["Not Started"] = (counts["Not Started"] ?? 0) + notStarted;
    return Object.entries(counts).map(([name, value]) => ({ name, value }));
  }, [contacts, totalStakeholders, contactedCount]);

  // ── Bar chart data (endorsements by category) ─────────────────────────────────
  const categoryData = useMemo(() => {
    const catMap: Record<string, { total: number; endorsed: number; met: number }> = {};
    stakeholders.forEach(s => {
      if (!catMap[s.category]) catMap[s.category] = { total: 0, endorsed: 0, met: 0 };
      catMap[s.category].total++;
    });
    contacts.forEach(c => {
      const s = stakeholders.find(st => st.id === c.stakeholderId || st.name === c.stakeholderName);
      if (!s) return;
      if (!catMap[s.category]) catMap[s.category] = { total: 0, endorsed: 0, met: 0 };
      if (c.status === "Endorsed") catMap[s.category].endorsed++;
      if (c.status === "Met" || c.status === "Endorsed") catMap[s.category].met++;
    });
    return Object.entries(catMap)
      .map(([cat, v]) => ({ category: cat.length > 12 ? cat.slice(0, 12) + "…" : cat, ...v }))
      .sort((a, b) => b.total - a.total);
  }, [contacts, stakeholders]);

  // ── Phase completion tracker ──────────────────────────────────────────────────
  const phaseGroups = useMemo(() => {
    const legCats = new Set(["Traditional Leaders", "Religious", "Women", "Ethnic/Regional"]);
    const mobCats = new Set(["Labour", "Youth", "Agriculture", "Commerce", "Pastoral"]);
    const conCats = new Set(["Professional", "Civil Society", "Diaspora", "Inclusion"]);
    const countPhase = (cats: Set<string>) => {
      const total = stakeholders.filter(s => cats.has(s.category)).length;
      const done = contacts.filter(c => {
        const s = stakeholders.find(st => st.name === c.stakeholderName);
        return s && cats.has(s.category) && (c.status === "Met" || c.status === "Endorsed");
      }).length;
      return { total, done, pct: total > 0 ? Math.round((done / total) * 100) : 0 };
    };
    return {
      Legitimacy:    countPhase(legCats),
      Mobilisation:  countPhase(mobCats),
      Consolidation: countPhase(conCats),
    };
  }, [contacts, stakeholders]);

  const phaseColors = {
    Legitimacy:    "oklch(0.65 0.18 280)",
    Mobilisation:  "oklch(0.65 0.18 145)",
    Consolidation: "oklch(0.72 0.18 50)",
  };

  const CustomTooltip = ({ active, payload }: any) => {
    if (!active || !payload?.length) return null;
    return (
      <div className="rounded border px-3 py-2 text-xs" style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)" }}>
        {payload.map((p: any) => (
          <div key={p.name}><span style={{ color: p.color }}>{p.name}: </span>{p.value}</div>
        ))}
      </div>
    );
  };

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>Engagement Progress Dashboard</div>
          <div className="text-xs mt-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>{candidateName} · {office} · {stateName}</div>
        </div>
        <div className="flex items-center gap-2">
          <label className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>Election Date:</label>
          <input
            type="date"
            value={electionDateStr}
            onChange={e => {
              setElectionDateStr(e.target.value);
              setElectionDate(new Date(e.target.value));
            }}
            className="px-2 py-1 text-xs rounded border"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
          />
        </div>
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-4 gap-3">
        {[
          { label: "Total Groups", value: totalStakeholders, icon: <Target className="w-4 h-4" />, color: "oklch(0.65 0.18 280)" },
          { label: "Coverage", value: `${coveragePct}%`, icon: <TrendingUp className="w-4 h-4" />, color: "oklch(0.65 0.18 145)" },
          { label: "Endorsed", value: endorsedCount, icon: <Trophy className="w-4 h-4" />, color: "oklch(0.72 0.18 50)" },
          { label: "Endorse Rate", value: `${endorsementRate}%`, icon: <CheckCircle2 className="w-4 h-4" />, color: "oklch(0.65 0.18 200)" },
        ].map(kpi => (
          <div key={kpi.label} className="rounded border p-3" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
            <div className="flex items-center gap-2 mb-2" style={{ color: kpi.color }}>{kpi.icon}
              <span className="text-xs tracking-wider" style={{ color: "oklch(0.55 0.01 240)" }}>{kpi.label.toUpperCase()}</span>
            </div>
            <div className="text-2xl font-bold" style={{ color: kpi.color }}>{kpi.value}</div>
          </div>
        ))}
      </div>

      {/* Countdown + Phase tracker */}
      <div className="grid grid-cols-2 gap-3">
        <CountdownTimer electionDate={electionDate} />
        <div className="rounded border p-4" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
          <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>PHASE COMPLETION</div>
          {(["Legitimacy", "Mobilisation", "Consolidation"] as const).map(phase => {
            const p = phaseGroups[phase];
            const c = phaseColors[phase];
            return (
              <div key={phase} className="mb-3">
                <div className="flex justify-between text-xs mb-1">
                  <span style={{ color: c }}>{phase}</span>
                  <span style={{ color: "oklch(0.55 0.01 240)" }}>{p.done}/{p.total} groups met</span>
                </div>
                <div className="h-2 rounded-full overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
                  <motion.div
                    className="h-full rounded-full"
                    style={{ background: c }}
                    initial={{ width: 0 }}
                    animate={{ width: `${p.pct}%` }}
                    transition={{ duration: 0.8, ease: "easeOut" }}
                  />
                </div>
                <div className="text-xs mt-0.5 text-right" style={{ color: "oklch(0.45 0.01 240)" }}>{p.pct}%</div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Charts row */}
      <div className="grid grid-cols-2 gap-3">
        {/* Donut: CRM pipeline */}
        <div className="rounded border p-4" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
          <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>CRM PIPELINE STATUS</div>
          {statusData.length === 0 || statusData.every(d => d.value === 0) ? (
            <div className="flex items-center justify-center h-40 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
              No contacts logged yet — add contacts in the CRM tab
            </div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <PieChart>
                <Pie data={statusData} cx="50%" cy="50%" innerRadius={55} outerRadius={80} paddingAngle={2} dataKey="value">
                  {statusData.map((entry) => (
                    <Cell key={entry.name} fill={STATUS_COLORS[entry.name] ?? "#6b7280"} />
                  ))}
                </Pie>
                <Tooltip content={<CustomTooltip />} />
                <Legend
                  formatter={(value) => <span style={{ color: "oklch(0.65 0.01 240)", fontSize: "11px" }}>{value}</span>}
                  iconSize={8}
                />
              </PieChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Bar: endorsements by category */}
        <div className="rounded border p-4" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
          <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>ENGAGEMENT BY CATEGORY</div>
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={categoryData} layout="vertical" margin={{ left: 0, right: 10, top: 0, bottom: 0 }}>
              <XAxis type="number" tick={{ fill: "oklch(0.45 0.01 240)", fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis type="category" dataKey="category" tick={{ fill: "oklch(0.55 0.01 240)", fontSize: 10 }} width={85} axisLine={false} tickLine={false} />
              <Tooltip content={<CustomTooltip />} />
              <Bar dataKey="total" name="Total" fill="oklch(0.28 0.01 240)" radius={[0, 2, 2, 0]} />
              <Bar dataKey="met" name="Met" fill="oklch(0.55 0.18 200)" radius={[0, 2, 2, 0]} />
              <Bar dataKey="endorsed" name="Endorsed" fill="oklch(0.55 0.18 145)" radius={[0, 2, 2, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Action items */}
      {contacts.length > 0 && (
        <div className="rounded border p-4" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
          <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>PENDING NEXT ACTIONS</div>
          <div className="space-y-2">
            {contacts.filter(c => c.nextAction && c.status !== "Endorsed" && c.status !== "Declined").slice(0, 5).map(c => (
              <div key={c.id} className="flex items-start gap-3 text-xs">
                <Clock className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" style={{ color: "oklch(0.72 0.18 50)" }} />
                <div>
                  <span className="font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>{c.contactName}</span>
                  <span className="mx-1" style={{ color: "oklch(0.45 0.01 240)" }}>·</span>
                  <span style={{ color: "oklch(0.65 0.18 145)" }}>{c.stakeholderName}</span>
                  <div style={{ color: "oklch(0.65 0.01 240)" }}>{c.nextAction}</div>
                </div>
              </div>
            ))}
            {contacts.filter(c => c.nextAction && c.status !== "Endorsed" && c.status !== "Declined").length > 5 && (
              <div className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                +{contacts.filter(c => c.nextAction && c.status !== "Endorsed" && c.status !== "Declined").length - 5} more pending actions
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

