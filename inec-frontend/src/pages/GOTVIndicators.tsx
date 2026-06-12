import { useEffect, useState, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  LineChart, Line, PieChart, Pie, Cell, RadarChart, Radar, PolarGrid,
  PolarAngleAxis, PolarRadiusAxis, Legend, Area, AreaChart,
} from 'recharts';
import {
  TrendingUp, TrendingDown, Target, Users, MapPin, MessageSquare,
  Award, FileText, BarChart3, Activity, AlertTriangle, RefreshCw,
} from 'lucide-react';

const BASE_URL = `${window.location.protocol}//${window.location.hostname}:8103`;
const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16'];

function getAuthHeaders() {
  const token = localStorage.getItem('auth_token') || '';
  const partyId = localStorage.getItem('gotv_party_id') || '1';
  return {
    'Authorization': `Bearer ${token}`,
    'X-GOTV-Party-ID': partyId,
    'X-GOTV-Party-Code': localStorage.getItem('gotv_party_code') || 'APC',
    'Content-Type': 'application/json',
  };
}

async function apiFetch(path: string, opts?: RequestInit) {
  const res = await fetch(`${BASE_URL}${path}`, { ...opts, headers: { ...getAuthHeaders(), ...opts?.headers } });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

// ─── Sub-tab types ──────────────────────────────────────────────────────────
type SubTab = 'cpi' | 'demographics' | 'surveys' | 'lga' | 'sentiment' | 'endorsements' | 'reports' | 'platform';

// ─── CPI Dashboard ──────────────────────────────────────────────────────────
function CPIDashboard() {
  const [cpi, setCPI] = useState<any>(null);
  const [history, setHistory] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [cpiRes, histRes] = await Promise.all([
        apiFetch('/gotv/koh/cpi/compute'),
        apiFetch('/gotv/koh/cpi/history?months=12'),
      ]);
      setCPI(cpiRes);
      setHistory(histRes.history || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading CPI...</div>;

  const cpiColor = (cpi?.cpi_score || 0) >= 70 ? 'text-green-600' : (cpi?.cpi_score || 0) >= 60 ? 'text-yellow-600' : 'text-red-600';
  const radarData = [
    { subject: 'Voting Intention', value: cpi?.voting_intention_pct || 0, fullMark: 100 },
    { subject: 'Favourability', value: cpi?.favourability_pct || 0, fullMark: 100 },
    { subject: 'Sentiment', value: cpi?.digital_sentiment || 0, fullMark: 100 },
    { subject: 'Ground Mob.', value: cpi?.ground_mobilisation || 0, fullMark: 100 },
    { subject: 'Endorsements', value: cpi?.endorsement_index || 0, fullMark: 100 },
    { subject: 'Share of Voice', value: cpi?.share_of_voice || 0, fullMark: 100 },
  ];

  return (
    <div className="space-y-6">
      {/* CPI Score Hero */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card className="col-span-1">
          <CardHeader><CardTitle>Composite Popularity Index</CardTitle></CardHeader>
          <CardContent className="text-center">
            <div className={`text-6xl font-bold ${cpiColor}`}>{cpi?.cpi_score?.toFixed(1) || '—'}</div>
            <div className="text-sm text-gray-500 mt-2">{cpi?.interpretation}</div>
            <div className="mt-4">
              <Progress value={cpi?.cpi_score || 0} className="h-3" />
            </div>
            <div className="flex justify-between text-xs text-gray-400 mt-1">
              <span>0</span><span>60 (threshold)</span><span>100</span>
            </div>
          </CardContent>
        </Card>

        <Card className="col-span-2">
          <CardHeader><CardTitle>CPI Components (Radar)</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <RadarChart data={radarData}>
                <PolarGrid />
                <PolarAngleAxis dataKey="subject" tick={{ fontSize: 11 }} />
                <PolarRadiusAxis angle={30} domain={[0, 100]} />
                <Radar name="Score" dataKey="value" stroke="#3b82f6" fill="#3b82f6" fillOpacity={0.3} />
              </RadarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Component Weights */}
      <Card>
        <CardHeader><CardTitle>CPI Breakdown by Weight</CardTitle></CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
            {[
              { label: 'Voting Intention', weight: '30%', value: cpi?.voting_intention_pct },
              { label: 'Favourability', weight: '25%', value: cpi?.favourability_pct },
              { label: 'Digital Sentiment', weight: '15%', value: cpi?.digital_sentiment },
              { label: 'Ground Mobilisation', weight: '15%', value: cpi?.ground_mobilisation },
              { label: 'Endorsement Index', weight: '10%', value: cpi?.endorsement_index },
              { label: 'Share of Voice', weight: '5%', value: cpi?.share_of_voice },
            ].map((c, i) => (
              <div key={i} className="bg-gray-50 rounded-lg p-3 text-center">
                <div className="text-xs text-gray-500">{c.label}</div>
                <div className="text-2xl font-bold mt-1">{c.value?.toFixed(1) || '0'}</div>
                <Badge variant="outline" className="mt-1">{c.weight}</Badge>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* CPI History */}
      {history.length > 0 && (
        <Card>
          <CardHeader><CardTitle>CPI Trend (Monthly)</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <AreaChart data={history}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="computed_at" tickFormatter={(v) => new Date(v).toLocaleDateString()} />
                <YAxis domain={[0, 100]} />
                <Tooltip />
                <Area type="monotone" dataKey="cpi_score" stroke="#3b82f6" fill="#3b82f6" fillOpacity={0.2} />
              </AreaChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// ─── Demographics Dashboard ─────────────────────────────────────────────────
function DemographicsDashboard() {
  const [dimension, setDimension] = useState('age_group');
  const [data, setData] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiFetch(`/gotv/koh/demographics?dimension=${dimension}`);
      setData(res.breakdown || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, [dimension]);

  useEffect(() => { loadData(); }, [loadData]);

  const dimensions = [
    { value: 'age_group', label: 'Age Group' },
    { value: 'gender', label: 'Gender' },
    { value: 'lga_code', label: 'LGA' },
    { value: 'socioeconomic_class', label: 'Socioeconomic Class' },
    { value: 'occupation_group', label: 'Occupation' },
    { value: 'education_level', label: 'Education' },
    { value: 'religion', label: 'Religion' },
    { value: 'ethnicity', label: 'Ethnicity' },
  ];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-2">
        {dimensions.map(d => (
          <Button key={d.value} variant={dimension === d.value ? 'default' : 'outline'} size="sm"
            onClick={() => setDimension(d.value)}>{d.label}</Button>
        ))}
      </div>

      {loading ? <div className="p-4 text-center">Loading...</div> : (
        <>
          <Card>
            <CardHeader><CardTitle>Contact Distribution by {dimensions.find(d => d.value === dimension)?.label}</CardTitle></CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={300}>
                <BarChart data={data}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="value" />
                  <YAxis />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="contact_count" fill="#3b82f6" name="Total Contacts" />
                  <Bar dataKey="pledged" fill="#10b981" name="Pledged" />
                  <Bar dataKey="confirmed" fill="#8b5cf6" name="Confirmed" />
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {data.map((item, i) => (
              <Card key={i}>
                <CardContent className="p-4">
                  <div className="font-medium">{item.value || 'Unknown'}</div>
                  <div className="text-sm text-gray-500">{item.contact_count} contacts</div>
                  <div className="flex items-center gap-2 mt-2">
                    <Progress value={item.pledge_rate} className="h-2 flex-1" />
                    <span className="text-sm font-medium">{item.pledge_rate?.toFixed(1)}%</span>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// ─── Survey Dashboard ───────────────────────────────────────────────────────
function SurveyDashboard() {
  const [surveys, setSurveys] = useState<any[]>([]);
  const [trend, setTrend] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [surveyRes, trendRes] = await Promise.all([
        apiFetch('/gotv/koh/surveys'),
        apiFetch('/gotv/koh/surveys/trend?indicator=voting_intention'),
      ]);
      setSurveys(surveyRes.surveys || []);
      setTrend(trendRes.trend || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading surveys...</div>;

  return (
    <div className="space-y-4">
      {/* Survey Waves */}
      <Card>
        <CardHeader><CardTitle>Survey Waves</CardTitle></CardHeader>
        <CardContent>
          {surveys.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No surveys yet. Create a survey to start tracking indicators.</div>
          ) : (
            <div className="space-y-2">
              {surveys.map((s: any) => (
                <div key={s.id} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                  <div>
                    <div className="font-medium">{s.name}</div>
                    <div className="text-sm text-gray-500">Wave {s.wave_number} • n={s.sample_size} • {s.methodology}</div>
                  </div>
                  <Badge>{s.status}</Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Voting Intention Trend */}
      {trend.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Voting Intention Trend Across Waves</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <LineChart data={trend}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="wave" label={{ value: 'Wave', position: 'bottom' }} />
                <YAxis domain={[0, 100]} label={{ value: '%', angle: -90, position: 'left' }} />
                <Tooltip />
                <Line type="monotone" dataKey="value" stroke="#3b82f6" strokeWidth={2} dot={{ r: 5 }} />
              </LineChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// ─── LGA Strategic Dashboard ────────────────────────────────────────────────
function LGADashboard() {
  const [tiers, setTiers] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiFetch('/gotv/koh/lga/dashboard');
      setTiers(res.tiers || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading LGA data...</div>;

  const tierColors: Record<number, string> = { 1: 'border-green-500', 2: 'border-yellow-500', 3: 'border-orange-500', 4: 'border-blue-500' };
  const tierBgs: Record<number, string> = { 1: 'bg-green-50', 2: 'bg-yellow-50', 3: 'bg-orange-50', 4: 'bg-blue-50' };

  return (
    <div className="space-y-4">
      {tiers.map((tier: any) => (
        <Card key={tier.tier} className={`border-l-4 ${tierColors[tier.tier]}`}>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Badge className={tierBgs[tier.tier]}>Tier {tier.tier}</Badge>
              {tier.tier_name}
              <span className="text-sm font-normal text-gray-500">— {tier.strategic_focus}</span>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
              <div className="text-center">
                <div className="text-2xl font-bold">{tier.total_contacts}</div>
                <div className="text-xs text-gray-500">Contacts</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">{tier.total_pledges}</div>
                <div className="text-xs text-gray-500">Pledges</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">{tier.lgas?.length || 0}</div>
                <div className="text-xs text-gray-500">LGAs</div>
              </div>
              <div className="text-center">
                <Badge variant={tier.kpi?.status === 'on_track' ? 'default' : 'secondary'}>
                  {tier.kpi?.target || '—'}
                </Badge>
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2">
              {tier.lgas?.map((lga: any) => (
                <div key={lga.lga_code} className="p-2 bg-gray-50 rounded flex justify-between items-center">
                  <div>
                    <div className="text-sm font-medium">{lga.lga_name}</div>
                    <div className="text-xs text-gray-500">{lga.contacts} contacts</div>
                  </div>
                  <Badge variant="outline">{lga.pledge_rate?.toFixed(1)}%</Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

// ─── Sentiment Dashboard ────────────────────────────────────────────────────
function SentimentDashboard() {
  const [sentiment, setSentiment] = useState<any>(null);
  const [sov, setSOV] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [sentRes, sovRes] = await Promise.all([
        apiFetch('/gotv/koh/social/sentiment?days=30'),
        apiFetch('/gotv/koh/social/share-of-voice'),
      ]);
      setSentiment(sentRes);
      setSOV(sovRes);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading sentiment...</div>;

  const sentimentColor = (sentiment?.sentiment_score || 50) >= 60 ? 'text-green-600' :
    (sentiment?.sentiment_score || 50) >= 40 ? 'text-yellow-600' : 'text-red-600';

  const pieData = [
    { name: 'Positive', value: sentiment?.positive || 0 },
    { name: 'Negative', value: sentiment?.negative || 0 },
    { name: 'Neutral', value: sentiment?.neutral || 0 },
  ];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Sentiment Score</div>
            <div className={`text-4xl font-bold mt-2 ${sentimentColor}`}>
              {sentiment?.sentiment_score?.toFixed(1) || '50.0'}%
            </div>
            <div className="text-xs text-gray-400 mt-1">positive mentions ratio</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Share of Voice</div>
            <div className="text-4xl font-bold mt-2 text-blue-600">
              {sov?.overall_share_of_voice?.toFixed(1) || '0'}%
            </div>
            <div className="text-xs text-gray-400 mt-1">of political conversation</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Total Mentions (30d)</div>
            <div className="text-4xl font-bold mt-2">{sentiment?.total_mentions || 0}</div>
            <div className="text-xs text-gray-400 mt-1">across all platforms</div>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader><CardTitle>Sentiment Breakdown</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={200}>
              <PieChart>
                <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={80} label>
                  {pieData.map((_, i) => <Cell key={i} fill={['#10b981', '#ef4444', '#9ca3af'][i]} />)}
                </Pie>
                <Tooltip />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {sentiment?.trend?.length > 0 && (
          <Card>
            <CardHeader><CardTitle>Sentiment Trend (Daily)</CardTitle></CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={200}>
                <AreaChart data={sentiment.trend}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="date" tickFormatter={(v) => v.slice(5)} />
                  <YAxis domain={[0, 100]} />
                  <Tooltip />
                  <Area type="monotone" dataKey="positive_pct" stroke="#10b981" fill="#10b981" fillOpacity={0.3} />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}

// ─── Endorsements Dashboard ─────────────────────────────────────────────────
function EndorsementsDashboard() {
  const [endorsements, setEndorsements] = useState<any[]>([]);
  const [score, setScore] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [listRes, scoreRes] = await Promise.all([
        apiFetch('/gotv/koh/endorsements'),
        apiFetch('/gotv/koh/endorsements/score'),
      ]);
      setEndorsements(listRes.endorsements || []);
      setScore(scoreRes);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading endorsements...</div>;

  return (
    <div className="space-y-4">
      {/* Coalition Index */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Coalition Index</div>
            <div className="text-4xl font-bold mt-2 text-purple-600">{score?.coalition_index?.toFixed(1) || '0'}</div>
            <Progress value={score?.coalition_index || 0} className="mt-2" />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Verified Endorsements</div>
            <div className="text-4xl font-bold mt-2">{score?.total_verified || 0}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-6 text-center">
            <div className="text-sm text-gray-500">Endorser Types Covered</div>
            <div className="text-4xl font-bold mt-2">{score?.distinct_types || 0} / {score?.max_types || 10}</div>
          </CardContent>
        </Card>
      </div>

      {/* By Type */}
      {score?.by_type?.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Endorsements by Category</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={score.by_type} layout="vertical">
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis type="number" />
                <YAxis dataKey="type" type="category" width={140} tick={{ fontSize: 11 }} />
                <Tooltip />
                <Bar dataKey="count" fill="#8b5cf6" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      {/* Endorsement List */}
      <Card>
        <CardHeader><CardTitle>Recent Endorsements ({endorsements.length})</CardTitle></CardHeader>
        <CardContent>
          {endorsements.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No endorsements logged yet.</div>
          ) : (
            <div className="space-y-2 max-h-96 overflow-y-auto">
              {endorsements.map((e: any) => (
                <div key={e.id} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                  <div>
                    <div className="font-medium flex items-center gap-2">
                      {e.endorser_name}
                      {e.verified && <Badge className="bg-green-100 text-green-800 text-xs">Verified</Badge>}
                    </div>
                    <div className="text-sm text-gray-500">
                      {e.endorser_type?.replace(/_/g, ' ')} • {e.lga_code || 'All LGAs'} • {e.date_endorsed}
                    </div>
                  </div>
                  <Badge variant="outline">{e.endorser_category || e.endorser_type}</Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ─── Reports Dashboard ──────────────────────────────────────────────────────
function ReportsDashboard() {
  const [reports, setReports] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiFetch('/gotv/koh/reports');
      setReports(res.reports || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const generateReport = async (type: string) => {
    setGenerating(true);
    try {
      await apiFetch(`/gotv/koh/reports/generate/${type}`, { method: 'POST' });
      await loadData();
    } catch (e) { console.error(e); }
    setGenerating(false);
  };

  const reportTypes = [
    { type: 'digital_performance', label: 'Digital Performance', freq: 'Weekly', icon: BarChart3 },
    { type: 'demographic_sentiment', label: 'Demographic Sentiment', freq: 'Monthly', icon: Users },
    { type: 'full_indicators', label: 'Full Indicators', freq: 'Monthly', icon: FileText },
    { type: 'cpi_brief', label: 'CPI Brief', freq: 'Monthly', icon: Target },
    { type: 'crisis_alert', label: 'Crisis Alert', freq: 'Real-time', icon: AlertTriangle },
  ];

  return (
    <div className="space-y-4">
      {/* Generate Report Buttons */}
      <Card>
        <CardHeader><CardTitle>Generate Reports</CardTitle></CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-3">
            {reportTypes.map(rt => (
              <Button key={rt.type} variant="outline" className="flex flex-col items-center gap-2 h-auto py-4"
                onClick={() => generateReport(rt.type)} disabled={generating}>
                <rt.icon className="w-5 h-5" />
                <span className="text-xs">{rt.label}</span>
                <Badge variant="secondary" className="text-xs">{rt.freq}</Badge>
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* Report History */}
      <Card>
        <CardHeader><CardTitle>Report History</CardTitle></CardHeader>
        <CardContent>
          {reports.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No reports generated yet.</div>
          ) : (
            <div className="space-y-2">
              {reports.map((r: any) => (
                <div key={r.id} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                  <div>
                    <div className="font-medium">{r.report_type?.replace(/_/g, ' ')}</div>
                    <div className="text-sm text-gray-500">{new Date(r.generated_at).toLocaleString()}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant="outline">{r.frequency}</Badge>
                    <Badge>{r.status}</Badge>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ─── Platform Analytics Dashboard ───────────────────────────────────────────
function PlatformDashboard() {
  const [platforms, setPlatforms] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await apiFetch('/gotv/koh/analytics/summary?days=7');
      setPlatforms(res.platforms || []);
    } catch (e) { console.error(e); }
    setLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  if (loading) return <div className="p-4 text-center">Loading analytics...</div>;

  return (
    <div className="space-y-4">
      {platforms.length === 0 ? (
        <Card>
          <CardContent className="p-8 text-center text-gray-500">
            No platform analytics data yet. Use the ingestion API to push data from Meta Business Suite, TikTok Analytics, X Analytics, or YouTube Studio.
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            {platforms.map((p: any) => (
              <Card key={p.platform}>
                <CardContent className="p-4">
                  <div className="font-medium capitalize">{p.platform}</div>
                  <div className="grid grid-cols-2 gap-2 mt-3 text-sm">
                    <div><span className="text-gray-500">Followers:</span> {p.followers?.toLocaleString()}</div>
                    <div><span className="text-gray-500">Reach:</span> {p.total_reach?.toLocaleString()}</div>
                    <div><span className="text-gray-500">Engagement:</span> {(p.engagement_rate * 100)?.toFixed(2)}%</div>
                    <div><span className="text-gray-500">Growth:</span> {p.follower_growth_pct?.toFixed(1)}%</div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          <Card>
            <CardHeader><CardTitle>Reach by Platform (7 days)</CardTitle></CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={250}>
                <BarChart data={platforms}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="platform" />
                  <YAxis />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="organic_reach" fill="#10b981" name="Organic" stackId="a" />
                  <Bar dataKey="paid_reach" fill="#3b82f6" name="Paid" stackId="a" />
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────
export default function GOTVIndicators() {
  const [activeTab, setActiveTab] = useState<SubTab>('cpi');

  const tabs: { key: SubTab; label: string; icon: any }[] = [
    { key: 'cpi', label: 'CPI', icon: Target },
    { key: 'demographics', label: 'Demographics', icon: Users },
    { key: 'surveys', label: 'Surveys', icon: FileText },
    { key: 'lga', label: 'LGA Strategy', icon: MapPin },
    { key: 'sentiment', label: 'Sentiment', icon: MessageSquare },
    { key: 'endorsements', label: 'Endorsements', icon: Award },
    { key: 'reports', label: 'Reports', icon: BarChart3 },
    { key: 'platform', label: 'Platform Analytics', icon: Activity },
  ];

  return (
    <div className="space-y-4">
      {/* Sub-tab Navigation */}
      <div className="flex flex-wrap gap-1 border-b pb-2">
        {tabs.map(tab => (
          <Button key={tab.key} variant={activeTab === tab.key ? 'default' : 'ghost'} size="sm"
            onClick={() => setActiveTab(tab.key)} className="gap-1">
            <tab.icon className="w-4 h-4" />
            {tab.label}
          </Button>
        ))}
      </div>

      {/* Tab Content */}
      {activeTab === 'cpi' && <CPIDashboard />}
      {activeTab === 'demographics' && <DemographicsDashboard />}
      {activeTab === 'surveys' && <SurveyDashboard />}
      {activeTab === 'lga' && <LGADashboard />}
      {activeTab === 'sentiment' && <SentimentDashboard />}
      {activeTab === 'endorsements' && <EndorsementsDashboard />}
      {activeTab === 'reports' && <ReportsDashboard />}
      {activeTab === 'platform' && <PlatformDashboard />}
    </div>
  );
}
