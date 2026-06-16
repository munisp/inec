import { useState, useEffect, useCallback } from 'react';

interface HealthData {
  database?: Record<string, unknown>;
  ingestion_queue?: Record<string, unknown>;
  websocket?: Record<string, unknown>;
  sse_connections?: number;
  rate_limiter?: Record<string, unknown>;
  middleware_modes?: Array<{ Name: string; IsReal: boolean; Connection: string }>;
}

export default function ScaleHealthPage() {
  const [health, setHealth] = useState<HealthData | null>(null);
  const [modes, setModes] = useState<Array<{ Name: string; IsReal: boolean; Connection: string }>>([]);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const loadHealth = useCallback(async () => {
    try {
      const opts = { credentials: 'include' as RequestCredentials };
      const [h, m] = await Promise.all([
        fetch('/scale/health', opts).then(r => r.ok ? r.json() : null),
        fetch('/middleware/modes', opts).then(r => r.ok ? r.json() : []),
      ]);
      if (h) setHealth(h);
      setModes(m);
    } catch { /* */ }
    setLoading(false);
  }, []);

  useEffect(() => { loadHealth(); }, [loadHealth]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(loadHealth, 5000);
    return () => clearInterval(interval);
  }, [autoRefresh, loadHealth]);

  const renderKV = (data: Record<string, unknown>, depth = 0): React.ReactNode => (
    <div className={depth > 0 ? 'ml-4 border-l-2 border-zinc-100 dark:border-zinc-700 pl-3' : ''}>
      {Object.entries(data).map(([k, v]) => {
        if (typeof v === 'object' && v !== null && !Array.isArray(v)) {
          return (
            <div key={k} className="mb-2">
              <span className="text-xs font-semibold text-zinc-500 uppercase tracking-wide">{k.replace(/_/g, ' ')}</span>
              {renderKV(v as Record<string, unknown>, depth + 1)}
            </div>
          );
        }
        return (
          <div key={k} className="flex justify-between py-1.5 border-b border-zinc-50 dark:border-zinc-800">
            <span className="text-sm text-zinc-500 dark:text-zinc-400 capitalize">{k.replace(/_/g, ' ')}</span>
            <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{String(v ?? '-')}</span>
          </div>
        );
      })}
    </div>
  );

  if (loading) return <div className="animate-pulse space-y-4 p-6">{[1,2,3,4].map(i => <div key={i} className="h-32 bg-zinc-100 dark:bg-zinc-800 rounded-xl" />)}</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900 dark:text-zinc-100">Scale Health</h1>
          <p className="text-sm text-zinc-500 dark:text-zinc-400">Real-time platform performance and capacity</p>
        </div>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm text-zinc-500 cursor-pointer">
            <input type="checkbox" checked={autoRefresh} onChange={e => setAutoRefresh(e.target.checked)} className="rounded" />
            Auto-refresh (5s)
          </label>
          <button onClick={loadHealth} className="px-3 py-1.5 bg-zinc-100 dark:bg-zinc-800 rounded-lg text-sm font-medium hover:bg-zinc-200 dark:hover:bg-zinc-700 transition">
            Refresh
          </button>
        </div>
      </div>

      {health && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {health.database && (
            <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-8 h-8 rounded-lg bg-blue-50 flex items-center justify-center text-blue-600">🗄️</div>
                <h3 className="font-semibold">Database</h3>
              </div>
              {renderKV(health.database)}
            </div>
          )}

          {health.ingestion_queue && (
            <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-8 h-8 rounded-lg bg-yellow-50 flex items-center justify-center text-yellow-600">📥</div>
                <h3 className="font-semibold">Ingestion Queue</h3>
              </div>
              {renderKV(health.ingestion_queue)}
            </div>
          )}

          {health.websocket && (
            <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-8 h-8 rounded-lg bg-purple-50 flex items-center justify-center text-purple-600">📡</div>
                <h3 className="font-semibold">WebSocket</h3>
              </div>
              {renderKV(health.websocket)}
            </div>
          )}

          {health.rate_limiter && (
            <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-8 h-8 rounded-lg bg-red-50 flex items-center justify-center text-red-600">🛡️</div>
                <h3 className="font-semibold">Rate Limiter</h3>
              </div>
              {renderKV(health.rate_limiter)}
            </div>
          )}
        </div>
      )}

      {health?.sse_connections !== undefined && (
        <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
          <div className="flex items-center gap-2 mb-1">
            <span className="text-sm font-semibold text-zinc-500">SSE Connections</span>
            <span className="text-2xl font-bold text-zinc-900 dark:text-zinc-100">{health.sse_connections}</span>
          </div>
        </div>
      )}

      {modes.length > 0 && (
        <div className="bg-white dark:bg-zinc-900 rounded-xl p-5 border border-zinc-200 dark:border-zinc-700 shadow-sm">
          <h3 className="font-semibold mb-3">Middleware Status</h3>
          <div className="space-y-2">
            {modes.map((m, i) => (
              <div key={i} className="flex items-center gap-3 py-2 border-b border-zinc-50 dark:border-zinc-800 last:border-0">
                <div className={`w-2.5 h-2.5 rounded-full ${m.IsReal ? 'bg-green-500' : 'bg-yellow-500'}`} />
                <span className="text-sm font-medium flex-1">{m.Name}</span>
                <span className="text-xs text-zinc-400 truncate max-w-[200px]">{m.Connection}</span>
                <span className={`px-2 py-0.5 rounded text-xs font-bold ${m.IsReal ? 'bg-green-50 text-green-700' : 'bg-yellow-50 text-yellow-700'}`}>
                  {m.IsReal ? 'REAL' : 'EMBEDDED'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
