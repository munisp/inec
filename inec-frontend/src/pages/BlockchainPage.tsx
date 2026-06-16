import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Lock, Link2, Hash, Server, Database, Globe, GitBranch, CheckCircle, XCircle } from 'lucide-react';

export default function BlockchainPage() {
  const [stats, setStats] = useState<any>(null);
  const [chain, setChain] = useState<any>(null);
  const [contracts, setContracts] = useState<any>(null);
  const [audit, setAudit] = useState<any>(null);
  const [prodStats, setProdStats] = useState<any>(null);
  const [fabricNet, setFabricNet] = useState<any>(null);
  const [fabricBlocks, setFabricBlocks] = useState<any>(null);
  const [fabricTxs, setFabricTxs] = useState<any>(null);
  const [chainVerify, setChainVerify] = useState<any>(null);
  const [ipfsStats, setIpfsStats] = useState<any>(null);
  const [ipfsObjects, setIpfsObjects] = useState<any>(null);
  const [ledgerStats, setLedgerStats] = useState<any>(null);
  const [ledgerAccounts, setLedgerAccounts] = useState<any>(null);
  const [ledgerTransfers, setLedgerTransfers] = useState<any>(null);
  const [merkleTrees, setMerkleTrees] = useState<any>(null);
  const [tab, setTab] = useState('production');

  useEffect(() => {
    api.getBlockchainStats().then(setStats).catch(err => console.error("API error:", err));
    api.getBlockchainChain(50).then(setChain).catch(err => console.error("API error:", err));
    api.getSmartContracts().then(setContracts).catch(err => console.error("API error:", err));
    api.getBlockchainAudit(50).then(setAudit).catch(err => console.error("API error:", err));
    api.getBlockchainProductionStats().then(setProdStats).catch(err => console.error("API error:", err));
    api.getFabricNetwork().then(setFabricNet).catch(err => console.error("API error:", err));
    api.getFabricBlocks(20).then(setFabricBlocks).catch(err => console.error("API error:", err));
    api.getFabricTransactions(50).then(setFabricTxs).catch(err => console.error("API error:", err));
    api.verifyFabricChain(100).then(setChainVerify).catch(err => console.error("API error:", err));
    api.getIPFSStats().then(setIpfsStats).catch(err => console.error("API error:", err));
    api.getIPFSObjects(50).then(setIpfsObjects).catch(err => console.error("API error:", err));
    api.getLedgerStats().then(setLedgerStats).catch(err => console.error("API error:", err));
    api.getLedgerAccounts().then(setLedgerAccounts).catch(err => console.error("API error:", err));
    api.getLedgerTransfers('inec-operational', 50).then(setLedgerTransfers).catch(err => console.error("API error:", err));
    api.getMerkleTrees(20).then(setMerkleTrees).catch(err => console.error("API error:", err));
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Blockchain & Distributed Ledger</h2>
        <p className="text-zinc-500 text-sm">Production-grade Hyperledger Fabric, TigerBeetle Ledger, IPFS Content Store & Merkle Trees</p>
      </div>

      {prodStats && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { label: 'Fabric Blocks', value: prodStats.fabric_network?.total_blocks || 0, icon: Link2, color: 'text-blue-600' },
            { label: 'Fabric TXs', value: prodStats.fabric_network?.total_transactions || 0, icon: GitBranch, color: 'text-indigo-600' },
            { label: 'IPFS Objects', value: prodStats.ipfs_store?.total_objects || 0, icon: Globe, color: 'text-cyan-600' },
            { label: 'TB Transfers', value: prodStats.tigerbeetle?.total_transfers || 0, icon: Database, color: 'text-green-600' },
            { label: 'Chain Integrity', value: prodStats.chain_integrity?.chain_valid ? 'Valid' : 'Broken', icon: prodStats.chain_integrity?.chain_valid ? CheckCircle : XCircle, color: prodStats.chain_integrity?.chain_valid ? 'text-emerald-600' : 'text-red-600' },
            { label: 'Merkle Trees', value: prodStats.merkle_trees || 0, icon: Hash, color: 'text-purple-600' },
          ].map((s, i) => (
            <Card key={i}>
              <CardContent className="pt-4 pb-3">
                <div className="flex items-center gap-2 mb-1">
                  <s.icon className={`w-4 h-4 ${s.color}`} />
                  <span className="text-xs text-zinc-500">{s.label}</span>
                </div>
                <p className="text-xl font-bold">{s.value}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="flex-wrap h-auto gap-1">
          <TabsTrigger value="production">Production Overview</TabsTrigger>
          <TabsTrigger value="fabric">Hyperledger Fabric</TabsTrigger>
          <TabsTrigger value="ipfs">IPFS Store</TabsTrigger>
          <TabsTrigger value="ledger">TigerBeetle Ledger</TabsTrigger>
          <TabsTrigger value="merkle">Merkle Trees</TabsTrigger>
          <TabsTrigger value="chain">Block Chain</TabsTrigger>
          <TabsTrigger value="contracts">Smart Contracts</TabsTrigger>
          <TabsTrigger value="audit">Audit Trail</TabsTrigger>
        </TabsList>

        <TabsContent value="production">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {prodStats?.components && Object.entries(prodStats.components).map(([key, val]: [string, any]) => (
              <Card key={key}>
                <CardContent className="pt-4 pb-3">
                  <div className="flex items-center gap-2 mb-2">
                    <CheckCircle className="w-4 h-4 text-green-600" />
                    <span className="text-sm font-semibold capitalize">{key.replace(/_/g, ' ')}</span>
                  </div>
                  <p className="text-xs text-zinc-600">{val as string}</p>
                </CardContent>
              </Card>
            ))}
            {chainVerify && (
              <Card>
                <CardContent className="pt-4 pb-3">
                  <div className="flex items-center gap-2 mb-2">
                    {chainVerify.chain_valid ? <CheckCircle className="w-4 h-4 text-green-600" /> : <XCircle className="w-4 h-4 text-red-600" />}
                    <span className="text-sm font-semibold">Chain Integrity Verification</span>
                  </div>
                  <p className="text-xs text-zinc-600">
                    {chainVerify.blocks_checked} blocks checked &mdash; {chainVerify.chain_valid ? 'All hashes valid' : `${chainVerify.broken_blocks?.length} broken blocks`}
                  </p>
                </CardContent>
              </Card>
            )}
          </div>
        </TabsContent>

        <TabsContent value="fabric">
          <div className="space-y-4">
            {fabricNet && (
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm flex items-center gap-2"><Server className="w-4 h-4" /> Network</CardTitle></CardHeader>
                  <CardContent className="text-xs space-y-1">
                    <p>Name: <span className="font-mono">{fabricNet.network_name}</span></p>
                    <p>Consensus: <Badge variant="outline" className="text-xs">{fabricNet.consensus}</Badge></p>
                    <p>Channels: {fabricNet.channels?.join(', ')}</p>
                    <p>Signature: <Badge variant="outline" className="text-xs">{fabricNet.signature_algorithm}</Badge></p>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm flex items-center gap-2"><Server className="w-4 h-4" /> Peers ({fabricNet.total_peers})</CardTitle></CardHeader>
                  <CardContent className="text-xs space-y-1">
                    {fabricNet.peers?.map((p: any) => (
                      <div key={p.peer_id} className="flex items-center justify-between">
                        <span className="font-mono">{p.peer_id}</span>
                        <div className="flex gap-1">
                          <Badge variant="outline" className="text-[10px]">{p.org}</Badge>
                          <Badge className={`text-[10px] ${p.role === 'endorser' ? 'bg-blue-100 text-blue-700' : 'bg-zinc-100 text-zinc-700'}`}>{p.role}</Badge>
                        </div>
                      </div>
                    ))}
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm flex items-center gap-2"><Server className="w-4 h-4" /> Orderers ({fabricNet.total_orderers})</CardTitle></CardHeader>
                  <CardContent className="text-xs space-y-1">
                    {fabricNet.orderers?.map((o: any) => (
                      <div key={o.orderer_id} className="flex items-center justify-between">
                        <span className="font-mono">{o.orderer_id}</span>
                        <Badge variant="outline" className="text-[10px]">{o.consensus_type}</Badge>
                      </div>
                    ))}
                    <div className="pt-2 border-t mt-2">
                      <p className="font-semibold">Chaincode ({fabricNet.total_chaincode})</p>
                      {fabricNet.chaincode?.map((cc: any) => (
                        <div key={cc.chaincode_id} className="flex items-center justify-between mt-1">
                          <span className="font-mono">{cc.chaincode_id} v{cc.version}</span>
                          <Badge className="text-[10px] bg-green-100 text-green-700">{cc.status}</Badge>
                        </div>
                      ))}
                    </div>
                  </CardContent>
                </Card>
              </div>
            )}
            <Card>
              <CardHeader><CardTitle className="text-sm">Fabric Blocks</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">Block #</th><th className="pb-2 pr-3">Channel</th><th className="pb-2 pr-3">Prev Hash</th><th className="pb-2 pr-3">Block Hash</th><th className="pb-2 pr-3">TXs</th><th className="pb-2">Created</th>
                    </tr></thead>
                    <tbody>
                      {fabricBlocks?.blocks?.map((b: any) => (
                        <tr key={b.block_number} className="border-b border-zinc-100">
                          <td className="py-2 pr-3 font-mono text-xs">#{b.block_number}</td>
                          <td className="py-2 pr-3 text-xs">{b.channel_id}</td>
                          <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{b.prev_hash}</td>
                          <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{b.block_hash}</td>
                          <td className="py-2 pr-3">{b.tx_count}</td>
                          <td className="py-2 text-xs text-zinc-500">{new Date(b.created_at).toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Fabric Transactions</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">TX ID</th><th className="pb-2 pr-3">Block</th><th className="pb-2 pr-3">Chaincode</th><th className="pb-2 pr-3">Function</th><th className="pb-2 pr-3">Creator MSP</th><th className="pb-2">Status</th>
                    </tr></thead>
                    <tbody>
                      {fabricTxs?.transactions?.map((tx: any) => (
                        <tr key={tx.tx_id} className="border-b border-zinc-100">
                          <td className="py-2 pr-3 font-mono text-xs">{tx.tx_id}</td>
                          <td className="py-2 pr-3">#{tx.block_number}</td>
                          <td className="py-2 pr-3 text-xs">{tx.chaincode_id}</td>
                          <td className="py-2 pr-3 text-xs">{tx.function}</td>
                          <td className="py-2 pr-3 text-xs">{tx.creator_msp}</td>
                          <td className="py-2">
                            <Badge className={`text-xs ${tx.validation_code === 'VALID' ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>{tx.validation_code}</Badge>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="ipfs">
          <div className="space-y-4">
            {ipfsStats && (
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Total Objects</p><p className="text-xl font-bold">{ipfsStats.total_objects}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Total Pins</p><p className="text-xl font-bold">{ipfsStats.total_pins}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Total Size</p><p className="text-xl font-bold">{(ipfsStats.total_size_bytes / 1024).toFixed(1)} KB</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Pinned</p><p className="text-xl font-bold">{ipfsStats.pinned}</p></CardContent></Card>
              </div>
            )}
            {ipfsStats?.by_type && (
              <Card>
                <CardHeader><CardTitle className="text-sm">Objects by Content Type</CardTitle></CardHeader>
                <CardContent>
                  <div className="space-y-2">
                    {ipfsStats.by_type.map((t: any, i: number) => (
                      <div key={i} className="flex items-center justify-between py-1 border-b border-zinc-50">
                        <div className="flex items-center gap-2">
                          <Globe className="w-4 h-4 text-cyan-600" />
                          <span className="text-sm">{t.content_type}</span>
                        </div>
                        <div className="text-right text-xs text-zinc-500">
                          {t.count} objects, {(t.size_bytes / 1024).toFixed(1)} KB
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )}
            <Card>
              <CardHeader><CardTitle className="text-sm">IPFS Objects</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">CID</th><th className="pb-2 pr-3">Content Type</th><th className="pb-2 pr-3">Data Hash</th><th className="pb-2 pr-3">Size</th><th className="pb-2 pr-3">Pinned</th><th className="pb-2">Created</th>
                    </tr></thead>
                    <tbody>
                      {ipfsObjects?.objects?.map((o: any) => (
                        <tr key={o.cid} className="border-b border-zinc-100">
                          <td className="py-2 pr-3 font-mono text-xs">{o.cid?.slice(0, 20)}...</td>
                          <td className="py-2 pr-3 text-xs">{o.content_type}</td>
                          <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{o.data_hash}</td>
                          <td className="py-2 pr-3 text-xs">{o.size_bytes} B</td>
                          <td className="py-2 pr-3">{o.pinned ? <CheckCircle className="w-3 h-3 text-green-600" /> : <XCircle className="w-3 h-3 text-zinc-300" />}</td>
                          <td className="py-2 text-xs text-zinc-500">{new Date(o.created_at).toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="ledger">
          <div className="space-y-4">
            {ledgerStats && (
              <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Accounts</p><p className="text-xl font-bold">{ledgerStats.total_accounts}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Transfers</p><p className="text-xl font-bold">{ledgerStats.total_transfers}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Posted</p><p className="text-xl font-bold text-green-600">{ledgerStats.posted}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Pending</p><p className="text-xl font-bold text-amber-600">{ledgerStats.pending}</p></CardContent></Card>
                <Card><CardContent className="pt-4 pb-3"><p className="text-xs text-zinc-500">Voided</p><p className="text-xl font-bold text-red-600">{ledgerStats.voided}</p></CardContent></Card>
              </div>
            )}
            {ledgerStats && (
              <Card>
                <CardHeader><CardTitle className="text-sm flex items-center gap-2"><Database className="w-4 h-4" /> Storage Info</CardTitle></CardHeader>
                <CardContent className="text-xs space-y-1">
                  <p>Storage: <Badge variant="outline" className="text-xs">{ledgerStats.storage}</Badge></p>
                  <p>Double-Entry: <Badge className="text-xs bg-green-100 text-green-700">{ledgerStats.double_entry ? 'Yes' : 'No'}</Badge></p>
                  <p>ACID Compliant: <Badge className="text-xs bg-green-100 text-green-700">{ledgerStats.acid_compliant ? 'Yes' : 'No'}</Badge></p>
                  <p>Total Posted Amount: <span className="font-mono font-bold">{ledgerStats.total_posted_amount?.toLocaleString()}</span></p>
                </CardContent>
              </Card>
            )}
            <Card>
              <CardHeader><CardTitle className="text-sm">Accounts</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">Account ID</th><th className="pb-2 pr-3">Ledger</th><th className="pb-2 pr-3">Credits Posted</th><th className="pb-2 pr-3">Debits Posted</th><th className="pb-2 pr-3">Credits Pending</th><th className="pb-2 pr-3">Debits Pending</th><th className="pb-2">Balance</th>
                    </tr></thead>
                    <tbody>
                      {ledgerAccounts?.accounts?.map((a: any) => (
                        <tr key={a.id} className="border-b border-zinc-100">
                          <td className="py-2 pr-3 font-mono text-xs">{a.id}</td>
                          <td className="py-2 pr-3">{a.ledger}</td>
                          <td className="py-2 pr-3 text-green-600">{a.credits_posted?.toLocaleString()}</td>
                          <td className="py-2 pr-3 text-red-600">{a.debits_posted?.toLocaleString()}</td>
                          <td className="py-2 pr-3 text-zinc-500">{a.credits_pending?.toLocaleString()}</td>
                          <td className="py-2 pr-3 text-zinc-500">{a.debits_pending?.toLocaleString()}</td>
                          <td className="py-2 font-bold">{a.balance?.toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle className="text-sm">Recent Transfers</CardTitle></CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead><tr className="border-b text-left text-zinc-500">
                      <th className="pb-2 pr-3">Transfer ID</th><th className="pb-2 pr-3">Debit</th><th className="pb-2 pr-3">Credit</th><th className="pb-2 pr-3">Amount</th><th className="pb-2 pr-3">Status</th><th className="pb-2">Created</th>
                    </tr></thead>
                    <tbody>
                      {ledgerTransfers?.transfers?.map((t: any) => (
                        <tr key={t.id} className="border-b border-zinc-100">
                          <td className="py-2 pr-3 font-mono text-xs">{t.id}</td>
                          <td className="py-2 pr-3 text-xs">{t.debit_account_id}</td>
                          <td className="py-2 pr-3 text-xs">{t.credit_account_id}</td>
                          <td className="py-2 pr-3 font-mono">{t.amount?.toLocaleString()}</td>
                          <td className="py-2 pr-3">
                            <Badge className={`text-xs ${t.status === 'POSTED' ? 'bg-green-100 text-green-700' : t.status === 'PENDING' ? 'bg-amber-100 text-amber-700' : 'bg-red-100 text-red-700'}`}>{t.status}</Badge>
                          </td>
                          <td className="py-2 text-xs text-zinc-500">{new Date(t.created_at).toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="merkle">
          <Card>
            <CardHeader><CardTitle className="text-sm flex items-center gap-2"><Hash className="w-4 h-4" /> Merkle Trees</CardTitle></CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-3">ID</th><th className="pb-2 pr-3">Root Hash</th><th className="pb-2 pr-3">Type</th><th className="pb-2 pr-3">Leaves</th><th className="pb-2 pr-3">Depth</th><th className="pb-2">Created</th>
                  </tr></thead>
                  <tbody>
                    {merkleTrees?.trees?.map((t: any) => (
                      <tr key={t.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-3">#{t.id}</td>
                        <td className="py-2 pr-3 font-mono text-xs text-zinc-500">{t.root_hash}</td>
                        <td className="py-2 pr-3 text-xs">{t.tree_type}</td>
                        <td className="py-2 pr-3">{t.leaf_count}</td>
                        <td className="py-2 pr-3">{t.depth}</td>
                        <td className="py-2 text-xs text-zinc-500">{new Date(t.created_at).toLocaleString()}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="chain">
          <Card>
            <CardHeader><CardTitle className="text-sm">Result Blockchain (Legacy)</CardTitle></CardHeader>
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
      </Tabs>
    </div>
  );
}
