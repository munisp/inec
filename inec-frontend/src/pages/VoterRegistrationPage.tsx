import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Users, Search, UserPlus, CreditCard, MapPin, BarChart3 } from 'lucide-react';

interface VoterStats {
  total: number; active: number; verified: number; registered: number;
  pvc_collected: number; pvc_collection_rate: number;
  by_state: Array<{state_code: string; name: string; count: number; pvc_collected: number}>;
  by_gender: Array<{gender: string; count: number}>;
}

export default function VoterRegistrationPage() {
  const [stats, setStats] = useState<VoterStats | null>(null);
  const [voters, setVoters] = useState<any[]>([]);
  const [centers, setCenters] = useState<any[]>([]);
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState('');
  const [total, setTotal] = useState(0);
  const [tab, setTab] = useState<'voters'|'stats'|'centers'>('stats');

  useEffect(() => { loadStats(); loadCenters(); }, []);
  useEffect(() => { if (tab === 'voters') loadVoters(); }, [tab, search, stateFilter]);

  const loadStats = async () => {
    try { const d = await api.getEMSVoterStats(); setStats(d); } catch {}
  };
  const loadVoters = async () => {
    try {
      const params: Record<string,string> = { limit: '50' };
      if (search) params.search = search;
      if (stateFilter) params.state_code = stateFilter;
      const d = await api.getEMSVoters(params);
      setVoters(d.voters || []); setTotal(d.total || 0);
    } catch {}
  };
  const loadCenters = async () => {
    try { const d = await api.getEMSRegistrationCenters(); setCenters(d || []); } catch {}
  };

  const statusColor = (s: string) => {
    switch(s) {
      case 'active': return 'bg-green-100 text-green-800';
      case 'verified': return 'bg-blue-100 text-blue-800';
      case 'registered': return 'bg-yellow-100 text-yellow-800';
      case 'suspended': return 'bg-red-100 text-red-800';
      default: return 'bg-zinc-100 text-zinc-800';
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-zinc-900">Voter Registration</h1>
          <p className="text-sm text-zinc-500">Manage voter roll, PVC collection, and registration centers</p>
        </div>
        <div className="flex gap-2">
          {(['stats','voters','centers'] as const).map(t => (
            <Button key={t} variant={tab === t ? 'default' : 'outline'} size="sm" onClick={() => setTab(t)} className="capitalize">{t}</Button>
          ))}
        </div>
      </div>

      {tab === 'stats' && stats && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
            {[
              { label: 'Total Voters', value: stats.total.toLocaleString(), icon: Users, color: 'text-blue-600' },
              { label: 'Active', value: stats.active.toLocaleString(), icon: Users, color: 'text-green-600' },
              { label: 'Verified', value: stats.verified.toLocaleString(), icon: Users, color: 'text-indigo-600' },
              { label: 'Registered', value: stats.registered.toLocaleString(), icon: UserPlus, color: 'text-yellow-600' },
              { label: 'PVC Collected', value: stats.pvc_collected.toLocaleString(), icon: CreditCard, color: 'text-purple-600' },
              { label: 'PVC Rate', value: `${stats.pvc_collection_rate}%`, icon: BarChart3, color: 'text-teal-600' },
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

          <div className="grid md:grid-cols-2 gap-6">
            <Card>
              <CardHeader><CardTitle className="text-sm">Voters by State (Top 10)</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {(stats.by_state || []).slice(0, 10).map((s: any) => (
                    <div key={s.state_code} className="flex items-center justify-between text-sm">
                      <span className="font-medium">{s.name}</span>
                      <div className="flex items-center gap-3">
                        <span>{Number(s.count).toLocaleString()} voters</span>
                        <Badge variant="outline" className="text-xs">{Number(s.pvc_collected)} PVC</Badge>
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Gender Distribution</CardTitle></CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {(stats.by_gender || []).map((g: any) => {
                    const pct = stats.total > 0 ? (Number(g.count) / stats.total * 100).toFixed(1) : '0';
                    return (
                      <div key={g.gender}>
                        <div className="flex justify-between text-sm mb-1">
                          <span>{g.gender === 'M' ? 'Male' : 'Female'}</span>
                          <span>{Number(g.count).toLocaleString()} ({pct}%)</span>
                        </div>
                        <div className="w-full bg-zinc-100 rounded-full h-2">
                          <div className={`h-2 rounded-full ${g.gender === 'M' ? 'bg-blue-500' : 'bg-pink-500'}`} style={{width: `${pct}%`}} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      )}

      {tab === 'voters' && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm">Voter Roll ({total.toLocaleString()} voters)</CardTitle>
              <div className="flex gap-2">
                <div className="relative">
                  <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-zinc-400" />
                  <Input placeholder="Search by name, VIN, PVC..." className="pl-8 w-64" value={search} onChange={e => setSearch(e.target.value)} />
                </div>
                <select className="border rounded px-2 py-1 text-sm" value={stateFilter} onChange={e => setStateFilter(e.target.value)}>
                  <option value="">All States</option>
                  {(stats?.by_state || []).map((s: any) => <option key={s.state_code} value={s.state_code}>{s.name}</option>)}
                </select>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead><tr className="border-b text-left text-zinc-500">
                  <th className="pb-2 pr-4">VIN</th><th className="pb-2 pr-4">Name</th><th className="pb-2 pr-4">Gender</th>
                  <th className="pb-2 pr-4">DOB</th><th className="pb-2 pr-4">State</th><th className="pb-2 pr-4">PU Code</th>
                  <th className="pb-2 pr-4">PVC</th><th className="pb-2">Status</th>
                </tr></thead>
                <tbody>
                  {voters.map((v: any) => (
                    <tr key={v.vin} className="border-b border-zinc-50 hover:bg-zinc-50">
                      <td className="py-2 pr-4 font-mono text-xs">{v.vin}</td>
                      <td className="py-2 pr-4">{v.first_name} {v.last_name}</td>
                      <td className="py-2 pr-4">{v.gender === 'M' ? 'Male' : 'Female'}</td>
                      <td className="py-2 pr-4">{v.date_of_birth}</td>
                      <td className="py-2 pr-4">{v.state_code}</td>
                      <td className="py-2 pr-4 font-mono text-xs">{v.polling_unit_code}</td>
                      <td className="py-2 pr-4">{v.pvc_collected === 1 ? <Badge className="bg-green-100 text-green-800 text-xs">Collected</Badge> : <Badge variant="outline" className="text-xs">Pending</Badge>}</td>
                      <td className="py-2"><Badge className={`text-xs ${statusColor(v.status)}`}>{v.status}</Badge></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'centers' && (
        <Card>
          <CardHeader><CardTitle className="text-sm flex items-center gap-2"><MapPin className="w-4 h-4" /> Registration Centers ({centers.length})</CardTitle></CardHeader>
          <CardContent>
            <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-4">
              {centers.map((c: any) => (
                <div key={c.code} className="border rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="font-medium text-sm">{c.name}</span>
                    <Badge className={c.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}>{c.status}</Badge>
                  </div>
                  <div className="text-xs text-zinc-500 space-y-1">
                    <p>Code: {c.code}</p>
                    <p>State: {c.state_code} | Capacity: {c.capacity}</p>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
