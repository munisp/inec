import { useState, useEffect, useCallback, useRef } from 'react';
import { api } from '../lib/api';
import { logger } from '../lib/utils';
import { Activity, AlertTriangle, Radio, Shield, Clock, RefreshCw, Zap, MapPin } from 'lucide-react';

interface StateVelocity {
  state_code: string;
  state_name: string;
  total_pus: number;
  reported_pus: number;
  completion_pct: number;
  stalled_pus: number;
  eta_complete: string;
  status: string;
}

interface Alert {
  id: number;
  level: string;
  state_code: string;
  message: string;
  auto_action?: string;
}

interface EscalationRule {
  name: string;
  condition: string;
  level: string;
  action: string;
  cooldown: number;
}

interface FeedItem {
  id: number;
  polling_unit_code: string;
  pu_name: string;
  state_name: string;
  lga_name: string;
  ward_name: string;
  total_votes_cast: number;
  status: string;
  submitted_at: string;
  tigerbeetle_status: string;
  hyperledger_status: string;
}

interface CommandData {
  states: StateVelocity[];
  alerts: Alert[];
  overall_pus: number;
  reported_pus: number;
  stalled_pus: number;
  completion_pct: number;
  load_shedding: number;
  timestamp: string;
}

const GEO_ZONES: Record<string, string[]> = {
  'North Central': ['BE', 'KO', 'KW', 'NA', 'NI', 'PL', 'FC'],
  'North East': ['AD', 'BA', 'BO', 'GO', 'TA', 'YO'],
  'North West': ['JI', 'KD', 'KN', 'KT', 'KB', 'SO', 'ZA'],
  'South East': ['AB', 'AN', 'EB', 'EN', 'IM'],
  'South South': ['AK', 'BY', 'CR', 'DE', 'ED', 'RI'],
  'South West': ['EK', 'LA', 'OG', 'ON', 'OS', 'OY'],
};

function getZoneForState(stateCode: string): string {
  for (const [zone, codes] of Object.entries(GEO_ZONES)) {
    if (codes.includes(stateCode)) return zone;
  }
  return 'Other';
}

