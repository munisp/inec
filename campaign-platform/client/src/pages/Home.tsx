import { useState, useCallback, useRef, useEffect } from "react";
import { Link } from "wouter";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import {
  AreaChart, Area, BarChart, Bar, LineChart, Line,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, ReferenceLine
} from "recharts";
import { motion, AnimatePresence } from "framer-motion";
import {
  Users, Calendar, MapPin, UserCheck, Megaphone, Share2,
  Scale, Search, Zap, BarChart2, FileText, ClipboardList,
  Globe, Star, DollarSign, Wallet, Radio, Mic2, TrendingUp, Activity,
  Settings, ChevronRight, Cpu, UserCog, Save, History, CheckCircle,
  AlertTriangle, Clock, Shield, Database, GitCompare, Download, Sparkles, X, Copy
} from "lucide-react";

// ─── Feature Grid ─────────────────────────────────────────────────────────────
const FEATURES = [
  { path: "/stakeholders", icon: Users, label: "Stakeholders Hub", desc: "Manage supporters & stakeholders", color: "#1A3A5C" },
  { path: "/endorsements", icon: Star, label: "Endorsements", desc: "Track endorsements & testimonials", color: "#008751" },
  { path: "/timeline", icon: Calendar, label: "Campaign Timeline", desc: "Plan & track campaign milestones", color: "#4A1525" },
  { path: "/registration", icon: UserCheck, label: "Voter Registration", desc: "Register & verify voters", color: "#1A3A5C" },
  { path: "/polling-units", icon: MapPin, label: "Polling Unit Locator", desc: "Map & manage polling units", color: "#008751" },
  { path: "/volunteers", icon: Users, label: "Volunteer Portal", desc: "Recruit, assign tasks & coordinate", color: "#4A1525" },
  { path: "/press-release", icon: Megaphone, label: "Press Release Generator", desc: "AI-powered press release drafting", color: "#1A3A5C" },
  { path: "/social-media", icon: Share2, label: "Social Media Center", desc: "Schedule & monitor social posts", color: "#008751" },
  { path: "/legal-compliance", icon: Scale, label: "Legal Compliance", desc: "Track INEC compliance requirements", color: "#C0392B" },
  { path: "/opposition-research", icon: Search, label: "Opposition Research", desc: "Analyse opponents & threats", color: "#4A1525" },
  { path: "/war-room", icon: Zap, label: "Election Day War Room", desc: "Live incident tracking & agents", color: "#C0392B" },
  { path: "/results", icon: BarChart2, label: "Results Projection", desc: "Live vote accumulation & projections", color: "#1A3A5C" },
  { path: "/manifesto", icon: FileText, label: "Manifesto Builder", desc: "AI-powered policy section drafting", color: "#008751" },
  { path: "/petition", icon: ClipboardList, label: "Petition Drive", desc: "Launch & track signature drives", color: "#4A1525" },
  { path: "/diaspora", icon: Globe, label: "Diaspora Outreach", desc: "Engage Nigerians abroad", color: "#1A3A5C" },
  { path: "/post-election", icon: TrendingUp, label: "Post-Election Analytics", desc: "Deep-dive results analysis", color: "#008751" },
  { path: "/fundraising", icon: DollarSign, label: "Fundraising Tracker", desc: "Track donations & donors", color: "#4A1525" },
  { path: "/budget", icon: Wallet, label: "Budget Planner", desc: "Plan & monitor campaign spend", color: "#1A3A5C" },
  { path: "/media-monitoring", icon: Radio, label: "Media Monitoring", desc: "Track press coverage & sentiment", color: "#008751" },
  { path: "/debate-coach", icon: Mic2, label: "Debate Coach", desc: "AI debate prep & talking points", color: "#4A1525" },
  { path: "/team", icon: UserCog, label: "Campaign Team", desc: "Manage team members & roles", color: "#1A3A5C" },
  { path: "/dashboard", icon: Activity, label: "KPI Dashboard", desc: "Live aggregated campaign metrics", color: "#008751" },
];

// ─── Simulation Engine ────────────────────────────────────────────────────────
interface SimulationResult {
  scenario: string;
  turnout: number;
  validVotes: number;
  rejectedBallots: number;
  bvasFailureRate: number;
  logisticsScore: number;
  securityIndex: number;
  certificationHours: number;
  confidence: number;
  monteCarloP5: number;
  monteCarloP50: number;
  monteCarloP95: number;
  disruptions: string[];
  timelineData: { hour: number; cumTurnout: number; incidents: number }[];
  lgaData: { lga: string; turnout: number; risk: string }[];
}

interface SimConfig {
  scenario: "baseline" | "optimistic" | "pessimistic" | "crisis";
  state: string;
  registeredVoters: number;
  pollingUnits: number;
  weatherSeverity: number;
  securityThreat: number;
  bvasReliability: number;
  staffTraining: number;
  iterations: number;
}

