import { useEffect, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Target, Users, Trophy, Brain, MapPin, Zap, BarChart3 } from 'lucide-react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts';

interface VoterScore {
  contact_id: string;
  overall_score: number;
  engagement_score: number;
  recency_score: number;
  responsiveness_score: number;
  loyalty_score: number;
  mobilization_readiness: number;
  segment: string;
  recommended_channel: string;
  recommended_action: string;
  factors: string[];
}

interface WinState {
  state_code: string;
  win_probability: number;
  confidence: number;
  our_projected_votes: number;
  total_projected_turnout: number;
  vote_share: number;
  margin: number;
  scenario: string;
  actions_to_improve: string[];
}

interface Allocation {
  ward_code: string;
  state_code: string;
  current_volunteers: number;
  recommended_additional: number;
  expected_pledge_gain: number;
  priority: string;
  reasoning: string;
}

interface MessageArm {
  variant_id: string;
  variant_text: string;
  impressions: number;
  conversions: number;
  conversion_rate: number;
  ucb_score: number;
  status: string;
}

const SEGMENT_COLORS: Record<string, string> = {
  hot: '#ef4444',
  warm: '#f59e0b',
  cool: '#3b82f6',
  cold: '#6b7280',
  dormant: '#d1d5db',
};

const PRIORITY_COLORS: Record<string, string> = {
  critical: 'bg-red-100 text-red-800',
  high: 'bg-orange-100 text-orange-800',
  medium: 'bg-yellow-100 text-yellow-800',
  low: 'bg-green-100 text-green-800',
};

const SCENARIO_COLORS: Record<string, string> = {
  winning: 'bg-green-100 text-green-800',
  competitive: 'bg-yellow-100 text-yellow-800',
  losing: 'bg-red-100 text-red-800',
};

