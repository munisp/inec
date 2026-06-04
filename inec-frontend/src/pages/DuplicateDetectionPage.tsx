import { useState } from 'react';
import { api } from '@/lib/api';

interface DuplicateCandidate {
  voter_a: string;
  voter_b: string;
  confidence: number;
  match_type: string;
  name_a?: string;
  name_b?: string;
}

interface ScanResult {
  total_duplicates: number;
  by_nin: DuplicateCandidate[];
  by_name_dob: DuplicateCandidate[];
  by_biometric: DuplicateCandidate[];
  by_phone: DuplicateCandidate[];
  scan_timestamp: string;
}

export default function DuplicateDetectionPage() {
  const [scanForm, setScanForm] = useState({ stateCode: '', modality: 'fingerprint' });
  const [scanning, setScanning] = useState(false);
  const [result, setResult] = useState<ScanResult | null>(null);
  const [resolveMsg, setResolveMsg] = useState('');
  const [error, setError] = useState('');

  const handleScan = async () => {
    setScanning(true); setResult(null); setError('');
    try {
      const res = await api.scanDuplicateVoters(scanForm.stateCode || undefined, scanForm.modality) as unknown as ScanResult;
      setResult(res);
    } catch (e: unknown) { setError(`Scan failed: ${(e as Error).message}`); }
    setScanning(false);
  };

  const handleResolve = async (voterA: string, voterB: string, decision: string) => {
    try {
      await api.resolveDuplicateVoter(voterA, voterB, decision);
      setResolveMsg(`${decision}: ${voterA} / ${voterB}`);
    } catch (e: unknown) { setResolveMsg(`Error: ${(e as Error).message}`); }
  };

  const allCandidates = result ? [
    ...result.by_nin.map(c => ({ ...c, category: 'NIN Match' })),
    ...result.by_name_dob.map(c => ({ ...c, category: 'Name+DOB Match' })),
    ...result.by_biometric.map(c => ({ ...c, category: 'Biometric' })),
    ...result.by_phone.map(c => ({ ...c, category: 'Phone Match' })),
  ] : [];

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

      {error && <p className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-3 rounded">{error}</p>}

      {/* Results */}
      {result && (
        <div className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {[
              { label: 'Total Duplicates', value: result.total_duplicates },
              { label: 'NIN Matches', value: result.by_nin.length },
              { label: 'Name+DOB Matches', value: result.by_name_dob.length },
              { label: 'Biometric Matches', value: result.by_biometric.length },
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
                <th className="text-left p-3 dark:text-gray-300">Voter A</th>
                <th className="text-left p-3 dark:text-gray-300">Voter B</th>
                <th className="text-left p-3 dark:text-gray-300">Confidence</th>
                <th className="text-left p-3 dark:text-gray-300">Match Type</th>
                <th className="text-left p-3 dark:text-gray-300">Actions</th>
              </tr></thead>
              <tbody>
                {allCandidates.map((c, i) => (
                  <tr key={i} className="border-b dark:border-gray-700">
                    <td className="p-3 font-mono text-xs dark:text-gray-300">{c.voter_a}</td>
                    <td className="p-3 font-mono text-xs dark:text-gray-300">{c.voter_b}</td>
                    <td className="p-3"><span className={`px-2 py-0.5 rounded text-xs ${c.confidence >= 0.95 ? 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-300' : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-300'}`}>{(c.confidence * 100).toFixed(1)}%</span></td>
                    <td className="p-3 dark:text-gray-300">{c.category}</td>
                    <td className="p-3 space-x-2">
                      <button onClick={() => handleResolve(c.voter_a, c.voter_b, 'merge')} className="text-blue-600 hover:underline text-xs">Merge</button>
                      <button onClick={() => handleResolve(c.voter_a, c.voter_b, 'dismiss')} className="text-gray-500 hover:underline text-xs">Dismiss</button>
                      <button onClick={() => handleResolve(c.voter_a, c.voter_b, 'flag')} className="text-orange-600 hover:underline text-xs">Flag</button>
                    </td>
                  </tr>
                ))}
                {allCandidates.length === 0 && (
                  <tr><td colSpan={5} className="p-4 text-center text-gray-500 dark:text-gray-400">No duplicate voters found</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
