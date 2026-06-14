/**
 * GOTVPlatform.tsx — New platform features (47 recommendations implementation)
 *
 * Sub-tabs: AI Alerts, Route Optimizer, Teams, Simulation, Ask GOTV,
 *           Experiments, Export, Crowd, Social Inbox, Federated Learning
 */
import { useEffect, useState, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';

import {
  AlertTriangle, Route, Trophy, Brain, MessageCircle,
  FlaskConical, Download, Camera, Inbox, Shield,
  ChevronRight,
} from 'lucide-react';

const API_BASE = '/gotv';


type SubTab = 'alerts' | 'route' | 'teams' | 'simulation' | 'ask' | 'experiments' | 'export' | 'crowd' | 'social' | 'federated';

interface Alert {
  alert_id: string;
  severity: string;
  category: string;
  message: string;
  action: string;
  created_at: string;
}

interface Team {
  name: string;
  members: number;
  total_doors: number;
  total_calls: number;
  total_rides: number;
  points: number;
  rank: number;
}

interface Variant {
  variant_id: string;
  text: string;
  language: string;
  impressions: number;
  conversions: number;
  conversion_rate_pct: number;
  is_retired: boolean;
}

export default function GOTVPlatform() {
  const [sub, setSub] = useState<SubTab>('alerts');
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [variants, setVariants] = useState<Variant[]>([]);
  const [nlQuery, setNlQuery] = useState('');
  const [nlAnswer, setNlAnswer] = useState<string | null>(null);
  const [simScenario, setSimScenario] = useState('add_canvassers');
  const [simCount, setSimCount] = useState(10);
  const [simResult, setSimResult] = useState<any>(null);
  const [loading, setLoading] = useState(false);

  const headers = { 'X-GOTV-Party-Code': 'APC' };

  const fetchAlerts = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/warroom/ai-alerts`, { headers });
      const data = await res.json();
      setAlerts(data.alerts || []);
    } catch { /* ignore */ }
  }, []);

  const fetchTeams = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/teams/leaderboard?group_by=ward`, { headers });
      const data = await res.json();
      setTeams(data.teams || []);
    } catch { /* ignore */ }
  }, []);

  const fetchExperiments = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/experiments`, { headers });
      const data = await res.json();
      setVariants(data.variants || []);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    fetchAlerts();
    fetchTeams();
    fetchExperiments();
  }, []);

  const askGOTV = async () => {
    if (!nlQuery.trim()) return;
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/nl/query`, {
        method: 'POST', headers: { ...headers, 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: nlQuery }),
      });
      const data = await res.json();
      setNlAnswer(data.answer);
    } catch { setNlAnswer('Error processing query'); }
    setLoading(false);
  };

  const runSimulation = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/simulation`, {
        method: 'POST', headers: { ...headers, 'Content-Type': 'application/json' },
        body: JSON.stringify({ scenario: simScenario, additional_count: simCount }),
      });
      setSimResult(await res.json());
    } catch { /* ignore */ }
    setLoading(false);
  };

  const exportData = async (entity: string) => {
    window.open(`${API_BASE}/export/${entity}?format=csv`, '_blank');
  };

  const severityColor: Record<string, string> = {
    critical: 'bg-red-500 text-white',
    warning: 'bg-yellow-500 text-white',
    info: 'bg-blue-500 text-white',
  };

  // ─── Sub-tab renderers ──────────────────────────────────────

  const renderAlerts = () => (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold">🤖 War Room AI Alerts</h3>
        <Button size="sm" onClick={fetchAlerts}>Refresh</Button>
      </div>
      {alerts.length === 0 ? (
        <Card><CardContent className="py-8 text-center text-muted-foreground">No active alerts — operations normal</CardContent></Card>
      ) : alerts.map(a => (
        <Card key={a.alert_id} className="border-l-4" style={{ borderLeftColor: a.severity === 'critical' ? '#ef4444' : a.severity === 'warning' ? '#eab308' : '#3b82f6' }}>
          <CardContent className="py-4">
            <div className="flex items-start justify-between">
              <div>
                <div className="flex items-center gap-2">
                  <Badge className={severityColor[a.severity] || 'bg-gray-500'}>{a.severity.toUpperCase()}</Badge>
                  <Badge variant="outline">{a.category}</Badge>
                </div>
                <p className="mt-2 font-medium">{a.message}</p>
                <p className="mt-1 text-sm text-muted-foreground">Recommended: {a.action}</p>
              </div>
              <Button size="sm" variant="outline"><ChevronRight className="h-4 w-4" /> Act</Button>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );

  const renderTeams = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">🏆 Team Leaderboard</h3>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {teams.slice(0, 3).map((t, i) => (
          <Card key={t.name} className={i === 0 ? 'border-yellow-400 border-2' : ''}>
            <CardHeader className="pb-2">
              <CardTitle className="text-lg">{i === 0 ? '🥇' : i === 1 ? '🥈' : '🥉'} {t.name}</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-bold">{t.points.toLocaleString()} pts</div>
              <div className="text-sm text-muted-foreground mt-1">{t.members} members</div>
              <div className="grid grid-cols-3 gap-2 mt-3 text-sm">
                <div>🚪 {t.total_doors}</div>
                <div>📞 {t.total_calls}</div>
                <div>🚗 {t.total_rides}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      <table className="w-full text-sm">
        <thead><tr className="border-b"><th className="text-left py-2">Rank</th><th className="text-left">Team</th><th className="text-right">Members</th><th className="text-right">Doors</th><th className="text-right">Calls</th><th className="text-right">Rides</th><th className="text-right">Points</th></tr></thead>
        <tbody>
          {teams.map(t => (
            <tr key={t.name} className="border-b"><td className="py-2">{t.rank}</td><td>{t.name}</td><td className="text-right">{t.members}</td><td className="text-right">{t.total_doors}</td><td className="text-right">{t.total_calls}</td><td className="text-right">{t.total_rides}</td><td className="text-right font-bold">{t.points.toLocaleString()}</td></tr>
          ))}
        </tbody>
      </table>
    </div>
  );

  const renderSimulation = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">🔮 Digital Twin Simulation</h3>
      <Card>
        <CardContent className="py-4 space-y-4">
          <div className="flex gap-4">
            <select className="border rounded px-3 py-2" value={simScenario} onChange={e => setSimScenario(e.target.value)}>
              <option value="add_canvassers">Add Canvassers</option>
              <option value="add_drivers">Add Drivers</option>
              <option value="increase_budget">Increase Budget</option>
            </select>
            <Input type="number" value={simCount} onChange={e => setSimCount(Number(e.target.value))} className="w-24" placeholder="Count" />
            <Button onClick={runSimulation} disabled={loading}>Run Simulation</Button>
          </div>
          {simResult && (
            <div className="bg-muted rounded-lg p-4 space-y-2">
              <div className="text-lg font-bold">{simResult.impact_summary}</div>
              <div className="text-sm text-muted-foreground">Cost: {simResult.cost_estimate}</div>
              <div className="text-sm">Confidence: <Badge variant="outline">{simResult.confidence_level}</Badge></div>
              {simResult.projected_state && (
                <pre className="text-xs bg-background rounded p-2 mt-2">{JSON.stringify(simResult.projected_state, null, 2)}</pre>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );

  const renderAskGOTV = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">💬 Ask GOTV (Natural Language)</h3>
      <Card>
        <CardContent className="py-4 space-y-4">
          <div className="flex gap-2">
            <Input value={nlQuery} onChange={e => setNlQuery(e.target.value)} placeholder="e.g., How many pledges in Lagos this week?" className="flex-1" onKeyDown={e => e.key === 'Enter' && askGOTV()} />
            <Button onClick={askGOTV} disabled={loading}>Ask</Button>
          </div>
          {nlAnswer && (
            <div className="bg-muted rounded-lg p-4">
              <div className="text-3xl font-bold">{nlAnswer}</div>
            </div>
          )}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
            {['How many contacts?', 'Total pledges?', 'Pending rides?', 'Active campaigns?', 'CPI score?', 'Approved volunteers?', 'Pledges in Lagos?', 'Contacts in Kano?'].map(q => (
              <Button key={q} size="sm" variant="outline" onClick={() => { setNlQuery(q); }}>
                {q}
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );

  const renderExperiments = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">🔬 A/B Experiments</h3>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {variants.slice(0, 6).map(v => (
          <Card key={v.variant_id}>
            <CardContent className="py-4">
              <div className="flex items-center justify-between">
                <Badge variant={v.is_retired ? 'destructive' : 'default'}>{v.is_retired ? 'Retired' : 'Active'}</Badge>
                <Badge variant="outline">{v.language}</Badge>
              </div>
              <p className="mt-2 text-sm">{v.text.substring(0, 100)}...</p>
              <div className="mt-3 grid grid-cols-3 gap-2 text-center">
                <div><div className="text-lg font-bold">{v.impressions}</div><div className="text-xs text-muted-foreground">Impressions</div></div>
                <div><div className="text-lg font-bold">{v.conversions}</div><div className="text-xs text-muted-foreground">Conversions</div></div>
                <div><div className="text-lg font-bold">{v.conversion_rate_pct}%</div><div className="text-xs text-muted-foreground">CVR</div></div>
              </div>
              <Progress value={v.conversion_rate_pct * 5} className="mt-2" />
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );

  const renderExport = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">📥 Data Export</h3>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {[
          { entity: 'contacts', label: 'Contacts', desc: 'All voter contacts with status and demographics' },
          { entity: 'volunteers', label: 'Volunteers', desc: 'Volunteer roster with vetting status and performance' },
          { entity: 'tasks', label: 'Tasks', desc: 'Task assignments with completion status' },
        ].map(e => (
          <Card key={e.entity}>
            <CardContent className="py-4 flex flex-col items-center text-center">
              <Download className="h-8 w-8 mb-2 text-muted-foreground" />
              <h4 className="font-semibold">{e.label}</h4>
              <p className="text-sm text-muted-foreground mt-1">{e.desc}</p>
              <div className="flex gap-2 mt-3">
                <Button size="sm" onClick={() => exportData(e.entity)}>CSV</Button>
                <Button size="sm" variant="outline" onClick={() => window.open(`${API_BASE}/export/${e.entity}?format=json`, '_blank')}>JSON</Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );

  const renderRoute = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">🗺️ Route Optimizer (TSP)</h3>
      <Card>
        <CardContent className="py-4">
          <p className="text-muted-foreground">AI-optimized walking routes for canvassers using nearest-neighbor + 2-opt improvement.</p>
          <div className="grid grid-cols-3 gap-4 mt-4">
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">~35%</div><div className="text-xs">Route Improvement</div></div>
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">30</div><div className="text-xs">Max Stops/Route</div></div>
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">NN+2opt</div><div className="text-xs">Algorithm</div></div>
          </div>
          <p className="mt-4 text-sm">Canvassers receive optimized routes via mobile app. Routes consider:</p>
          <ul className="text-sm mt-2 space-y-1 list-disc pl-4">
            <li>Contacts not yet door-knocked in assigned ward</li>
            <li>Distance minimization (haversine-based)</li>
            <li>Time windows (morning/afternoon slots)</li>
            <li>Priority scoring (hot leads first)</li>
          </ul>
        </CardContent>
      </Card>
    </div>
  );

  const renderCrowd = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">📸 Crowd Estimation</h3>
      <Card>
        <CardContent className="py-4">
          <p className="text-muted-foreground">Estimate rally attendance from venue area. Production: CSRNet ONNX model for photo-based estimation.</p>
          <div className="grid grid-cols-2 gap-4 mt-4">
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">±15%</div><div className="text-xs">Accuracy</div></div>
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">CSRNet</div><div className="text-xs">Model</div></div>
          </div>
        </CardContent>
      </Card>
    </div>
  );

  const renderSocial = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">📱 Social Media Command Center</h3>
      <Card>
        <CardContent className="py-4">
          <div className="grid grid-cols-4 gap-4 mb-4">
            {['Twitter/X', 'Facebook', 'WhatsApp', 'Instagram'].map(p => (
              <div key={p} className="text-center p-3 bg-muted rounded">
                <div className="font-semibold">{p}</div>
                <div className="text-xs text-muted-foreground">Connected</div>
              </div>
            ))}
          </div>
          <p className="text-muted-foreground">Unified inbox for all social platforms. AI-suggested responses with human-in-the-loop approval. Escalation workflow for negative sentiment.</p>
          <div className="mt-4 space-y-2">
            <div className="flex items-center justify-between p-2 border rounded">
              <div className="flex items-center gap-2"><Badge>Twitter</Badge> <span className="text-sm">@voter123: Great rally today!</span></div>
              <Badge className="bg-green-500 text-white">positive</Badge>
            </div>
            <div className="flex items-center justify-between p-2 border rounded">
              <div className="flex items-center gap-2"><Badge>Facebook</Badge> <span className="text-sm">No improvement in my area...</span></div>
              <Badge className="bg-red-500 text-white">negative</Badge>
            </div>
            <div className="flex items-center justify-between p-2 border rounded">
              <div className="flex items-center gap-2"><Badge>WhatsApp</Badge> <span className="text-sm">When is the next town hall?</span></div>
              <Badge className="bg-gray-500 text-white">neutral</Badge>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );

  const renderFederated = () => (
    <div className="space-y-4">
      <h3 className="text-lg font-semibold">🔒 Federated Learning</h3>
      <Card>
        <CardContent className="py-4">
          <div className="flex items-center gap-2 mb-4">
            <Shield className="h-5 w-5 text-green-500" />
            <span className="font-semibold text-green-500">Privacy-Preserving ML</span>
          </div>
          <p className="text-muted-foreground">Cross-party model training without sharing raw voter data. Each party trains locally, shares only encrypted gradients.</p>
          <div className="grid grid-cols-3 gap-4 mt-4">
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">ε=1.0</div><div className="text-xs">Differential Privacy</div></div>
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">3</div><div className="text-xs">Supported Models</div></div>
            <div className="text-center p-4 bg-muted rounded"><div className="text-2xl font-bold">0</div><div className="text-xs">Active Parties</div></div>
          </div>
          <div className="mt-4">
            <h4 className="font-semibold mb-2">Supported Models:</h4>
            <ul className="space-y-1 text-sm">
              <li>✓ Turnout Prediction — Improve accuracy with more historical data</li>
              <li>✓ Sentiment Classification — Better Nigerian language understanding</li>
              <li>✓ Pledge Conversion — Predict which contacts convert</li>
            </ul>
          </div>
        </CardContent>
      </Card>
    </div>
  );

  const subTabs: { key: SubTab; label: string; icon: typeof AlertTriangle }[] = [
    { key: 'alerts', label: 'AI Alerts', icon: AlertTriangle },
    { key: 'route', label: 'Routes', icon: Route },
    { key: 'teams', label: 'Teams', icon: Trophy },
    { key: 'simulation', label: 'Simulation', icon: Brain },
    { key: 'ask', label: 'Ask GOTV', icon: MessageCircle },
    { key: 'experiments', label: 'Experiments', icon: FlaskConical },
    { key: 'export', label: 'Export', icon: Download },
    { key: 'crowd', label: 'Crowd', icon: Camera },
    { key: 'social', label: 'Social', icon: Inbox },
    { key: 'federated', label: 'Federated', icon: Shield },
  ];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-2">
        {subTabs.map(st => (
          <Button key={st.key} size="sm" variant={sub === st.key ? 'default' : 'outline'} onClick={() => setSub(st.key)}>
            <st.icon className="h-3 w-3 mr-1" /> {st.label}
          </Button>
        ))}
      </div>
      {sub === 'alerts' && renderAlerts()}
      {sub === 'route' && renderRoute()}
      {sub === 'teams' && renderTeams()}
      {sub === 'simulation' && renderSimulation()}
      {sub === 'ask' && renderAskGOTV()}
      {sub === 'experiments' && renderExperiments()}
      {sub === 'export' && renderExport()}
      {sub === 'crowd' && renderCrowd()}
      {sub === 'social' && renderSocial()}
      {sub === 'federated' && renderFederated()}
    </div>
  );
}
