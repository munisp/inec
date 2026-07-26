import { useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';

type RecordValue = Record<string, unknown>;

interface IReVPortalStatus {
  status?: string;
  required?: boolean;
  portal_connection_id?: number;
  submissions?: Record<string, number>;
}

interface IReVReceipt extends RecordValue {
  submission_status?: string;
  external_receipt_id?: string;
  external_transaction_id?: string;
  external_status?: string;
  payload_sha256?: string;
  evidence_event_hash?: string;
  acknowledged_at?: string;
  submitted_at?: string;
  last_error_code?: string;
  last_error_detail?: string;
}

interface IntegrityJourney {
  result?: RecordValue;
  policy_version_id?: number;
  events?: RecordValue[];
  artifacts?: RecordValue[];
  reconciliation_cases?: RecordValue[];
  fabric_anchors?: RecordValue[];
  verification?: {
    chain_valid?: boolean;
    signature_checked?: boolean;
    signature_valid?: boolean;
    event_count?: number;
    failure_reasons?: string[];
  };
}

function value(record: RecordValue | undefined | null, key: string) {
  const item = record?.[key];
  return item === undefined || item === null || item === '' ? '—' : String(item);
}

function hash(valueToShorten: unknown) {
  const text = typeof valueToShorten === 'string' ? valueToShorten : '';
  return text ? `${text.slice(0, 14)}…${text.slice(-10)}` : '—';
}

function title(valueToFormat: unknown) {
  return String(valueToFormat || 'not available').replace(/_/g, ' ');
}

export default function EvidenceJourneyPage() {
  const initialResultId = useMemo(() => {
    const hashQuery = window.location.hash.split('?', 2)[1] || '';
    return new URLSearchParams(hashQuery).get('result_id') || '';
  }, []);
  const [resultId, setResultId] = useState(initialResultId);
  const [electionId, setElectionId] = useState('');
  const [journey, setJourney] = useState<IntegrityJourney | null>(null);
  const [irevStatus, setIReVStatus] = useState<IReVPortalStatus | null>(null);
  const [irevReceipt, setIReVReceipt] = useState<IReVReceipt | null>(null);
  const [materials, setMaterials] = useState<RecordValue[]>([]);
  const [loading, setLoading] = useState(false);
  const [verificationLoading, setVerificationLoading] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState('');

  const parsedResultId = Number(resultId);
  const canLoad = Number.isInteger(parsedResultId) && parsedResultId > 0;
  const verification = journey?.verification;
  const fabricAnchors = journey?.fabric_anchors || [];
  const committedFabricAnchors = fabricAnchors.filter((anchor) => anchor.status === 'committed');
  const irevReceiptState = String(irevReceipt?.submission_status || 'not_recorded').toLowerCase();
  const irevReceiptAccepted = irevReceiptState === 'acknowledged' || irevReceiptState === 'accepted';

  const copy = async (label: string, text: unknown) => {
    if (typeof text !== 'string' || !navigator.clipboard) return;
    await navigator.clipboard.writeText(text);
    setCopied(label);
    window.setTimeout(() => setCopied(''), 1600);
  };

  const loadJourney = async () => {
    if (!canLoad) {
      setError('Enter a positive result ID.');
      return;
    }
    setLoading(true);
    setError('');
    try {
      const response = await api.getIntegrityJourney(parsedResultId) as IntegrityJourney;
      setJourney(response);
      const identifier = response.result?.election_id;
      if (typeof identifier === 'number') setElectionId(String(identifier));
      const [portalResult, receiptResult] = await Promise.allSettled([
        api.getIReVStatus() as Promise<IReVPortalStatus>,
        api.getIReVReceipt(parsedResultId) as Promise<IReVReceipt>,
      ]);
      setIReVStatus(portalResult.status === 'fulfilled' ? portalResult.value : null);
      // A 404 means that no receipt has been recorded. It must never be displayed
      // as a successful portal submission or be used to infer result validity.
      setIReVReceipt(receiptResult.status === 'fulfilled' ? receiptResult.value : null);
    } catch (caught: unknown) {
      setJourney(null);
      setError(`Evidence journey is unavailable: ${(caught as Error).message}`);
    } finally {
      setLoading(false);
    }
  };

  const verifyChain = async () => {
    if (!canLoad) return;
    setVerificationLoading(true);
    setError('');
    try {
      const response = await api.verifyIntegrityJourney(parsedResultId) as IntegrityJourney['verification'];
      setJourney((current) => current ? { ...current, verification: response } : current);
    } catch (caught: unknown) {
      setError(`Verification could not be completed: ${(caught as Error).message}`);
    } finally {
      setVerificationLoading(false);
    }
  };

  const loadMaterials = async () => {
    const parsedElectionId = Number(electionId);
    if (!Number.isInteger(parsedElectionId) || parsedElectionId <= 0) {
      setError('Enter a positive election ID to retrieve approved materials.');
      return;
    }
    setError('');
    try {
      const response = await api.listMaterialManifests(parsedElectionId) as RecordValue | RecordValue[];
      const manifestItems = Array.isArray(response)
        ? response
        : response.material_manifests;
      setMaterials(Array.isArray(manifestItems) ? manifestItems as RecordValue[] : []);
    } catch (caught: unknown) {
      setMaterials([]);
      setError(`Material manifest retrieval failed: ${(caught as Error).message}`);
    }
  };

  useEffect(() => {
    if (initialResultId) void loadJourney();
  // `loadJourney` intentionally reads the initial query once on entry.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialResultId]);

  return (
    <section aria-label="Result evidence journey" className="space-y-6">
      <header className="max-w-4xl">
        <p className="text-sm font-semibold uppercase tracking-[0.16em] text-green-700 dark:text-green-400">Publicly safe evidence</p>
        <h1 className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">Verify a result lifecycle</h1>
        <p className="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">Inspect the policy-bound evidence chain for a result. Hashes and signatures make changes detectable; they do not replace lawful collation, reconciliation, or authorised result declaration.</p>
      </header>

      <div className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
        <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
          <div>
            <label htmlFor="result-id" className="mb-1 block text-sm font-medium text-zinc-800 dark:text-zinc-100">Result ID</label>
            <input id="result-id" inputMode="numeric" value={resultId} onChange={(event) => setResultId(event.target.value)} placeholder="Published result ID" className="w-full border border-zinc-300 bg-white px-3 py-2 outline-none ring-green-700 focus:ring-2 dark:border-zinc-600 dark:bg-zinc-800 dark:text-white" />
          </div>
          <div className="flex flex-wrap gap-2"><button onClick={loadJourney} disabled={loading} className="bg-green-700 px-5 py-2 text-sm font-semibold text-white hover:bg-green-800 disabled:opacity-50">{loading ? 'Loading…' : 'Load evidence'}</button>{journey && <button onClick={verifyChain} disabled={verificationLoading} className="border border-zinc-300 px-4 py-2 text-sm font-semibold text-zinc-800 hover:bg-zinc-50 dark:border-zinc-600 dark:text-zinc-100 dark:hover:bg-zinc-800">{verificationLoading ? 'Verifying…' : 'Verify chain'}</button>}</div>
        </div>
      </div>

      {error && <div role="alert" className="border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-200">{error}</div>}

      {journey && (
        <>
          <section className="grid gap-px border border-zinc-200 bg-zinc-200 sm:grid-cols-2 lg:grid-cols-4 dark:border-zinc-700 dark:bg-zinc-700">
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Polling unit</p><p className="mt-1 font-mono text-sm font-semibold text-zinc-950 dark:text-white">{value(journey.result, 'polling_unit_code')}</p></div>
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Result state</p><p className="mt-1 text-sm font-semibold text-zinc-950 dark:text-white">{title(journey.result?.status)}</p></div>
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Evidence events</p><p className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">{verification?.event_count ?? journey.events?.length ?? 0}</p></div>
            <div className="bg-white p-4 dark:bg-zinc-900"><p className="text-xs uppercase tracking-wide text-zinc-500">Policy version</p><p className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">{journey.policy_version_id || '—'}</p></div>
          </section>

          <section className={`border p-5 ${verification?.chain_valid && verification.signature_valid !== false ? 'border-green-300 bg-green-50 dark:border-green-900 dark:bg-green-950/20' : 'border-amber-300 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/20'}`}>
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-zinc-950 dark:text-white">Evidence verification</h2><p className="mt-1 text-sm text-zinc-700 dark:text-zinc-200">The verification service recomputes chain links and, when signatures exist, checks the configured signer.</p></div><div className="flex gap-2 text-xs font-semibold"><span className="border border-current px-2 py-1">Chain: {verification?.chain_valid ? 'valid' : 'unverified'}</span><span className="border border-current px-2 py-1">Signature: {verification?.signature_checked ? (verification?.signature_valid ? 'valid' : 'invalid') : 'not checked'}</span></div></div>
            {verification?.failure_reasons?.length ? <ul className="mt-3 list-disc space-y-1 pl-5 text-sm text-amber-900 dark:text-amber-100">{verification.failure_reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul> : null}
          </section>

          <section className={`border p-5 ${irevReceiptAccepted ? 'border-green-300 bg-green-50 dark:border-green-900 dark:bg-green-950/20' : 'border-amber-300 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/20'}`}>
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-zinc-950 dark:text-white">Authorized IReV portal receipt</h2><p className="mt-1 text-sm text-zinc-700 dark:text-zinc-200">A receipt is evidence of an external portal response only after its state and reference are verified. It does not replace lawful collation or declaration.</p></div><span className="border border-current px-2 py-1 text-xs font-semibold">{title(irevReceiptState)}</span></div>
            <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4"><div><p className="text-xs uppercase tracking-wide text-zinc-500">Portal state</p><p className="mt-1 font-semibold text-zinc-950 dark:text-white">{title(irevStatus?.status || 'unavailable')}</p></div><div><p className="text-xs uppercase tracking-wide text-zinc-500">Receipt reference</p><p className="mt-1 font-mono text-xs text-zinc-950 dark:text-white">{hash(irevReceipt?.external_receipt_id)}</p></div><div><p className="text-xs uppercase tracking-wide text-zinc-500">External status</p><p className="mt-1 font-semibold text-zinc-950 dark:text-white">{title(irevReceipt?.external_status)}</p></div><div><p className="text-xs uppercase tracking-wide text-zinc-500">Acknowledged</p><p className="mt-1 text-xs text-zinc-950 dark:text-white">{value(irevReceipt, 'acknowledged_at')}</p></div></div>
            {!irevReceipt ? <p className="mt-4 text-sm text-amber-900 dark:text-amber-100">No verified IReV receipt is recorded for this result. This is not evidence of portal acceptance; the authorized interface may be unavailable, unconfigured, pending, or have rejected the submission.</p> : irevReceipt.last_error_code ? <p className="mt-4 text-sm text-amber-900 dark:text-amber-100">Receipt reconciliation requires attention: {value(irevReceipt, 'last_error_code')}.</p> : null}
          </section>

          <section className={`border p-5 ${fabricAnchors.length > 0 && committedFabricAnchors.length === fabricAnchors.length ? 'border-sky-300 bg-sky-50 dark:border-sky-900 dark:bg-sky-950/20' : 'border-amber-300 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/20'}`}>
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-zinc-950 dark:text-white">Consortium ledger anchors</h2><p className="mt-1 text-sm text-zinc-700 dark:text-zinc-200">Only signed evidence hashes and verification receipts are committed to Hyperledger Fabric. EC8A documents and private reconciliation details remain off-chain.</p></div><span className="border border-current px-2 py-1 text-xs font-semibold">Committed: {committedFabricAnchors.length}/{fabricAnchors.length}</span></div>
            {fabricAnchors.length === 0 ? <p className="mt-3 text-sm text-amber-900 dark:text-amber-100">No consortium receipt has been recorded for this result. This does not invalidate the local evidence chain, but it is not independent Fabric confirmation.</p> : <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[720px] text-sm"><thead><tr className="border-b border-current/20 text-left"><th className="pb-2">Anchor</th><th className="pb-2">State</th><th className="pb-2">Fabric transaction</th><th className="pb-2">Channel / chaincode</th><th className="pb-2">Receipt</th></tr></thead><tbody>{fabricAnchors.map((anchor) => <tr key={String(anchor.id || anchor.anchor_id)} className="border-b border-current/10"><td className="py-3"><button onClick={() => copy(`fabric-anchor-${String(anchor.id)}`, anchor.anchor_id)} className="font-mono text-xs text-sky-800 hover:underline dark:text-sky-300">{copied === `fabric-anchor-${String(anchor.id)}` ? 'Copied' : hash(anchor.anchor_id)}</button></td><td className="py-3 font-medium">{title(anchor.status)}</td><td className="py-3"><button onClick={() => copy(`fabric-tx-${String(anchor.id)}`, anchor.transaction_id)} className="font-mono text-xs text-sky-800 hover:underline dark:text-sky-300">{copied === `fabric-tx-${String(anchor.id)}` ? 'Copied' : hash(anchor.transaction_id)}</button></td><td className="py-3 text-xs">{value(anchor, 'channel')} / {value(anchor, 'chaincode')}</td><td className="py-3 text-xs">{value(anchor, 'receipt_sha256') === '—' ? 'Not committed' : <button onClick={() => copy(`fabric-receipt-${String(anchor.id)}`, anchor.receipt_sha256)} className="font-mono text-sky-800 hover:underline dark:text-sky-300">{copied === `fabric-receipt-${String(anchor.id)}` ? 'Copied' : hash(anchor.receipt_sha256)}</button>}</td></tr>)}</tbody></table></div>}
          </section>

          <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
            <h2 className="text-lg font-semibold text-zinc-950 dark:text-white">Immutable event sequence</h2>
            <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[720px] text-sm"><thead><tr className="border-b border-zinc-200 text-left dark:border-zinc-700"><th className="pb-2">#</th><th className="pb-2">Event</th><th className="pb-2">Evidence hash</th><th className="pb-2">Signer</th><th className="pb-2">Time</th></tr></thead><tbody>{(journey.events || []).map((event) => <tr key={String(event.sequence_no)} className="border-b border-zinc-100 dark:border-zinc-800"><td className="py-3 font-mono">{value(event, 'sequence_no')}</td><td className="py-3 font-medium dark:text-white">{title(event.event_type)}</td><td className="py-3"><button onClick={() => copy(`event-${String(event.sequence_no)}`, event.event_hash)} className="font-mono text-xs text-green-700 hover:underline dark:text-green-400">{copied === `event-${String(event.sequence_no)}` ? 'Copied' : hash(event.event_hash)}</button></td><td className="py-3 text-xs dark:text-zinc-200">{value(event, 'signer_status')}</td><td className="py-3 text-xs text-zinc-500">{value(event, 'created_at')}</td></tr>)}</tbody></table></div>
          </section>

          <div className="grid gap-5 lg:grid-cols-2">
            <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900"><h2 className="text-lg font-semibold text-zinc-950 dark:text-white">Reconciliation cases</h2>{(journey.reconciliation_cases || []).length === 0 ? <p className="mt-3 text-sm text-green-700 dark:text-green-300">No reconciliation case is linked to this result.</p> : <ul className="mt-3 space-y-3">{(journey.reconciliation_cases || []).map((caseItem) => <li key={String(caseItem.id)} className="border-l-2 border-amber-500 pl-3"><p className="font-medium text-zinc-950 dark:text-white">{title(caseItem.case_type)} · {title(caseItem.severity)}</p><p className="mt-1 text-sm text-zinc-700 dark:text-zinc-200">{value(caseItem, 'description')}</p><p className="mt-1 text-xs text-zinc-500">State: {title(caseItem.status)} · Blocking: {String(Boolean(caseItem.blocking))}</p></li>)}</ul>}</section>
            <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900"><h2 className="text-lg font-semibold text-zinc-950 dark:text-white">Source artifacts</h2>{(journey.artifacts || []).length === 0 ? <p className="mt-3 text-sm text-zinc-600 dark:text-zinc-300">No evidence artifact is linked to this result yet.</p> : <ul className="mt-3 space-y-3">{(journey.artifacts || []).map((artifact) => <li key={String(artifact.id)} className="border-b border-zinc-100 pb-3 last:border-0 dark:border-zinc-800"><p className="font-medium text-zinc-950 dark:text-white">{title(artifact.artifact_kind)}</p><button onClick={() => copy(`artifact-${String(artifact.id)}`, artifact.content_sha256)} className="mt-1 break-all font-mono text-xs text-green-700 hover:underline dark:text-green-400">{copied === `artifact-${String(artifact.id)}` ? 'Copied' : hash(artifact.content_sha256)}</button><p className="mt-1 text-xs text-zinc-500">{value(artifact, 'original_filename')} · {value(artifact, 'byte_size')} bytes</p></li>)}</ul>}</section>
          </div>
        </>
      )}

      <section className="border border-zinc-200 bg-white p-5 dark:border-zinc-700 dark:bg-zinc-900">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between"><div><h2 className="text-lg font-semibold text-zinc-950 dark:text-white">Approved election materials</h2><p className="mt-1 text-sm text-zinc-600 dark:text-zinc-300">Retrieve versioned, hash-addressed templates and lists for a specific election.</p></div><div className="flex gap-2"><input inputMode="numeric" value={electionId} onChange={(event) => setElectionId(event.target.value)} placeholder="Election ID" className="w-32 border border-zinc-300 bg-white px-3 py-2 text-sm dark:border-zinc-600 dark:bg-zinc-800 dark:text-white" /><button onClick={loadMaterials} className="bg-zinc-900 px-4 py-2 text-sm font-semibold text-white hover:bg-zinc-700 dark:bg-white dark:text-zinc-900">Load materials</button></div></div>
        {materials.length > 0 && <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[600px] text-sm"><thead><tr className="border-b border-zinc-200 text-left dark:border-zinc-700"><th className="pb-2">Material</th><th className="pb-2">Version</th><th className="pb-2">Status</th><th className="pb-2">Manifest hash</th></tr></thead><tbody>{materials.map((material) => <tr key={String(material.id)} className="border-b border-zinc-100 dark:border-zinc-800"><td className="py-3 font-medium dark:text-white">{title(material.material_type)}</td><td className="py-3 dark:text-zinc-200">{value(material, 'version')}</td><td className="py-3 dark:text-zinc-200">{title(material.status)}</td><td className="py-3"><button onClick={() => copy(`material-${String(material.id)}`, material.manifest_sha256)} className="font-mono text-xs text-green-700 hover:underline dark:text-green-400">{copied === `material-${String(material.id)}` ? 'Copied' : hash(material.manifest_sha256)}</button></td></tr>)}</tbody></table></div>}
      </section>
    </section>
  );
}
