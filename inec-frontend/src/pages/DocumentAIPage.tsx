import { useState } from 'react';
import { api } from '@/lib/api';

interface AnalysisResult {
  report_id: number;
  status: string;
  ocr_confidence?: number;
  tampering_detected?: boolean;
  ocr?: {
    serial_number: string | null;
    polling_unit_code: string | null;
    party_results: Array<{ party_code: string; votes: number; confidence: number }>;
    total_valid_votes: number | null;
    confidence_score: number;
    extraction_warnings: string[];
  };
  vlm?: {
    is_valid_ec8a: boolean;
    tampering_detected: boolean;
    tampering_confidence: number;
    tampering_indicators: string[];
    document_quality: string;
    completeness_score: number;
    analysis_summary: string;
  };
  combined_confidence?: number;
  requires_manual_review?: boolean;
}

export default function DocumentAIPage() {
  const [reportId, setReportId] = useState('');
  const [result, setResult] = useState<AnalysisResult | null>(null);
  const [statusResult, setStatusResult] = useState<AnalysisResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleAnalyze = async () => {
    if (!reportId) return;
    setLoading(true); setResult(null); setError('');
    try {
      const res = await api.analyzeDocument(parseInt(reportId)) as unknown as AnalysisResult;
      setResult(res);
    } catch (e: unknown) { setError(`Analysis failed: ${(e as Error).message}`); }
    setLoading(false);
  };

  const handleCheckStatus = async () => {
    if (!reportId) return;
    setError('');
    try {
      const res = await api.getDocumentAnalysisStatus(parseInt(reportId)) as unknown as AnalysisResult;
      setStatusResult(res);
    } catch (e: unknown) { setError(`Status check failed: ${(e as Error).message}`); }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold dark:text-white">Document AI Analysis</h1>
      <p className="text-gray-500 dark:text-gray-400">Analyze election documents (EC8A forms) using PaddleOCR, VLM, and DocLing for automated extraction, validation, and tampering detection.</p>

      {/* Analyze Form */}
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
        <h2 className="text-lg font-semibold mb-4 dark:text-white">Analyze Document</h2>
        <div className="flex gap-3 items-end">
          <div className="flex-1">
            <label className="block text-sm text-gray-500 dark:text-gray-400 mb-1">Observer Report ID</label>
            <input className="w-full border dark:border-gray-600 rounded px-3 py-2 dark:bg-gray-700 dark:text-white" placeholder="Enter report ID from observer uploads" value={reportId} onChange={e => setReportId(e.target.value)} />
          </div>
          <button onClick={handleAnalyze} disabled={loading} className="bg-green-600 text-white px-6 py-2 rounded hover:bg-green-700 disabled:opacity-50">
            {loading ? 'Analyzing...' : 'Run Analysis'}
          </button>
          <button onClick={handleCheckStatus} className="bg-gray-600 text-white px-4 py-2 rounded hover:bg-gray-700">Check Status</button>
        </div>
      </div>

      {error && <p className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-3 rounded">{error}</p>}

      {/* Status */}
      {statusResult && !result && (
        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
          <p className="dark:text-white">Report #{statusResult.report_id}: <span className="font-semibold">{statusResult.status}</span></p>
          {statusResult.ocr_confidence != null && <p className="text-sm text-gray-500 dark:text-gray-400">OCR Confidence: {(statusResult.ocr_confidence * 100).toFixed(1)}%</p>}
          {statusResult.tampering_detected != null && <p className="text-sm text-gray-500 dark:text-gray-400">Tampering: {statusResult.tampering_detected ? 'Detected' : 'None'}</p>}
        </div>
      )}

      {/* Full Analysis Result */}
      {result && (
        <div className="space-y-4">
          {/* Summary cards */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Combined Confidence</p>
              <p className="text-2xl font-bold dark:text-white">{((result.combined_confidence || 0) * 100).toFixed(1)}%</p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Manual Review</p>
              <p className={`text-2xl font-bold ${result.requires_manual_review ? 'text-orange-600' : 'text-green-600'}`}>
                {result.requires_manual_review ? 'Required' : 'Not Needed'}
              </p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Document Valid</p>
              <p className={`text-2xl font-bold ${result.vlm?.is_valid_ec8a ? 'text-green-600' : 'text-red-600'}`}>
                {result.vlm?.is_valid_ec8a ? 'Yes' : 'No'}
              </p>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded-lg p-4 shadow">
              <p className="text-sm text-gray-500 dark:text-gray-400">Tampering</p>
              <p className={`text-2xl font-bold ${result.vlm?.tampering_detected ? 'text-red-600' : 'text-green-600'}`}>
                {result.vlm?.tampering_detected ? 'Detected' : 'Clean'}
              </p>
            </div>
          </div>

          {/* OCR Results */}
          {result.ocr && (
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <h3 className="text-lg font-semibold mb-3 dark:text-white">OCR Extraction</h3>
              <div className="grid md:grid-cols-3 gap-4 mb-4">
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Serial Number</p><p className="font-mono dark:text-white">{result.ocr.serial_number || 'N/A'}</p></div>
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Polling Unit Code</p><p className="font-mono dark:text-white">{result.ocr.polling_unit_code || 'N/A'}</p></div>
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Total Valid Votes</p><p className="font-bold dark:text-white">{result.ocr.total_valid_votes ?? 'N/A'}</p></div>
              </div>
              {result.ocr.party_results?.length > 0 && (
                <table className="w-full text-sm">
                  <thead><tr className="border-b dark:border-gray-600">
                    <th className="text-left py-2 dark:text-gray-300">Party</th>
                    <th className="text-right py-2 dark:text-gray-300">Votes</th>
                    <th className="text-right py-2 dark:text-gray-300">Confidence</th>
                  </tr></thead>
                  <tbody>
                    {result.ocr.party_results.map((p, i) => (
                      <tr key={i} className="border-b dark:border-gray-700">
                        <td className="py-2 font-medium dark:text-white">{p.party_code}</td>
                        <td className="py-2 text-right dark:text-gray-300">{p.votes.toLocaleString()}</td>
                        <td className="py-2 text-right"><span className={`px-2 py-0.5 rounded text-xs ${p.confidence >= 0.9 ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300' : 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-300'}`}>{(p.confidence * 100).toFixed(0)}%</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {result.ocr.extraction_warnings?.length > 0 && (
                <div className="mt-3 p-2 bg-yellow-50 dark:bg-yellow-900/20 rounded">
                  <p className="text-sm font-medium text-yellow-800 dark:text-yellow-300">Warnings:</p>
                  {result.ocr.extraction_warnings.map((w, i) => <p key={i} className="text-xs text-yellow-700 dark:text-yellow-400">• {w}</p>)}
                </div>
              )}
            </div>
          )}

          {/* VLM Analysis */}
          {result.vlm && (
            <div className="bg-white dark:bg-gray-800 rounded-lg p-6 shadow">
              <h3 className="text-lg font-semibold mb-3 dark:text-white">Visual Language Model Analysis</h3>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">{result.vlm.analysis_summary}</p>
              <div className="grid md:grid-cols-3 gap-4">
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Document Quality</p><p className="font-medium dark:text-white">{result.vlm.document_quality}</p></div>
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Completeness</p><p className="font-medium dark:text-white">{(result.vlm.completeness_score * 100).toFixed(0)}%</p></div>
                <div><p className="text-sm text-gray-500 dark:text-gray-400">Tampering Confidence</p><p className="font-medium dark:text-white">{(result.vlm.tampering_confidence * 100).toFixed(1)}%</p></div>
              </div>
              {result.vlm.tampering_indicators?.length > 0 && (
                <div className="mt-3 p-2 bg-red-50 dark:bg-red-900/20 rounded">
                  <p className="text-sm font-medium text-red-800 dark:text-red-300">Tampering Indicators:</p>
                  {result.vlm.tampering_indicators.map((t, i) => <p key={i} className="text-xs text-red-700 dark:text-red-400">• {t}</p>)}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
