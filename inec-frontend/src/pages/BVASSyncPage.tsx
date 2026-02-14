import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { RefreshCw, Wifi, WifiOff, Battery, Activity, AlertTriangle, CheckCircle2 } from 'lucide-react';

export default function BVASSyncPage() {
  const [stats, setStats] = useState<any>(null);
  const [queue, setQueue] = useState<any[]>([]);
  const [tab, setTab] = useState<'overview'|'queue'|'heartbeats'>('overview');

  useEffect(() => { loadData(); }, []);

  const loadData = async () => {
    try {
      const [s, q] = await Promise.all([api.getEMSSyncStats(), api.getEMSSyncQueue()]);
      setStats(s); setQueue(q || []);
    } catch {}
  };

  const resolveConflict = async (id: number) => {
    try { await api.resolveEMSSyncConflict(id, 'accepted'); loadData(); } catch {}
  };

  const statusColor = (s: string) => {
    switch(s) {
      case 'synced': return 'bg-green-100 text-green-800';
      case 'queued': return 'bg-yellow-100 text-yellow-800';
      case 'conflict': return 'bg-red-100 text-red-800';
      case 'failed': return 'bg-red-100 text-red-800';
      case 'resolved': return 'bg-blue-100 text-blue-800';
      default: return 'bg-zinc-100 text-zinc-800';
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">BVAS Sync Engine</h1>
          <p className="text-sm text-zinc-500">Offline queue management, conflict resolution, and device heartbeat monitoring</p>
        </div>
        <div className="flex gap-2">
          {(['overview','queue','heartbeats'] as const).map(t => (
            <Button key={t} variant={tab === t ? 'default' : 'outline'} size="sm" onClick={() => setTab(t)} className="capitalize">{t}</Button>
          ))}
          <Button variant="outline" size="sm" onClick={loadData}><RefreshCw className="w-4 h-4" /></Button>
        </div>
      </div>

      {tab === 'overview' && stats && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
            {[
              { label: 'Total Items', value: stats.total, icon: Activity, color: 'text-blue-600' },
              { label: 'Synced', value: stats.synced, icon: CheckCircle2, color: 'text-green-600' },
              { label: 'Queued', value: stats.queued, icon: RefreshCw, color: 'text-yellow-600' },
              { label: 'Conflicts', value: stats.conflicts, icon: AlertTriangle, color: 'text-red-600' },
              { label: 'Failed', value: stats.failed, icon: WifiOff, color: 'text-red-600' },
              { label: 'Offline Devices', value: stats.offline_devices, icon: WifiOff, color: 'text-orange-600' },
            ].map(s => (
              <Card key={s.label}>
                <CardContent className="p-4">
                  <div className="flex items-center gap-2 mb-1">
                    <s.icon className={`w-4 h-4 ${s.color}`} />
                    <span className="text-xs text-zinc-500">{s.label}</span>
                  </div>
                  <p className="text-xl font-bold">{s.value}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          {stats.total > 0 && (
            <Card>
              <CardHeader><CardTitle className="text-sm">Sync Health</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  <div>
                    <div className="flex justify-between text-sm mb-1">
                      <span>Sync Success Rate</span>
                      <span className="font-medium">{stats.total > 0 ? ((stats.synced / stats.total) * 100).toFixed(1) : 0}%</span>
                    </div>
                    <div className="w-full bg-zinc-100 rounded-full h-3">
                      <div className="h-3 rounded-full bg-green-500" style={{width: `${stats.total > 0 ? (stats.synced / stats.total) * 100 : 0}%`}} />
                    </div>
                  </div>
                  <div className="grid grid-cols-3 gap-4 text-center">
                    <div className="p-3 bg-green-50 rounded-lg">
                      <Wifi className="w-5 h-5 text-green-600 mx-auto mb-1" />
                      <p className="text-lg font-bold text-green-700">{stats.synced}</p>
                      <p className="text-xs text-green-600">Synced</p>
                    </div>
                    <div className="p-3 bg-yellow-50 rounded-lg">
                      <RefreshCw className="w-5 h-5 text-yellow-600 mx-auto mb-1" />
                      <p className="text-lg font-bold text-yellow-700">{stats.queued}</p>
                      <p className="text-xs text-yellow-600">Pending</p>
                    </div>
                    <div className="p-3 bg-red-50 rounded-lg">
                      <AlertTriangle className="w-5 h-5 text-red-600 mx-auto mb-1" />
                      <p className="text-lg font-bold text-red-700">{stats.conflicts}</p>
                      <p className="text-xs text-red-600">Conflicts</p>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </>
      )}

      {tab === 'queue' && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Sync Queue ({queue.length} items)</CardTitle></CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="border-b text-left text-zinc-500">
                  <th className="pb-2 pr-4">ID</th><th className="pb-2 pr-4">Device</th><th className="pb-2 pr-4">Type</th>
                  <th className="pb-2 pr-4">Priority</th><th className="pb-2 pr-4">Status</th><th className="pb-2 pr-4">Created</th><th className="pb-2">Actions</th>
                </tr></thead>
                <tbody>
                  {queue.map((item: any) => (
                    <tr key={item.id} className="border-b border-zinc-50 hover:bg-zinc-50">
                      <td className="py-2 pr-4 font-mono text-xs">#{item.id}</td>
                      <td className="py-2 pr-4 font-mono text-xs">{item.device_id}</td>
                      <td className="py-2 pr-4 capitalize">{item.sync_type}</td>
                      <td className="py-2 pr-4">{item.priority}</td>
                      <td className="py-2 pr-4"><Badge className={`text-xs ${statusColor(item.status)}`}>{item.status}</Badge></td>
                      <td className="py-2 pr-4 text-xs">{item.created_at ? new Date(item.created_at).toLocaleString() : '-'}</td>
                      <td className="py-2">
                        {item.status === 'conflict' && (
                          <Button size="sm" variant="outline" onClick={() => resolveConflict(item.id)}>Resolve</Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {queue.length === 0 && <p className="text-center text-zinc-400 py-8">Sync queue is empty</p>}
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'heartbeats' && stats && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Recent Heartbeats</CardTitle></CardHeader>
          <CardContent>
            <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-4">
              {(stats.recent_heartbeats || []).map((hb: any, i: number) => (
                <div key={i} className="border rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="font-mono text-xs font-medium">{hb.device_id}</span>
                    <Badge variant="outline" className="text-xs">{hb.firmware_version || 'N/A'}</Badge>
                  </div>
                  <div className="space-y-1 text-xs text-zinc-500">
                    <div className="flex items-center gap-1"><Battery className="w-3 h-3" /> Battery: {hb.battery_level}%</div>
                    <div className="flex items-center gap-1"><Wifi className="w-3 h-3" /> Signal: {hb.signal_strength}%</div>
                    <div className="flex items-center gap-1"><Activity className="w-3 h-3" /> Queue: {hb.sync_queue_size} items</div>
                    <div className="flex items-center gap-1"><RefreshCw className="w-3 h-3" /> Uptime: {Math.floor((hb.uptime_seconds || 0) / 3600)}h</div>
                  </div>
                </div>
              ))}
            </div>
            {(!stats.recent_heartbeats || stats.recent_heartbeats.length === 0) && (
              <p className="text-center text-zinc-400 py-8">No heartbeat data yet</p>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
