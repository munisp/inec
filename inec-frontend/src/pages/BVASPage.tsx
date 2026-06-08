import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, LineChart, Line, PieChart, Pie, Cell } from 'recharts';

interface DeviceSummary {
  total: number; active: number; offline: number; faulty: number;
  avg_battery: number; low_battery_count: number;
}
interface AccreditationSummary {
  total: number; biometric_match: number; pvc_verified: number;
  biometric_pass_rate: number; pvc_verify_rate: number;
}
interface ReconciliationRow {
  polling_unit_code: string; pu_name: string;
  bvas_accredited: number; result_accredited: number;
  discrepancy: number; discrepancy_pct: number;
  flag_level: string; biometric_pass_rate: number; pvc_verify_rate: number;
}

export default function BVASPage() {
  const [devices, setDevices] = useState<DeviceSummary | null>(null);
  const [accreditation, setAccreditation] = useState<AccreditationSummary | null>(null);
  const [reconciliation, setReconciliation] = useState<ReconciliationRow[]>([]);
  const [totalFlagged, setTotalFlagged] = useState(0);
  const [timeline, setTimeline] = useState<any[]>([]);
  const [feed, setFeed] = useState<any[]>([]);
  const [ingestion, setIngestion] = useState<any>(null);
  const [stateBreakdown, setStateBreakdown] = useState<any[]>([]);
  const [flaggedOnly, setFlaggedOnly] = useState(false);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<'overview' | 'devices' | 'reconciliation' | 'ingestion'>('overview');

  useEffect(() => {
    loadAll();
    const iv = setInterval(loadAll, 30000);
    return () => clearInterval(iv);
  }, [flaggedOnly]);

  async function loadAll() {
    try {
      const [summary, recon, tl, fd, ing] = await Promise.all([
        api.getBVASSummary(1).catch(() => null),
        api.getBVASReconciliation(1, flaggedOnly).catch(() => ({ reconciliation: [], total_flagged: 0 })),
        api.getBVASAccreditationTimeline(1, 'hour').catch(() => ({ data: [] })),
        api.getBVASAccreditationFeed(1, 20).catch(() => []),
        api.getIngestionStats().catch(() => null),
      ]);
      if (summary) {
        setDevices(summary.devices);
        setAccreditation(summary.accreditation);
        setStateBreakdown(summary.state_breakdown || []);
      }
      setReconciliation(recon?.reconciliation || []);
      setTotalFlagged(recon?.total_flagged || 0);
      setTimeline(tl?.data || []);
      setFeed(Array.isArray(fd) ? fd : []);
      setIngestion(ing);
    } catch { /* ignore */ } finally { setLoading(false); }
  }

  const flagColor = (level: string) => {
    switch (level) {
      case 'critical': return 'bg-red-100 text-red-800';
      case 'warning': return 'bg-yellow-100 text-yellow-800';
      case 'minor': return 'bg-orange-100 text-orange-800';
      default: return 'bg-green-100 text-green-800';
    }
  };

  if (loading) return <div className="flex items-center justify-center h-64"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-green-600" /></div>;

  const devicePie = devices ? [
    { name: 'Active', value: devices.active, color: '#10b981' },
    { name: 'Offline', value: devices.offline, color: '#f59e0b' },
    { name: 'Faulty', value: devices.faulty, color: '#ef4444' },
    { name: 'Other', value: devices.total - devices.active - devices.offline - devices.faulty, color: '#6b7280' },
  ].filter(d => d.value > 0) : [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">BVAS Integration</h2>
          <p className="text-sm text-gray-500">Bimodal Voter Accreditation System monitoring & reconciliation</p>
        </div>
        <div className="flex gap-2">
          {(['overview', 'devices', 'reconciliation', 'ingestion'] as const).map(t => (
            <button key={t} onClick={() => setTab(t)}
              className={`px-3 py-1.5 rounded text-sm font-medium ${tab === t ? 'bg-green-600 text-white' : 'bg-gray-100 text-gray-700 hover:bg-gray-200'}`}>
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
        </div>
      </div>

      {tab === 'overview' && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <StatCard label="Total Devices" value={devices?.total || 0} sub={`${devices?.active || 0} active`} color="green" />
            <StatCard label="Accreditations" value={accreditation?.total || 0} sub={`${accreditation?.biometric_pass_rate || 0}% biometric`} color="blue" />
            <StatCard label="Flagged PUs" value={totalFlagged} sub="discrepancy > 10%" color={totalFlagged > 0 ? 'red' : 'green'} />
            <StatCard label="Avg Battery" value={`${devices?.avg_battery || 0}%`} sub={`${devices?.low_battery_count || 0} low battery`} color={devices && devices.avg_battery < 30 ? 'red' : 'green'} />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="bg-white rounded-lg shadow p-4">
              <h3 className="font-semibold mb-3">Device Status</h3>
              <ResponsiveContainer width="100%" height={200}>
                <PieChart>
                  <Pie data={devicePie} cx="50%" cy="50%" outerRadius={70} dataKey="value" label={({ name, value }) => `${name}: ${value}`}>
                    {devicePie.map((entry, i) => <Cell key={i} fill={entry.color} />)}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="bg-white rounded-lg shadow p-4">
              <h3 className="font-semibold mb-3">Accreditation Rates</h3>
              <div className="space-y-3 mt-4">
                <RateBar label="Biometric Match" rate={accreditation?.biometric_pass_rate || 0} color="green" />
                <RateBar label="PVC Verification" rate={accreditation?.pvc_verify_rate || 0} color="blue" />
              </div>
            </div>
          </div>

          <div className="bg-white rounded-lg shadow p-4">
            <h3 className="font-semibold mb-3">Accreditation Timeline</h3>
            <ResponsiveContainer width="100%" height={250}>
              <LineChart data={timeline}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="time_bucket" tick={{ fontSize: 10 }} />
                <YAxis />
                <Tooltip />
                <Line type="monotone" dataKey="cumulative" stroke="#10b981" strokeWidth={2} name="Cumulative" />
                <Line type="monotone" dataKey="accreditations" stroke="#3b82f6" strokeWidth={2} name="Per Period" />
              </LineChart>
            </ResponsiveContainer>
          </div>

          <div className="bg-white rounded-lg shadow p-4">
            <h3 className="font-semibold mb-3">Accreditations by State</h3>
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={stateBreakdown.slice(0, 15)}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="code" tick={{ fontSize: 10 }} />
                <YAxis />
                <Tooltip />
                <Bar dataKey="accreditation_count" fill="#10b981" name="Accreditations" />
                <Bar dataKey="device_count" fill="#3b82f6" name="Devices" />
              </BarChart>
            </ResponsiveContainer>
          </div>

          <div className="bg-white rounded-lg shadow p-4">
            <h3 className="font-semibold mb-3">Live Accreditation Feed</h3>
            <div className="divide-y max-h-64 overflow-y-auto">
              {feed.length === 0 ? <p className="text-sm text-gray-500 py-2">No recent accreditations</p> :
                feed.slice(0, 15).map((e: any, i: number) => (
                  <div key={i} className="py-2 flex justify-between items-center text-sm">
                    <div>
                      <span className="font-medium">{e.pu_name || e.polling_unit_code}</span>
                      <span className="text-gray-500 ml-2">via {e.method}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className={`text-xs px-1.5 py-0.5 rounded ${e.biometric_match ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                        Bio: {e.biometric_match ? 'Pass' : 'Fail'}
                      </span>
                      <span className={`text-xs px-1.5 py-0.5 rounded ${e.pvc_verified ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                        PVC: {e.pvc_verified ? 'OK' : 'Fail'}
                      </span>
                    </div>
                  </div>
                ))}
            </div>
          </div>
        </>
      )}

      {tab === 'devices' && (
        <DevicesTab />
      )}

      {tab === 'reconciliation' && (
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <h3 className="font-semibold text-lg">BVAS ↔ Results Reconciliation</h3>
            <label className="flex items-center gap-1 text-sm">
              <input type="checkbox" checked={flaggedOnly} onChange={e => setFlaggedOnly(e.target.checked)} className="rounded" />
              Flagged only
            </label>
            <span className="text-sm text-gray-500">{reconciliation.length} polling units{totalFlagged > 0 && `, ${totalFlagged} flagged`}</span>
          </div>
          <div className="bg-white rounded-lg shadow overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-3 py-2 text-left">Polling Unit</th>
                  <th className="px-3 py-2 text-right">BVAS Accredited</th>
                  <th className="px-3 py-2 text-right">Result Accredited</th>
                  <th className="px-3 py-2 text-right">Discrepancy</th>
                  <th className="px-3 py-2 text-right">%</th>
                  <th className="px-3 py-2 text-center">Flag</th>
                  <th className="px-3 py-2 text-right">Bio Rate</th>
                  <th className="px-3 py-2 text-right">PVC Rate</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {reconciliation.slice(0, 100).map((row, i) => (
                  <tr key={i} className="hover:bg-gray-50">
                    <td className="px-3 py-2">
                      <div className="font-medium">{row.pu_name}</div>
                      <div className="text-xs text-gray-400">{row.polling_unit_code}</div>
                    </td>
                    <td className="px-3 py-2 text-right font-mono">{row.bvas_accredited}</td>
                    <td className="px-3 py-2 text-right font-mono">{row.result_accredited}</td>
                    <td className="px-3 py-2 text-right font-mono">{row.discrepancy}</td>
                    <td className="px-3 py-2 text-right font-mono">{row.discrepancy_pct}%</td>
                    <td className="px-3 py-2 text-center">
                      <span className={`text-xs px-1.5 py-0.5 rounded ${flagColor(row.flag_level)}`}>{row.flag_level}</span>
                    </td>
                    <td className="px-3 py-2 text-right font-mono">{row.biometric_pass_rate}%</td>
                    <td className="px-3 py-2 text-right font-mono">{row.pvc_verify_rate}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {tab === 'ingestion' && (
        <div className="space-y-4">
          <h3 className="font-semibold text-lg">Ingestion Engine</h3>
          {ingestion && (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <StatCard label="Total Jobs" value={ingestion.total_jobs} sub={`${ingestion.completed} completed`} color="blue" />
              <StatCard label="Pending" value={ingestion.pending} sub={`${ingestion.in_progress} in progress`} color="yellow" />
              <StatCard label="Failed" value={ingestion.failed} sub={`${ingestion.dead_letter_count} in DLQ`} color={ingestion.failed > 0 ? 'red' : 'green'} />
              <StatCard label="Throughput" value={`${ingestion.throughput_per_sec}/s`} sub={`${ingestion.avg_latency_ms}ms avg`} color="green" />
            </div>
          )}
          <div className="bg-white rounded-lg shadow p-4">
            <h4 className="font-medium mb-3">Engine Capabilities</h4>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm">
              <div className="border rounded p-3">
                <div className="font-medium text-green-700">Batch Upload</div>
                <p className="text-gray-500 mt-1">Upload up to 500 results per batch with automatic deduplication and idempotency keys</p>
              </div>
              <div className="border rounded p-3">
                <div className="font-medium text-blue-700">Retry + Backpressure</div>
                <p className="text-gray-500 mt-1">Exponential backoff retry (up to 3x) with dead-letter queue for permanently failed jobs</p>
              </div>
              <div className="border rounded p-3">
                <div className="font-medium text-purple-700">Offline Sync</div>
                <p className="text-gray-500 mt-1">Field agents queue submissions offline; sync automatically when connectivity resumes</p>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, sub, color }: { label: string; value: string | number; sub: string; color: string }) {
  const colors: Record<string, string> = {
    green: 'border-green-200 bg-green-50', blue: 'border-blue-200 bg-blue-50',
    red: 'border-red-200 bg-red-50', yellow: 'border-yellow-200 bg-yellow-50',
  };
  const textColors: Record<string, string> = {
    green: 'text-green-700', blue: 'text-blue-700', red: 'text-red-700', yellow: 'text-yellow-700',
  };
  return (
    <div className={`rounded-lg border p-4 ${colors[color] || colors.green}`}>
      <div className="text-xs text-gray-500 uppercase tracking-wide">{label}</div>
      <div className={`text-2xl font-bold mt-1 ${textColors[color] || textColors.green}`}>{value}</div>
      <div className="text-xs text-gray-500 mt-1">{sub}</div>
    </div>
  );
}

function RateBar({ label, rate, color }: { label: string; rate: number; color: string }) {
  const bg = color === 'green' ? 'bg-green-500' : 'bg-blue-500';
  return (
    <div>
      <div className="flex justify-between text-sm mb-1">
        <span>{label}</span>
        <span className="font-medium">{rate}%</span>
      </div>
      <div className="w-full bg-gray-200 rounded-full h-2.5">
        <div className={`${bg} h-2.5 rounded-full`} style={{ width: `${Math.min(rate, 100)}%` }} />
      </div>
    </div>
  );
}

function DevicesTab() {
  const [devices, setDevices] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getBVASDevices({ election_id: '1', limit: '50' })
      .then(d => setDevices(Array.isArray(d) ? d : []))
      .catch(e => console.error('bvas devices:', e))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-8 text-gray-500">Loading devices...</div>;

  const statusColor = (s: string) => {
    switch (s) {
      case 'active': return 'bg-green-100 text-green-700';
      case 'deployed': return 'bg-blue-100 text-blue-700';
      case 'offline': return 'bg-yellow-100 text-yellow-700';
      case 'faulty': return 'bg-red-100 text-red-700';
      default: return 'bg-gray-100 text-gray-700';
    }
  };

  return (
    <div className="bg-white rounded-lg shadow overflow-x-auto">
      <table className="min-w-full text-sm">
        <thead className="bg-gray-50">
          <tr>
            <th className="px-3 py-2 text-left">Device ID</th>
            <th className="px-3 py-2 text-left">Serial</th>
            <th className="px-3 py-2 text-left">Polling Unit</th>
            <th className="px-3 py-2 text-center">Status</th>
            <th className="px-3 py-2 text-right">Battery</th>
            <th className="px-3 py-2 text-left">Firmware</th>
            <th className="px-3 py-2 text-left">Last Sync</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {devices.map((d: any) => (
            <tr key={d.id} className="hover:bg-gray-50">
              <td className="px-3 py-2 font-mono text-xs">{d.id}</td>
              <td className="px-3 py-2 font-mono text-xs">{d.serial_number}</td>
              <td className="px-3 py-2">
                <div>{d.pu_name || '-'}</div>
                <div className="text-xs text-gray-400">{d.polling_unit_code}</div>
              </td>
              <td className="px-3 py-2 text-center">
                <span className={`text-xs px-1.5 py-0.5 rounded ${statusColor(d.status)}`}>{d.status}</span>
              </td>
              <td className="px-3 py-2 text-right">
                <span className={d.battery_level < 20 ? 'text-red-600 font-bold' : ''}>{d.battery_level}%</span>
              </td>
              <td className="px-3 py-2 text-xs">{d.firmware_version}</td>
              <td className="px-3 py-2 text-xs text-gray-500">{d.last_sync_at || '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
