import { useState, useEffect, useCallback } from 'react';
import { api } from '../lib/api';
import logger from '../lib/logger';

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

export default function CommandCenterPage() {
  const [data, setData] = useState<{
    states: StateVelocity[];
    alerts: Alert[];
    overall_pus: number;
    reported_pus: number;
    completion_pct: number;
    load_shedding: number;
  } | null>(null);
  const [loadLevel, setLoadLevel] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      const res = await api.getCommandCenterLive();
      setData(res);
      setLoadLevel(res.load_shedding || 0);
    } catch (e) {
      logger.error('Command center fetch failed', e);
    }
  }, []);

  useEffect(() => {
    fetchData();
    if (!autoRefresh) return;
    const interval = setInterval(fetchData, 5000);
    return () => clearInterval(interval);
  }, [fetchData, autoRefresh]);

  const handleLoadShed = async (level: number) => {
    try {
      await api.setLoadShedding(level);
      setLoadLevel(level);
    } catch (e) {
      logger.error('Load shedding update failed', e);
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
      case 'EMERGENCY': return 'bg-red-600 text-white';
      case 'CRITICAL': return 'bg-red-500 text-white';
      case 'WARN': return 'bg-yellow-500 text-black';
      default: return 'bg-blue-500 text-white';
    }
  };

  return (
    <div className="p-6 max-w-7xl mx-auto" role="main" aria-label="Election Command Center">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold dark:text-white">Election Command Center</h1>
          <p className="text-gray-500 dark:text-gray-400">War Room — Real-time election monitoring</p>
        </div>
        <label className="flex items-center gap-2 text-sm dark:text-gray-300">
          <input type="checkbox" checked={autoRefresh} onChange={(e) => setAutoRefresh(e.target.checked)} className="rounded" />
          Auto-refresh (5s)
        </label>
      </div>

      {/* Overall Stats */}
      {data && (
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
            <p className="text-sm text-gray-500 dark:text-gray-400">Total PUs</p>
            <p className="text-2xl font-bold dark:text-white">{data.overall_pus?.toLocaleString()}</p>
          </div>
          <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
            <p className="text-sm text-gray-500 dark:text-gray-400">Reported</p>
            <p className="text-2xl font-bold text-green-600">{data.reported_pus?.toLocaleString()}</p>
          </div>
          <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
            <p className="text-sm text-gray-500 dark:text-gray-400">Completion</p>
            <p className="text-2xl font-bold text-blue-600">{data.completion_pct?.toFixed(1)}%</p>
          </div>
          <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
            <p className="text-sm text-gray-500 dark:text-gray-400">Load Shedding</p>
            <div className="flex gap-1 mt-1">
              {[0, 1, 2, 3].map((l) => (
                <button key={l} onClick={() => handleLoadShed(l)}
                  className={`px-3 py-1 rounded text-sm font-medium ${loadLevel === l ? 'bg-blue-600 text-white' : 'bg-gray-200 dark:bg-gray-700 dark:text-gray-300'}`}>
                  {l === 0 ? 'Off' : `L${l}`}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Alerts */}
      {data?.alerts && data.alerts.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-2 dark:text-white">Active Alerts</h2>
          <div className="space-y-2">
            {data.alerts.map((alert, i) => (
              <div key={i} className={`p-3 rounded-lg ${levelColor(alert.level)}`}>
                <span className="font-bold">[{alert.level}]</span> {alert.message}
                {alert.auto_action && <span className="ml-2 opacity-75">→ {alert.auto_action}</span>}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* State Velocity Table */}
      {data?.states && (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
          <h2 className="text-lg font-semibold p-4 border-b dark:border-gray-700 dark:text-white">State Velocities</h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm" aria-label="State reporting progress">
              <thead className="bg-gray-50 dark:bg-gray-700">
                <tr>
                  <th className="text-left p-3 dark:text-gray-300">State</th>
                  <th className="text-right p-3 dark:text-gray-300">Total PUs</th>
                  <th className="text-right p-3 dark:text-gray-300">Reported</th>
                  <th className="text-right p-3 dark:text-gray-300">Stalled</th>
                  <th className="text-center p-3 dark:text-gray-300">Progress</th>
                  <th className="text-center p-3 dark:text-gray-300">Status</th>
                  <th className="text-center p-3 dark:text-gray-300">ETA</th>
                </tr>
              </thead>
              <tbody>
                {data.states.map((s) => (
                  <tr key={s.state_code} className="border-t dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-750">
                    <td className="p-3 font-medium dark:text-white">{s.state_name || s.state_code}</td>
                    <td className="p-3 text-right dark:text-gray-300">{s.total_pus.toLocaleString()}</td>
                    <td className="p-3 text-right dark:text-gray-300">{s.reported_pus.toLocaleString()}</td>
                    <td className="p-3 text-right dark:text-gray-300">{s.stalled_pus.toLocaleString()}</td>
                    <td className="p-3">
                      <div className="w-full bg-gray-200 dark:bg-gray-600 rounded-full h-2">
                        <div className={`h-2 rounded-full ${statusColor(s.status)}`} style={{ width: `${Math.min(s.completion_pct, 100)}%` }} />
                      </div>
                      <p className="text-xs text-center mt-1 dark:text-gray-400">{s.completion_pct.toFixed(1)}%</p>
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
        </div>
      )}
    </div>
  );
}
