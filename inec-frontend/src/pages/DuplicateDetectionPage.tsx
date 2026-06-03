import { useState } from 'react';
import { api } from '@/lib/api';

interface DuplicateCandidate {
  source_vin: string;
  candidate_vin: string;
  match_score: number;
  modality: string;
  source_name?: string;
  candidate_name?: string;
  source_state?: string;
  candidate_state?: string;
}

interface ScanResult {
  scan_id: string;
  total_scanned: number;
  duplicates_found: number;
  candidates: DuplicateCandidate[];
}

export default function DuplicateDetectionPage() {
  const [scanForm, setScanForm] = useState({ stateCode: '', modality: 'fingerprint' });
  const [scanning, setScanning] = useState(false);
  const [result, setResult] = useState<ScanResult | null>(null);
  const [resolveMsg, setResolveMsg] = useState('');

  const handleScan = async () => {
    setScanning(true); setResult(null);
    try {
      const res = await api.scanDuplicateVoters(scanForm.stateCode || undefined, scanForm.modality) as unknown as ScanResult;
      setResult(res);
    } catch {}
    setScanning(false);
  };

  const handleResolve = async (sourceVin: string, candidateVin: string, action: string) => {
    try {
      await api.resolveDuplicateVoter(sourceVin, candidateVin, action);
      setResolveMsg(`${action}: ${sourceVin} / ${candidateVin}`);
      if (result) {
        setResult({ ...result, candidates: result.candidates.filter(c => !(c.source_vin === sourceVin && c.candidate_vin === candidateVin)) });
      }
    } catch (e: unknown) { setResolveMsg(`Error: ${(e as Error).message}`); }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold dark:text-white">Duplicate Voter Detection</h1>

      {/* Scan Form */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
        <h2 className="text-lg font-semibold mb-4 dark:text-white">Run Deduplication Scan</h2>
        <div className="flex flex-wrap gap-3 items-end">
          <div>
            <label className="block text-sm text-gray-500 dark:text-gray-400 mb-1">State Code (optional)</label>
            <input className="border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="e.g. KN, LA" value={scanForm.stateCode} onChange={e => setScanForm({ ...scanForm, stateCode: e.target.value })} />
          </div>
          <div>
            <label className="block text-sm text-gray-500 dark:text-gray-400 mb-1">Biometric Modality</label>
            <select className="border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" value={scanForm.modality} onChange={e => setScanForm({ ...scanForm, modality: e.target.value })}>
              <option value="fingerprint">Fingerprint</option>
              <option value="facial">Facial</option>
              <option value="iris">Iris</option>
            </select>
          </div>
          <button onClick={handleScan} disabled={scanning} className="bg-green-600 text-white px-6 py-2 rounded hover:bg-green-700 disabled:opacity-50">
            {scanning ? 'Scanning...' : 'Start Scan'}
          </button>
        </div>
      </div>

      {/* Results */}
      {result && (
        <div className="space-y-4">
          <div className="grid grid-cols-3 gap-4">
            {[
              { label: 'Total Scanned', value: result.total_scanned },
              { label: 'Duplicates Found', value: result.duplicates_found },
              { label: 'Match Rate', value: result.total_scanned > 0 ? `${((result.duplicates_found / result.total_scanned) * 100).toFixed(2)}%` : '0%' },
            ].map(s => (
              <div key={s.label} className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
                <p className="text-sm text-gray-500 dark:text-gray-400">{s.label}</p>
                <p className="text-2xl font-bold dark:text-white">{s.value}</p>
              </div>
            ))}
          </div>

          {resolveMsg && <p className="text-sm text-blue-600 dark:text-blue-400">{resolveMsg}</p>}

          <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="border-b dark:border-gray-600">
                <th className="text-left p-3 dark:text-gray-300">Source VIN</th>
                <th className="text-left p-3 dark:text-gray-300">Candidate VIN</th>
                <th className="text-left p-3 dark:text-gray-300">Score</th>
                <th className="text-left p-3 dark:text-gray-300">Modality</th>
                <th className="text-left p-3 dark:text-gray-300">Actions</th>
              </tr></thead>
              <tbody>
                {result.candidates.map((c, i) => (
                  <tr key={i} className="border-b dark:border-gray-700">
                    <td className="p-3 font-mono text-xs dark:text-gray-300">{c.source_vin}</td>
                    <td className="p-3 font-mono text-xs dark:text-gray-300">{c.candidate_vin}</td>
                    <td className="p-3"><span className={`px-2 py-0.5 rounded text-xs ${c.match_score >= 0.95 ? 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-300' : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-300'}`}>{(c.match_score * 100).toFixed(1)}%</span></td>
                    <td className="p-3 dark:text-gray-300">{c.modality}</td>
                    <td className="p-3 space-x-2">
                      <button onClick={() => handleResolve(c.source_vin, c.candidate_vin, 'merge')} className="text-blue-600 hover:underline text-xs">Merge</button>
                      <button onClick={() => handleResolve(c.source_vin, c.candidate_vin, 'dismiss')} className="text-gray-500 hover:underline text-xs">Dismiss</button>
                      <button onClick={() => handleResolve(c.source_vin, c.candidate_vin, 'flag')} className="text-orange-600 hover:underline text-xs">Flag</button>
                    </td>
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
