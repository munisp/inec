import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Lock, Link2, FileCheck, Shield, Activity, Hash } from 'lucide-react';

export default function BlockchainPage() {
  const [stats, setStats] = useState<any>(null);
  const [chain, setChain] = useState<any>(null);
  const [contracts, setContracts] = useState<any>(null);
  const [audit, setAudit] = useState<any>(null);
  const [tab, setTab] = useState('overview');

  useEffect(() => {
    api.getBlockchainStats().then(setStats).catch(() => {});
    api.getBlockchainChain(50).then(setChain).catch(() => {});
    api.getSmartContracts().then(setContracts).catch(() => {});
    api.getBlockchainAudit(50).then(setAudit).catch(() => {});
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Blockchain-Enhanced Result Transmission</h2>
        <p className="text-zinc-500 text-sm">Immutable audit trail with smart contract validation</p>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { label: 'Total Blocks', value: stats.total_blocks, icon: Link2, color: 'blue' },
            { label: 'Validated', value: stats.validated, icon: FileCheck, color: 'green' },
            { label: 'Pending', value: stats.pending, icon: Activity, color: 'amber' },
            { label: 'Disputed', value: stats.disputed, icon: Shield, color: 'red' },
            { label: 'Integrity Rate', value: `${stats.integrity_rate?.toFixed(1)}%`, icon: Lock, color: 'emerald' },
            { label: 'Audit Entries', value: stats.audit_entries, icon: Hash, color: 'purple' },
          ].map((s, i) => (
            <Card key={i}>
              <CardContent className="pt-4 pb-3">
                <div className="flex items-center gap-2 mb-1">
                  <s.icon className={`w-4 h-4 text-${s.color}-600`} />
                  <span className="text-xs text-zinc-500">{s.label}</span>
                </div>
                <p className="text-xl font-bold">{s.value}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="overview">Block Chain</TabsTrigger>
          <TabsTrigger value="contracts">Smart Contracts</TabsTrigger>
          <TabsTrigger value="audit">Audit Trail</TabsTrigger>
          <TabsTrigger value="levels">By Level</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <Card>
            <CardHeader><CardTitle className="text-sm">Result Blockchain</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-3">Block #</th><th className="pb-2 pr-3">Result ID</th><th className="pb-2 pr-3">EC8A Hash</th><th className="pb-2 pr-3">Block Hash</th><th className="pb-2 pr-3">Level</th><th className="pb-2 pr-3">Validators</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {chain?.blocks?.map((b: any) => (
                      <tr key={b.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-3 font-mono text-xs">#{b.block_index}</td>
                        <td className="py-2 pr-3">{b.result_id}</td>
                        <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{b.ec8a_hash?.slice(0, 12)}...</td>
                        <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{b.block_hash}</td>
                        <td className="py-2 pr-3"><Badge variant="outline" className="text-xs capitalize">{b.level?.replace('_', ' ')}</Badge></td>
                        <td className="py-2 pr-3">{b.validators}</td>
                        <td className="py-2">
                          <Badge variant={b.validation_status === 'validated' ? 'default' : b.validation_status === 'disputed' ? 'destructive' : 'outline'} className="text-xs">
                            {b.validation_status}
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="contracts">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm flex items-center justify-between">
                Smart Contracts
                {stats && <div className="flex gap-2 text-xs font-normal">
                  <Badge variant="outline">Active: {stats.smart_contracts?.active}</Badge>
                  <Badge className="bg-green-100 text-green-700">Executed: {stats.smart_contracts?.executed}</Badge>
                </div>}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-3">Contract ID</th><th className="pb-2 pr-3">Type</th><th className="pb-2 pr-3">Level</th><th className="pb-2 pr-3">Area</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {contracts?.contracts?.map((c: any) => (
                      <tr key={c.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-3 font-mono text-xs">{c.contract_id}</td>
                        <td className="py-2 pr-3 capitalize text-xs">{c.type?.replace('_', ' ')}</td>
                        <td className="py-2 pr-3 capitalize">{c.level?.replace('_', ' ')}</td>
                        <td className="py-2 pr-3">{c.area_code}</td>
                        <td className="py-2">
                          <Badge variant={c.status === 'executed' ? 'default' : 'outline'} className="text-xs">{c.status}</Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="audit">
          <Card>
            <CardHeader><CardTitle className="text-sm">Blockchain Audit Trail</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-2">
                {audit?.entries?.map((e: any) => (
                  <div key={e.id} className="flex items-center justify-between py-2 border-b border-zinc-50">
                    <div className="flex items-center gap-3">
                      <div className="w-8 h-8 rounded-full bg-blue-50 flex items-center justify-center">
                        <Lock className="w-4 h-4 text-blue-600" />
                      </div>
                      <div>
                        <p className="text-sm font-medium">{e.action?.replace('_', ' ')}</p>
                        <p className="text-xs text-zinc-500">{e.entity_type} #{e.entity_id} by {e.actor}</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <p className="font-mono text-xs text-zinc-400">{e.tx_hash}</p>
                      <p className="text-xs text-zinc-500">{new Date(e.timestamp).toLocaleString()}</p>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="levels">
          <Card>
            <CardHeader><CardTitle className="text-sm">Validation by Level</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-4">
                {stats?.by_level?.map((l: any, i: number) => (
                  <div key={i}>
                    <div className="flex justify-between mb-1">
                      <span className="text-sm font-medium capitalize">{l.level?.replace('_', ' ')}</span>
                      <span className="text-sm">{l.validated}/{l.total} validated</span>
                    </div>
                    <div className="w-full h-3 bg-zinc-100 rounded-full overflow-hidden">
                      <div className="h-full bg-green-500 rounded-full" style={{ width: `${l.total > 0 ? (l.validated / l.total) * 100 : 0}%` }} />
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
