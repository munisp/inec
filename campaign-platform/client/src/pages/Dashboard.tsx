/**
 * Campaign Dashboard — Live KPI overview aggregating all 21 modules
 * Palette: #4A1525 (burgundy), #008751 (green), #1A3A5C (navy), #F5F0EB (paper)
 */
import { Link } from "wouter";
import { trpc } from "@/lib/trpc";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import {
  Users, ShieldCheck, DollarSign, Calendar, FileText, UserPlus,
  AlertTriangle, TrendingUp, ChevronLeft, BarChart2, Clock, CheckCircle,
} from "lucide-react";
import {
  RadialBarChart, RadialBar, PieChart, Pie, Cell,
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from "recharts";

function KPICard({
  label, value, sub, icon: Icon, color, href,
}: {
  label: string; value: string | number; sub?: string;
  icon: React.ElementType; color: string; href?: string;
}) {
  const inner = (
    <div
      className="bg-white border border-gray-200 p-4 rounded-lg hover:shadow-md transition-shadow cursor-pointer"
      style={{ borderTop: `3px solid ${color}` }}
    >
      <div className="flex items-start justify-between mb-2">
        <p className="text-xs font-bold uppercase tracking-widest text-gray-500">{label}</p>
        <Icon size={16} style={{ color }} className="opacity-60 mt-0.5" />
      </div>
      <p className="font-mono text-2xl font-bold" style={{ color }}>{value}</p>
      {sub && <p className="text-xs text-gray-400 mt-1">{sub}</p>}
    </div>
  );
  return href ? <Link href={href}>{inner}</Link> : inner;
}

export default function Dashboard() {
  const { profileId, profile } = useCandidateProfile();
  const candidateName = profile?.candidateName;
  const partyName = profile?.partyName;
  const stateName = profile?.stateName;

  const { data: kpis, isLoading } = trpc.dashboard.kpis.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId, refetchInterval: 30000 }
  );

  const complianceData = kpis ? [
    { name: "Compliant", value: kpis.complianceCompliant, color: "#008751" },
    { name: "Pending", value: kpis.complianceTotal - kpis.complianceCompliant, color: "#E5E7EB" },
  ] : [];

  const fundingData = kpis && kpis.totalBudget > 0 ? [
    { name: "Raised", value: kpis.totalFundraising, color: "#008751" },
    { name: "Gap", value: Math.max(0, kpis.totalBudget - kpis.totalFundraising), color: "#F0EBE8" },
  ] : [];

  const milestoneData = kpis ? [
    { name: "Done", value: kpis.completedMilestones, fill: "#008751" },
    { name: "Remaining", value: kpis.totalMilestones - kpis.completedMilestones, fill: "#E5E7EB" },
  ] : [];

  const formatNGN = (n: number) => {
    if (n >= 1_000_000_000) return `₦${(n / 1_000_000_000).toFixed(1)}B`;
    if (n >= 1_000_000) return `₦${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `₦${(n / 1_000).toFixed(0)}K`;
    return `₦${n}`;
  };

  return (
    <div className="min-h-screen pb-20" style={{ background: "#F5F0EB", fontFamily: "'Inter', sans-serif" }}>
      {/* Header */}
      <header className="flex items-center justify-between px-4 sm:px-6 py-3 border-b border-gray-300" style={{ background: "#4A1525" }}>
        <div className="flex items-center gap-3">
          <Link href="/" className="text-white opacity-70 hover:opacity-100 transition-opacity">
            <ChevronLeft size={20} />
          </Link>
          <div className="w-7 h-7 flex items-center justify-center rounded-sm" style={{ background: "#008751" }}>
            <BarChart2 size={14} className="text-white" />
          </div>
          <div>
            <h1 className="text-white font-bold text-base sm:text-lg leading-none">Campaign Dashboard</h1>
            <p className="text-xs" style={{ color: "#C9B8BE" }}>LIVE KPI OVERVIEW · ALL 21 MODULES</p>
          </div>
        </div>
        <div className="hidden sm:flex items-center gap-4 text-right">
          <div>
            <p className="text-xs" style={{ color: "#C9B8BE" }}>CANDIDATE</p>
            <p className="text-sm font-bold text-white">{candidateName ?? "—"}</p>
          </div>
          <div>
            <p className="text-xs" style={{ color: "#C9B8BE" }}>PARTY · STATE</p>
            <p className="text-sm font-bold text-white">{partyName ?? "—"} · {stateName ?? "—"}</p>
          </div>
          {/* Election countdown */}
          <div className="border-l border-white/20 pl-4">
            <p className="text-xs" style={{ color: "#C9B8BE" }}>ELECTION COUNTDOWN</p>
            {(() => {
              const electionDay = new Date("2027-02-20T08:00:00");
              const diff = electionDay.getTime() - Date.now();
              if (diff <= 0) return <p className="text-sm font-bold text-red-300">Election Day!</p>;
              const days = Math.floor(diff / (1000 * 60 * 60 * 24));
              const hrs = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
              return <p className="text-sm font-mono font-bold" style={{ color: days < 30 ? "#FCA5A5" : "#86EFAC" }}>{days}d {hrs}h</p>;
            })()}
          </div>
        </div>
      </header>

      <div className="max-w-6xl mx-auto px-4 sm:px-6 py-6">
        {isLoading && (
          <div className="flex items-center justify-center py-20">
            <div className="w-10 h-10 border-4 border-gray-200 rounded-full animate-spin" style={{ borderTopColor: "#008751" }} />
          </div>
        )}

        {!isLoading && kpis && (
          <>
            {/* Alert Banner */}
            {kpis.criticalIncidents > 0 && (
              <div className="flex items-center gap-3 px-4 py-3 rounded-lg mb-5 border border-red-200" style={{ background: "#FEF2F2" }}>
                <AlertTriangle size={18} className="text-red-600 flex-shrink-0" />
                <p className="text-sm text-red-700 font-semibold">
                  {kpis.criticalIncidents} critical incident{kpis.criticalIncidents > 1 ? "s" : ""} require immediate attention in the War Room.
                </p>
                <Link href="/war-room" className="ml-auto text-xs text-red-600 font-bold underline whitespace-nowrap">View →</Link>
              </div>
            )}

            {/* KPI Grid */}
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3 mb-6">
              <KPICard label="Volunteers" value={kpis.totalVolunteers.toLocaleString()} sub="Registered" icon={Users} color="#1A3A5C" href="/volunteers" />
              <KPICard label="Compliance Score" value={`${kpis.complianceScore}%`} sub={`${kpis.complianceCompliant}/${kpis.complianceTotal} items`} icon={ShieldCheck} color={kpis.complianceScore >= 80 ? "#008751" : kpis.complianceScore >= 50 ? "#F59E0B" : "#C0392B"} href="/legal-compliance" />
              <KPICard label="Fundraising" value={formatNGN(kpis.totalFundraising)} sub={kpis.totalBudget > 0 ? `of ${formatNGN(kpis.totalBudget)} budget` : "Total raised"} icon={DollarSign} color="#008751" href="/fundraising" />
              <KPICard label="Days to Deadline" value={kpis.daysToNextDeadline !== null ? kpis.daysToNextDeadline : "—"} sub={kpis.nextDeadlineDate ? new Date(kpis.nextDeadlineDate).toLocaleDateString() : "No upcoming deadlines"} icon={Clock} color={kpis.daysToNextDeadline !== null && kpis.daysToNextDeadline <= 7 ? "#C0392B" : "#4A1525"} href="/timeline" />
              <KPICard label="Petitions" value={kpis.totalPetitions.toLocaleString()} sub="Active drives" icon={FileText} color="#4A1525" href="/petition" />
              <KPICard label="Team Members" value={kpis.totalTeamMembers.toLocaleString()} sub="Campaign staff" icon={UserPlus} color="#1A3A5C" href="/team" />
              <KPICard label="Milestones" value={`${kpis.completedMilestones}/${kpis.totalMilestones}`} sub="Completed" icon={CheckCircle} color="#008751" href="/timeline" />
              <KPICard label="Active Incidents" value={kpis.activeIncidents.toLocaleString()} sub={kpis.criticalIncidents > 0 ? `${kpis.criticalIncidents} critical` : "No critical"} icon={AlertTriangle} color={kpis.activeIncidents > 0 ? "#C0392B" : "#008751"} href="/war-room" />
            </div>

            {/* Charts Row */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
              {/* Compliance Donut */}
              <div className="bg-white border border-gray-200 rounded-lg p-4" style={{ borderTop: "3px solid #008751" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Compliance Status</p>
                <div className="flex items-center justify-center">
                  <PieChart width={160} height={160}>
                    <Pie data={complianceData} cx={75} cy={75} innerRadius={45} outerRadius={70} dataKey="value" startAngle={90} endAngle={-270}>
                      {complianceData.map((entry, i) => <Cell key={i} fill={entry.color} />)}
                    </Pie>
                  </PieChart>
                </div>
                <div className="flex justify-center gap-4 text-xs mt-1">
                  <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block" style={{ background: "#008751" }} />Compliant</span>
                  <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block bg-gray-200" />Pending</span>
                </div>
              </div>

              {/* Fundraising vs Budget Donut */}
              <div className="bg-white border border-gray-200 rounded-lg p-4" style={{ borderTop: "3px solid #4A1525" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Fundraising vs Budget</p>
                {fundingData.length > 0 ? (
                  <>
                    <div className="flex items-center justify-center">
                      <PieChart width={160} height={160}>
                        <Pie data={fundingData} cx={75} cy={75} innerRadius={45} outerRadius={70} dataKey="value" startAngle={90} endAngle={-270}>
                          {fundingData.map((entry, i) => <Cell key={i} fill={entry.color} />)}
                        </Pie>
                      </PieChart>
                    </div>
                    <div className="flex justify-center gap-4 text-xs mt-1">
                      <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block" style={{ background: "#008751" }} />Raised</span>
                      <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full inline-block bg-gray-200" />Gap</span>
                    </div>
                  </>
                ) : (
                  <div className="flex items-center justify-center h-32 text-gray-400 text-sm">No budget data yet</div>
                )}
              </div>

              {/* Milestone Progress Bar */}
              <div className="bg-white border border-gray-200 rounded-lg p-4" style={{ borderTop: "3px solid #1A3A5C" }}>
                <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Timeline Progress</p>
                {milestoneData[0]?.value !== undefined && kpis.totalMilestones > 0 ? (
                  <div style={{ height: 160 }}>
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={milestoneData} layout="vertical" margin={{ left: 0, right: 20 }}>
                        <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                        <XAxis type="number" tick={{ fontSize: 10 }} />
                        <YAxis type="category" dataKey="name" tick={{ fontSize: 11 }} width={70} />
                        <Tooltip />
                        <Bar dataKey="value" radius={[0, 4, 4, 0]}>
                          {milestoneData.map((entry, i) => <Cell key={i} fill={entry.fill} />)}
                        </Bar>
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-32 text-gray-400 text-sm">No timeline data yet</div>
                )}
              </div>
            </div>

            {/* Quick Links */}
            <div className="bg-white border border-gray-200 rounded-lg p-4">
              <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-3">Quick Navigation</p>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                {[
                  ["/stakeholders", "Stakeholders Hub"],
                  ["/endorsements", "Endorsements"],
                  ["/timeline", "Campaign Timeline"],
                  ["/registration", "Voter Registration"],
                  ["/polling-units", "Polling Units"],
                  ["/volunteers", "Volunteer Portal"],
                  ["/press-release", "Press Release"],
                  ["/social-media", "Social Media"],
                  ["/legal-compliance", "Legal Compliance"],
                  ["/opposition-research", "Opposition Research"],
                  ["/war-room", "War Room"],
                  ["/results", "Results Projection"],
                  ["/manifesto", "Manifesto Builder"],
                  ["/petition", "Petition Drive"],
                  ["/diaspora", "Diaspora Outreach"],
                  ["/post-election", "Post-Election Analytics"],
                  ["/fundraising", "Fundraising"],
                  ["/budget", "Budget Planner"],
                  ["/debate-coach", "Debate Coach"],
                  ["/media-monitoring", "Media Monitoring"],
                  ["/team", "Campaign Team"],
                ].map(([href, label]) => (
                  <Link
                    key={href}
                    href={href}
                    className="px-3 py-2 text-xs font-medium rounded border border-gray-200 text-gray-700 hover:bg-gray-50 hover:border-gray-400 transition-all text-center"
                  >
                    {label}
                  </Link>
                ))}
              </div>
            </div>
          </>
        )}

        {!isLoading && !kpis && (
          <div className="text-center py-20 text-gray-400">
            <TrendingUp size={48} className="mx-auto mb-4 opacity-30" />
            <p className="text-sm">No campaign data yet. Start by editing your profile and adding data to the modules.</p>
            <Link href="/" className="mt-4 inline-block px-5 py-2 rounded text-sm text-white font-semibold" style={{ background: "#4A1525" }}>
              Go to Hub
            </Link>
          </div>
        )}
      </div>
    </div>
  );
}
