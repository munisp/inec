import { useState, useEffect } from 'react';
import { api } from '../lib/api';

interface IntegrityResult {
  polling_unit_code: string;
  composite_score: number;
  benford_compliance: number;
  geofence_compliance: number;
  timing_score: number;
  observer_presence: number;
  anomaly_score: number;
  rating: string;
  total_votes: number;
}

interface HeatmapData {
  heatmap: IntegrityResult[];
  total: number;
}

export default function IntegrityScorePage() {
  const [data, setData] = useState<HeatmapData | null>(null);
  const [stateFilter, setStateFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    setLoading(true);
    api.getIntegrityHeatmap(1, stateFilter || undefined)
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [stateFilter]);

  const ratingColor = (rating: string) => {
    switch (rating) {
      case 'excellent': return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
      case 'good': return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
      case 'fair': return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-300';
      case 'poor': return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300';
      default: return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-300';
    }
  };

  const scoreBar = (score: number, label: string) => (
    <div className="flex items-center gap-2" title={`${label}: ${(score * 100).toFixed(0)}%`}>
      <span className="text-xs w-16 text-gray-500 dark:text-gray-400">{label}</span>
      <div className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
        <div className={`h-full rounded-full ${score >= 0.8 ? 'bg-green-500' : score >= 0.5 ? 'bg-yellow-500' : 'bg-red-500'}`}
          style={{ width: `${score * 100}%` }} />
      </div>
      <span className="text-xs w-8 text-right dark:text-gray-400">{(score * 100).toFixed(0)}%</span>
    </div>
  );

  const filtered = (data?.heatmap || []).filter(r =>
    !search || r.polling_unit_code.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="p-6 dark:text-white" role="main" aria-label="AI Integrity Score page">
      <div className="flex items-center justify-between mb-6 flex-wrap gap-4">
        <h1 className="text-2xl font-bold dark:text-white">AI Integrity Score Heatmap</h1>
        <div className="flex gap-2">
          <input type="text" placeholder="Search PU code..." value={search}
            onChange={e => setSearch(e.target.value)}
            className="px-3 py-2 border rounded-lg dark:bg-gray-800 dark:border-gray-600 dark:text-white text-sm"
            aria-label="Search by polling unit code" />
          <select value={stateFilter} onChange={e => setStateFilter(e.target.value)}
            className="px-3 py-2 border rounded-lg dark:bg-gray-800 dark:border-gray-600 dark:text-white text-sm"
            aria-label="Filter by state">
            <option value="">All States</option>
            {['FC', 'LA', 'KN', 'RV', 'OG', 'KD', 'AB', 'EN', 'OY', 'ED'].map(s => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        </div>
      </div>

      {loading && <div className="text-center py-20 text-gray-500" role="status">Loading integrity scores...</div>}

      {!loading && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6" role="region" aria-label="Summary statistics">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Total PUs Analyzed</p>
              <p className="text-2xl font-bold dark:text-white">{data?.total || 0}</p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Excellent</p>
              <p className="text-2xl font-bold text-green-600">{filtered.filter(r => r.rating === 'excellent').length}</p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Fair/Poor</p>
              <p className="text-2xl font-bold text-yellow-600">{filtered.filter(r => r.rating === 'fair' || r.rating === 'poor').length}</p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Avg Score</p>
              <p className="text-2xl font-bold dark:text-white">
                {filtered.length > 0 ? (filtered.reduce((s, r) => s + r.composite_score, 0) / filtered.length * 100).toFixed(0) : 0}%
              </p>
            </div>
          </div>

          <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
            <table className="w-full" role="table" aria-label="Integrity score details">
              <thead className="bg-gray-50 dark:bg-gray-700">
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">PU Code</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">Score</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">Rating</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">Breakdown</th>
                  <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">Votes</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-600">
                {filtered.slice(0, 50).map(r => (
                  <tr key={r.polling_unit_code} className="hover:bg-gray-50 dark:hover:bg-gray-700">
                    <td className="px-4 py-3 font-mono text-sm dark:text-gray-300">{r.polling_unit_code}</td>
                    <td className="px-4 py-3">
                      <span className="text-lg font-bold dark:text-white">{(r.composite_score * 100).toFixed(0)}%</span>
                    </td>
                    <td className="px-4 py-3">
                      <span className={`px-2 py-1 rounded-full text-xs font-medium ${ratingColor(r.rating)}`}>{r.rating}</span>
                    </td>
                    <td className="px-4 py-3 min-w-[200px]">
                      <div className="space-y-1">
                        {scoreBar(r.benford_compliance, 'Benford')}
                        {scoreBar(r.geofence_compliance, 'GeoFnc')}
                        {scoreBar(r.timing_score, 'Timing')}
                        {scoreBar(r.observer_presence, 'Observ')}
                        {scoreBar(r.anomaly_score, 'Anomly')}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-sm dark:text-gray-300">{r.total_votes.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {filtered.length === 0 && (
              <div className="text-center py-8 text-gray-500 dark:text-gray-400">No integrity data available</div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
