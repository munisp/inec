import { useState } from 'react';
import { api } from '@/lib/api';

type ExportType = 'results' | 'voters' | 'collation' | 'audit';

interface ExportConfig {
  type: ExportType;
  label: string;
  description: string;
  params: Record<string, string>;
}

const EXPORTS: ExportConfig[] = [
  { type: 'results', label: 'Election Results', description: 'All submitted results with party scores, validation status, and timestamps', params: {} },
  { type: 'voters', label: 'Voter Registry', description: 'Registered voters with demographics and polling unit assignments (admin only)', params: {} },
  { type: 'collation', label: 'Collation Data', description: 'Aggregated results by ward, LGA, state, and national levels', params: {} },
  { type: 'audit', label: 'Audit Trail', description: 'Complete audit log of all system actions with user attribution', params: {} },
];

export default function ExportCenterPage() {
  const [downloading, setDownloading] = useState<string | null>(null);
  const [results, setResults] = useState<Record<string, unknown>>({});
  const [electionId, setElectionId] = useState('1');
  const [stateCode, setStateCode] = useState('');
  const [collationLevel, setCollationLevel] = useState('state');
  const [auditStart, setAuditStart] = useState('');
  const [auditEnd, setAuditEnd] = useState('');

  const handleExport = async (type: ExportType) => {
    setDownloading(type);
    try {
      let data: unknown;
      switch (type) {
        case 'results': data = await api.exportResults(parseInt(electionId)); break;
        case 'voters': data = await api.exportVoters(stateCode || undefined); break;
        case 'collation': data = await api.exportCollation(parseInt(electionId), collationLevel); break;
        case 'audit': data = await api.exportAudit(auditStart || undefined, auditEnd || undefined); break;
      }
      setResults(prev => ({ ...prev, [type]: data }));

      // Auto-download as JSON
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `inec-${type}-export-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {}
    setDownloading(null);
  };

  const downloadCSV = (type: string) => {
    const token = localStorage.getItem('token') || localStorage.getItem('inec_token');
    const apiUrl = import.meta.env.VITE_API_URL ?? '';
    if (type === 'polling-units-csv') {
      window.open(`${apiUrl}/geo/reports/polling-units.csv?token=${token}`, '_blank');
    } else if (type === 'polling-units-geojson') {
      window.open(`${apiUrl}/geo/reports/polling-units.geojson?token=${token}`, '_blank');
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold dark:text-white">Export Center</h1>
      <p className="text-gray-500 dark:text-gray-400">Download election data in JSON format. All exports include real-time data from the database.</p>

      {/* Global Filters */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow flex flex-wrap gap-4 items-end">
        <div>
          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Election ID</label>
          <input className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white w-24" value={electionId} onChange={e => setElectionId(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">State Code (voters)</label>
          <input className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white w-24" placeholder="e.g. KN" value={stateCode} onChange={e => setStateCode(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Collation Level</label>
          <select className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white" value={collationLevel} onChange={e => setCollationLevel(e.target.value)}>
            <option value="ward">Ward</option>
            <option value="lga">LGA</option>
            <option value="state">State</option>
            <option value="national">National</option>
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Audit Start</label>
          <input type="date" className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white" value={auditStart} onChange={e => setAuditStart(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Audit End</label>
          <input type="date" className="border dark:border-gray-600 rounded px-3 py-1.5 text-sm dark:bg-gray-700 dark:text-white" value={auditEnd} onChange={e => setAuditEnd(e.target.value)} />
        </div>
      </div>

      {/* Export Cards */}
      <div className="grid md:grid-cols-2 gap-4">
        {EXPORTS.map(exp => (
          <div key={exp.type} className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
            <h3 className="text-lg font-semibold dark:text-white">{exp.label}</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1 mb-4">{exp.description}</p>
            <button onClick={() => handleExport(exp.type)} disabled={downloading === exp.type}
              className="bg-green-600 text-white px-4 py-2 rounded hover:bg-green-700 disabled:opacity-50 text-sm">
              {downloading === exp.type ? 'Exporting...' : 'Download JSON'}
            </button>
            {results[exp.type] && <p className="text-xs text-green-600 dark:text-green-400 mt-2">Export ready — file downloaded.</p>}
          </div>
        ))}
      </div>

      {/* Geo Exports */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
        <h3 className="text-lg font-semibold dark:text-white mb-3">Geographic Data Exports</h3>
        <div className="flex gap-3">
          <button onClick={() => downloadCSV('polling-units-csv')} className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 text-sm">Polling Units CSV</button>
          <button onClick={() => downloadCSV('polling-units-geojson')} className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 text-sm">Polling Units GeoJSON</button>
        </div>
      </div>
    </div>
  );
}
