import { useMemo, useState } from 'react';
import { api } from '@/lib/api';

type Severity = 'low' | 'medium' | 'high' | 'critical' | string;

interface EvidenceFinding {
  code: string;
  severity: Severity;
  detail: string;
  indicators?: string[];
}

interface AnalysisResult {
  report_id: number;
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
  assessment_status?: string;
  decision?: string;
  manifest_sha256?: string;
  engine_versions?: Record<string, string>;
  findings?: EvidenceFinding[];
  image_evidence?: {
    content_sha256?: string;
    perceptual_hash?: string;
    width?: number;
    height?: number;
    blur_method?: string;
  };
}

interface AnalysisResponse {
  analysis?: AnalysisResult;
  artifact_id?: number;
  observer_report_status?: string;
  reconciliation_case_id?: number;
}

interface StatusResult {
  report_id: number;
  status: string;
  ocr_confidence?: number;
  tampering_detected?: boolean;
  requires_review?: boolean;
  assessment_status?: string;
  decision?: string;
  manifest_sha256?: string;
  engine_versions?: Record<string, string>;
  findings?: EvidenceFinding[];
}

const severityStyles: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-200',
  high: 'bg-orange-100 text-orange-800 dark:bg-orange-900/40 dark:text-orange-200',
  medium: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200',
  low: 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200',
};

function shortHash(hash?: string) {
  if (!hash) return 'Not available';
  return `${hash.slice(0, 12)}…${hash.slice(-10)}`;
}

function titleCase(value?: string) {
  return (value || 'not available').replace(/_/g, ' ');
}

