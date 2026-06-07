import { useState, useEffect } from 'react';
import { api } from '@/lib/api';

interface GeofenceStats {
  total_checks: number;
  within_fence: number;
  outside_fence: number;
  violation_rate: number;
  by_state?: Array<{ state: string; violations: number }>;
}

interface CheckResult {
  within_geofence: boolean;
  distance_m: number;
  polling_unit?: string;
  message: string;
}

export default function GeofencingPage() {
  const [stats, setStats] = useState<GeofenceStats | null>(null);
  const [checkResult, setCheckResult] = useState<CheckResult | null>(null);
  const [form, setForm] = useState({ lat: '', lng: '', puCode: '', bvasSerial: '' });
  const [spoofForm, setSpoofForm] = useState({ lat: '', lng: '', deviceId: '', accuracy: '' });
  const [spoofResult, setSpoofResult] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => { api.getGeofenceStats(1).then(setStats).catch(() => {}); }, []);

  const handleCheck = async () => {
    if (!form.lat || !form.lng || !form.puCode) return;
    setLoading(true);
    try {
      const res = await api.geofenceCheck(parseFloat(form.lat), parseFloat(form.lng), form.puCode, form.bvasSerial || undefined) as unknown as CheckResult;
      setCheckResult(res);
    } catch (e: unknown) { setCheckResult({ within_geofence: false, distance_m: 0, message: (e as Error).message }); }
    setLoading(false);
  };

  const handleSpoofCheck = async () => {
    if (!spoofForm.lat || !spoofForm.lng || !spoofForm.deviceId) return;
    setLoading(true);
    try {
      const res = await api.gpsSpoofCheck(
        parseFloat(spoofForm.lat), parseFloat(spoofForm.lng),
        spoofForm.deviceId, spoofForm.accuracy ? parseFloat(spoofForm.accuracy) : undefined
      ) as unknown as Record<string, unknown>;
      setSpoofResult(res);
    } catch (e: unknown) { setSpoofResult({ error: (e as Error).message }); }
    setLoading(false);
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold dark:text-white">Geofencing & GPS Verification</h1>

      {/* Stats */}
      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {[
            { label: 'Total Checks', value: stats.total_checks, color: 'blue' },
            { label: 'Within Fence', value: stats.within_fence, color: 'green' },
            { label: 'Outside Fence', value: stats.outside_fence, color: 'red' },
            { label: 'Violation Rate', value: `${((stats.violation_rate || 0) * 100).toFixed(1)}%`, color: stats.violation_rate > 0.1 ? 'red' : 'green' },
          ].map(s => (
            <div key={s.label} className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">{s.label}</p>
              <p className={`text-2xl font-bold ${s.color === 'red' ? 'text-red-600' : s.color === 'green' ? 'text-green-600' : 'dark:text-white'}`}>{s.value}</p>
            </div>
          ))}
        </div>
      )}
      {/* Compliance Progress Bar */}
      {stats && stats.total_checks > 0 && (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <div className="flex justify-between text-sm mb-2">
            <span className="text-gray-600 dark:text-gray-400">Geofence Compliance</span>
            <span className="font-semibold dark:text-white">{((1 - (stats.violation_rate || 0)) * 100).toFixed(1)}%</span>
          </div>
          <div className="w-full h-3 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
            <div className={`h-full rounded-full transition-all ${(stats.violation_rate || 0) > 0.1 ? 'bg-red-500' : 'bg-green-500'}`}
              style={{ width: `${((1 - (stats.violation_rate || 0)) * 100)}%` }} />
          </div>
          <div className="flex justify-between text-xs text-gray-400 mt-1">
            <span>{stats.within_fence} compliant</span>
            <span>{stats.outside_fence} violations</span>
          </div>
        </div>
      )}

      <div className="grid md:grid-cols-2 gap-6">
        {/* Geofence Check */}
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">Geofence Check</h2>
          <div className="space-y-3">
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Latitude (e.g. 9.0579)" value={form.lat} onChange={e => setForm({ ...form, lat: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Longitude (e.g. 7.4951)" value={form.lng} onChange={e => setForm({ ...form, lng: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Polling Unit Code" value={form.puCode} onChange={e => setForm({ ...form, puCode: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="BVAS Serial (e.g. BVAS-001)" value={form.bvasSerial} onChange={e => setForm({ ...form, bvasSerial: e.target.value })} />
            <button onClick={handleCheck} disabled={loading} className="w-full bg-green-600 text-white rounded py-2 hover:bg-green-700 disabled:opacity-50">
              {loading ? 'Checking...' : 'Check Location'}
            </button>
          </div>
          {checkResult && (
            <div className={`mt-4 p-3 rounded ${checkResult.within_geofence ? 'bg-green-50 dark:bg-green-900/30 text-green-800 dark:text-green-300' : 'bg-red-50 dark:bg-red-900/30 text-red-800 dark:text-red-300'}`}>
              <p className="font-medium">{checkResult.within_geofence ? '✓ Within Geofence' : '✗ Outside Geofence'}</p>
              <p className="text-sm">Distance: {checkResult.distance_m?.toFixed(1)}m — {checkResult.message}</p>
            </div>
          )}
        </div>

        {/* GPS Spoof Detection */}
        <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
          <h2 className="text-lg font-semibold mb-4 dark:text-white">GPS Spoof Detection</h2>
          <div className="space-y-3">
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Latitude" value={spoofForm.lat} onChange={e => setSpoofForm({ ...spoofForm, lat: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Longitude" value={spoofForm.lng} onChange={e => setSpoofForm({ ...spoofForm, lng: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Device ID" value={spoofForm.deviceId} onChange={e => setSpoofForm({ ...spoofForm, deviceId: e.target.value })} />
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="GPS Accuracy (optional)" value={spoofForm.accuracy} onChange={e => setSpoofForm({ ...spoofForm, accuracy: e.target.value })} />
            <button onClick={handleSpoofCheck} disabled={loading} className="w-full bg-orange-600 text-white rounded py-2 hover:bg-orange-700 disabled:opacity-50">
              {loading ? 'Analyzing...' : 'Check for Spoofing'}
            </button>
          </div>
          {spoofResult && (
            <div className="mt-4 p-3 rounded bg-gray-50 dark:bg-gray-700">
              <pre className="text-xs overflow-auto dark:text-gray-300">{JSON.stringify(spoofResult, null, 2)}</pre>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
