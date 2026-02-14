import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Globe, RefreshCw, ArrowRightLeft, CheckCircle2, AlertCircle, Activity } from 'lucide-react';

export default function PortalIntegrationPage() {
  const [hubStatus, setHubStatus] = useState<any>(null);
  const [selectedPortal, setSelectedPortal] = useState<any>(null);
  const [syncLog, setSyncLog] = useState<any[]>([]);
  const [syncing, setSyncing] = useState<number | null>(null);

  useEffect(() => { loadStatus(); loadSyncLog(); }, []);

  const loadStatus = async () => {
    try { const d = await api.getEMSPortalStatus(); setHubStatus(d); } catch {}
  };
  const loadSyncLog = async () => {
    try { const d = await api.getEMSPortalSyncLog(); setSyncLog(d || []); } catch {}
  };
  const loadPortal = async (id: number) => {
    try { const d = await api.getEMSPortal(id); setSelectedPortal(d); } catch {}
  };
  const triggerSync = async (id: number) => {
    setSyncing(id);
    try { await api.syncEMSPortal(id); await loadStatus(); await loadSyncLog(); if (selectedPortal?.id === id) await loadPortal(id); } catch {}
    setSyncing(null);
  };

  const portalTypeIcon = (type: string) => {
    const colors: Record<string, string> = {
      irev: 'bg-green-100 text-green-700', icnp: 'bg-blue-100 text-blue-700',
      press: 'bg-purple-100 text-purple-700', croms: 'bg-orange-100 text-orange-700',
      bvas_portal: 'bg-red-100 text-red-700', custom: 'bg-zinc-100 text-zinc-700',
    };
    return colors[type] || colors.custom;
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">Portal Integration Hub</h1>
          <p className="text-sm text-zinc-500">Secure API connections to IReV, ICNP, PRESS, CROMS, and BVAS portals</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => { loadStatus(); loadSyncLog(); }}><RefreshCw className="w-4 h-4 mr-1" /> Refresh</Button>
      </div>

      {hubStatus && (
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
          {[
            { label: 'Total Portals', value: hubStatus.total_portals, color: 'text-blue-600' },
            { label: 'Active', value: hubStatus.active_portals, color: 'text-green-600' },
            { label: 'Total Syncs', value: hubStatus.total_syncs, color: 'text-purple-600' },
            { label: 'Records Synced', value: hubStatus.total_synced?.toLocaleString(), color: 'text-indigo-600' },
            { label: 'Success Rate', value: `${hubStatus.success_rate}%`, color: 'text-teal-600' },
          ].map(s => (
            <Card key={s.label}>
              <CardContent className="p-4">
                <span className="text-xs text-zinc-500">{s.label}</span>
                <p className={`text-xl font-bold ${s.color}`}>{s.value}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <div className="grid lg:grid-cols-3 gap-6">
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-zinc-700">Connected Portals</h3>
          {(hubStatus?.portals || []).map((p: any) => (
            <Card key={p.id} className={`cursor-pointer transition-colors ${selectedPortal?.id === p.id ? 'ring-2 ring-green-500' : 'hover:bg-zinc-50'}`}
              onClick={() => loadPortal(p.id)}>
              <CardContent className="p-4">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${portalTypeIcon(p.portal_type)}`}>
                      <Globe className="w-4 h-4" />
                    </div>
                    <div>
                      <p className="font-medium text-sm">{p.portal_name}</p>
                      <p className="text-xs text-zinc-400 uppercase">{p.portal_type}</p>
                    </div>
                  </div>
                  <Badge className={p.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}>{p.status}</Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-zinc-400">Last sync: {p.last_sync_at ? new Date(p.last_sync_at).toLocaleString() : 'Never'}</span>
                  <Button size="sm" variant="ghost" onClick={(e) => { e.stopPropagation(); triggerSync(p.id); }} disabled={syncing === p.id}>
                    <ArrowRightLeft className={`w-3 h-3 ${syncing === p.id ? 'animate-spin' : ''}`} />
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="lg:col-span-2 space-y-4">
          {selectedPortal ? (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm">{selectedPortal.portal_name}</CardTitle>
                  <Button size="sm" onClick={() => triggerSync(selectedPortal.id)} disabled={syncing === selectedPortal.id}>
                    <ArrowRightLeft className="w-4 h-4 mr-1" /> Sync Now
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-4 mb-4">
                  <div className="p-3 bg-zinc-50 rounded-lg">
                    <p className="text-xs text-zinc-500">Type</p>
                    <p className="font-medium capitalize">{selectedPortal.portal_type}</p>
                  </div>
                  <div className="p-3 bg-zinc-50 rounded-lg">
                    <p className="text-xs text-zinc-500">Base URL</p>
                    <p className="font-mono text-xs truncate">{selectedPortal.base_url}</p>
                  </div>
                  <div className="p-3 bg-green-50 rounded-lg">
                    <p className="text-xs text-zinc-500">Records Synced</p>
                    <p className="font-bold text-green-700">{selectedPortal.total_records_synced?.toLocaleString()}</p>
                  </div>
                  <div className="p-3 bg-red-50 rounded-lg">
                    <p className="text-xs text-zinc-500">Records Failed</p>
                    <p className="font-bold text-red-700">{selectedPortal.total_records_failed}</p>
                  </div>
                </div>
                <h4 className="text-sm font-medium mb-2">Recent Syncs</h4>
                <div className="space-y-2">
                  {(selectedPortal.recent_syncs || []).slice(0, 8).map((s: any, i: number) => (
                    <div key={i} className="flex items-center justify-between text-sm border-b border-zinc-50 py-1.5">
                      <div className="flex items-center gap-2">
                        {s.status === 'completed' ? <CheckCircle2 className="w-3.5 h-3.5 text-green-600" /> : <AlertCircle className="w-3.5 h-3.5 text-red-600" />}
                        <span className="capitalize">{s.sync_type}</span>
                        <Badge variant="outline" className="text-xs">{s.entity_type}</Badge>
                      </div>
                      <div className="flex items-center gap-3">
                        <span className="text-xs text-green-600">{s.records_synced} synced</span>
                        {Number(s.records_failed) > 0 && <span className="text-xs text-red-600">{s.records_failed} failed</span>}
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader><CardTitle className="text-sm">Sync Activity Log</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {syncLog.slice(0, 15).map((s: any, i: number) => (
                    <div key={i} className="flex items-center justify-between text-sm border-b border-zinc-50 py-1.5">
                      <div className="flex items-center gap-2">
                        <Activity className="w-3.5 h-3.5 text-zinc-400" />
                        <span>{s.portal_name}</span>
                        <Badge variant="outline" className="text-xs capitalize">{s.sync_type} / {s.entity_type}</Badge>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-green-600">{s.records_synced}</span>
                        <Badge className={s.status === 'completed' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}>{s.status}</Badge>
                      </div>
                    </div>
                  ))}
                  {syncLog.length === 0 && <p className="text-center text-zinc-400 py-8">No sync activity yet</p>}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