export default function DocumentAIPage() {
  const [reportId, setReportId] = useState('');
  const [result, setResult] = useState<AnalysisResult | null>(null);
  const [analysisMeta, setAnalysisMeta] = useState<AnalysisResponse | null>(null);
  const [statusResult, setStatusResult] = useState<StatusResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState('');

  const findings = useMemo(() => result?.findings || [], [result]);
  const needsReview = Boolean(result?.requires_manual_review);

  const copyHash = async (label: string, hash?: string) => {
    if (!hash || !navigator.clipboard) return;
    await navigator.clipboard.writeText(hash);
    setCopied(label);
    window.setTimeout(() => setCopied(''), 1800);
  };

  const reportNumber = () => {
    const parsed = Number(reportId);
    if (!Number.isInteger(parsed) || parsed <= 0) {
      setError('Enter a positive observer report ID.');
      return null;
    }
    return parsed;
  };

  const handleAnalyze = async () => {
    const parsedReportId = reportNumber();
    if (!parsedReportId) return;
    setLoading(true);
    setResult(null);
    setAnalysisMeta(null);
    setStatusResult(null);
    setError('');
    try {
      const raw = await api.analyzeDocument(parsedReportId) as unknown as AnalysisResponse | AnalysisResult;
      let analysis: AnalysisResult;
      let response: AnalysisResponse;
      if ('analysis' in raw && raw.analysis) {
        analysis = raw.analysis;
        response = raw;
      } else {
        analysis = raw as AnalysisResult;
        response = { analysis };
      }
      setResult(analysis);
      setAnalysisMeta(response);
    } catch (e: unknown) {
      setError(`Evidence analysis unavailable: ${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  };

  const handleCheckStatus = async () => {
    const parsedReportId = reportNumber();
    if (!parsedReportId) return;
    setError('');
    try {
      const response = await api.getDocumentAnalysisStatus(parsedReportId) as unknown as StatusResult;
      setStatusResult(response);
    } catch (e: unknown) {
      setError(`Status retrieval failed: ${(e as Error).message}`);
    }
  };

  return (
    <section aria-label="Document evidence analysis" className="space-y-6">
      <header className="max-w-4xl">
        <p className="text-sm font-semibold uppercase tracking-[0.16em] text-green-700 dark:text-green-400">Election evidence</p>
        <h1 className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">Document integrity review</h1>
        <p className="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">
          Create a content-addressed evidence bundle from an observer EC8A image. PaddleOCR, a configured VLM, and Docling contribute structured evidence; the platform routes disagreements or quality concerns to accountable review and never treats an AI score as a result decision.
        </p>
      </header>

      <div className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
        <div className="flex flex-col gap-4 md:flex-row md:items-end">
          <div className="min-w-0 flex-1">
            <label htmlFor="observer-report-id" className="mb-1 block text-sm font-medium text-zinc-800 dark:text-zinc-100">Observer report ID</label>
            <input
              id="observer-report-id"
              inputMode="numeric"
              className="w-full border border-zinc-300 bg-white px-3 py-2 text-zinc-950 outline-none ring-green-700 focus:ring-2 dark:border-zinc-600 dark:bg-zinc-800 dark:text-white"
              placeholder="Enter a report ID from observer uploads"
              value={reportId}
              onChange={(event) => setReportId(event.target.value)}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <button onClick={handleAnalyze} disabled={loading} className="bg-green-700 px-5 py-2 text-sm font-semibold text-white hover:bg-green-800 disabled:cursor-not-allowed disabled:opacity-50">
              {loading ? 'Creating evidence bundle…' : 'Create evidence bundle'}
            </button>
            <button onClick={handleCheckStatus} className="border border-zinc-300 px-4 py-2 text-sm font-semibold text-zinc-800 hover:bg-zinc-50 dark:border-zinc-600 dark:text-zinc-100 dark:hover:bg-zinc-800">
              Check latest status
            </button>
          </div>
        </div>
      </div>

      {error && <div role="alert" className="border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-200">{error}</div>}

      {statusResult && !result && (
        <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h2 className="font-semibold text-zinc-950 dark:text-white">Report #{statusResult.report_id}</h2>
              <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-300">Observer workflow status: <span className="font-medium">{titleCase(statusResult.status)}</span></p>
            </div>
            {statusResult.assessment_status && <span className="border border-zinc-300 px-2 py-1 text-xs font-semibold uppercase tracking-wide dark:border-zinc-600">{titleCase(statusResult.assessment_status)}</span>}
          </div>
          {statusResult.manifest_sha256 && <p className="mt-3 break-all font-mono text-xs text-zinc-500 dark:text-zinc-400">Manifest: {statusResult.manifest_sha256}</p>}
        </section>
      )}

      {result && (
        <div className="space-y-5">
          <section className={`border p-5 ${needsReview ? 'border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/20' : 'border-green-200 bg-green-50 dark:border-green-900 dark:bg-green-950/20'}`}>
            <div className="flex flex-col justify-between gap-3 md:flex-row md:items-start">
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-zinc-600 dark:text-zinc-300">Assessment state</p>
                <h2 className="mt-1 text-xl font-bold text-zinc-950 dark:text-white">{titleCase(result.assessment_status)}</h2>
                <p className="mt-2 max-w-3xl text-sm text-zinc-700 dark:text-zinc-200">{titleCase(result.decision)}. This screen records documentary evidence only; authorised electoral workflow controls result validation and finalisation.</p>
              </div>
              <div className="text-left md:text-right">
                <p className="text-xs text-zinc-600 dark:text-zinc-300">Observer report status</p>
                <p className="font-semibold text-zinc-950 dark:text-white">{titleCase(analysisMeta?.observer_report_status)}</p>
                {analysisMeta?.reconciliation_case_id && <p className="mt-1 text-xs text-amber-800 dark:text-amber-200">Reconciliation case #{analysisMeta.reconciliation_case_id} opened</p>}
              </div>
            </div>
          </section>

          <section className="grid gap-px border border-zinc-200 bg-zinc-200 sm:grid-cols-2 lg:grid-cols-4 dark:border-zinc-700 dark:bg-zinc-700">
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Evidence confidence</p><p className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">{((result.combined_confidence || 0) * 100).toFixed(1)}%</p></div>
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Manual review</p><p className={`mt-1 text-lg font-bold ${needsReview ? 'text-amber-700 dark:text-amber-300' : 'text-green-700 dark:text-green-300'}`}>{needsReview ? 'Required' : 'Not currently required'}</p></div>
            <button onClick={() => copyHash('manifest', result.manifest_sha256)} className="bg-white p-4 text-left hover:bg-zinc-50 dark:bg-zinc-900 dark:hover:bg-zinc-800"><p className="text-xs uppercase tracking-wide text-zinc-500">Manifest SHA-256</p><p className="mt-1 break-all font-mono text-xs text-zinc-900 dark:text-zinc-100">{shortHash(result.manifest_sha256)}</p><p className="mt-2 text-xs text-green-700 dark:text-green-400">{copied === 'manifest' ? 'Copied' : 'Copy manifest hash'}</p></button>
            <button onClick={() => copyHash('content', result.image_evidence?.content_sha256)} className="bg-white p-4 text-left hover:bg-zinc-50 dark:bg-zinc-900 dark:hover:bg-zinc-800"><p className="text-xs uppercase tracking-wide text-zinc-500">Document SHA-256</p><p className="mt-1 break-all font-mono text-xs text-zinc-900 dark:text-zinc-100">{shortHash(result.image_evidence?.content_sha256)}</p><p className="mt-2 text-xs text-green-700 dark:text-green-400">{copied === 'content' ? 'Copied' : 'Copy document hash'}</p></button>
          </section>

          <section className="grid gap-5 lg:grid-cols-[1.2fr_0.8fr]">
            <div className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
              <h3 className="text-lg font-semibold text-zinc-950 dark:text-white">Evidence findings</h3>
              <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-300">Findings are review prompts, not automatic result determinations.</p>
              {findings.length === 0 ? <p className="mt-4 text-sm text-green-700 dark:text-green-300">No policy findings were recorded by the configured engines.</p> : (
                <ul className="mt-4 space-y-3">
                  {findings.map((finding, index) => (
                    <li key={`${finding.code}-${index}`} className="border-l-2 border-zinc-300 pl-3 dark:border-zinc-600">
                      <div className="flex flex-wrap items-center gap-2"><span className={`px-2 py-0.5 text-xs font-semibold uppercase ${severityStyles[finding.severity] || severityStyles.low}`}>{finding.severity}</span><span className="font-mono text-xs text-zinc-600 dark:text-zinc-300">{finding.code}</span></div>
                      <p className="mt-1 text-sm text-zinc-800 dark:text-zinc-100">{finding.detail}</p>
                      {finding.indicators?.length ? <p className="mt-1 text-xs text-zinc-600 dark:text-zinc-300">Indicators: {finding.indicators.join(', ')}</p> : null}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
              <h3 className="text-lg font-semibold text-zinc-950 dark:text-white">Engine provenance</h3>
              <dl className="mt-4 space-y-3 text-sm">
                {Object.entries(result.engine_versions || {}).map(([engine, version]) => <div key={engine} className="flex items-start justify-between gap-4 border-b border-zinc-100 pb-2 dark:border-zinc-800"><dt className="font-medium text-zinc-700 dark:text-zinc-200">{titleCase(engine)}</dt><dd className="max-w-[60%] break-all text-right font-mono text-xs text-zinc-500 dark:text-zinc-400">{version}</dd></div>)}
              </dl>
              {result.image_evidence && <div className="mt-5 border-t border-zinc-100 pt-4 text-xs text-zinc-600 dark:border-zinc-800 dark:text-zinc-300"><p>Image: {result.image_evidence.width || '—'} × {result.image_evidence.height || '—'} px</p><p className="mt-1">Quality method: {titleCase(result.image_evidence.blur_method)}</p></div>}
            </div>
          </section>

          {result.ocr && <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900"><h3 className="text-lg font-semibold text-zinc-950 dark:text-white">PaddleOCR extraction</h3><div className="mt-4 grid gap-4 sm:grid-cols-3"><div><p className="text-xs uppercase tracking-wide text-zinc-500">Serial number</p><p className="mt-1 break-all font-mono text-sm dark:text-white">{result.ocr.serial_number || 'Not detected'}</p></div><div><p className="text-xs uppercase tracking-wide text-zinc-500">Polling unit</p><p className="mt-1 break-all font-mono text-sm dark:text-white">{result.ocr.polling_unit_code || 'Not detected'}</p></div><div><p className="text-xs uppercase tracking-wide text-zinc-500">Total valid votes</p><p className="mt-1 text-sm font-semibold dark:text-white">{result.ocr.total_valid_votes ?? 'Not detected'}</p></div></div>{result.ocr.party_results?.length > 0 && <div className="mt-5 overflow-x-auto"><table className="w-full text-sm"><thead><tr className="border-b border-zinc-200 text-left dark:border-zinc-700"><th className="py-2">Party</th><th className="py-2 text-right">Votes</th><th className="py-2 text-right">OCR confidence</th></tr></thead><tbody>{result.ocr.party_results.map((party) => <tr key={party.party_code} className="border-b border-zinc-100 dark:border-zinc-800"><td className="py-2 font-medium dark:text-white">{party.party_code}</td><td className="py-2 text-right dark:text-zinc-200">{party.votes.toLocaleString()}</td><td className="py-2 text-right dark:text-zinc-200">{(party.confidence * 100).toFixed(0)}%</td></tr>)}</tbody></table></div>}</section>}
        </div>
      )}
    </section>
  );
}