export default function CommandCenterPage() {
  const [data, setData] = useState<CommandData | null>(null);
  const [loadLevel, setLoadLevel] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [liveFeed, setLiveFeed] = useState<FeedItem[]>([]);
  const [escalationRules, setEscalationRules] = useState<EscalationRule[]>([]);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [activeTab, setActiveTab] = useState<'states' | 'zones'>('states');
  const sseRef = useRef<EventSource | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);
      const [live, feed, rules] = await Promise.all([
        api.getCommandCenterLive(),
        api.getLiveFeed(1, 10),
        api.getEscalationConfig(),
      ]);
      setData(live);
      setLoadLevel(live.load_shedding || 0);
      setLiveFeed(feed);
      setEscalationRules(rules.rules || []);
      setLastUpdated(new Date());
    } catch (e) {
      logger.error(e);
      setError(e instanceof Error ? e.message : 'Failed to load command center data');
    }
  }, []);

  useEffect(() => {
    fetchData();
    if (!autoRefresh) return;
    const interval = setInterval(fetchData, 5000);
    return () => clearInterval(interval);
  }, [fetchData, autoRefresh]);

  useEffect(() => {
    try {
      const baseUrl = window.location.origin.replace('5175', '8088');
      const es = new EventSource(`${baseUrl}/command-center/stream`, { withCredentials: true });
      es.onmessage = (event) => {
        try {
          const update = JSON.parse(event.data);
          if (update.states) {
            setData(prev => prev ? { ...prev, states: update.states, timestamp: update.timestamp } : prev);
            setLastUpdated(new Date());
          }
        } catch { /* ignore parse errors */ }
      };
      sseRef.current = es;
      return () => es.close();
    } catch { /* SSE not available */ }
  }, []);

  const handleLoadShed = async (level: number) => {
    try {
      await api.setLoadShedding(level);
      setLoadLevel(level);
    } catch (e) {
      logger.error(e);
    }
  };

  const statusColor = (status: string) => {
    switch (status) {
      case 'green': return 'bg-green-500';
      case 'amber': return 'bg-yellow-500';
      case 'red': return 'bg-red-500';
      default: return 'bg-gray-400';
    }
  };

  const levelColor = (level: string) => {
    switch (level) {
      case 'EMERGENCY': return 'bg-red-700 text-white border-red-900';
      case 'CRITICAL': return 'bg-red-500 text-white border-red-700';
      case 'WARN': return 'bg-yellow-500 text-black border-yellow-600';
      default: return 'bg-blue-500 text-white border-blue-700';
    }
  };

  const loadLevelLabel = (l: number) => {
    switch (l) {
      case 0: return { label: 'Off', desc: 'All requests served', color: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' };
      case 1: return { label: 'L1', desc: 'Analytics shed', color: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200' };
      case 2: return { label: 'L2', desc: 'All reads shed', color: 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200' };
      case 3: return { label: 'L3', desc: 'Submissions only', color: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200' };
      default: return { label: `L${l}`, desc: '', color: 'bg-gray-100 text-gray-800' };
    }
  };

  const computeZoneSummary = () => {
    if (!data?.states) return [];
    const zones: Record<string, { total: number; reported: number; stalled: number; states: number }> = {};
    for (const s of data.states) {
      const zone = getZoneForState(s.state_code);
      if (!zones[zone]) zones[zone] = { total: 0, reported: 0, stalled: 0, states: 0 };
      zones[zone].total += s.total_pus;
      zones[zone].reported += s.reported_pus;
      zones[zone].stalled += s.stalled_pus;
      zones[zone].states += 1;
    }
    return Object.entries(zones).map(([name, z]) => ({
      name,
      ...z,
      pct: z.total > 0 ? (z.reported / z.total) * 100 : 0,
      status: z.total > 0 ? ((z.reported / z.total) * 100 >= 90 ? 'green' : (z.reported / z.total) * 100 >= 50 ? 'amber' : 'red') : 'red',
    })).sort((a, b) => b.pct - a.pct);
  };

  const formatTime = (ts: string) => {
    try {
      const d = new Date(ts);
      return d.toLocaleTimeString('en-NG', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch { return ts; }
  };

  if (error && !data) return (
    <div className="flex flex-col items-center justify-center h-64 gap-4" role="main" aria-label="Command Center Error">
      <AlertTriangle className="w-8 h-8 text-red-500" />
      <p className="text-zinc-600 dark:text-zinc-400">{error}</p>
      <button onClick={fetchData} className="px-4 py-2 bg-green-700 text-white rounded-lg hover:bg-green-800 text-sm">Retry</button>
    </div>
  );

  if (!data) return (
    <div className="flex items-center justify-center h-64" role="main" aria-label="Loading Command Center">
      <Activity className="w-6 h-6 animate-spin text-green-700" />
    </div>
  );

  const zoneSummary = computeZoneSummary();
  const currentLoadInfo = loadLevelLabel(loadLevel);

  return (
    <div className="p-6 max-w-7xl mx-auto space-y-6" role="main" aria-label="Election Command Center">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-red-100 dark:bg-red-900 rounded-lg">
            <Radio className="w-6 h-6 text-red-600 dark:text-red-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold dark:text-white">Election Command Center</h1>
            <p className="text-gray-500 dark:text-gray-400 text-sm">War Room — Real-time election monitoring &amp; control</p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          {lastUpdated && (
            <span className="text-xs text-gray-400 flex items-center gap-1">
              <Clock className="w-3 h-3" />
              Last update: {lastUpdated.toLocaleTimeString()}
            </span>
          )}
          <label className="flex items-center gap-2 text-sm dark:text-gray-300 cursor-pointer">
            <input type="checkbox" checked={autoRefresh} onChange={(e) => setAutoRefresh(e.target.checked)} className="rounded" />
            <RefreshCw className={`w-3 h-3 ${autoRefresh ? 'animate-spin' : ''}`} />
            Auto-refresh (5s)
          </label>
        </div>
      </div>

      {/* Overall Progress Bar */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-medium dark:text-gray-300">Overall Election Progress</span>
          <span className="text-sm font-bold dark:text-white">{data.completion_pct.toFixed(1)}%</span>
        </div>
        <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-4">
          <div
            className={`h-4 rounded-full transition-all duration-500 ${
              data.completion_pct >= 90 ? 'bg-green-500' : data.completion_pct >= 50 ? 'bg-yellow-500' : 'bg-red-500'
            }`}
            style={{ width: `${Math.min(data.completion_pct, 100)}%` }}
          />
        </div>
        <div className="flex justify-between mt-1 text-xs text-gray-500 dark:text-gray-400">
          <span>{data.reported_pus.toLocaleString()} of {data.overall_pus.toLocaleString()} PUs reported</span>
          <span>{data.stalled_pus.toLocaleString()} stalled</span>
        </div>
      </div>

      {/* Summary Stats Grid */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex items-center gap-2 mb-1">
            <MapPin className="w-4 h-4 text-gray-500" />
            <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Total PUs</p>
          </div>
          <p className="text-2xl font-bold dark:text-white">{data.overall_pus.toLocaleString()}</p>
          <p className="text-xs text-gray-400">{data.states?.length || 0} states</p>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex items-center gap-2 mb-1">
            <Activity className="w-4 h-4 text-green-500" />
            <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Reported</p>
          </div>
          <p className="text-2xl font-bold text-green-600">{data.reported_pus.toLocaleString()}</p>
          <p className="text-xs text-green-500">{data.completion_pct.toFixed(1)}% complete</p>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex items-center gap-2 mb-1">
            <AlertTriangle className="w-4 h-4 text-red-500" />
            <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Stalled</p>
          </div>
          <p className="text-2xl font-bold text-red-600">{data.stalled_pus.toLocaleString()}</p>
          <p className="text-xs text-red-500">{data.overall_pus > 0 ? ((data.stalled_pus / data.overall_pus) * 100).toFixed(1) : 0}% pending</p>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex items-center gap-2 mb-1">
            <AlertTriangle className="w-4 h-4 text-yellow-500" />
            <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Alerts</p>
          </div>
          <p className="text-2xl font-bold text-yellow-600">{data.alerts?.length || 0}</p>
          <p className="text-xs text-gray-400">
            {data.alerts?.filter(a => a.level === 'EMERGENCY' || a.level === 'CRITICAL').length || 0} critical
          </p>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex items-center gap-2 mb-1">
            <Shield className="w-4 h-4 text-blue-500" />
            <p className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Load Shedding</p>
          </div>
          <span className={`inline-block px-2 py-1 rounded text-sm font-medium ${currentLoadInfo.color}`}>
            {currentLoadInfo.label}: {currentLoadInfo.desc}
          </span>
          <div className="flex gap-1 mt-2">
            {[0, 1, 2, 3].map((l) => (
              <button key={l} onClick={() => handleLoadShed(l)}
                className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${loadLevel === l ? 'bg-blue-600 text-white' : 'bg-gray-200 dark:bg-gray-700 dark:text-gray-300 hover:bg-gray-300'}`}>
                {l === 0 ? 'Off' : `L${l}`}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Alerts Banner */}
      {data.alerts && data.alerts.length > 0 && (
        <div className="space-y-2">
          <h2 className="text-sm font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide flex items-center gap-2">
            <AlertTriangle className="w-4 h-4" /> Active Alerts ({data.alerts.length})
          </h2>
          {data.alerts.map((alert, i) => (
            <div key={i} className={`p-3 rounded-lg border ${levelColor(alert.level)} flex items-center justify-between`}>
              <div>
                <span className="font-bold text-sm">[{alert.level}]</span>
                <span className="ml-2 text-sm">{alert.message}</span>
                {alert.state_code && <span className="ml-2 opacity-75 text-xs">({alert.state_code})</span>}
              </div>
              {alert.auto_action && (
                <span className="text-xs opacity-75 bg-black/10 px-2 py-0.5 rounded">Action: {alert.auto_action}</span>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Two-column: State/Zone Table + Live Feed & Escalation */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* State/Zone Table — 2 cols */}
        <div className="lg:col-span-2 bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
          <div className="flex items-center justify-between p-4 border-b dark:border-gray-700">
            <div className="flex gap-2">
              <button
                onClick={() => setActiveTab('states')}
                className={`px-3 py-1 rounded text-sm font-medium ${activeTab === 'states' ? 'bg-green-700 text-white' : 'bg-gray-100 dark:bg-gray-700 dark:text-gray-300'}`}>
                By State ({data.states?.length || 0})
              </button>
              <button
                onClick={() => setActiveTab('zones')}
                className={`px-3 py-1 rounded text-sm font-medium ${activeTab === 'zones' ? 'bg-green-700 text-white' : 'bg-gray-100 dark:bg-gray-700 dark:text-gray-300'}`}>
                By Geo-Zone ({zoneSummary.length})
              </button>
            </div>
          </div>

          {activeTab === 'states' && (
            <div className="overflow-x-auto max-h-[500px] overflow-y-auto">
              <table className="w-full text-sm" aria-label="State reporting progress">
                <thead className="bg-gray-50 dark:bg-gray-700 sticky top-0">
                  <tr>
                    <th className="text-left p-3 dark:text-gray-300">State</th>
                    <th className="text-right p-3 dark:text-gray-300">Total PUs</th>
                    <th className="text-right p-3 dark:text-gray-300">Reported</th>
                    <th className="text-right p-3 dark:text-gray-300">Stalled</th>
                    <th className="text-center p-3 dark:text-gray-300 w-40">Progress</th>
                    <th className="text-center p-3 dark:text-gray-300">Status</th>
                    <th className="text-center p-3 dark:text-gray-300">ETA</th>
                  </tr>
                </thead>
                <tbody>
                  {data.states?.map((s) => (
                    <tr key={s.state_code} className="border-t dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-750">
                      <td className="p-3 dark:text-white">
                        <span className="font-medium">{s.state_name || s.state_code}</span>
                        <span className="text-xs text-gray-400 ml-1">({s.state_code})</span>
                      </td>
                      <td className="p-3 text-right dark:text-gray-300">{s.total_pus.toLocaleString()}</td>
                      <td className="p-3 text-right text-green-600">{s.reported_pus.toLocaleString()}</td>
                      <td className="p-3 text-right text-red-500">{s.stalled_pus.toLocaleString()}</td>
                      <td className="p-3">
                        <div className="w-full bg-gray-200 dark:bg-gray-600 rounded-full h-2">
                          <div className={`h-2 rounded-full transition-all ${statusColor(s.status)}`} style={{ width: `${Math.min(s.completion_pct, 100)}%` }} />
                        </div>
                        <p className="text-xs text-center mt-0.5 dark:text-gray-400">{s.completion_pct.toFixed(1)}%</p>
                      </td>
                      <td className="p-3 text-center">
                        <span className={`inline-block w-3 h-3 rounded-full ${statusColor(s.status)}`} title={s.status} />
                      </td>
                      <td className="p-3 text-center text-xs dark:text-gray-400">{s.eta_complete}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {activeTab === 'zones' && (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" aria-label="Geo-zone reporting progress">
                <thead className="bg-gray-50 dark:bg-gray-700">
                  <tr>
                    <th className="text-left p-3 dark:text-gray-300">Geo-Zone</th>
                    <th className="text-right p-3 dark:text-gray-300">States</th>
                    <th className="text-right p-3 dark:text-gray-300">Total PUs</th>
                    <th className="text-right p-3 dark:text-gray-300">Reported</th>
                    <th className="text-right p-3 dark:text-gray-300">Stalled</th>
                    <th className="text-center p-3 dark:text-gray-300 w-40">Progress</th>
                    <th className="text-center p-3 dark:text-gray-300">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {zoneSummary.map((z) => (
                    <tr key={z.name} className="border-t dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-750">
                      <td className="p-3 font-medium dark:text-white">{z.name}</td>
                      <td className="p-3 text-right dark:text-gray-300">{z.states}</td>
                      <td className="p-3 text-right dark:text-gray-300">{z.total.toLocaleString()}</td>
                      <td className="p-3 text-right text-green-600">{z.reported.toLocaleString()}</td>
                      <td className="p-3 text-right text-red-500">{z.stalled.toLocaleString()}</td>
                      <td className="p-3">
                        <div className="w-full bg-gray-200 dark:bg-gray-600 rounded-full h-3">
                          <div className={`h-3 rounded-full transition-all ${statusColor(z.status)}`} style={{ width: `${Math.min(z.pct, 100)}%` }} />
                        </div>
                        <p className="text-xs text-center mt-0.5 dark:text-gray-400">{z.pct.toFixed(1)}%</p>
                      </td>
                      <td className="p-3 text-center">
                        <span className={`inline-block w-3 h-3 rounded-full ${statusColor(z.status)}`} title={z.status} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Right column: Live Feed + Escalation Rules */}
        <div className="space-y-6">
          {/* Live Activity Feed */}
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow">
            <h2 className="text-sm font-semibold p-4 border-b dark:border-gray-700 dark:text-gray-300 uppercase tracking-wide flex items-center gap-2">
              <Zap className="w-4 h-4 text-yellow-500" /> Live Feed
            </h2>
            <div className="max-h-64 overflow-y-auto divide-y dark:divide-gray-700">
              {liveFeed.length === 0 && (
                <p className="p-4 text-sm text-gray-400">No recent submissions</p>
              )}
              {liveFeed.map((item) => (
                <div key={item.id} className="p-3 text-xs hover:bg-gray-50 dark:hover:bg-gray-750">
                  <div className="flex items-center justify-between">
                    <span className="font-medium dark:text-white">{item.polling_unit_code}</span>
                    <span className="text-gray-400">{formatTime(item.submitted_at)}</span>
                  </div>
                  <p className="text-gray-500 dark:text-gray-400 mt-0.5">
                    {item.state_name} &gt; {item.lga_name} &gt; {item.ward_name}
                  </p>
                  <div className="flex items-center gap-2 mt-1">
                    <span className="text-gray-600 dark:text-gray-300">{item.total_votes_cast.toLocaleString()} votes</span>
                    <span className={`px-1.5 py-0.5 rounded text-xs ${
                      item.status === 'validated' ? 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300' :
                      item.status === 'pending' ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300' :
                      'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
                    }`}>{item.status}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Escalation Rules */}
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow">
            <h2 className="text-sm font-semibold p-4 border-b dark:border-gray-700 dark:text-gray-300 uppercase tracking-wide flex items-center gap-2">
              <Shield className="w-4 h-4 text-blue-500" /> Escalation Rules
            </h2>
            <div className="divide-y dark:divide-gray-700">
              {escalationRules.map((rule, i) => (
                <div key={i} className="p-3 text-xs">
                  <div className="flex items-center justify-between">
                    <span className="font-medium dark:text-white">{rule.name.replace(/_/g, ' ')}</span>
                    <span className={`px-1.5 py-0.5 rounded text-xs font-bold ${
                      rule.level === 'EMERGENCY' ? 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300' :
                      rule.level === 'CRITICAL' ? 'bg-red-100 text-red-600 dark:bg-red-900 dark:text-red-300' :
                      'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300'
                    }`}>{rule.level}</span>
                  </div>
                  <p className="text-gray-500 dark:text-gray-400 mt-0.5">
                    When: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">{rule.condition}</code>
                  </p>
                  <p className="text-gray-500 dark:text-gray-400">
                    Action: <span className="text-blue-600 dark:text-blue-400">{rule.action.replace(/_/g, ' ')}</span>
                    {' · '}Cooldown: {Math.round(rule.cooldown / 1e9 / 60)}m
                  </p>
                </div>
              ))}
              {escalationRules.length === 0 && (
                <p className="p-4 text-sm text-gray-400">No escalation rules configured</p>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Error indicator */}
      {error && (
        <div className="text-xs text-center text-red-400 mt-2">
          Warning: Some data may be stale. Last error: {error}
        </div>
      )}
    </div>
  );
}
