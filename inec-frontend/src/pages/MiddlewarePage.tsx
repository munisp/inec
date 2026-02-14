import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

interface MWStatus {
  name: string;
  status: string;
  mode: string;
  details?: Record<string, unknown>;
}

export default function MiddlewarePage() {
  const [statuses, setStatuses] = useState<MWStatus[]>([]);
  const [health, setHealth] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const [kafkaTopics, setKafkaTopics] = useState<string[]>([]);
  const [redisStats, setRedisStats] = useState<Record<string, unknown> | null>(null);
  const [apisixRoutes, setApisixRoutes] = useState<unknown[]>([]);
  const [lakehouseTables, setLakehouseTables] = useState<string[]>([]);

  useEffect(() => {
    loadAll();
    const interval = setInterval(loadAll, 15000);
    return () => clearInterval(interval);
  }, []);

  async function loadAll() {
    try {
      const [statusRes, healthRes] = await Promise.all([
        api.getMiddlewareStatus().catch(() => ({ middleware: [] })),
        api.getMiddlewareHealth().catch(() => null),
      ]);
      const mwArr = statusRes?.middleware || (Array.isArray(statusRes) ? statusRes : []);
      setStatuses(mwArr.map((m: any) => ({ name: m.name, status: m.connected ? 'connected' : 'disconnected', mode: m.mode, details: m.details })));
      setHealth(healthRes ? { status: healthRes.all_connected ? 'healthy' : 'degraded', ...healthRes } : null);

      const [kt, rs, ar, lt] = await Promise.all([
        api.getKafkaTopics().catch(() => []),
        api.getRedisStats().catch(() => null),
        api.getAPISIXRoutes().catch(() => []),
        api.getLakehouseTables().catch(() => []),
      ]);
      setKafkaTopics(Array.isArray(kt) ? kt : (kt as any)?.topics || []);
      const redisData = rs?.status || rs;
      setRedisStats(redisData && typeof redisData === 'object' && !Array.isArray(redisData)
        ? Object.fromEntries(Object.entries(redisData).map(([k, v]) => [k, typeof v === 'object' ? JSON.stringify(v) : v]))
        : null);
      setApisixRoutes(Array.isArray(ar) ? ar : (ar as any)?.routes || []);
      setLakehouseTables(Array.isArray(lt) ? lt : (lt as any)?.tables || []);
    } catch {
    } finally {
      setLoading(false);
    }
  }

  const statusColor = (s: string) => {
    if (s === 'connected' || s === 'healthy') return 'bg-green-100 text-green-800 border-green-200';
    if (s === 'embedded' || s === 'fallback') return 'bg-blue-100 text-blue-800 border-blue-200';
    if (s === 'degraded') return 'bg-yellow-100 text-yellow-800 border-yellow-200';
    return 'bg-red-100 text-red-800 border-red-200';
  };

  const modeIcon = (m: string) => {
    if (m === 'external') return '\u2601\uFE0F';
    if (m === 'embedded') return '\uD83D\uDCE6';
    return '\u2699\uFE0F';
  };

  if (loading) return <div className="flex items-center justify-center h-64"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-green-700" /></div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-zinc-900">Middleware Status</h2>
          <p className="text-sm text-zinc-500">10 enterprise middleware integrations</p>
        </div>
        {health && (
          <Badge variant="outline" className={health.status === 'healthy' ? 'bg-green-50 text-green-700 border-green-200' : 'bg-yellow-50 text-yellow-700 border-yellow-200'}>
            {String(health.status || 'unknown').toUpperCase()}
          </Badge>
        )}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-4">
        {statuses.map((mw) => (
          <Card key={mw.name} className="border">
            <CardHeader className="pb-2 pt-4 px-4">
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm font-semibold">{mw.name}</CardTitle>
                <span className="text-xs">{modeIcon(mw.mode)}</span>
              </div>
            </CardHeader>
            <CardContent className="px-4 pb-4">
              <Badge variant="outline" className={`text-xs ${statusColor(mw.status)}`}>
                {mw.status}
              </Badge>
              <p className="text-xs text-zinc-500 mt-1">{mw.mode}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Kafka Topics</CardTitle>
          </CardHeader>
          <CardContent>
            {kafkaTopics.length === 0 ? (
              <p className="text-sm text-zinc-500">No topics (embedded mode)</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {kafkaTopics.map((t: string) => (
                  <Badge key={t} variant="secondary" className="text-xs">{t}</Badge>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Redis Stats</CardTitle>
          </CardHeader>
          <CardContent>
            {redisStats ? (
              <div className="space-y-1 text-sm">
                {Object.entries(redisStats).map(([k, v]) => (
                  <div key={k} className="flex justify-between">
                    <span className="text-zinc-500">{k}</span>
                    <span className="font-mono">{String(v)}</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-sm text-zinc-500">Embedded cache active</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">APISIX Routes</CardTitle>
          </CardHeader>
          <CardContent>
            {apisixRoutes.length === 0 ? (
              <p className="text-sm text-zinc-500">Embedded routing active</p>
            ) : (
              <div className="space-y-1">
                {apisixRoutes.map((r: any, i: number) => (
                  <div key={i} className="text-xs font-mono text-zinc-700">
                    {r.uri || r.id || JSON.stringify(r).slice(0, 60)}
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Lakehouse Tables</CardTitle>
          </CardHeader>
          <CardContent>
            {lakehouseTables.length === 0 ? (
              <p className="text-sm text-zinc-500">Embedded analytics active</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {lakehouseTables.map((t: string) => (
                  <Badge key={t} variant="outline" className="text-xs">{t}</Badge>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Architecture Overview</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Event Streaming</h4>
              <p className="text-zinc-600">Kafka (event bus) + Fluvio (edge ingestion)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Workflow Orchestration</h4>
              <p className="text-zinc-600">Temporal (4-phase result lifecycle)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Service Mesh</h4>
              <p className="text-zinc-600">Dapr (pub/sub, state, invocation)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Auth & SSO</h4>
              <p className="text-zinc-600">Keycloak (OIDC, MFA, SSO)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Authorization</h4>
              <p className="text-zinc-600">Permify (RBAC/ABAC per action)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Caching</h4>
              <p className="text-zinc-600">Redis (dashboard cache, rate limiting)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Financial Ledger</h4>
              <p className="text-zinc-600">TigerBeetle (vote accounting)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">API Gateway</h4>
              <p className="text-zinc-600">APISIX (rate limiting, auth, routing)</p>
            </div>
            <div>
              <h4 className="font-semibold text-zinc-900 mb-1">Analytics</h4>
              <p className="text-zinc-600">Lakehouse (DuckDB/Delta Lake analytics)</p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
