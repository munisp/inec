import { useState, useEffect } from 'react';

interface KYCResult {
  user_id: number;
  status: string;
  identity_match_score: number;
  document_verified: boolean;
  face_match_score: number;
  liveness_passed: boolean;
  risk_score: number;
  checks_performed: string[];
  flags: string[];
  verification_timestamp: string;
}

interface LivenessResult {
  user_id: number;
  passed: boolean;
  confidence: number;
  method: string;
  anti_spoofing_score: number;
  checks: Array<{ name: string; passed: boolean; value?: number; note?: string }>;
  timestamp: string;
}

const API = '/kyc';

export default function KYCVerificationPage() {
  const [kycStatus, setKycStatus] = useState<KYCResult | null>(null);
  const [livenessResult, setLivenessResult] = useState<LivenessResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [idFile, setIdFile] = useState<File | null>(null);
  const [selfieFile, setSelfieFile] = useState<File | null>(null);
  const [idType, setIdType] = useState('nin');
  const [idNumber, setIdNumber] = useState('');
  const [step, setStep] = useState<'status' | 'upload' | 'liveness' | 'result'>('status');

  useEffect(() => {
    fetch(`${API}/status?user_id=1`, { headers: { Authorization: `Bearer ${localStorage.getItem('token')}` } })
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d) setKycStatus(d); })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleVerify = async () => {
    if (!idFile || !selfieFile || !idNumber) return;
    setUploading(true);
    const form = new FormData();
    form.append('id_document', idFile);
    form.append('selfie', selfieFile);
    form.append('id_type', idType);
    form.append('id_number', idNumber);
    try {
      const res = await fetch(`${API}/verify`, {
        method: 'POST', body: form,
        headers: { Authorization: `Bearer ${localStorage.getItem('token')}` },
      });
      if (res.ok) {
        const data = await res.json();
        setKycStatus(data);
        setStep('result');
      }
    } catch { /* */ }
    setUploading(false);
  };

  const handleLiveness = async () => {
    if (!selfieFile) return;
    setUploading(true);
    const form = new FormData();
    form.append('selfie', selfieFile);
    try {
      const res = await fetch(`${API}/liveness`, {
        method: 'POST', body: form,
        headers: { Authorization: `Bearer ${localStorage.getItem('token')}` },
      });
      if (res.ok) {
        const data = await res.json();
        setLivenessResult(data);
      }
    } catch { /* */ }
    setUploading(false);
  };

  const statusColor = (s: string) => s === 'verified' ? 'text-green-600 bg-green-50' : s === 'pending_review' ? 'text-yellow-600 bg-yellow-50' : 'text-red-600 bg-red-50';
  const statusIcon = (s: string) => s === 'verified' ? '✓' : s === 'pending_review' ? '◔' : '✕';

  if (loading) return <div className="animate-pulse space-y-4 p-6">{[1,2,3].map(i => <div key={i} className="h-24 bg-zinc-100 dark:bg-zinc-800 rounded-xl" />)}</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900 dark:text-zinc-100">KYC Verification</h1>
          <p className="text-sm text-zinc-500 dark:text-zinc-400">Identity verification for platform users</p>
        </div>
        {kycStatus && (
          <span className={`px-3 py-1.5 rounded-full text-sm font-semibold ${statusColor(kycStatus.status)}`}>
            {statusIcon(kycStatus.status)} {kycStatus.status.replace(/_/g, ' ')}
          </span>
        )}
      </div>

      {/* Step indicators */}
      <div className="flex items-center gap-2">
        {['status', 'upload', 'liveness', 'result'].map((s, i) => (
          <div key={s} className="flex items-center gap-2">
            <button
              onClick={() => setStep(s as typeof step)}
              className={`w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold transition-colors ${
                step === s ? 'bg-green-700 text-white' : 'bg-zinc-100 dark:bg-zinc-800 text-zinc-400'
              }`}
            >{i + 1}</button>
            {i < 3 && <div className={`w-8 h-0.5 ${step === s ? 'bg-green-700' : 'bg-zinc-200 dark:bg-zinc-700'}`} />}
          </div>
        ))}
      </div>

      {step === 'status' && kycStatus && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
            <h3 className="text-sm font-semibold text-zinc-500 mb-3">Verification Scores</h3>
            {[
              { label: 'Identity Match', value: kycStatus.identity_match_score, color: 'green' },
              { label: 'Face Match', value: kycStatus.face_match_score, color: 'blue' },
              { label: 'Risk Score', value: kycStatus.risk_score, color: 'red' },
            ].map(m => (
              <div key={m.label} className="mb-3">
                <div className="flex justify-between text-sm mb-1">
                  <span className="text-zinc-600 dark:text-zinc-400">{m.label}</span>
                  <span className="font-semibold">{(m.value * 100).toFixed(1)}%</span>
                </div>
                <div className="h-2 bg-zinc-100 dark:bg-zinc-800 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full bg-${m.color}-500`} style={{ width: `${m.value * 100}%` }} />
                </div>
              </div>
            ))}
          </div>
          <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
            <h3 className="text-sm font-semibold text-zinc-500 mb-3">Check Results</h3>
            <div className="space-y-2">
              <div className="flex justify-between"><span className="text-sm text-zinc-600 dark:text-zinc-400">Document Verified</span><span className={kycStatus.document_verified ? 'text-green-600' : 'text-red-600'}>{kycStatus.document_verified ? 'Yes' : 'No'}</span></div>
              <div className="flex justify-between"><span className="text-sm text-zinc-600 dark:text-zinc-400">Liveness Passed</span><span className={kycStatus.liveness_passed ? 'text-green-600' : 'text-red-600'}>{kycStatus.liveness_passed ? 'Yes' : 'No'}</span></div>
            </div>
            {kycStatus.flags.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-1">
                {kycStatus.flags.map((f, i) => <span key={i} className="px-2 py-0.5 bg-red-50 text-red-600 text-xs rounded-full font-medium">{f}</span>)}
              </div>
            )}
          </div>
          <div className="md:col-span-2 flex gap-3">
            <button onClick={() => setStep('upload')} className="px-4 py-2 bg-green-700 text-white rounded-lg text-sm font-semibold hover:bg-green-800 transition">Re-verify Identity</button>
          </div>
        </div>
      )}

      {step === 'status' && !kycStatus && (
        <div className="bg-white dark:bg-zinc-900 rounded-xl p-8 border border-zinc-200 dark:border-zinc-700 text-center">
          <div className="w-16 h-16 rounded-full bg-green-50 flex items-center justify-center mx-auto mb-4"><span className="text-3xl">🛡️</span></div>
          <h3 className="text-lg font-bold mb-2">Start Verification</h3>
          <p className="text-sm text-zinc-500 mb-4">Upload your government-issued ID and a selfie to verify your identity.</p>
          <button onClick={() => setStep('upload')} className="px-6 py-2.5 bg-green-700 text-white rounded-lg font-semibold hover:bg-green-800 transition">Begin KYC</button>
        </div>
      )}

      {step === 'upload' && (
        <div className="bg-white dark:bg-zinc-900 rounded-xl p-6 border border-zinc-200 dark:border-zinc-700 space-y-4">
          <h3 className="text-lg font-bold">Upload Documents</h3>
          <div>
            <label className="block text-sm font-medium text-zinc-700 dark:text-zinc-300 mb-1">ID Type</label>
            <select value={idType} onChange={e => setIdType(e.target.value)} className="w-full border border-zinc-300 dark:border-zinc-600 rounded-lg px-3 py-2 bg-white dark:bg-zinc-800 text-sm">
              <option value="nin">NIN (National Identification Number)</option>
              <option value="voters_card">Voter's Card</option>
              <option value="passport">International Passport</option>
              <option value="drivers_license">Driver's License</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-zinc-700 dark:text-zinc-300 mb-1">ID Number</label>
            <input type="text" value={idNumber} onChange={e => setIdNumber(e.target.value)} placeholder="Enter your ID number" className="w-full border border-zinc-300 dark:border-zinc-600 rounded-lg px-3 py-2 bg-white dark:bg-zinc-800 text-sm" />
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-zinc-700 dark:text-zinc-300 mb-1">ID Document Photo</label>
              <label className="flex flex-col items-center justify-center w-full h-32 border-2 border-dashed border-zinc-300 dark:border-zinc-600 rounded-xl cursor-pointer hover:bg-zinc-50 dark:hover:bg-zinc-800 transition">
                <span className="text-2xl mb-1">{idFile ? '✓' : '📄'}</span>
                <span className="text-sm text-zinc-500">{idFile ? idFile.name : 'Click to upload'}</span>
                <input type="file" accept="image/*" className="hidden" onChange={e => setIdFile(e.target.files?.[0] || null)} />
              </label>
            </div>
            <div>
              <label className="block text-sm font-medium text-zinc-700 dark:text-zinc-300 mb-1">Selfie Photo</label>
              <label className="flex flex-col items-center justify-center w-full h-32 border-2 border-dashed border-zinc-300 dark:border-zinc-600 rounded-xl cursor-pointer hover:bg-zinc-50 dark:hover:bg-zinc-800 transition">
                <span className="text-2xl mb-1">{selfieFile ? '✓' : '🤳'}</span>
                <span className="text-sm text-zinc-500">{selfieFile ? selfieFile.name : 'Click to upload'}</span>
                <input type="file" accept="image/*" className="hidden" onChange={e => setSelfieFile(e.target.files?.[0] || null)} />
              </label>
            </div>
          </div>
          <div className="flex gap-3">
            <button onClick={handleVerify} disabled={uploading || !idFile || !selfieFile || !idNumber} className="px-4 py-2 bg-green-700 text-white rounded-lg text-sm font-semibold hover:bg-green-800 transition disabled:opacity-50 disabled:cursor-not-allowed">
              {uploading ? 'Verifying...' : 'Verify Identity'}
            </button>
            <button onClick={() => { setStep('liveness'); handleLiveness(); }} disabled={!selfieFile} className="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-semibold hover:bg-purple-700 transition disabled:opacity-50 disabled:cursor-not-allowed">
              Liveness Check
            </button>
          </div>
        </div>
      )}

      {step === 'liveness' && livenessResult && (
        <div className="bg-white dark:bg-zinc-900 rounded-xl p-6 border border-zinc-200 dark:border-zinc-700">
          <div className="flex items-center gap-3 mb-4">
            <div className={`w-10 h-10 rounded-full flex items-center justify-center ${livenessResult.passed ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
              {livenessResult.passed ? '✓' : '✕'}
            </div>
            <div>
              <h3 className="font-bold text-lg">Liveness: {livenessResult.passed ? 'PASSED' : 'FAILED'}</h3>
              <p className="text-sm text-zinc-500">Confidence: {(livenessResult.confidence * 100).toFixed(1)}% | Anti-Spoofing: {(livenessResult.anti_spoofing_score * 100).toFixed(1)}%</p>
            </div>
          </div>
          <div className="space-y-2">
            {livenessResult.checks.map((c, i) => (
              <div key={i} className="flex items-center gap-2">
                <span className={c.passed ? 'text-green-600' : 'text-red-600'}>{c.passed ? '✓' : '✕'}</span>
                <span className="text-sm">{c.name}</span>
                {c.value !== undefined && <span className="text-xs text-zinc-400 ml-auto">{c.value.toFixed(2)}</span>}
              </div>
            ))}
          </div>
        </div>
      )}

      {step === 'result' && kycStatus && (
        <div className={`bg-white dark:bg-zinc-900 rounded-xl p-6 border-2 ${kycStatus.status === 'verified' ? 'border-green-300' : kycStatus.status === 'pending_review' ? 'border-yellow-300' : 'border-red-300'}`}>
          <div className="text-center mb-4">
            <div className="text-4xl mb-2">{kycStatus.status === 'verified' ? '🎉' : kycStatus.status === 'pending_review' ? '⏳' : '❌'}</div>
            <h3 className="text-xl font-bold">{kycStatus.status.replace(/_/g, ' ').toUpperCase()}</h3>
          </div>
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div className="bg-zinc-50 dark:bg-zinc-800 rounded-lg p-3"><span className="text-zinc-500 block">Face Match</span><span className="font-bold">{(kycStatus.face_match_score * 100).toFixed(1)}%</span></div>
            <div className="bg-zinc-50 dark:bg-zinc-800 rounded-lg p-3"><span className="text-zinc-500 block">Identity Match</span><span className="font-bold">{(kycStatus.identity_match_score * 100).toFixed(1)}%</span></div>
            <div className="bg-zinc-50 dark:bg-zinc-800 rounded-lg p-3"><span className="text-zinc-500 block">Risk Score</span><span className="font-bold">{(kycStatus.risk_score * 100).toFixed(0)}%</span></div>
            <div className="bg-zinc-50 dark:bg-zinc-800 rounded-lg p-3"><span className="text-zinc-500 block">Document</span><span className="font-bold">{kycStatus.document_verified ? 'Verified' : 'Unverified'}</span></div>
          </div>
          <button onClick={() => setStep('status')} className="mt-4 w-full py-2 bg-zinc-100 dark:bg-zinc-800 rounded-lg text-sm font-semibold hover:bg-zinc-200 dark:hover:bg-zinc-700 transition">Back to Status</button>
        </div>
      )}
    </div>
  );
}