function runSimulation(config: SimConfig): SimulationResult {
  const seed = config.scenario.charCodeAt(0) + config.weatherSeverity + config.securityThreat;
  const rng = (n: number) => ((Math.sin(seed * n + n * 7.3) + 1) / 2);
  const scenarioMultipliers = {
    baseline:    { turnout: 1.00, failure: 1.00, security: 1.00, cert: 1.00 },
    optimistic:  { turnout: 1.18, failure: 0.40, security: 0.30, cert: 0.75 },
    pessimistic: { turnout: 0.78, failure: 2.20, security: 1.80, cert: 1.60 },
    crisis:      { turnout: 0.55, failure: 4.50, security: 3.50, cert: 2.80 },
  };
  const m = scenarioMultipliers[config.scenario];
  const baseTurnout = 0.42 + (config.staffTraining / 100) * 0.12 - (config.weatherSeverity / 100) * 0.15;
  const turnout = Math.min(0.95, Math.max(0.20, baseTurnout * m.turnout + (rng(1) - 0.5) * 0.04));
  const bvasFailure = Math.min(0.35, Math.max(0.005, (1 - config.bvasReliability / 100) * m.failure * 0.12));
  const rejectedRate = 0.015 + bvasFailure * 0.3 + (rng(2) - 0.5) * 0.005;
  const totalVotes = Math.round(config.registeredVoters * turnout);
  const validVotes = Math.round(totalVotes * (1 - rejectedRate));
  const logisticsScore = Math.min(100, Math.max(10, 85 - config.weatherSeverity * 0.4 - (1 - config.bvasReliability / 100) * 20 + rng(3) * 10));
  const securityIndex = Math.min(100, Math.max(5, 90 - config.securityThreat * 0.6 * m.security + rng(4) * 8));
  const certHours = Math.round((24 + config.pollingUnits / 800 * 12) * m.cert * (1 + (1 - logisticsScore / 100) * 0.5));
  const confidence = Math.round(70 + (config.bvasReliability - 50) * 0.3 + (config.staffTraining - 50) * 0.2 - config.securityThreat * 0.2);
  const variance = config.scenario === "crisis" ? 0.15 : config.scenario === "pessimistic" ? 0.08 : 0.04;
  const p50 = turnout;
  const p5 = Math.max(0.15, p50 - variance * 2);
  const p95 = Math.min(0.95, p50 + variance * 1.5);
  const disruptions: string[] = [];
  if (config.weatherSeverity > 60) disruptions.push("Severe weather affecting 23 LGAs");
  if (config.securityThreat > 50) disruptions.push("Security incidents at 12 polling units");
  if (config.bvasReliability < 70) disruptions.push("BVAS connectivity failures in 8 zones");
  if (config.scenario === "crisis") disruptions.push("Coordinated infrastructure attack detected");
  if (config.scenario === "pessimistic") disruptions.push("Ballot paper shortage in 5 LGAs");
  if (disruptions.length === 0) disruptions.push("No significant disruptions detected");
  const timelineData = Array.from({ length: 13 }, (_, i) => {
    const hour = 8 + i;
    const progress = i / 12;
    const cumTurnout = Math.round(turnout * config.registeredVoters * (progress ** 0.7) * (1 + (rng(i + 10) - 0.5) * 0.06));
    const incidents = config.scenario === "crisis" ? Math.round(rng(i + 20) * 8) :
                      config.scenario === "pessimistic" ? Math.round(rng(i + 30) * 3) : Math.round(rng(i + 40) * 0.8);
    return { hour, cumTurnout, incidents };
  });
  const lgas = ["Abuja Municipal", "Gwagwalada", "Kuje", "Bwari", "Abaji", "Kwali"];
  const lgaData = lgas.map((lga, i) => ({
    lga, turnout: Math.round((turnout + (rng(i + 50) - 0.5) * 0.12) * 100),
    risk: config.scenario === "crisis" ? "HIGH" : config.scenario === "pessimistic" && rng(i + 60) > 0.5 ? "MEDIUM" : "LOW",
  }));
  return {
    scenario: config.scenario, turnout: Math.round(turnout * 100), validVotes,
    rejectedBallots: totalVotes - validVotes, bvasFailureRate: Math.round(bvasFailure * 100 * 10) / 10,
    logisticsScore: Math.round(logisticsScore), securityIndex: Math.round(securityIndex),
    certificationHours: certHours, confidence: Math.min(98, Math.max(30, confidence)),
    monteCarloP5: Math.round(p5 * 100), monteCarloP50: Math.round(p50 * 100), monteCarloP95: Math.round(p95 * 100),
    disruptions, timelineData, lgaData,
  };
}

const SCENARIO_META = {
  baseline:    { label: "Baseline",    color: "#1A3A5C", bg: "#EBF2F8", border: "#1A3A5C" },
  optimistic:  { label: "Optimistic",  color: "#008751", bg: "#E6F4EE", border: "#008751" },
  pessimistic: { label: "Pessimistic", color: "#C0392B", bg: "#FBEAE9", border: "#C0392B" },
  crisis:      { label: "Crisis",      color: "#4A1525", bg: "#F5E8EB", border: "#4A1525" },
};

