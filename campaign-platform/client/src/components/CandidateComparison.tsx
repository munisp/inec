/**
 * CandidateComparison — Side-by-side stakeholder overlap analysis
 * Compares two candidate profiles: shared endorsement targets,
 * competing engagement schedules, and unique advantages.
 * Design: Civic Data Observatory dark theme
 */
import { useState, useMemo } from "react";
import { Users, TrendingUp, AlertTriangle, CheckCircle2, Target } from "lucide-react";
import type { Stakeholder } from "./StakeholderTypes";

// Candidate profile input
interface CandidateProfile {
  name: string;
  party: string;
  office: string;
  religion: "Christian" | "Muslim" | "Mixed";
  gender: "Male" | "Female";
  incumbentAdvantage: boolean;
}

interface Props {
  primaryStakeholders: Stakeholder[];
  primaryName: string;
  primaryOffice: string;
  stateName: string;
}

// Generate a rival stakeholder list by slightly adjusting priorities
function generateRivalStakeholders(base: Stakeholder[], rival: CandidateProfile): Stakeholder[] {
  return base.map(s => {
    let adj = 0;
    // Incumbents have stronger traditional ruler and labour ties
    if (rival.incumbentAdvantage && (s.category === "Traditional Rulers" || s.category === "Labour & Workers")) adj += 1.5;
    // Gender affects women associations
    if (rival.gender === "Female" && s.category === "Women Associations") adj += 2;
    if (rival.gender === "Male" && s.category === "Women Associations") adj -= 0.5;
    // Religion affects religious bodies
    if (rival.religion === "Christian" && s.subcategory?.includes("CAN")) adj += 1;
    if (rival.religion === "Muslim" && s.subcategory?.includes("JNI")) adj += 1;
    // Random variance to make it realistic
    adj += (Math.random() - 0.5) * 1.5;
    return { ...s, priority: Math.max(1, Math.min(10, s.priority + adj)) };
  });
}

const PARTIES = ["APC", "PDP", "LP", "NNPP", "APGA", "SDP", "ADC", "YPP"];

