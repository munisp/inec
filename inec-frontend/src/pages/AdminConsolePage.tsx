import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Settings, Users, Package, ChevronRight, Clock, CheckCircle2, Truck, BarChart3, Shield, Search } from 'lucide-react';

const LIFECYCLE_PHASES = ['created','configured','staff_deployed','materials_deployed','monitoring','voting_open','voting_closed','collation','declaration','certified','archived'];

export default function AdminConsolePage() {
  const [emsDash, setEmsDash] = useState<any>(null);
  const [lifecycle, setLifecycle] = useState<any>(null);
  const [staff, setStaff] = useState<any[]>([]);
  const [materials, setMaterials] = useState<any[]>([]);
  const [materialStats, setMaterialStats] = useState<any>(null);
  const [tab, setTab] = useState<'dashboard'|'lifecycle'|'staff'|'materials'>('dashboard');
  const [search, setSearch] = useState('');

  useEffect(() => { loadAll(); }, []);

  const loadAll = async () => {
    try {
      const [d, lc, s, m, ms] = await Promise.all([
        api.getEMSDashboard(1),
        api.getEMSLifecycle(1),
        api.getEMSStaff(),
        api.getEMSMaterials(),
        api.getEMSMaterialStats(),
      ]);
      setEmsDash(d); setLifecycle(lc); setStaff(s || []); setMaterials(m || []); setMaterialStats(ms);
    } catch {}
  };

  const phaseStatus = (phase: string) => {
    const phases = lifecycle?.phases || [];
    const found = phases.find((p: any) => p.phase === phase);
    if (found) return 'completed';
    const currentIdx = LIFECYCLE_PHASES.indexOf(lifecycle?.current_phase || '');
    const phaseIdx = LIFECYCLE_PHASES.indexOf(phase);
    if (phaseIdx <= currentIdx) return 'completed';
    if (phaseIdx === currentIdx + 1) return 'next';
    return 'pending';
  };

  const materialStatusColor = (s: string) => {
    switch(s) {
      case 'delivered': case 'acknowledged': return 'bg-green-100 text-green-800';
      case 'in_transit': case 'dispatched': return 'bg-blue-100 text-blue-800';
      case 'allocated': return 'bg-yellow-100 text-yellow-800';
      default: return 'bg-zinc-100 text-zinc-800';
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">Admin Console</h1>
          <p className="text-sm text-zinc-500">Election lifecycle management, staff deployment, and materials tracking</p>
        </div>
        <div className="flex gap-2">
          {(['dashboard','lifecycle','staff','materials'] as const).map(t => (
            <Button key={t} variant={tab === t ? 'default' : 'outline'} size="sm" onClick={() => setTab(t)} className="capitalize">{t}</Button>
          ))}
        </div>
      </div>

      {tab === 'dashboard' && emsDash && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
            {[
              { label: 'Total Voters', value: emsDash.voter_registration?.total_voters?.toLocaleString(), sub: `${emsDash.voter_registration?.pvc_rate}% PVC rate`, icon: Users, color: 'text-blue-600' },
              { label: 'Workflow Phase', value: emsDash.workflow?.current_phase || 'N/A', sub: emsDash.workflow?.status || '', icon: Settings, color: 'text-green-600' },
              { label: 'BVAS Sync Rate', value: `${emsDash.bvas_sync?.sync_rate}%`, sub: `${emsDash.bvas_sync?.conflicts} conflicts`, icon: Shield, color: 'text-purple-600' },
              { label: 'Active Portals', value: `${emsDash.portal_hub?.active}/${emsDash.portal_hub?.total_portals}`, sub: 'connected', icon: Settings, color: 'text-indigo-600' },
              { label: 'Validation Rate', value: `${emsDash.validation?.pass_rate}%`, sub: `${emsDash.validation?.total_checks} checks`, icon: CheckCircle2, color: 'text-teal-600' },
              { label: 'Materials Delivered', value: `${emsDash.materials?.delivery_rate}%`, sub: `${emsDash.materials?.delivered}/${emsDash.materials?.total_items}`, icon: Package, color: 'text-orange-600' },
              { label: 'Staff Deployed', value: emsDash.staff_deployed, sub: 'personnel', icon: Users, color: 'text-pink-600' },
              { label: 'Election ID', value: emsDash.election_id, sub: 'active', icon: BarChart3, color: 'text-cyan-600' },
            ].map(s => (
              <Card key={s.label}>
                <CardContent className="p-4">
                  <div className="flex items-center gap-2 mb-1">
                    <s.icon className={`w-4 h-4 ${s.color}`} />
                    <span className="text-xs text-zinc-500">{s.label}</span>
                  </div>
                  <p className="text-lg font-bold capitalize">{s.value}</p>
                  <p className="text-xs text-zinc-400">{s.sub}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="grid md:grid-cols-2 gap-6">
            <Card>
              <CardHeader><CardTitle className="text-sm">EMS Module Health</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {[
                    { name: 'Voter Registration', status: emsDash.voter_registration?.total_voters > 0 ? 'operational' : 'no data' },
                    { name: 'Workflow Engine', status: emsDash.workflow?.status === 'active' ? 'operational' : 'idle' },
                    { name: 'BVAS Sync Engine', status: emsDash.bvas_sync?.sync_rate > 80 ? 'operational' : 'degraded' },
                    { name: 'Portal Integration Hub', status: emsDash.portal_hub?.active > 0 ? 'operational' : 'disconnected' },
                    { name: 'Data Validation Pipeline', status: emsDash.validation?.pass_rate > 0 ? 'operational' : 'idle' },
                    { name: 'Materials Tracking', status: emsDash.materials?.total_items > 0 ? 'operational' : 'no data' },
                  ].map(m => (
                    <div key={m.name} className="flex items-center justify-between">
                      <span className="text-sm">{m.name}</span>
                      <Badge className={m.status === 'operational' ? 'bg-green-100 text-green-800' : m.status === 'degraded' ? 'bg-yellow-100 text-yellow-800' : 'bg-zinc-100 text-zinc-500'}>
                        {m.status}
                      </Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Quick Actions</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <Button variant="outline" className="w-full justify-start" size="sm" onClick={() => setTab('lifecycle')}>
                    <Clock className="w-4 h-4 mr-2" /> View Election Lifecycle
                  </Button>
                  <Button variant="outline" className="w-full justify-start" size="sm" onClick={() => setTab('staff')}>
                    <Users className="w-4 h-4 mr-2" /> Manage Staff Assignments
                  </Button>
                  <Button variant="outline" className="w-full justify-start" size="sm" onClick={() => setTab('materials')}>
                    <Package className="w-4 h-4 mr-2" /> Track Election Materials
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      )}

      {tab === 'lifecycle' && lifecycle && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Election Lifecycle — Current: <Badge className="ml-2 capitalize bg-green-100 text-green-800">{lifecycle.current_phase}</Badge></CardTitle></CardHeader>
          <CardContent>
            <div className="space-y-2">
              {LIFECYCLE_PHASES.map((phase, i) => {
                const status = phaseStatus(phase);
                return (
                  <div key={phase} className={`flex items-center gap-3 p-3 rounded-lg border ${
                    status === 'completed' ? 'bg-green-50 border-green-200' :
                    status === 'next' ? 'bg-blue-50 border-blue-200' :
                    'bg-zinc-50 border-zinc-200'
                  }`}>
                    <div className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold ${
                      status === 'completed' ? 'bg-green-500 text-white' :
                      status === 'next' ? 'bg-blue-500 text-white' :
                      'bg-zinc-200 text-zinc-500'
                    }`}>{i + 1}</div>
                    <div className="flex-1">
                      <span className="font-medium text-sm capitalize">{phase.replace(/_/g, ' ')}</span>
                    </div>
                    {status === 'completed' && <CheckCircle2 className="w-4 h-4 text-green-600" />}
                    {status === 'next' && <ChevronRight className="w-4 h-4 text-blue-600" />}
                    {status === 'pending' && <Clock className="w-4 h-4 text-zinc-400" />}
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'staff' && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm">Staff Assignments ({staff.filter((s: any) => !search || s.full_name?.toLowerCase().includes(search.toLowerCase()) || s.role?.toLowerCase().includes(search.toLowerCase()) || s.area_code?.toLowerCase().includes(search.toLowerCase())).length} of {staff.length})</CardTitle>
              <div className="relative w-48">
                <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
                <Input placeholder="Search staff..." value={search} onChange={e => setSearch(e.target.value)} className="pl-8 h-8 text-xs" />
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="border-b text-left text-zinc-500">
                  <th className="pb-2 pr-4">Name</th><th className="pb-2 pr-4">Role</th><th className="pb-2 pr-4">Area Type</th>
                  <th className="pb-2 pr-4">Area Code</th><th className="pb-2">Status</th>
                </tr></thead>
                <tbody>
                  {staff.filter((s: any) => !search || s.full_name?.toLowerCase().includes(search.toLowerCase()) || s.role?.toLowerCase().includes(search.toLowerCase()) || s.area_code?.toLowerCase().includes(search.toLowerCase())).map((s: any) => (
                    <tr key={s.id} className="border-b border-zinc-50 hover:bg-zinc-50">
                      <td className="py-2 pr-4 font-medium">{s.full_name}</td>
                      <td className="py-2 pr-4 capitalize">{s.role?.replace(/_/g, ' ')}</td>
                      <td className="py-2 pr-4 capitalize">{s.area_type?.replace(/_/g, ' ')}</td>
                      <td className="py-2 pr-4 font-mono text-xs">{s.area_code}</td>
                      <td className="py-2"><Badge className={s.status === 'active' || s.status === 'deployed' ? 'bg-green-100 text-green-800' : 'bg-zinc-100 text-zinc-800'}>{s.status}</Badge></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {staff.length === 0 && <p className="text-center text-zinc-400 py-8">No staff assignments</p>}
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'materials' && (
        <>
          {materialStats && (
            <div className="grid md:grid-cols-2 gap-6">
              <Card>
                <CardHeader><CardTitle className="text-sm">Materials by Status</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2">
                    {(materialStats.by_status || []).map((s: any) => (
                      <div key={s.status} className="flex items-center justify-between">
                        <Badge className={`capitalize ${materialStatusColor(s.status)}`}>{s.status}</Badge>
                        <span className="text-sm">{s.count} items ({Number(s.total_qty).toLocaleString()} units)</span>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
              <Card>
                <CardHeader><CardTitle className="text-sm">Materials by Type</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2">
                    {(materialStats.by_type || []).map((t: any) => (
                      <div key={t.material_type} className="flex items-center justify-between">
                        <span className="text-sm capitalize">{t.material_type?.replace(/_/g, ' ')}</span>
                        <span className="text-sm font-medium">{Number(t.total_qty).toLocaleString()} units</span>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            </div>
          )}
          <Card>
            <CardHeader><CardTitle className="text-sm flex items-center gap-2"><Truck className="w-4 h-4" /> Material Shipments ({materials.length})</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">Type</th><th className="pb-2 pr-4">Qty</th><th className="pb-2 pr-4">Destination</th>
                    <th className="pb-2 pr-4">Tracking</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {materials.slice(0, 50).map((m: any) => (
                      <tr key={m.id} className="border-b border-zinc-50 hover:bg-zinc-50">
                        <td className="py-2 pr-4 capitalize">{m.material_type?.replace(/_/g, ' ')}</td>
                        <td className="py-2 pr-4">{Number(m.quantity).toLocaleString()}</td>
                        <td className="py-2 pr-4">{m.destination_type}: {m.destination_code}</td>
                        <td className="py-2 pr-4 font-mono text-xs">{m.tracking_number || '-'}</td>
                        <td className="py-2"><Badge className={`text-xs ${materialStatusColor(m.status)}`}>{m.status}</Badge></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