function SliderControl({ label, value, onChange, color = "#4A1525" }: { label: string; value: number; onChange: (v: number) => void; color?: string }) {
  return (
    <div className="mb-4">
      <div className="flex justify-between items-center mb-1">
        <span className="text-xs font-semibold uppercase tracking-wider text-gray-300">{label}</span>
        <span className="font-mono text-sm font-bold text-white">{value}</span>
      </div>
      <input type="range" min={0} max={100} value={value} onChange={e => onChange(Number(e.target.value))}
        className="w-full h-1.5 appearance-none rounded-full cursor-pointer"
        style={{ accentColor: color, background: `linear-gradient(to right, ${color} ${value}%, #444 ${value}%)` }} />
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────
function ComparisonPanel({ runA, runB }: { runA: any; runB: any }) {
  if (!runA || !runB) return null;
  const fields = [
    { label: "Turnout", a: runA.projectedTurnout, b: runB.projectedTurnout, unit: "%" },
    { label: "Confidence", a: runA.modelConfidence, b: runB.modelConfidence, unit: "%" },
    { label: "BVAS Fail", a: runA.bvasFailureRate, b: runB.bvasFailureRate, unit: "%" },
    { label: "Logistics", a: runA.logisticsScore, b: runB.logisticsScore, unit: "/100" },
    { label: "Security", a: runA.securityIndex, b: runB.securityIndex, unit: "/100" },
    { label: "Cert ETA", a: runA.certificationEta, b: runB.certificationEta, unit: "h" },
    { label: "P50 Median", a: runA.monteCarloP50, b: runB.monteCarloP50, unit: "%" },
  ];
  return (
    <div className="mb-4 bg-white border-2 p-4" style={{ borderColor: "#4A1525" }}>
      <div className="flex items-center gap-2 mb-3">
        <span className="text-xs font-semibold uppercase tracking-widest" style={{ color: "#4A1525" }}>Run Comparison</span>
      </div>
      <div className="grid grid-cols-3 gap-2 mb-2 text-xs font-semibold uppercase tracking-wider text-gray-500">
        <div>Metric</div>
        <div className="text-center">
          <span className="px-2 py-0.5 text-xs rounded" style={{ background: SCENARIO_META[runA.scenario as keyof typeof SCENARIO_META]?.bg ?? "#eee", color: SCENARIO_META[runA.scenario as keyof typeof SCENARIO_META]?.color ?? "#333" }}>{runA.scenario}</span>
        </div>
        <div className="text-center">
          <span className="px-2 py-0.5 text-xs rounded" style={{ background: SCENARIO_META[runB.scenario as keyof typeof SCENARIO_META]?.bg ?? "#eee", color: SCENARIO_META[runB.scenario as keyof typeof SCENARIO_META]?.color ?? "#333" }}>{runB.scenario}</span>
        </div>
      </div>
      {fields.map(f => {
        const diff = f.a != null && f.b != null ? f.b - f.a : null;
        const isPositive = diff !== null && diff > 0;
        const isNegative = diff !== null && diff < 0;
        return (
          <div key={f.label} className="grid grid-cols-3 gap-2 py-1.5 border-t border-gray-100 text-sm">
            <div className="text-xs text-gray-500 font-medium">{f.label}</div>
            <div className="text-center font-mono font-bold text-gray-800">{f.a != null ? `${f.a}${f.unit}` : "—"}</div>
            <div className="text-center font-mono font-bold" style={{ color: isPositive ? "#008751" : isNegative ? "#C0392B" : "#333" }}>
              {f.b != null ? `${f.b}${f.unit}` : "—"}
              {diff !== null && diff !== 0 && <span className="text-xs ml-1">({diff > 0 ? "+" : ""}{diff.toFixed(1)})</span>}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function SensitivityHeatmap({ config }: { config: SimConfig }) {
  const steps = [0, 20, 40, 60, 80, 100];
  const heatCells = steps.flatMap(w => steps.map(s => {
    const hr = runSimulation({ ...config, weatherSeverity: w, securityThreat: s });
    return { w, s, turnout: hr.turnout };
  }));
  const minT = Math.min(...heatCells.map(c => c.turnout));
  const maxT = Math.max(...heatCells.map(c => c.turnout));
  const lerp = (t: number): string => {
    const norm = maxT === minT ? 0.5 : (t - minT) / (maxT - minT);
    if (norm < 0.5) {
      return `rgb(${Math.round(192 + (245 - 192) * norm * 2)},${Math.round(57 + (197 - 57) * norm * 2)},${Math.round(43 + (24 - 43) * norm * 2)})`;
    }
    return `rgb(${Math.round(245 + (0 - 245) * (norm - 0.5) * 2)},${Math.round(197 + (135 - 197) * (norm - 0.5) * 2)},${Math.round(24 + (81 - 24) * (norm - 0.5) * 2)})`;
  };
  return (
    <div>
      <p className="text-xs text-gray-500 mb-3">Projected turnout (%) across Weather Severity (X) × Security Threat (Y). Current config highlighted.</p>
      <div style={{ overflowX: "auto" }}>
        <table className="text-xs border-collapse" style={{ minWidth: 340 }}>
          <thead>
            <tr>
              <th className="text-gray-400 font-normal pr-2 pb-1 text-right" style={{ fontSize: 9 }}>Sec↓ / Wx→</th>
              {steps.map(w => <th key={w} className="text-center font-mono pb-1" style={{ fontSize: 9, width: 44 }}>{w}</th>)}
            </tr>
          </thead>
          <tbody>
            {steps.map(s => (
              <tr key={s}>
                <td className="text-right font-mono pr-2" style={{ fontSize: 9 }}>{s}</td>
                {steps.map(w => {
                  const cell = heatCells.find(c => c.w === w && c.s === s)!;
                  const isCurrent = Math.abs(w - config.weatherSeverity) < 15 && Math.abs(s - config.securityThreat) < 15;
                  return (
                    <td key={w} title={`Weather ${w}, Security ${s}: ${cell.turnout}%`}
                      style={{ background: lerp(cell.turnout), width: 44, height: 32, textAlign: "center", fontFamily: "monospace", fontWeight: isCurrent ? 700 : 400, border: isCurrent ? "2px solid #4A1525" : "1px solid rgba(255,255,255,0.3)", fontSize: 10, color: cell.turnout > (minT + maxT) / 2 ? "#1a1a1a" : "#fff" }}>
                      {cell.turnout}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex items-center gap-2 mt-3">
        <span className="text-xs text-gray-400">Low</span>
        <div style={{ flex: 1, height: 8, background: "linear-gradient(to right, #C0392B, #F5C518, #008751)", borderRadius: 4 }} />
        <span className="text-xs text-gray-400">High turnout</span>
      </div>
    </div>
  );
}

export default function Home() {
  const { profile, isLoading, profileId } = useCandidateProfile();
  const utils = trpc.useUtils();

  const [activeTab, setActiveTab] = useState<"hub" | "simulation">("hub");
  const [config, setConfig] = useState<SimConfig>({
    scenario: "baseline", state: "FCT — Abuja",
    registeredVoters: 1_250_000, pollingUnits: 3_200,
    weatherSeverity: 20, securityThreat: 15, bvasReliability: 85, staffTraining: 75, iterations: 1000,
  });
  const [result, setResult] = useState<SimulationResult | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [chartTab, setChartTab] = useState<"timeline" | "lga" | "montecarlo" | "heatmap">("timeline");
  const [runCount, setRunCount] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [narrative, setNarrative] = useState<string | null>(null);
  const [compareIds, setCompareIds] = useState<[number | null, number | null]>([null, null]);
  const [runLabel, setRunLabel] = useState("");
  const [historyFilter, setHistoryFilter] = useState("");

  // Live unresolved incident count for War Room badge
  const { data: warRoomData } = trpc.warRoom.incidents.useQuery(
    { profileId: profileId! }, { enabled: !!profileId, refetchInterval: 15_000 }
  );
  const unresolvedCount = (warRoomData ?? []).filter((inc: any) => inc.status !== "resolved").length;

  const { data: simHistory = [] } = trpc.simulation.history.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );

  const saveSimMut = trpc.simulation.save.useMutation({
    onSuccess: () => { utils.simulation.history.invalidate(); setRunLabel(""); toast.success("Simulation run saved to history"); },
    onError: (e) => toast.error("Save failed: " + e.message),
  });
  const narrativeMut = trpc.simulation.narrative.useMutation({
    onSuccess: (data) => setNarrative(typeof data.narrative === "string" ? data.narrative : String(data.narrative ?? "")),
    onError: (e) => toast.error("AI narrative failed: " + e.message),
  });

  const handleRun = useCallback(() => {
    setIsRunning(true);
    setResult(null);
    setNarrative(null);
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      const r = runSimulation(config);
      setResult(r);
      setIsRunning(false);
      setRunCount(c => c + 1);
    }, 1200);
  }, [config]);

  useEffect(() => {
    handleRun();
    return () => { if (timerRef.current) clearTimeout(timerRef.current); };
  }, []);

  // Auto-generate AI narrative whenever a new run completes
  useEffect(() => {
    if (!result || runCount === 0) return;
    narrativeMut.mutate({
      scenario: result.scenario,
      stateCode: config.state,
      projectedTurnout: result.turnout,
      validVotesCast: result.validVotes,
      bvasFailureRate: result.bvasFailureRate,
      logisticsScore: result.logisticsScore,
      securityIndex: result.securityIndex,
      certificationEta: result.certificationHours,
      rejectedBallots: result.rejectedBallots,
      monteCarloP5: result.monteCarloP5,
      monteCarloP50: result.monteCarloP50,
      monteCarloP95: result.monteCarloP95,
      modelConfidence: result.confidence,
      disruptions: result.disruptions,
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runCount]);

  const handleSaveRun = () => {
    if (!result || !profileId) return toast.error("Run a simulation first");
    saveSimMut.mutate({
      profileId,
      scenario: result.scenario,
      stateCode: config.state,
      iterations: config.iterations,
      weatherSeverity: config.weatherSeverity,
      securityThreat: config.securityThreat,
      bvasReliability: config.bvasReliability,
      staffTraining: config.staffTraining,
      projectedTurnout: result.turnout,
      validVotesCast: result.validVotes,
      bvasFailureRate: result.bvasFailureRate,
      certificationEta: result.certificationHours,
      logisticsScore: result.logisticsScore,
      securityIndex: result.securityIndex,
      rejectedBallots: result.rejectedBallots,
      monteCarloP50: result.monteCarloP50,
      monteCarloP5: result.monteCarloP5,
      monteCarloP95: result.monteCarloP95,
      modelConfidence: result.confidence,
      disruptions: result.disruptions,
      aiNarrative: narrative ?? undefined,
      label: runLabel || undefined,
    });
  };

  const handleGenerateNarrative = () => {
    if (!result) return toast.error("Run a simulation first");
    narrativeMut.mutate({
      scenario: result.scenario,
      stateCode: config.state,
      projectedTurnout: result.turnout,
      validVotesCast: result.validVotes,
      bvasFailureRate: result.bvasFailureRate,
      logisticsScore: result.logisticsScore,
      securityIndex: result.securityIndex,
      certificationEta: result.certificationHours,
      rejectedBallots: result.rejectedBallots,
      monteCarloP5: result.monteCarloP5,
      monteCarloP50: result.monteCarloP50,
      monteCarloP95: result.monteCarloP95,
      modelConfidence: result.confidence,
      disruptions: result.disruptions,
    });
  };

  const handleExportCSV = () => {
    if (simHistory.length === 0) return toast.error("No saved runs to export");
    const headers = ["#","Scenario","State","Turnout%","Confidence%","BVAS Fail%","Logistics","Security","Cert ETA(h)","Rejected","P5%","P50%","P95%","Date"];
    const rows = [...simHistory].reverse().map((r: any, i: number) => [
      simHistory.length - i,
      r.scenario ?? "",
      r.stateCode ?? "",
      r.projectedTurnout ?? "",
      r.modelConfidence ?? "",
      r.bvasFailureRate ?? "",
      r.logisticsScore ?? "",
      r.securityIndex ?? "",
      r.certificationEta ?? "",
      r.rejectedBallots ?? "",
      r.monteCarloP5 ?? "",
      r.monteCarloP50 ?? "",
      r.monteCarloP95 ?? "",
      new Date(r.runAt ?? r.createdAt ?? Date.now()).toLocaleString("en-NG"),
    ]);
    const csv = [headers, ...rows].map(row => row.map(v => `"${String(v).replace(/"/g,'""')}"`).join(",")).join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a"); a.href = url; a.download = "simulation_history.csv"; a.click();
    URL.revokeObjectURL(url);
    toast.success("CSV exported");
  };

  const handleExportPDF = async () => {
    if (simHistory.length === 0) return toast.error("No saved runs to export");
    const { jsPDF } = await import("jspdf");
    const { default: autoTable } = await import("jspdf-autotable");
    const doc = new jsPDF({ orientation: "landscape" });
    doc.setFontSize(14);
    doc.text("INEC Digital Twin — Simulation History", 14, 14);
    doc.setFontSize(9);
    doc.text(`Exported: ${new Date().toLocaleString("en-NG")}`, 14, 20);
    const head = [["#","Scenario","State","Turnout%","Conf%","BVAS%","Logistics","Security","Cert(h)","Date"]];
    const body = [...simHistory].reverse().map((r: any, i: number) => [
      simHistory.length - i,
      r.scenario ?? "—",
      r.stateCode ?? "—",
      r.projectedTurnout != null ? r.projectedTurnout + "%" : "—",
      r.modelConfidence != null ? r.modelConfidence + "%" : "—",
      r.bvasFailureRate != null ? r.bvasFailureRate + "%" : "—",
      r.logisticsScore != null ? r.logisticsScore + "/100" : "—",
      r.securityIndex != null ? r.securityIndex + "/100" : "—",
      r.certificationEta != null ? r.certificationEta + "h" : "—",
      new Date(r.runAt ?? r.createdAt ?? Date.now()).toLocaleDateString("en-NG"),
    ]);
    autoTable(doc, { head, body, startY: 26, styles: { fontSize: 8 }, headStyles: { fillColor: [74, 21, 37] } });
    doc.save("simulation_history.pdf");
    toast.success("PDF exported");
  };
  const handleShareReport = () => {
    if (!result) return toast.error("Run a simulation first");
    const lines = [
      `📊 INEC Digital Twin — Simulation Report`,
      `Scenario: ${result.scenario.toUpperCase()} | State: ${config.state}`,
      ``,
      `📈 Key Metrics:`,
      `  • Projected Turnout: ${result.turnout}%`,
      `  • Valid Votes Cast: ${result.validVotes.toLocaleString()}`,
      `  • BVAS Failure Rate: ${result.bvasFailureRate}%`,
      `  • Logistics Score: ${result.logisticsScore}/100`,
      `  • Security Index: ${result.securityIndex}/100`,
      `  • Certification ETA: ${result.certificationHours}h`,
      `  • Model Confidence: ${result.confidence}%`,
      ``,
      `📉 Monte Carlo Range: ${result.monteCarloP5}% – ${result.monteCarloP50}% – ${result.monteCarloP95}%`,
      ``,
      `⚠️ Disruptions:`,
      ...result.disruptions.map(d => `  • ${d}`),
    ];
    if (narrative) {
      lines.push(``, `🤖 AI Briefing:`, narrative);
    }
    lines.push(``, `Generated by INEC Campaign Intelligence Platform`);
    const text = lines.join('\n');
    navigator.clipboard.writeText(text)
      .then(() => toast.success("Report copied to clipboard — paste into WhatsApp, email, or a document"))
      .catch(() => toast.error("Clipboard copy failed — please copy manually"));
  };

  const sm = SCENARIO_META[config.scenario];

  const mcData = result ? Array.from({ length: 20 }, (_, i) => {
    const x = result.monteCarloP5 + (result.monteCarloP95 - result.monteCarloP5) * (i / 19);
    const mu = result.monteCarloP50;
    const sigma = (result.monteCarloP95 - result.monteCarloP5) / 4;
    const density = Math.round(Math.exp(-0.5 * ((x - mu) / sigma) ** 2) / (sigma * Math.sqrt(2 * Math.PI)) * 800);
    return { turnout: Math.round(x), density };
  }) : [];

  return (
    <div className="min-h-screen pb-16 sm:pb-0" style={{ background: "#F5F0EB", fontFamily: "'Inter', sans-serif" }}>
      {/* Header */}
      <header style={{ background: "#4A1525" }} className="px-4 sm:px-6 py-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded flex items-center justify-center" style={{ background: "#008751" }}>
            <Cpu size={18} className="text-white" />
          </div>
          <div>
            <h1 className="text-white font-bold text-base sm:text-lg leading-tight" style={{ fontFamily: "'Playfair Display', serif" }}>
              INEC Campaign Intelligence
            </h1>
            <p className="text-xs" style={{ color: "#C9B8BE" }}>Digital Twin Platform · v14.0</p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {profile && (
            <div className="hidden sm:block text-right">
              <p className="text-white text-sm font-semibold">{profile.candidateName ?? "—"}</p>
              <p className="text-xs" style={{ color: "#C9B8BE" }}>{profile.partyName ?? "—"} · {profile.office ?? "—"}</p>
            </div>
          )}
          <Link href="/profile">
            <Button variant="ghost" size="sm" className="text-white hover:bg-white/10 gap-1.5">
              <Settings size={14} /> <span className="hidden sm:inline">Profile</span>
            </Button>
          </Link>
        </div>
      </header>

      {/* Tab switcher */}
      <div style={{ background: "#3A0F1A" }} className="px-4 sm:px-6 flex gap-1">
        {(["hub", "simulation"] as const).map(t => (
          <button key={t} onClick={() => setActiveTab(t)}
            className="px-4 py-2.5 text-xs font-semibold uppercase tracking-widest transition-all"
            style={{ color: activeTab === t ? "white" : "#C9B8BE", borderBottom: activeTab === t ? "2px solid #008751" : "2px solid transparent" }}>
            {t === "hub" ? "Campaign Tools" : "Simulation Engine"}
          </button>
        ))}
      </div>

      {/* ── Hub Tab ── */}
      {activeTab === "hub" && (
        <div className="max-w-7xl mx-auto px-4 sm:px-6 py-6">
          {/* Welcome Banner */}
          {!isLoading && !profile?.candidateName && (
            <div className="mb-6 p-4 bg-amber-50 border border-amber-200 rounded flex items-center justify-between gap-4">
              <div className="flex items-center gap-3">
                <AlertTriangle size={18} className="text-amber-600 flex-shrink-0" />
                <p className="text-sm text-amber-800">Set up your candidate profile to personalise all 22 campaign tools.</p>
              </div>
              <Link href="/profile">
                <Button size="sm" style={{ background: "#4A1525", color: "white" }}>Set Up Profile</Button>
              </Link>
            </div>
          )}

          {/* Quick Access */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
            {[
              { path: "/dashboard", icon: Activity, label: "KPI Dashboard", color: "#1A3A5C" },
              { path: "/war-room", icon: Zap, label: "War Room", color: "#C0392B", badge: unresolvedCount > 0 ? unresolvedCount : undefined },
              { path: "/legal-compliance", icon: Scale, label: "Compliance", color: "#008751" },
              { path: "/timeline", icon: Calendar, label: "Timeline", color: "#4A1525" },
            ].map(q => (
              <Link key={q.path} href={q.path}>
                <div className="bg-white border border-gray-200 rounded p-4 flex items-center gap-3 cursor-pointer hover:shadow-sm transition-shadow"
                  style={{ borderLeft: `4px solid ${q.color}` }}>
                  <q.icon size={20} style={{ color: q.color }} />
                  <span className="text-sm font-semibold text-gray-800 flex-1">{q.label}</span>
                  {(q as any).badge && (
                    <span className="flex items-center justify-center min-w-5 h-5 px-1 rounded-full text-white text-xs font-bold flex-shrink-0" style={{ background: "#C0392B" }}>
                      {(q as any).badge > 99 ? "99+" : (q as any).badge}
                    </span>
                  )}
                </div>
              </Link>
            ))}
          </div>

          {/* All Features Grid */}
          <h2 className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">All Campaign Tools</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
            {FEATURES.map(f => (
              <Link key={f.path} href={f.path}>
                <div className="bg-white border border-gray-200 rounded p-4 flex items-start gap-3 cursor-pointer hover:shadow-sm transition-shadow group">
                  <div className="w-9 h-9 rounded flex items-center justify-center flex-shrink-0" style={{ background: f.color + "18" }}>
                    <f.icon size={16} style={{ color: f.color }} />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="font-semibold text-sm text-gray-900 leading-tight">{f.label}</p>
                    <p className="text-xs text-gray-500 mt-0.5 leading-tight">{f.desc}</p>
                  </div>
                  <ChevronRight size={14} className="text-gray-300 group-hover:text-gray-500 flex-shrink-0 mt-0.5 transition-colors" />
                </div>
              </Link>
            ))}
          </div>
        </div>
      )}

      {/* ── Simulation Engine Tab ── */}
      {activeTab === "simulation" && (
        <div className="flex flex-col lg:flex-row flex-1">
          {/* Sidebar */}
          <aside className="w-full lg:w-72 flex-shrink-0" style={{ background: "#2C0D1A", borderRight: "1px solid #3D1520" }}>
            <div className="p-4 border-b" style={{ borderColor: "#3D1520" }}>
              <p className="text-xs font-semibold uppercase tracking-widest mb-3" style={{ color: "#C9B8BE" }}>Scenario</p>
              <div className="grid grid-cols-2 gap-2">
                {(["baseline", "optimistic", "pessimistic", "crisis"] as const).map(s => (
                  <button key={s} onClick={() => setConfig(c => ({ ...c, scenario: s }))}
                    className="py-2 px-3 text-xs font-semibold uppercase tracking-wide transition-all"
                    style={{ background: config.scenario === s ? SCENARIO_META[s].color : "transparent", color: config.scenario === s ? "white" : "#C9B8BE", border: `1px solid ${config.scenario === s ? SCENARIO_META[s].color : "#3D1520"}` }}>
                    {SCENARIO_META[s].label}
                  </button>
                ))}
              </div>
            </div>
            <div className="p-4 border-b" style={{ borderColor: "#3D1520" }}>
              <p className="text-xs font-semibold uppercase tracking-widest mb-4" style={{ color: "#C9B8BE" }}>Parameters</p>
              <SliderControl label="Weather Severity" value={config.weatherSeverity} onChange={v => setConfig(c => ({ ...c, weatherSeverity: v }))} color="#C0392B" />
              <SliderControl label="Security Threat" value={config.securityThreat} onChange={v => setConfig(c => ({ ...c, securityThreat: v }))} color="#C0392B" />
              <SliderControl label="BVAS Reliability %" value={config.bvasReliability} onChange={v => setConfig(c => ({ ...c, bvasReliability: v }))} color="#008751" />
              <SliderControl label="Staff Training Score" value={config.staffTraining} onChange={v => setConfig(c => ({ ...c, staffTraining: v }))} color="#008751" />
              <div className="mt-4">
                <p className="text-xs font-semibold uppercase tracking-widest mb-2" style={{ color: "#C9B8BE" }}>Iterations</p>
                <div className="grid grid-cols-3 gap-1">
                  {[100, 1000, 10000].map(n => (
                    <button key={n} onClick={() => setConfig(c => ({ ...c, iterations: n }))}
                      className="py-1.5 text-xs font-mono font-bold transition-all"
                      style={{ background: config.iterations === n ? "#008751" : "transparent", color: config.iterations === n ? "white" : "#C9B8BE", border: `1px solid ${config.iterations === n ? "#008751" : "#3D1520"}` }}>
                      {n.toLocaleString()}
                    </button>
                  ))}
                </div>
              </div>
            </div>
            <div className="p-4 space-y-2">
              <button onClick={handleRun} disabled={isRunning}
                className="w-full py-3 font-semibold text-sm uppercase tracking-widest flex items-center justify-center gap-2 transition-all active:scale-95"
                style={{ background: isRunning ? "#3D1520" : "#008751", color: "white", cursor: isRunning ? "wait" : "pointer" }}>
                {isRunning ? <><Activity size={14} className="animate-pulse" /> Running…</> : <><Zap size={14} /> Run Simulation</>}
              </button>
              {result && (
                <>
                  <input
                    type="text"
                    value={runLabel}
                    onChange={e => setRunLabel(e.target.value)}
                    placeholder="Label this run (optional)…"
                    maxLength={120}
                    className="w-full px-3 py-2 text-xs bg-transparent border text-white placeholder-gray-500 focus:outline-none focus:border-gray-400"
                    style={{ borderColor: "#3D1520" }}
                  />
                  <button onClick={handleSaveRun} disabled={saveSimMut.isPending}
                    className="w-full py-2 font-semibold text-xs uppercase tracking-widest flex items-center justify-center gap-2 transition-all border"
                    style={{ background: "transparent", color: "#C9B8BE", borderColor: "#3D1520" }}>
                    {saveSimMut.isPending ? <><Activity size={12} className="animate-spin" /> Saving…</> : <><Save size={12} /> Save Run to History</>}
                  </button>
                  <button onClick={handleShareReport}
                    className="w-full py-2 font-semibold text-xs uppercase tracking-widest flex items-center justify-center gap-2 transition-all border"
                    style={{ background: "transparent", color: "#C9B8BE", borderColor: "#3D1520" }}>
                    <Copy size={12} /> Copy Report
                  </button>
                </>
              )}
            </div>
          </aside>

          {/* Main Results */}
          <main className="flex-1 overflow-auto p-4 sm:p-6">
            <Tabs defaultValue="results">
              <TabsList className="mb-4">
                <TabsTrigger value="results">Current Run</TabsTrigger>
                <TabsTrigger value="history">
                  <History size={12} className="mr-1" />
                  History ({simHistory.length})
                </TabsTrigger>
              </TabsList>

              <TabsContent value="results">
                <AnimatePresence mode="wait">
                  {isRunning && (
                    <motion.div key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
                      className="flex flex-col items-center justify-center min-h-64">
                      <div className="w-12 h-12 border-4 border-gray-200 rounded-full animate-spin mb-4" style={{ borderTopColor: "#008751" }} />
                      <p style={{ fontFamily: "'Playfair Display', serif", fontSize: "20px", color: "#4A1525", fontWeight: 700 }}>
                        Running {config.iterations.toLocaleString()} Iterations
                      </p>
                    </motion.div>
                  )}
                  {!isRunning && result && (
                    <motion.div key={`result-${runCount}`} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.4 }}>
                      <div className="flex items-center justify-between mb-4 pb-3 border-b-2" style={{ borderColor: sm.color }}>
                        <div>
                          <span className="text-xs font-bold uppercase tracking-widest px-2 py-0.5 mr-2" style={{ background: sm.bg, color: sm.color, border: `1px solid ${sm.border}` }}>{sm.label}</span>
                          <span className="text-xs text-gray-500 font-mono">{config.state}</span>
                        </div>
                        <div className="text-right">
                          <p className="text-xs text-gray-500">Confidence</p>
                          <p className="font-mono text-2xl font-bold" style={{ color: sm.color }}>{result.confidence}%</p>
                        </div>
                      </div>
                      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-4">
                        {[
                          { label: "Turnout", value: `${result.turnout}%`, icon: TrendingUp, color: sm.color },
                          { label: "Valid Votes", value: result.validVotes.toLocaleString(), icon: CheckCircle, color: "#008751" },
                          { label: "BVAS Failure", value: `${result.bvasFailureRate}%`, icon: Cpu, color: result.bvasFailureRate > 5 ? "#C0392B" : "#1A3A5C" },
                          { label: "Cert. ETA", value: `${result.certificationHours}h`, icon: Clock, color: "#1A3A5C" },
                          { label: "Logistics", value: `${result.logisticsScore}/100`, icon: Database, color: result.logisticsScore < 60 ? "#C0392B" : "#008751" },
                          { label: "Security", value: `${result.securityIndex}/100`, icon: Shield, color: result.securityIndex < 60 ? "#C0392B" : "#1A3A5C" },
                          { label: "Rejected", value: result.rejectedBallots.toLocaleString(), icon: AlertTriangle, color: result.rejectedBallots > 10000 ? "#C0392B" : "#666" },
                          { label: "P50 Median", value: `${result.monteCarloP50}%`, icon: BarChart2, color: "#4A1525" },
                        ].map(k => (
                          <div key={k.label} className="bg-white border border-gray-200 p-3" style={{ borderTop: `3px solid ${k.color}` }}>
                            <div className="flex items-start justify-between">
                              <div>
                                <p className="text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1">{k.label}</p>
                                <p className="font-mono text-lg font-bold" style={{ color: k.color }}>{k.value}</p>
                              </div>
                              <k.icon size={14} style={{ color: k.color }} className="mt-1 opacity-60" />
                            </div>
                          </div>
                        ))}
                      </div>
                      <div className="mb-4 flex flex-wrap gap-2">
                        {result.disruptions.map(d => (
                          <div key={d} className={`flex items-center gap-2 px-3 py-1.5 text-xs font-medium ${result.scenario === "crisis" || result.scenario === "pessimistic" ? "bg-red-50 text-red-800 border border-red-200" : "bg-green-50 text-green-800 border border-green-200"}`}>
                            {result.scenario === "crisis" || result.scenario === "pessimistic" ? <AlertTriangle size={11} /> : <CheckCircle size={11} />}
                            {d}
                          </div>
                        ))}
                      </div>
                      {/* AI Narrative Panel */}
                      <div className="mb-4 bg-white border border-gray-200 p-4" style={{ borderTop: "3px solid #008751" }}>
                        <div className="flex items-center justify-between mb-2">
                          <div className="flex items-center gap-2">
                            <Sparkles size={14} style={{ color: "#008751" }} />
                            <span className="text-xs font-semibold uppercase tracking-widest" style={{ color: "#008751" }}>AI Analyst Briefing</span>
                          </div>
                          <button onClick={handleGenerateNarrative} disabled={narrativeMut.isPending}
                            className="flex items-center gap-1.5 px-3 py-1 text-xs font-semibold uppercase tracking-widest transition-all active:scale-95"
                            style={{ background: narrativeMut.isPending ? "#ccc" : "#008751", color: "white", cursor: narrativeMut.isPending ? "wait" : "pointer" }}>
                            {narrativeMut.isPending ? <><Activity size={11} className="animate-spin" /> Generating…</> : <><Sparkles size={11} /> Generate</>}
                          </button>
                        </div>
                        {narrative ? (
                          <p className="text-sm text-gray-700 leading-relaxed">{narrative}</p>
                        ) : (
                          <p className="text-xs text-gray-400 italic">Click "Generate" to get an AI-powered plain-English briefing of this simulation result.</p>
                        )}
                      </div>
                      <div className="bg-white border border-gray-200 p-4" style={{ borderTop: `3px solid #1A3A5C` }}>
                        <div className="flex items-center gap-4 mb-4 border-b border-gray-100 pb-3 flex-wrap">
                          {(["timeline", "lga", "montecarlo", "heatmap"] as const).map(tab => (
                            <button key={tab} onClick={() => setChartTab(tab)}
                              className="text-xs font-semibold uppercase tracking-widest pb-1 transition-all"
                              style={{ color: chartTab === tab ? "#4A1525" : "#999", borderBottom: chartTab === tab ? "2px solid #4A1525" : "2px solid transparent" }}>
                              {tab === "timeline" ? "Turnout Timeline" : tab === "lga" ? "LGA Breakdown" : tab === "montecarlo" ? "Monte Carlo" : "Sensitivity"}
                            </button>
                          ))}
                        </div>
                        {chartTab === "timeline" && (
                          <ResponsiveContainer width="100%" height={220}>
                            <AreaChart data={result.timelineData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                              <defs><linearGradient id="tg" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor={sm.color} stopOpacity={0.3} /><stop offset="95%" stopColor={sm.color} stopOpacity={0.02} /></linearGradient></defs>
                              <CartesianGrid strokeDasharray="3 3" stroke="#F0EBE8" />
                              <XAxis dataKey="hour" tickFormatter={h => `${h}:00`} tick={{ fontSize: 10 }} />
                              <YAxis tickFormatter={v => (v / 1000).toFixed(0) + "k"} tick={{ fontSize: 10 }} />
                              <Tooltip labelFormatter={h => `Hour ${h}:00`} />
                              <Area type="monotone" dataKey="cumTurnout" name="Turnout" stroke={sm.color} fill="url(#tg)" strokeWidth={2} dot={false} />
                            </AreaChart>
                          </ResponsiveContainer>
                        )}
                        {chartTab === "lga" && (
                          <ResponsiveContainer width="100%" height={220}>
                            <BarChart data={result.lgaData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                              <CartesianGrid strokeDasharray="3 3" stroke="#F0EBE8" />
                              <XAxis dataKey="lga" tick={{ fontSize: 9 }} />
                              <YAxis tickFormatter={v => v + "%"} tick={{ fontSize: 10 }} domain={[0, 100]} />
                              <Tooltip formatter={(v: number) => [v + "%", "Turnout"]} />
                              <ReferenceLine y={result.turnout} stroke="#4A1525" strokeDasharray="4 4" />
                              <Bar dataKey="turnout" fill={sm.color} radius={[2, 2, 0, 0]} />
                            </BarChart>
                          </ResponsiveContainer>
                        )}
                        {chartTab === "montecarlo" && (
                          <ResponsiveContainer width="100%" height={220}>
                            <AreaChart data={mcData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                              <defs><linearGradient id="mcg" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#1A3A5C" stopOpacity={0.4} /><stop offset="95%" stopColor="#1A3A5C" stopOpacity={0.02} /></linearGradient></defs>
                              <CartesianGrid strokeDasharray="3 3" stroke="#F0EBE8" />
                              <XAxis dataKey="turnout" tickFormatter={v => v + "%"} tick={{ fontSize: 10 }} />
                              <YAxis tick={{ fontSize: 10 }} />
                              <Tooltip labelFormatter={v => `Turnout: ${v}%`} />
                              <ReferenceLine x={result.monteCarloP50} stroke="#4A1525" strokeDasharray="4 4" />
                              <Area type="monotone" dataKey="density" stroke="#1A3A5C" fill="url(#mcg)" strokeWidth={2} dot={false} />
                            </AreaChart>
                          </ResponsiveContainer>
                        )}
                        {chartTab === "heatmap" && (
                          <SensitivityHeatmap config={config} />
                        )}
                      </div>
                    </motion.div>
                  )}
                </AnimatePresence>
              </TabsContent>

              <TabsContent value="history">
                {simHistory.length === 0 ? (
                  <div className="text-center py-20 text-gray-500">
                    <History size={48} className="mx-auto mb-4 opacity-30" />
                    <p>No saved runs yet — run a simulation and click "Save Run to History"</p>
                  </div>
                ) : (
                  <>
                    {/* Toolbar: export + compare */}
                    <div className="flex flex-wrap items-center justify-between gap-2 mb-3">
                      <div className="flex items-center gap-2 flex-1 min-w-0">
                        <p className="text-xs text-gray-500 font-mono whitespace-nowrap">{simHistory.length} run{simHistory.length !== 1 ? "s" : ""}</p>
                        <input
                          type="text"
                          value={historyFilter}
                          onChange={e => setHistoryFilter(e.target.value)}
                          placeholder="Filter by label or scenario…"
                          className="flex-1 min-w-0 px-2 py-1 text-xs border border-gray-200 rounded focus:outline-none focus:border-gray-400"
                        />
                      </div>
                      <div className="flex gap-2">
                        <button onClick={handleExportCSV}
                          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold uppercase tracking-widest border transition-all active:scale-95"
                          style={{ borderColor: "#1A3A5C", color: "#1A3A5C" }}>
                          <Download size={11} /> CSV
                        </button>
                        <button onClick={handleExportPDF}
                          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold uppercase tracking-widest border transition-all active:scale-95"
                          style={{ borderColor: "#4A1525", color: "#4A1525" }}>
                          <Download size={11} /> PDF
                        </button>
                        {compareIds[0] !== null && compareIds[1] !== null && (
                          <button onClick={() => setCompareIds([null, null])}
                            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold uppercase tracking-widest transition-all active:scale-95"
                            style={{ background: "#C0392B", color: "white" }}>
                            <X size={11} /> Clear Compare
                          </button>
                        )}
                        {simHistory.length >= 2 && compareIds[0] === null && compareIds[1] === null && (
                          <button onClick={() => {
                            const latest = simHistory[0];
                            const second = simHistory[1];
                            if (latest && second) setCompareIds([second.id, latest.id]);
                          }}
                            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold uppercase tracking-widest border transition-all active:scale-95"
                            style={{ borderColor: "#008751", color: "#008751" }}>
                            Compare to Latest
                          </button>
                        )}
                      </div>
                    </div>

                    {/* Comparison panel */}
                    {compareIds[0] !== null && compareIds[1] !== null && (
                      <ComparisonPanel runA={simHistory.find((r: any) => r.id === compareIds[0]) as any} runB={simHistory.find((r: any) => r.id === compareIds[1]) as any} />
                    )}

                    {/* Run list */}
                    <div className="space-y-3">
                      {[...simHistory].reverse().filter((run: any) => {
                        if (!historyFilter.trim()) return true;
                        const q = historyFilter.toLowerCase();
                        return (run.label ?? "").toLowerCase().includes(q) || (run.scenario ?? "").toLowerCase().includes(q) || (run.stateCode ?? "").toLowerCase().includes(q);
                      }).map((run: any, i: number) => {
                        const isSelected = compareIds[0] === run.id || compareIds[1] === run.id;
                        const selIdx = compareIds[0] === run.id ? 0 : compareIds[1] === run.id ? 1 : -1;
                        const handleCompareToggle = () => {
                          if (isSelected) {
                            setCompareIds(prev => [prev[0] === run.id ? null : prev[0], prev[1] === run.id ? null : prev[1]]);
                          } else if (compareIds[0] === null) {
                            setCompareIds(prev => [run.id, prev[1]]);
                          } else if (compareIds[1] === null) {
                            setCompareIds(prev => [prev[0], run.id]);
                          } else {
                            toast.error("Clear one selection first");
                          }
                        };
                        return (
                          <div key={run.id} className="bg-white border border-gray-200 rounded p-4"
                            style={{ borderLeft: isSelected ? `4px solid ${selIdx === 0 ? "#1A3A5C" : "#C0392B"}` : undefined }}>
                            <div className="flex items-center justify-between mb-2 flex-wrap gap-2">
                              <div className="flex items-center gap-2">
                                <span className="font-mono text-xs text-gray-400">#{simHistory.length - i}</span>
                                <Badge style={{ background: SCENARIO_META[run.scenario as keyof typeof SCENARIO_META]?.bg ?? "#F0F0F0", color: SCENARIO_META[run.scenario as keyof typeof SCENARIO_META]?.color ?? "#666" }}>
                                  {run.scenario}
                                </Badge>
                                <span className="text-xs text-gray-500">{run.stateCode}</span>
                                {run.label && (
                                  <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-gray-100 text-gray-700 max-w-[160px] truncate">{run.label}</span>
                                )}
                              </div>
                              <div className="flex items-center gap-2">
                                <span className="text-xs text-gray-400">{new Date(run.runAt ?? run.createdAt ?? Date.now()).toLocaleString("en-NG")}</span>
                                <button onClick={handleCompareToggle}
                                  className="flex items-center gap-1 px-2 py-0.5 text-xs font-semibold border transition-all"
                                  style={{ borderColor: isSelected ? (selIdx === 0 ? "#1A3A5C" : "#C0392B") : "#ccc", color: isSelected ? (selIdx === 0 ? "#1A3A5C" : "#C0392B") : "#999", background: isSelected ? (selIdx === 0 ? "#EBF2F8" : "#FBEAE9") : "transparent" }}>
                                  <GitCompare size={10} />
                                  {isSelected ? (selIdx === 0 ? "A" : "B") : "Compare"}
                                </button>
                              </div>
                            </div>
                            <div className="grid grid-cols-3 sm:grid-cols-6 gap-3 text-center">
                              {[
                                { label: "Turnout", value: `${run.projectedTurnout ?? "—"}%` },
                                { label: "Confidence", value: `${run.modelConfidence ?? "—"}%` },
                                { label: "BVAS Fail", value: `${run.bvasFailureRate ?? "—"}%` },
                                { label: "Logistics", value: `${run.logisticsScore ?? "—"}/100` },
                                { label: "Security", value: `${run.securityIndex ?? "—"}/100` },
                                { label: "Cert ETA", value: `${run.certificationEta ?? "—"}h` },
                              ].map(k => (
                                <div key={k.label}>
                                  <p className="text-xs text-gray-400">{k.label}</p>
                                  <p className="font-mono text-sm font-bold text-gray-800">{k.value}</p>
                                </div>
                              ))}
                            </div>
                            {run.aiNarrative && (
                              <div className="mt-3 pt-3 border-t border-gray-100">
                                <p className="text-xs text-gray-500 font-semibold uppercase tracking-wider mb-1 flex items-center gap-1">
                                  <span style={{ color: "#008751" }}>✦</span> AI Briefing
                                </p>
                                <p className="text-xs text-gray-600 leading-relaxed italic">{run.aiNarrative}</p>
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </>
                )}
              </TabsContent>
            </Tabs>
          </main>
        </div>
      )}
    </div>
  );
}