export default function GOTVScoring() {
  const [activeTab, setActiveTab] = useState<'overview' | 'voters' | 'win' | 'allocation' | 'bandit'>('overview');
  const [summary, setSummary] = useState<any>(null);
  const [voters, setVoters] = useState<VoterScore[]>([]);
  const [winData, setWinData] = useState<any>(null);
  const [allocations, setAllocations] = useState<any>(null);
  const [banditData, setBanditData] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  const token = localStorage.getItem('auth_token');
  const partyId = localStorage.getItem('gotv_party_id') || '1';
  const headers: HeadersInit = {
    'Authorization': `Bearer ${token}`,
    'X-GOTV-Party-ID': partyId,
    'Content-Type': 'application/json',
  };
  const BASE = `${window.location.protocol}//${window.location.hostname}:8103`;

  useEffect(() => {
    loadData();
  }, []);

  async function loadData() {
    setLoading(true);
    try {
      const [sumRes, voterRes, winRes, allocRes, banditRes] = await Promise.allSettled([
        fetch(`${BASE}/gotv/scoring/summary`, { headers }),
        fetch(`${BASE}/gotv/scoring/voters/batch`, { method: 'POST', headers, body: JSON.stringify({ limit: 50 }) }),
        fetch(`${BASE}/gotv/scoring/win-probability`, { headers }),
        fetch(`${BASE}/gotv/scoring/allocation/optimize?available_volunteers=20`, { headers }),
        fetch(`${BASE}/gotv/scoring/optimize/messages`, { headers }),
      ]);

      if (sumRes.status === 'fulfilled' && sumRes.value.ok) setSummary(await sumRes.value.json());
      if (voterRes.status === 'fulfilled' && voterRes.value.ok) {
        const d = await voterRes.value.json();
        setVoters(d.voters || []);
      }
      if (winRes.status === 'fulfilled' && winRes.value.ok) setWinData(await winRes.value.json());
      if (allocRes.status === 'fulfilled' && allocRes.value.ok) setAllocations(await allocRes.value.json());
      if (banditRes.status === 'fulfilled' && banditRes.value.ok) setBanditData(await banditRes.value.json());
    } catch (e) {
      console.error('Scoring data load error:', e);
    }
    setLoading(false);
  }

  const segmentPieData = summary?.segment_distribution
    ? Object.entries(summary.segment_distribution).map(([name, value]) => ({ name, value }))
    : [];

  const tabs = [
    { id: 'overview', label: 'Overview', icon: BarChart3 },
    { id: 'voters', label: 'Voter Scores', icon: Users },
    { id: 'win', label: 'Win Probability', icon: Trophy },
    { id: 'allocation', label: 'Resource Allocation', icon: MapPin },
    { id: 'bandit', label: 'Message Optimizer', icon: Brain },
  ] as const;

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold flex items-center gap-2">
            <Target className="h-6 w-6 text-purple-600" />
            Scoring Engine
          </h1>
          <p className="text-sm text-gray-500 mt-1">
            Cambridge Analytica-grade analytics — individual voter scoring, win probability, resource optimization
          </p>
        </div>
        <Button onClick={loadData} disabled={loading} variant="outline" size="sm">
          {loading ? 'Loading...' : 'Refresh'}
        </Button>
      </div>

      {/* Tab Navigation */}
      <div className="flex gap-1 bg-gray-100 p-1 rounded-lg">
        {tabs.map(tab => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`flex items-center gap-1.5 px-3 py-2 rounded-md text-sm font-medium transition ${
              activeTab === tab.id ? 'bg-white shadow text-purple-700' : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <tab.icon className="h-4 w-4" />
            {tab.label}
          </button>
        ))}
      </div>

      {/* Overview Tab */}
      {activeTab === 'overview' && summary && (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm">Average Score</CardTitle></CardHeader>
            <CardContent>
              <div className="text-3xl font-bold">{summary.average_score}/100</div>
              <p className="text-xs text-gray-500 mt-1">{summary.contacts_sampled} contacts scored</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm">Segment Distribution</CardTitle></CardHeader>
            <CardContent>
              <div className="h-40">
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie data={segmentPieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={60}>
                      {segmentPieData.map((entry: any, i: number) => (
                        <Cell key={i} fill={SEGMENT_COLORS[entry.name] || '#999'} />
                      ))}
                    </Pie>
                    <Tooltip />
                  </PieChart>
                </ResponsiveContainer>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2"><CardTitle className="text-sm">Actionable Insights</CardTitle></CardHeader>
            <CardContent>
              <ul className="space-y-2 text-sm">
                {(summary.actionable_insights || []).map((insight: string, i: number) => (
                  <li key={i} className="flex items-start gap-2">
                    <Zap className="h-4 w-4 text-yellow-500 mt-0.5 flex-shrink-0" />
                    {insight}
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>

          {winData && (
            <Card className="md:col-span-3">
              <CardHeader><CardTitle className="text-sm">Election Outlook</CardTitle></CardHeader>
              <CardContent>
                <div className="grid grid-cols-3 gap-4 text-center">
                  <div>
                    <div className="text-2xl font-bold text-green-600">{winData.winning_states}</div>
                    <div className="text-xs text-gray-500">Winning</div>
                  </div>
                  <div>
                    <div className="text-2xl font-bold text-yellow-600">{winData.competitive_states}</div>
                    <div className="text-xs text-gray-500">Competitive</div>
                  </div>
                  <div>
                    <div className="text-2xl font-bold text-red-600">{winData.losing_states}</div>
                    <div className="text-xs text-gray-500">Losing</div>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      )}

      {/* Voter Scores Tab */}
      {activeTab === 'voters' && (
        <Card>
          <CardHeader><CardTitle>Individual Voter Scores (Top 50)</CardTitle></CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left">
                    <th className="p-2">Contact</th>
                    <th className="p-2">Score</th>
                    <th className="p-2">Segment</th>
                    <th className="p-2">Engagement</th>
                    <th className="p-2">Recency</th>
                    <th className="p-2">Response</th>
                    <th className="p-2">Channel</th>
                    <th className="p-2">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {voters.slice(0, 50).map((v) => (
                    <tr key={v.contact_id} className="border-b hover:bg-gray-50">
                      <td className="p-2 font-mono text-xs">{v.contact_id.slice(0, 12)}...</td>
                      <td className="p-2 font-bold">{v.overall_score}</td>
                      <td className="p-2">
                        <Badge style={{ backgroundColor: SEGMENT_COLORS[v.segment], color: '#fff' }}>
                          {v.segment}
                        </Badge>
                      </td>
                      <td className="p-2">{v.engagement_score}</td>
                      <td className="p-2">{v.recency_score}</td>
                      <td className="p-2">{v.responsiveness_score}</td>
                      <td className="p-2">{v.recommended_channel}</td>
                      <td className="p-2 text-xs text-gray-600 max-w-[200px] truncate">{v.recommended_action}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Win Probability Tab */}
      {activeTab === 'win' && winData && (
        <div className="space-y-4">
          <Card>
            <CardHeader><CardTitle>Win Probability by State</CardTitle></CardHeader>
            <CardContent>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={(winData.states || []).slice(0, 15)}>
                    <XAxis dataKey="state_code" />
                    <YAxis domain={[0, 1]} tickFormatter={(v: number) => `${(v * 100).toFixed(0)}%`} />
                    <Tooltip formatter={(v: number) => `${(v * 100).toFixed(1)}%`} />
                    <Bar dataKey="win_probability" fill="#8b5cf6" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </CardContent>
          </Card>

          <div className="grid gap-3">
            {(winData.states || []).map((s: WinState) => (
              <Card key={s.state_code}>
                <CardContent className="p-4 flex items-center justify-between">
                  <div>
                    <span className="font-bold text-lg">{s.state_code}</span>
                    <Badge className={`ml-2 ${SCENARIO_COLORS[s.scenario]}`}>{s.scenario}</Badge>
                  </div>
                  <div className="text-right">
                    <div className="text-2xl font-bold">{(s.win_probability * 100).toFixed(1)}%</div>
                    <div className="text-xs text-gray-500">
                      Margin: {s.margin > 0 ? '+' : ''}{s.margin} | Share: {(s.vote_share * 100).toFixed(1)}%
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </div>
      )}

      {/* Resource Allocation Tab */}
      {activeTab === 'allocation' && allocations && (
        <div className="space-y-4">
          <Card>
            <CardHeader><CardTitle>Optimal Volunteer Deployment</CardTitle></CardHeader>
            <CardContent>
              <div className="grid grid-cols-3 gap-4 mb-4 text-center">
                <div>
                  <div className="text-2xl font-bold text-blue-600">{allocations.allocated}</div>
                  <div className="text-xs text-gray-500">Volunteers Allocated</div>
                </div>
                <div>
                  <div className="text-2xl font-bold text-green-600">{allocations.total_expected_pledge_gain}</div>
                  <div className="text-xs text-gray-500">Expected Pledge Gain</div>
                </div>
                <div>
                  <div className="text-2xl font-bold text-gray-600">{allocations.unallocated}</div>
                  <div className="text-xs text-gray-500">Unallocated</div>
                </div>
              </div>

              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left">
                    <th className="p-2">Ward</th>
                    <th className="p-2">State</th>
                    <th className="p-2">Priority</th>
                    <th className="p-2">Current</th>
                    <th className="p-2">+ Deploy</th>
                    <th className="p-2">Expected Gain</th>
                    <th className="p-2">Reasoning</th>
                  </tr>
                </thead>
                <tbody>
                  {(allocations.allocations || []).map((a: Allocation, i: number) => (
                    <tr key={i} className="border-b">
                      <td className="p-2 font-mono text-xs">{a.ward_code}</td>
                      <td className="p-2">{a.state_code}</td>
                      <td className="p-2"><Badge className={PRIORITY_COLORS[a.priority]}>{a.priority}</Badge></td>
                      <td className="p-2">{a.current_volunteers}</td>
                      <td className="p-2 font-bold text-blue-600">+{a.recommended_additional}</td>
                      <td className="p-2 font-bold text-green-600">+{a.expected_pledge_gain}</td>
                      <td className="p-2 text-xs text-gray-600">{a.reasoning}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Message Optimizer Tab */}
      {activeTab === 'bandit' && banditData && (
        <div className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Brain className="h-5 w-5" />
                Multi-Armed Bandit — Message Optimization
              </CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-gray-600 mb-4">
                Algorithm: UCB1 + Thompson Sampling. Balances exploration (try under-tested variants)
                vs exploitation (use best performer). Total impressions: {banditData.total_impressions}
              </p>

              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left">
                    <th className="p-2">Variant</th>
                    <th className="p-2">Impressions</th>
                    <th className="p-2">Conversions</th>
                    <th className="p-2">Rate</th>
                    <th className="p-2">UCB Score</th>
                    <th className="p-2">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {(banditData.arms || []).map((arm: MessageArm) => (
                    <tr key={arm.variant_id} className="border-b">
                      <td className="p-2 text-xs max-w-[250px] truncate">{arm.variant_text}</td>
                      <td className="p-2">{arm.impressions}</td>
                      <td className="p-2">{arm.conversions}</td>
                      <td className="p-2 font-bold">{(arm.conversion_rate * 100).toFixed(1)}%</td>
                      <td className="p-2">{arm.ucb_score.toFixed(3)}</td>
                      <td className="p-2">
                        <Badge className={
                          arm.status === 'exploiting' ? 'bg-green-100 text-green-800' :
                          arm.status === 'retired' ? 'bg-red-100 text-red-800' :
                          'bg-blue-100 text-blue-800'
                        }>
                          {arm.status}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
