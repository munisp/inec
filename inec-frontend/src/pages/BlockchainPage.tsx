import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Link2, RefreshCw, ShieldCheck } from 'lucide-react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

interface FabricAnchorHealth {
  enabled?: boolean;
  required?: boolean;
  status?: 'disabled' | 'unavailable' | 'degraded' | 'healthy' | string;
  reason?: string;
  pending?: number;
  failed?: number;
  unavailable?: number;
  channel?: string;
  chaincode?: string;
  consortium_msps?: string[];
}

function statusLabel(status?: string) {
  return String(status || 'unavailable').replace(/_/g, ' ');
}

export default function BlockchainPage() {
  const [health, setHealth] = useState<FabricAnchorHealth | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const response = await api.getFabricAnchorHealth() as FabricAnchorHealth;
      setHealth(response);
    } catch (caught: unknown) {
      setHealth(null);
      setError((caught as Error).message || 'Consortium gateway health is unavailable.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const healthy = health?.status === 'healthy';
  const configured = Boolean(health?.enabled);
  const needsAttention = !healthy || Number(health?.pending || 0) > 0 || Number(health?.failed || 0) > 0 || Number(health?.unavailable || 0) > 0;

  return (
    <section className="space-y-6" aria-label="Consortium evidence anchoring">
      <header className="max-w-4xl">
        <p className="text-sm font-semibold uppercase tracking-[0.16em] text-sky-700 dark:text-sky-400">Independent provenance</p>
        <h1 className="mt-1 text-2xl font-bold text-zinc-950 dark:text-white">Hyperledger Fabric evidence anchoring</h1>
        <p className="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">The platform anchors only signed evidence hashes and verification receipts to the approved consortium channel. Election documents, voter records, OCR/VLM analysis, and private reconciliation details remain off-chain.</p>
      </header>

      {error ? <div role="alert" className="border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/20 dark:text-amber-100"><div className="flex gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /><div><p className="font-semibold">Consortium confirmation is unavailable</p><p className="mt-1">{error}</p></div></div></div> : null}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card><CardContent className="pt-5"><p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Gateway state</p><p className={`mt-2 text-xl font-bold capitalize ${healthy ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'}`}>{loading ? 'Checking…' : statusLabel(health?.status)}</p></CardContent></Card>
        <Card><CardContent className="pt-5"><p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Pending anchors</p><p className="mt-2 text-xl font-bold text-zinc-950 dark:text-white">{loading ? '—' : health?.pending ?? 0}</p></CardContent></Card>
        <Card><CardContent className="pt-5"><p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Failed / unavailable</p><p className="mt-2 text-xl font-bold text-zinc-950 dark:text-white">{loading ? '—' : `${health?.failed ?? 0} / ${health?.unavailable ?? 0}`}</p></CardContent></Card>
        <Card><CardContent className="pt-5"><p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Enforcement</p><p className="mt-2 text-xl font-bold text-zinc-950 dark:text-white">{health?.required ? 'Required' : configured ? 'Optional' : 'Disabled'}</p></CardContent></Card>
      </div>

      <Card className={healthy && !needsAttention ? 'border-emerald-300 dark:border-emerald-900' : 'border-amber-300 dark:border-amber-900'}>
        <CardHeader><CardTitle className="flex items-center gap-2 text-base">{healthy && !needsAttention ? <CheckCircle2 className="h-5 w-5 text-emerald-700 dark:text-emerald-400" /> : <AlertTriangle className="h-5 w-5 text-amber-700 dark:text-amber-400" />} Consortium health and governance</CardTitle></CardHeader>
        <CardContent className="grid gap-5 text-sm md:grid-cols-2">
          <div className="space-y-2"><p className="text-zinc-500">Channel and chaincode</p><p className="font-mono text-xs text-zinc-950 dark:text-white">{health?.channel || 'Not configured'} / {health?.chaincode || 'Not configured'}</p><p className="pt-2 text-zinc-500">Consortium organizations</p><p className="font-mono text-xs text-zinc-950 dark:text-white">{Array.isArray(health?.consortium_msps) && health?.consortium_msps.length ? health.consortium_msps.join(', ') : 'Not independently confirmed'}</p></div>
          <div className="space-y-2"><p className="text-zinc-500">Interpretation</p><p className="leading-6 text-zinc-700 dark:text-zinc-200">A local evidence signature is not a Fabric confirmation. A result receives independent consortium provenance only when its Evidence Journey lists a <strong>committed</strong> anchor with both a Fabric transaction ID and receipt hash.</p>{health?.reason ? <p className="border-l-2 border-amber-500 pl-3 text-xs text-amber-800 dark:text-amber-100">{health.reason}</p> : null}</div>
        </CardContent>
      </Card>

      <div className="grid gap-4 md:grid-cols-2">
        <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><Link2 className="h-5 w-5 text-sky-700 dark:text-sky-400" /> Verify a result receipt</CardTitle></CardHeader><CardContent className="space-y-4 text-sm text-zinc-600 dark:text-zinc-300"><p>Open the Evidence Journey from an individual published result to inspect the local signed lifecycle separately from each Fabric anchor request and committed receipt.</p><button type="button" onClick={() => { window.location.hash = '#/evidence'; }} className="bg-sky-700 px-4 py-2 font-semibold text-white hover:bg-sky-800">Open Evidence Journey</button></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><ShieldCheck className="h-5 w-5 text-sky-700 dark:text-sky-400" /> Data boundary</CardTitle></CardHeader><CardContent className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">The Fabric contract validates deterministic signed commitments and prevents conflicting rewrites. It does not store EC8A images, voter data, private event payloads, or document-analysis output. Those remain within the governed PostgreSQL evidence system.</CardContent></Card>
      </div>

      <button type="button" onClick={() => void load()} disabled={loading} className="inline-flex items-center gap-2 border border-zinc-300 px-4 py-2 text-sm font-semibold text-zinc-800 hover:bg-zinc-50 disabled:opacity-50 dark:border-zinc-600 dark:text-zinc-100 dark:hover:bg-zinc-800"><RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />{loading ? 'Checking Gateway…' : 'Refresh consortium status'}</button>
    </section>
  );
}
