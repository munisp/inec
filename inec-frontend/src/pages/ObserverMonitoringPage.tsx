import { useState, useEffect, useRef } from 'react';
import { Eye, Radio, Upload, Bell, MapPin, Activity, Users, FileText, AlertTriangle } from 'lucide-react';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8088';

interface ResultEvent {
  type: string;
  data: Record<string, unknown>;
  timestamp: string;
}

interface ObserverReport {
  id: number;
  observer_id: number;
  polling_unit_code: string;
  report_type: string;
  description: string;
  photo_url: string;
  status: string;
  created_at: string;
}

interface AlertRule {
  id: number;
  party_code: string;
  state_code: string;
  lga_code: string;
  alert_type: string;
  is_active: boolean;
}

interface PartyDashboard {
  party_code: string;
  total_votes: number;
  polling_units_with_results: number;
  total_polling_units: number;
  coverage_pct: number;
  state_breakdown: Record<string, unknown>[];
  recent_results: Record<string, unknown>[];
}

export default function ObserverMonitoringPage() {
  const [tab, setTab] = useState<'live' | 'reports' | 'alerts' | 'party'>('live');
  const [events, setEvents] = useState<ResultEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [reports, setReports] = useState<ObserverReport[]>([]);
  const [alertRules, setAlertRules] = useState<AlertRule[]>([]);
  const [partyData, setPartyData] = useState<PartyDashboard | null>(null);
  const [selectedParty, setSelectedParty] = useState('APC');
  const [stats, setStats] = useState<Record<string, number>>({});
  const eventSourceRef = useRef<EventSource | null>(null);

  const token = localStorage.getItem('token') || '';

  // SSE Connection for live updates
  useEffect(() => {
    if (tab !== 'live') return;
    const es = new EventSource(`${API}/observer/stream?token=${token}`);
    eventSourceRef.current = es;

    es.addEventListener('connected', () => {
      setConnected(true);
    });

    es.addEventListener('result_submitted', (e) => {
      const data = JSON.parse(e.data);
      setEvents(prev => [{ type: 'result_submitted', data, timestamp: new Date().toISOString() }, ...prev].slice(0, 100));
    });

    es.addEventListener('observer_report', (e) => {
      const data = JSON.parse(e.data);
      setEvents(prev => [{ type: 'observer_report', data, timestamp: new Date().toISOString() }, ...prev].slice(0, 100));
    });

    es.addEventListener('observer_checkin', (e) => {
      const data = JSON.parse(e.data);
      setEvents(prev => [{ type: 'observer_checkin', data, timestamp: new Date().toISOString() }, ...prev].slice(0, 100));
    });

    es.onerror = () => setConnected(false);

    return () => { es.close(); setConnected(false); };
  }, [tab, token]);

  // Fetch observer stats
  useEffect(() => {
    fetch(`${API}/observer/stats`, { headers: { Authorization: `Bearer ${token}` } })
      .then(r => r.json()).then(setStats).catch(e => console.error('observer stats:', e));
  }, [token]);

  // Fetch reports
  useEffect(() => {
    if (tab === 'reports') {
      fetch(`${API}/observer/reports`, { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json()).then(d => setReports(Array.isArray(d) ? d : [])).catch(e => console.error('observer reports:', e));
    }
  }, [tab, token]);

  // Fetch alert rules
  useEffect(() => {
    if (tab === 'alerts') {
      fetch(`${API}/observer/alerts`, { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json()).then(d => setAlertRules(Array.isArray(d) ? d : [])).catch(e => console.error('alert rules:', e));
    }
  }, [tab, token]);

  // Fetch party dashboard
  useEffect(() => {
    if (tab === 'party') {
      fetch(`${API}/observer/party-dashboard?party=${selectedParty}`, { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json()).then(setPartyData).catch(e => console.error('party data:', e));
    }
  }, [tab, selectedParty, token]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-zinc-900">Observer Monitoring</h1>
        <div className="flex items-center gap-2">
          <span className={`inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium ${connected ? 'bg-green-100 text-green-700' : 'bg-zinc-100 text-zinc-500'}`}>
            <Radio className="w-3 h-3" />
            {connected ? 'Live' : 'Disconnected'}
          </span>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        <StatCard icon={Users} label="Observers" value={stats.total_observers || 0} />
        <StatCard icon={MapPin} label="Active Check-ins" value={stats.active_check_ins || 0} />
        <StatCard icon={FileText} label="Reports Today" value={stats.reports_today || 0} />
        <StatCard icon={Bell} label="Alert Rules" value={stats.active_alert_rules || 0} />
        <StatCard icon={Activity} label="Live Streams" value={stats.active_sse_streams || 0} />
      </div>

      {/* Tabs */}
      <div className="flex border-b border-zinc-200">
        {[
          { key: 'live', icon: Radio, label: 'Live Feed' },
          { key: 'reports', icon: Upload, label: 'Reports' },
          { key: 'alerts', icon: Bell, label: 'Alerts' },
          { key: 'party', icon: Eye, label: 'Party Dashboard' },
        ].map(t => (
          <button key={t.key} onClick={() => setTab(t.key as typeof tab)}
            className={`flex items-center gap-2 px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === t.key ? 'border-green-600 text-green-700' : 'border-transparent text-zinc-500 hover:text-zinc-700'}`}>
            <t.icon className="w-4 h-4" />{t.label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      {tab === 'live' && <LiveFeedTab events={events} connected={connected} />}
      {tab === 'reports' && <ReportsTab reports={reports} token={token} />}
      {tab === 'alerts' && <AlertsTab rules={alertRules} token={token} onRefresh={() => {
        fetch(`${API}/observer/alerts`, { headers: { Authorization: `Bearer ${token}` } })
          .then(r => r.json()).then(d => setAlertRules(Array.isArray(d) ? d : [])).catch(e => console.error('alert refresh:', e));
      }} />}
      {tab === 'party' && <PartyTab data={partyData} party={selectedParty} onPartyChange={setSelectedParty} />}
    </div>
  );
}

function StatCard({ icon: Icon, label, value }: { icon: React.ElementType; label: string; value: number }) {
  return (
    <div className="bg-white rounded-lg border border-zinc-200 p-3">
      <div className="flex items-center gap-2 text-zinc-500 text-xs mb-1">
        <Icon className="w-3 h-3" />{label}
      </div>
      <div className="text-xl font-bold text-zinc-900">{value}</div>
    </div>
  );
}

function LiveFeedTab({ events, connected }: { events: ResultEvent[]; connected: boolean }) {
  if (!connected && events.length === 0) {
    return (
      <div className="text-center py-12 text-zinc-500">
        <Radio className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p>Connecting to live feed...</p>
        <p className="text-xs mt-1">Results will appear here in real-time as they are submitted from polling units.</p>
      </div>
    );
  }

  return (
    <div className="space-y-2 max-h-[500px] overflow-y-auto">
      {events.length === 0 && (
        <div className="text-center py-8 text-zinc-400">
          <p>Waiting for results...</p>
        </div>
      )}
      {events.map((event, i) => (
        <div key={i} className="flex items-start gap-3 p-3 bg-white rounded-lg border border-zinc-100">
          <div className={`w-2 h-2 rounded-full mt-2 ${event.type === 'result_submitted' ? 'bg-green-500' : event.type === 'observer_report' ? 'bg-blue-500' : 'bg-amber-500'}`} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-xs font-medium text-zinc-700 uppercase">{event.type.replace('_', ' ')}</span>
              <span className="text-xs text-zinc-400">{new Date(event.timestamp).toLocaleTimeString()}</span>
            </div>
            <pre className="text-xs text-zinc-600 mt-1 whitespace-pre-wrap overflow-hidden">{JSON.stringify(event.data, null, 2).slice(0, 200)}</pre>
          </div>
        </div>
      ))}
    </div>
  );
}

function ReportsTab({ reports, token }: { reports: ObserverReport[]; token: string }) {
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const handleUpload = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const formData = new FormData(form);
    setUploading(true);
    try {
      await fetch(`${API}/observer/reports`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      });
      form.reset();
    } catch { /* handled */ }
    setUploading(false);
  };

  return (
    <div className="space-y-4">
      {/* Upload Form */}
      <form onSubmit={handleUpload} className="bg-white rounded-lg border border-zinc-200 p-4 space-y-3">
        <h3 className="font-medium text-sm text-zinc-700">Submit Observer Report</h3>
        <div className="grid grid-cols-2 gap-3">
          <input name="polling_unit_code" placeholder="Polling Unit Code" required className="border rounded px-3 py-2 text-sm" />
          <input name="election_id" type="number" placeholder="Election ID" required className="border rounded px-3 py-2 text-sm" />
          <select name="report_type" className="border rounded px-3 py-2 text-sm">
            <option value="result_photo">Result Sheet Photo</option>
            <option value="irregularity">Irregularity</option>
            <option value="observation">General Observation</option>
          </select>
          <input ref={fileRef} name="photo" type="file" accept="image/*,.pdf" className="border rounded px-3 py-2 text-sm" />
        </div>
        <textarea name="description" placeholder="Description..." className="w-full border rounded px-3 py-2 text-sm" rows={2} />
        <button type="submit" disabled={uploading} className="bg-green-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-green-700 disabled:opacity-50">
          {uploading ? 'Uploading...' : 'Submit Report'}
        </button>
      </form>

      {/* Reports List */}
      <div className="space-y-2">
        {reports.map(report => (
          <div key={report.id} className="bg-white rounded-lg border border-zinc-200 p-3 flex items-start gap-3">
            {report.photo_url ? (
              <img src={`${API}${report.photo_url}`} alt="Report" className="w-16 h-16 rounded object-cover" />
            ) : (
              <div className="w-16 h-16 rounded bg-zinc-100 flex items-center justify-center">
                <FileText className="w-6 h-6 text-zinc-400" />
              </div>
            )}
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <span className="text-xs font-medium">{report.polling_unit_code}</span>
                <span className={`text-xs px-1.5 py-0.5 rounded ${report.status === 'pending' ? 'bg-amber-100 text-amber-700' : report.status === 'flagged' ? 'bg-red-100 text-red-700' : 'bg-green-100 text-green-700'}`}>
                  {report.status}
                </span>
              </div>
              <p className="text-xs text-zinc-600 mt-1">{report.description || report.report_type}</p>
              <p className="text-xs text-zinc-400 mt-1">{new Date(report.created_at).toLocaleString()}</p>
            </div>
          </div>
        ))}
        {reports.length === 0 && <p className="text-center py-8 text-zinc-400 text-sm">No reports yet</p>}
      </div>
    </div>
  );
}

function AlertsTab({ rules, token, onRefresh }: { rules: AlertRule[]; token: string; onRefresh: () => void }) {
  const handleCreate = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = Object.fromEntries(new FormData(form));
    await fetch(`${API}/observer/alerts`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    form.reset();
    onRefresh();
  };

  const handleDelete = async (id: number) => {
    await fetch(`${API}/observer/alerts/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` },
    });
    onRefresh();
  };

  return (
    <div className="space-y-4">
      <form onSubmit={handleCreate} className="bg-white rounded-lg border border-zinc-200 p-4 space-y-3">
        <h3 className="font-medium text-sm text-zinc-700">Create Alert Rule</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <input name="party_code" placeholder="Party Code (e.g. APC)" className="border rounded px-3 py-2 text-sm" />
          <input name="state_code" placeholder="State Code" className="border rounded px-3 py-2 text-sm" />
          <input name="lga_code" placeholder="LGA Code" className="border rounded px-3 py-2 text-sm" />
          <select name="alert_type" className="border rounded px-3 py-2 text-sm">
            <option value="result_submitted">Result Submitted</option>
            <option value="anomaly_detected">Anomaly Detected</option>
            <option value="geofence_violation">Geofence Violation</option>
          </select>
        </div>
        <button type="submit" className="bg-green-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-green-700">
          Create Alert
        </button>
      </form>

      <div className="space-y-2">
        {rules.map(rule => (
          <div key={rule.id} className="bg-white rounded-lg border border-zinc-200 p-3 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Bell className="w-4 h-4 text-amber-500" />
              <div>
                <span className="text-sm font-medium">{rule.alert_type.replace('_', ' ')}</span>
                <div className="text-xs text-zinc-500 mt-0.5">
                  {[rule.party_code && `Party: ${rule.party_code}`, rule.state_code && `State: ${rule.state_code}`, rule.lga_code && `LGA: ${rule.lga_code}`].filter(Boolean).join(' • ') || 'All events'}
                </div>
              </div>
            </div>
            <button onClick={() => handleDelete(rule.id)} className="text-xs text-red-500 hover:text-red-700">Delete</button>
          </div>
        ))}
        {rules.length === 0 && <p className="text-center py-8 text-zinc-400 text-sm">No alert rules configured</p>}
      </div>
    </div>
  );
}

function PartyTab({ data, party, onPartyChange }: { data: PartyDashboard | null; party: string; onPartyChange: (p: string) => void }) {
  const parties = ['APC', 'PDP', 'LP', 'NNPP', 'ADC', 'SDP', 'APGA', 'YPP'];

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium text-zinc-700">Select Party:</label>
        <select value={party} onChange={e => onPartyChange(e.target.value)} className="border rounded px-3 py-2 text-sm">
          {parties.map(p => <option key={p} value={p}>{p}</option>)}
        </select>
      </div>

      {data && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div className="bg-white rounded-lg border p-3">
              <div className="text-xs text-zinc-500">Total Votes</div>
              <div className="text-xl font-bold text-green-700">{data.total_votes.toLocaleString()}</div>
            </div>
            <div className="bg-white rounded-lg border p-3">
              <div className="text-xs text-zinc-500">PUs Reported</div>
              <div className="text-xl font-bold">{data.polling_units_with_results}</div>
            </div>
            <div className="bg-white rounded-lg border p-3">
              <div className="text-xs text-zinc-500">Total PUs</div>
              <div className="text-xl font-bold">{data.total_polling_units}</div>
            </div>
            <div className="bg-white rounded-lg border p-3">
              <div className="text-xs text-zinc-500">Coverage</div>
              <div className="text-xl font-bold text-blue-600">{data.coverage_pct}%</div>
            </div>
          </div>

          {/* State Breakdown */}
          {Array.isArray(data.state_breakdown) && data.state_breakdown.length > 0 && (
            <div className="bg-white rounded-lg border p-4">
              <h3 className="text-sm font-medium text-zinc-700 mb-3">State Breakdown</h3>
              <div className="space-y-2 max-h-64 overflow-y-auto">
                {data.state_breakdown.map((state: Record<string, unknown>, i: number) => (
                  <div key={i} className="flex items-center justify-between text-sm">
                    <span className="text-zinc-700">{String(state.state_code || 'Unknown')}</span>
                    <div className="flex items-center gap-3">
                      <span className="text-zinc-500">{String(state.pu_count || 0)} PUs</span>
                      <span className="font-medium">{Number(state.votes || 0).toLocaleString()} votes</span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Recent Results */}
          {Array.isArray(data.recent_results) && data.recent_results.length > 0 && (
            <div className="bg-white rounded-lg border p-4">
              <h3 className="text-sm font-medium text-zinc-700 mb-3">Recent Results</h3>
              <div className="space-y-1 max-h-48 overflow-y-auto">
                {data.recent_results.map((r: Record<string, unknown>, i: number) => (
                  <div key={i} className="flex items-center justify-between text-xs py-1 border-b border-zinc-50">
                    <span className="text-zinc-600">{String(r.polling_unit_code || '')}</span>
                    <span className="font-medium">{Number(r.votes || 0).toLocaleString()} votes</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}

      {!data && (
        <div className="text-center py-8 text-zinc-400">
          <AlertTriangle className="w-8 h-8 mx-auto mb-2 opacity-50" />
          <p>Loading party data...</p>
        </div>
      )}
    </div>
  );
}