export default function CandidateComparison({ primaryStakeholders, primaryName, primaryOffice, stateName }: Props) {
  const [rival, setRival] = useState<CandidateProfile>({
    name: "Chukwuemeka Obi",
    party: "PDP",
    office: primaryOffice,
    religion: "Christian",
    gender: "Male",
    incumbentAdvantage: false,
  });
  const [showComparison, setShowComparison] = useState(false);

  const rivalStakeholders = useMemo(
    () => generateRivalStakeholders(primaryStakeholders, rival),
    [primaryStakeholders, rival]
  );

  // Compute overlap and unique advantages
  const analysis = useMemo(() => {
    if (primaryStakeholders.length === 0) return null;

    const primaryMap = new Map(primaryStakeholders.map(s => [s.id, s.priority]));
    const rivalMap = new Map(rivalStakeholders.map(s => [s.id, s.priority]));

    const shared: Array<{ stakeholder: Stakeholder; primaryScore: number; rivalScore: number; delta: number }> = [];
    const primaryAdvantage: Stakeholder[] = [];
    const rivalAdvantage: Stakeholder[] = [];

    primaryStakeholders.forEach(s => {
      const ps = primaryMap.get(s.id) ?? 0;
      const rs = rivalMap.get(s.id) ?? 0;
      const delta = ps - rs;
      if (Math.abs(delta) < 1.5) {
        shared.push({ stakeholder: s, primaryScore: ps, rivalScore: rs, delta });
      } else if (delta > 0) {
        primaryAdvantage.push(s);
      } else {
        rivalAdvantage.push(s);
      }
    });

    const primaryTotal = primaryStakeholders.reduce((a, s) => a + s.priority, 0);
    const rivalTotal = rivalStakeholders.reduce((a, s) => a + s.priority, 0);

    return {
      shared: shared.sort((a, b) => b.primaryScore - a.primaryScore).slice(0, 8),
      primaryAdvantage: primaryAdvantage.sort((a, b) => b.priority - a.priority).slice(0, 5),
      rivalAdvantage: rivalAdvantage.sort((a, b) => b.priority - a.priority).slice(0, 5),
      primaryTotal,
      rivalTotal,
      primaryWinPct: Math.round((primaryTotal / (primaryTotal + rivalTotal)) * 100),
    };
  }, [primaryStakeholders, rivalStakeholders]);

  if (primaryStakeholders.length === 0) {
    return (
      <div className="flex items-center justify-center h-40 text-sm" style={{ color: "oklch(0.45 0.01 240)" }}>
        Generate a stakeholder plan first to enable comparison mode.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Rival profile configurator */}
      <div className="p-4 rounded" style={{ background: "oklch(0.13 0.008 240)", border: "1px solid oklch(0.22 0.01 240)" }}>
        <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>RIVAL CANDIDATE PROFILE</div>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
          <div>
            <label className="block text-xs mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>NAME</label>
            <input
              value={rival.name}
              onChange={e => setRival(r => ({ ...r, name: e.target.value }))}
              className="w-full px-2 py-1.5 rounded text-xs border"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)" }}
            />
          </div>
          <div>
            <label className="block text-xs mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>PARTY</label>
            <select
              value={rival.party}
              onChange={e => setRival(r => ({ ...r, party: e.target.value }))}
              className="w-full px-2 py-1.5 rounded text-xs border"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)" }}
            >
              {PARTIES.map(p => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div>
            <label className="block text-xs mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>RELIGION</label>
            <select
              value={rival.religion}
              onChange={e => setRival(r => ({ ...r, religion: e.target.value as CandidateProfile["religion"] }))}
              className="w-full px-2 py-1.5 rounded text-xs border"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)" }}
            >
              <option value="Christian">Christian</option>
              <option value="Muslim">Muslim</option>
              <option value="Mixed">Mixed</option>
            </select>
          </div>
          <div>
            <label className="block text-xs mb-1" style={{ color: "oklch(0.55 0.01 240)" }}>GENDER</label>
            <select
              value={rival.gender}
              onChange={e => setRival(r => ({ ...r, gender: e.target.value as "Male" | "Female" }))}
              className="w-full px-2 py-1.5 rounded text-xs border"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)" }}
            >
              <option value="Male">Male</option>
              <option value="Female">Female</option>
            </select>
          </div>
          <div className="flex items-end">
            <label className="flex items-center gap-2 text-xs cursor-pointer" style={{ color: "oklch(0.65 0.01 240)" }}>
              <input
                type="checkbox"
                checked={rival.incumbentAdvantage}
                onChange={e => setRival(r => ({ ...r, incumbentAdvantage: e.target.checked }))}
                className="rounded"
              />
              Incumbent Advantage
            </label>
          </div>
          <div className="flex items-end">
            <button
              onClick={() => setShowComparison(true)}
              className="w-full px-3 py-1.5 rounded text-xs font-bold"
              style={{ background: "oklch(0.55 0.18 145)", color: "white" }}
            >
              Run Comparison
            </button>
          </div>
        </div>
      </div>

      {showComparison && analysis && (
        <>
          {/* Overall strength bar */}
          <div className="p-4 rounded" style={{ background: "oklch(0.13 0.008 240)", border: "1px solid oklch(0.22 0.01 240)" }}>
            <div className="text-xs font-bold tracking-wider mb-3" style={{ color: "oklch(0.55 0.01 240)" }}>OVERALL STAKEHOLDER STRENGTH</div>
            <div className="flex items-center gap-3 mb-2">
              <span className="text-xs font-bold w-32 truncate" style={{ color: "oklch(0.55 0.18 145)" }}>{primaryName}</span>
              <div className="flex-1 h-4 rounded overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
                <div
                  className="h-full rounded transition-all"
                  style={{ width: `${analysis.primaryWinPct}%`, background: "oklch(0.55 0.18 145)" }}
                />
              </div>
              <span className="text-xs font-bold w-8 text-right" style={{ color: "oklch(0.55 0.18 145)" }}>{analysis.primaryWinPct}%</span>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-xs font-bold w-32 truncate" style={{ color: "oklch(0.65 0.18 30)" }}>{rival.name} ({rival.party})</span>
              <div className="flex-1 h-4 rounded overflow-hidden" style={{ background: "oklch(0.22 0.01 240)" }}>
                <div
                  className="h-full rounded transition-all"
                  style={{ width: `${100 - analysis.primaryWinPct}%`, background: "oklch(0.65 0.18 30)" }}
                />
              </div>
              <span className="text-xs font-bold w-8 text-right" style={{ color: "oklch(0.65 0.18 30)" }}>{100 - analysis.primaryWinPct}%</span>
            </div>
          </div>

          {/* Three-column split */}
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            {/* Primary advantages */}
            <div className="p-3 rounded" style={{ background: "oklch(0.12 0.015 145)", border: "1px solid oklch(0.28 0.08 145)" }}>
              <div className="flex items-center gap-1.5 mb-2">
                <CheckCircle2 className="w-3.5 h-3.5" style={{ color: "oklch(0.55 0.18 145)" }} />
                <span className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.55 0.18 145)" }}>YOUR ADVANTAGES</span>
              </div>
              {analysis.primaryAdvantage.map(s => (
                <div key={s.id} className="flex items-center justify-between py-1 border-b" style={{ borderColor: "oklch(0.2 0.01 240)" }}>
                  <span className="text-xs" style={{ color: "oklch(0.75 0.01 240)" }}>{s.name}</span>
                  <span className="text-xs font-bold" style={{ color: "oklch(0.55 0.18 145)" }}>+{s.priority.toFixed(1)}</span>
                </div>
              ))}
              {analysis.primaryAdvantage.length === 0 && (
                <p className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>No clear advantages — focus on contested groups.</p>
              )}
            </div>

            {/* Contested / shared */}
            <div className="p-3 rounded" style={{ background: "oklch(0.12 0.01 240)", border: "1px solid oklch(0.28 0.01 240)" }}>
              <div className="flex items-center gap-1.5 mb-2">
                <Target className="w-3.5 h-3.5" style={{ color: "oklch(0.75 0.18 80)" }} />
                <span className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.75 0.18 80)" }}>CONTESTED GROUPS</span>
              </div>
              {analysis.shared.slice(0, 5).map(item => (
                <div key={item.stakeholder.id} className="py-1 border-b" style={{ borderColor: "oklch(0.2 0.01 240)" }}>
                  <div className="flex items-center justify-between">
                    <span className="text-xs" style={{ color: "oklch(0.75 0.01 240)" }}>{item.stakeholder.name}</span>
                    <span className="text-xs" style={{ color: item.delta >= 0 ? "oklch(0.55 0.18 145)" : "oklch(0.65 0.18 30)" }}>
                      {item.delta >= 0 ? "+" : ""}{item.delta.toFixed(1)}
                    </span>
                  </div>
                  <div className="flex gap-1 mt-0.5">
                    <div className="h-1 rounded" style={{ width: `${item.primaryScore * 10}%`, background: "oklch(0.55 0.18 145)", maxWidth: "50%" }} />
                    <div className="h-1 rounded" style={{ width: `${item.rivalScore * 10}%`, background: "oklch(0.65 0.18 30)", maxWidth: "50%" }} />
                  </div>
                </div>
              ))}
            </div>

            {/* Rival advantages */}
            <div className="p-3 rounded" style={{ background: "oklch(0.12 0.015 30)", border: "1px solid oklch(0.28 0.08 30)" }}>
              <div className="flex items-center gap-1.5 mb-2">
                <AlertTriangle className="w-3.5 h-3.5" style={{ color: "oklch(0.65 0.18 30)" }} />
                <span className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.65 0.18 30)" }}>RIVAL'S ADVANTAGES</span>
              </div>
              {analysis.rivalAdvantage.map(s => (
                <div key={s.id} className="flex items-center justify-between py-1 border-b" style={{ borderColor: "oklch(0.2 0.01 240)" }}>
                  <span className="text-xs" style={{ color: "oklch(0.75 0.01 240)" }}>{s.name}</span>
                  <span className="text-xs font-bold" style={{ color: "oklch(0.65 0.18 30)" }}>Priority</span>
                </div>
              ))}
              {analysis.rivalAdvantage.length === 0 && (
                <p className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>No rival advantages detected.</p>
              )}
            </div>
          </div>

          {/* Strategy recommendation */}
          <div className="p-4 rounded" style={{ background: "oklch(0.13 0.008 240)", border: "1px solid oklch(0.22 0.01 240)" }}>
            <div className="flex items-center gap-1.5 mb-2">
              <TrendingUp className="w-3.5 h-3.5" style={{ color: "oklch(0.55 0.18 145)" }} />
              <span className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.55 0.18 145)" }}>STRATEGIC RECOMMENDATION</span>
            </div>
            <p className="text-xs leading-relaxed" style={{ color: "oklch(0.72 0.01 240)" }}>
              {analysis.primaryWinPct >= 60
                ? `${primaryName} holds a strong stakeholder advantage (${analysis.primaryWinPct}%). Consolidate your base by locking in endorsements from your top ${analysis.primaryAdvantage.length} advantage groups early. Focus contested groups on turnout mobilisation rather than persuasion.`
                : analysis.primaryWinPct >= 50
                ? `The race is competitive. ${primaryName} leads by a narrow margin (${analysis.primaryWinPct}%). Prioritise the ${analysis.shared.length} contested stakeholder groups — winning even 3–4 of them will create a decisive gap. Neutralise ${rival.name}'s advantages by co-opting their key endorsers through shared policy positions.`
                : `${rival.name} currently holds a stakeholder advantage. ${primaryName} must urgently engage the ${analysis.rivalAdvantage.length} groups where the rival leads. Consider a coalition strategy: partner with civil society and professional bodies to build a credibility bridge into the rival's base.`
              }
            </p>
          </div>
        </>
      )}
    </div>
  );
}
