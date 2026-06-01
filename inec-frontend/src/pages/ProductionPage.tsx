import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

function StatusBadge({ status }: { status: string }) {
  const color = status === 'active' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800';
  return <Badge className={color}>{status}</Badge>;
}

function MetricCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <Card>
      <CardContent className="p-4">
        <p className="text-xs text-zinc-500">{label}</p>
        <p className="text-2xl font-bold text-zinc-900">{value}</p>
        {sub && <p className="text-xs text-zinc-400 mt-1">{sub}</p>}
      </CardContent>
    </Card>
  );
}

export default function ProductionPage() {
  const [tab, setTab] = useState('overview');
  const [status, setStatus] = useState<Record<string, any> | null>(null);
  const [hsm, setHsm] = useState<Record<string, any> | null>(null);
  const [sms, setSms] = useState<Record<string, any> | null>(null);
  const [pad, setPad] = useState<Record<string, any> | null>(null);
  const [ipfs, setIpfs] = useState<Record<string, any> | null>(null);
  const [fabric, setFabric] = useState<Record<string, any> | null>(null);
  const [ledger, setLedger] = useState<Record<string, any> | null>(null);
  const [dbMetrics, setDbMetrics] = useState<Record<string, any> | null>(null);
  const [pgpool, setPgpool] = useState<Record<string, any> | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadAll();
  }, []);

  async function loadAll() {
    setLoading(true);
    try {
      const [s, h, sm, p, i, f, l, d, pg] = await Promise.allSettled([
        api.getProductionStatus(),
        api.getProductionHSMStats(),
        api.getProductionSMSStats(),
        api.getProductionPADStats(),
        api.getProductionIPFSStats(),
        api.getProductionFabricStats(),
        api.getProductionLedgerStats(),
        api.getDBMetrics(),
        api.getPgpoolStatus(),
      ]);
      if (s.status === 'fulfilled') setStatus(s.value);
      if (h.status === 'fulfilled') setHsm(h.value);
      if (sm.status === 'fulfilled') setSms(sm.value);
      if (p.status === 'fulfilled') setPad(p.value);
      if (i.status === 'fulfilled') setIpfs(i.value);
      if (f.status === 'fulfilled') setFabric(f.value);
      if (l.status === 'fulfilled') setLedger(l.value);
      if (d.status === 'fulfilled') setDbMetrics(d.value);
      if (pg.status === 'fulfilled') setPgpool(pg.value);
    } catch (e) { console.error(e); }
    setLoading(false);
  }

  if (loading) return <div className="flex items-center justify-center h-64"><p className="text-zinc-500">Loading production dashboard...</p></div>;

  const components = status?.components || {};

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-zinc-900">Production Infrastructure</h2>
          <p className="text-sm text-zinc-500">Real-time monitoring of production-grade components</p>
        </div>
        <Button onClick={loadAll} variant="outline" size="sm">Refresh</Button>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
        {Object.entries(components).map(([key, val]: [string, any]) => (
          <Card key={key} className="border-l-4 border-l-green-500">
            <CardContent className="p-3">
              <p className="text-xs font-medium text-zinc-500 uppercase">{key.replace('_', ' ')}</p>
              <StatusBadge status={val.status || 'unknown'} />
            </CardContent>
          </Card>
        ))}
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="flex flex-wrap">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="hsm">HSM</TabsTrigger>
          <TabsTrigger value="sms">SMS Gateway</TabsTrigger>
          <TabsTrigger value="pad">PAD Engine</TabsTrigger>
          <TabsTrigger value="ipfs">IPFS</TabsTrigger>
          <TabsTrigger value="fabric">Fabric</TabsTrigger>
          <TabsTrigger value="ledger">Ledger</TabsTrigger>
          <TabsTrigger value="database">Database</TabsTrigger>
          <TabsTrigger value="pgpool">Pgpool-II</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <Card>
              <CardHeader><CardTitle className="text-sm">HSM</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">Algorithm:</span> {hsm?.algorithm}</p>
                <p><span className="text-zinc-500">Mode:</span> {hsm?.mode}</p>
                <p><span className="text-zinc-500">Keys:</span> {hsm?.total_keys} ({hsm?.active_keys} active)</p>
                <p><span className="text-zinc-500">Operations:</span> {hsm?.total_ops_logged}</p>
                <div className="flex flex-wrap gap-1 mt-1">{hsm?.compliance?.map((c: string) => <Badge key={c} variant="outline" className="text-xs">{c}</Badge>)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">SMS Gateway</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">Provider:</span> {sms?.provider}</p>
                <p><span className="text-zinc-500">Mode:</span> <Badge variant={sms?.mode === 'live' ? 'default' : 'secondary'}>{sms?.mode}</Badge></p>
                <p><span className="text-zinc-500">Sent:</span> {sms?.total_sent}</p>
                <p><span className="text-zinc-500">Failed:</span> {sms?.total_failed}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">PAD Engine</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">ISO Compliance:</span> {pad?.iso_compliance}</p>
                <p><span className="text-zinc-500">Models:</span> {pad?.models?.length}</p>
                <p><span className="text-zinc-500">Total Checks:</span> {pad?.total_checks}</p>
                <p><span className="text-zinc-500">Attacks Blocked:</span> {pad?.attacks_blocked}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">IPFS</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">CID Version:</span> {ipfs?.cid_version}</p>
                <p><span className="text-zinc-500">Objects:</span> {ipfs?.total_objects}</p>
                <p><span className="text-zinc-500">DAG Nodes:</span> {ipfs?.total_dag_nodes}</p>
                <p><span className="text-zinc-500">Codecs:</span> {ipfs?.codecs?.join(', ')}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Fabric</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">Blocks:</span> {fabric?.total_blocks}</p>
                <p><span className="text-zinc-500">Transactions:</span> {fabric?.total_transactions}</p>
                <p><span className="text-zinc-500">Endorsements:</span> {fabric?.total_endorsements}</p>
                <p><span className="text-zinc-500">Consensus:</span> {fabric?.consensus}</p>
                <p><span className="text-zinc-500">Peers:</span> {fabric?.peers?.join(', ')}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">TigerBeetle Ledger</CardTitle></CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p><span className="text-zinc-500">Accounts:</span> {ledger?.total_accounts}</p>
                <p><span className="text-zinc-500">Transfers:</span> {ledger?.total_transfers}</p>
                <p><span className="text-zinc-500">Journal Entries:</span> {ledger?.journal_entries}</p>
                <p><span className="text-zinc-500">ACID:</span> <Badge variant="outline">{ledger?.acid ? 'Yes' : 'No'}</Badge></p>
                <p><span className="text-zinc-500">Idempotency:</span> <Badge variant="outline">{ledger?.idempotency ? 'Yes' : 'No'}</Badge></p>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="hsm" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Total Keys" value={hsm?.total_keys || 0} />
            <MetricCard label="Active Keys" value={hsm?.active_keys || 0} />
            <MetricCard label="Operations" value={hsm?.total_ops_logged || 0} />
            <MetricCard label="Rotated Keys" value={hsm?.rotated_keys || 0} />
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">HSM Configuration</CardTitle></CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                <div><span className="text-zinc-500">Encryption:</span> {hsm?.algorithm}</div>
                <div><span className="text-zinc-500">Signing:</span> {hsm?.signing}</div>
                <div><span className="text-zinc-500">Key Wrapping:</span> {hsm?.key_wrapping}</div>
                <div><span className="text-zinc-500">KDF:</span> {hsm?.kdf}</div>
                <div><span className="text-zinc-500">Mode:</span> {hsm?.mode}</div>
                <div><span className="text-zinc-500">Cache Size:</span> {hsm?.cache_size}</div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="sms" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Provider" value={sms?.provider || '-'} />
            <MetricCard label="Mode" value={sms?.mode || '-'} />
            <MetricCard label="Total Sent" value={sms?.total_sent || 0} />
            <MetricCard label="Failed" value={sms?.total_failed || 0} />
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">Supported Providers</CardTitle></CardHeader>
            <CardContent>
              <div className="flex gap-2">
                <Badge variant="outline">Africa's Talking</Badge>
                <Badge variant="outline">Twilio</Badge>
                <Badge variant="outline">Termii</Badge>
              </div>
              <p className="text-xs text-zinc-400 mt-2">Set AT_API_KEY + AT_USERNAME env vars for live mode</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="pad" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="ISO Compliance" value={pad?.iso_compliance || '-'} />
            <MetricCard label="Total Checks" value={pad?.total_checks || 0} />
            <MetricCard label="Attacks Detected" value={pad?.attacks_detected || 0} />
            <MetricCard label="Attacks Blocked" value={pad?.attacks_blocked || 0} />
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {pad?.models?.map((m: any) => (
              <Card key={m.name}>
                <CardHeader><CardTitle className="text-sm">{m.name} v{m.version}</CardTitle></CardHeader>
                <CardContent className="space-y-1 text-sm">
                  <p><span className="text-zinc-500">Modality:</span> {m.modality}</p>
                  <p><span className="text-zinc-500">Accuracy:</span> {(m.accuracy * 100).toFixed(1)}%</p>
                  <p><span className="text-zinc-500">FAR:</span> {(m.far * 100).toFixed(2)}%</p>
                  <p><span className="text-zinc-500">FRR:</span> {(m.frr * 100).toFixed(2)}%</p>
                  <p><span className="text-zinc-500">ISO Level:</span> <Badge variant="outline">{m.iso_level}</Badge></p>
                  <div className="flex flex-wrap gap-1 mt-1">
                    {m.attack_types?.map((a: string) => <Badge key={a} variant="secondary" className="text-xs">{a}</Badge>)}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="ipfs" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="CID Version" value={ipfs?.cid_version || '-'} />
            <MetricCard label="Total Objects" value={ipfs?.total_objects || 0} />
            <MetricCard label="DAG Nodes" value={ipfs?.total_dag_nodes || 0} />
            <MetricCard label="Total Pins" value={ipfs?.total_pins || 0} />
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">IPFS Configuration</CardTitle></CardHeader>
            <CardContent className="text-sm space-y-2">
              <p><span className="text-zinc-500">Multihash:</span> {ipfs?.multihash}</p>
              <p><span className="text-zinc-500">Replication Factor:</span> {ipfs?.replication_factor}</p>
              <p><span className="text-zinc-500">Codecs:</span> {ipfs?.codecs?.join(', ')}</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="fabric" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Blocks" value={fabric?.total_blocks || 0} />
            <MetricCard label="Transactions" value={fabric?.total_transactions || 0} />
            <MetricCard label="Endorsements" value={fabric?.total_endorsements || 0} />
            <MetricCard label="Consensus" value={fabric?.consensus || '-'} />
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">Network Peers</CardTitle></CardHeader>
            <CardContent>
              <div className="flex gap-2 flex-wrap">
                {fabric?.peers?.map((p: string) => <Badge key={p} variant="outline">{p}</Badge>)}
              </div>
              <p className="text-xs text-zinc-400 mt-2">Endorsement Policy: {fabric?.endorsement_policy}</p>
              <p className="text-xs text-zinc-400">Signing: {fabric?.signing}</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="ledger" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Accounts" value={ledger?.total_accounts || 0} />
            <MetricCard label="Transfers" value={ledger?.total_transfers || 0} />
            <MetricCard label="Journal Entries" value={ledger?.journal_entries || 0} />
            <MetricCard label="ACID" value={ledger?.acid ? 'Enabled' : 'Disabled'} />
          </div>
          {ledger?.accounts && (
            <Card>
              <CardHeader><CardTitle className="text-sm">Accounts</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="py-2 pr-4">Account</th>
                      <th className="py-2 pr-4">Ledger</th>
                      <th className="py-2 pr-4">Balance</th>
                      <th className="py-2 pr-4">Debits Posted</th>
                      <th className="py-2 pr-4">Credits Posted</th>
                    </tr></thead>
                    <tbody>
                      {ledger.accounts.map((a: any) => (
                        <tr key={a.id} className="border-b">
                          <td className="py-2 pr-4 font-mono text-xs">{a.id}</td>
                          <td className="py-2 pr-4">{a.ledger}</td>
                          <td className="py-2 pr-4 font-medium">{a.balance?.toLocaleString()}</td>
                          <td className="py-2 pr-4">{a.debits_posted?.toLocaleString()}</td>
                          <td className="py-2 pr-4">{a.credits_posted?.toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="database" className="space-y-4">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <MetricCard label="Total Reads" value={dbMetrics?.total_reads || 0} />
            <MetricCard label="Total Writes" value={dbMetrics?.total_writes || 0} />
            <MetricCard label="Slow Queries" value={dbMetrics?.slow_queries || 0} />
            <MetricCard label="Cache Hits" value={dbMetrics?.cache_hits || 0} />
          </div>
          <Card>
            <CardHeader><CardTitle className="text-sm">Scaling Layer</CardTitle></CardHeader>
            <CardContent className="text-sm space-y-2">
              <p><span className="text-zinc-500">Read/Write Split:</span> <Badge variant="outline">{dbMetrics?.patterns?.read_write_split ? 'Enabled' : 'Disabled'}</Badge></p>
              <p><span className="text-zinc-500">Prepared Stmt Cache:</span> <Badge variant="outline">{dbMetrics?.patterns?.prepared_stmt_cache ? 'Enabled' : 'Disabled'}</Badge></p>
              <p><span className="text-zinc-500">Slow Query Detection:</span> <Badge variant="outline">{dbMetrics?.patterns?.slow_query_detection ? 'Enabled' : 'Disabled'}</Badge></p>
              <p><span className="text-zinc-500">Avg Read Latency:</span> {dbMetrics?.avg_read_latency_us ? `${(dbMetrics.avg_read_latency_us / 1000).toFixed(2)}ms` : '-'}</p>
              <p><span className="text-zinc-500">Avg Write Latency:</span> {dbMetrics?.avg_write_latency_us ? `${(dbMetrics.avg_write_latency_us / 1000).toFixed(2)}ms` : '-'}</p>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="pgpool" className="space-y-4">
          <Card>
            <CardHeader><CardTitle className="text-sm">Pgpool-II Status</CardTitle></CardHeader>
            <CardContent className="text-sm space-y-2">
              <p><span className="text-zinc-500">Enabled:</span> <Badge variant={pgpool?.enabled ? 'default' : 'secondary'}>{pgpool?.enabled ? 'Yes' : 'No'}</Badge></p>
              <p><span className="text-zinc-500">Mode:</span> {pgpool?.mode || 'direct-connect'}</p>
              {pgpool?.enabled && (
                <>
                  <p><span className="text-zinc-500">Active Connections:</span> {pgpool?.active_connections}</p>
                  <p><span className="text-zinc-500">Idle Connections:</span> {pgpool?.idle_connections}</p>
                </>
              )}
              {!pgpool?.enabled && (
                <p className="text-zinc-400 text-xs mt-2">
                  Set PGPOOL_ENABLED=true with PG_PASSWORD, REPLICATOR_PASSWORD, and PGPOOL_ADMIN_PASSWORD to enable HA mode.
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
