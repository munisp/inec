import { useEffect, useState } from 'react';
import { logger } from '@/lib/utils';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Shield, Search, CheckCircle, XCircle, Activity, FileText, Link2 } from 'lucide-react';

interface AuditEntry {
  id: number; action: string; entity_type: string; entity_id: string;
  user_id: number; details: string; block_hash: string; prev_block_hash: string;
  timestamp: string; username: string; full_name: string;
}
interface VerifyData {
  result_id: number;
  audit_entries: AuditEntry[];
  chain_valid: boolean;
  dual_ledger: { tigerbeetle_status: string; hyperledger_status: string; tigerbeetle_transfer_id: string; hyperledger_tx_id: string } | null;
export default function AuditPage() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [searchId, setSearchId] = useState('');
  const [verifyData, setVerifyData] = useState<VerifyData | null>(null);
  const [stats, setStats] = useState<{ total_entries: number; action_counts: Array<{ action: string; count: number }>; latest_block_hash: string | null } | null>(null);
  useEffect(() => {
    loadData();
  }, []);
  async function loadData() {
    setLoading(true);
    try {
      const [trail, auditStats] = await Promise.all([api.getAuditTrail(), api.getAuditStats()]);
      setEntries(trail.entries);
      setTotal(trail.total);
      setStats(auditStats);
    } catch (e) { logger.error(e); }
    finally { setLoading(false); }
  }
  async function handleVerify() {
    if (!searchId) return;
      const data = await api.verifyResult(parseInt(searchId));
      setVerifyData(data);
  const actionColors: Record<string, string> = {
    RESULT_SUBMITTED: 'bg-blue-100 text-blue-800',
    RESULT_VALIDATED: 'bg-amber-100 text-amber-800',
    RESULT_FINALIZED: 'bg-green-100 text-green-800',
    RESULT_DISPUTED: 'bg-red-100 text-red-800',
    ELECTION_CREATED: 'bg-purple-100 text-purple-800',
    USER_LOGIN: 'bg-zinc-100 text-zinc-600',
  };
  if (loading) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;
  return (
    <div className="space-y-6">
      {stats && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-4 pb-4">
              <p className="text-xs text-zinc-500">Total Audit Entries</p>
              <p className="text-2xl font-bold">{stats.total_entries}</p>
            </CardContent>
          </Card>
          {stats.action_counts.slice(0, 3).map(ac => (
            <Card key={ac.action}>
              <CardContent className="pt-4 pb-4">
                <p className="text-xs text-zinc-500">{ac.action.replace(/_/g, ' ')}</p>
                <p className="text-2xl font-bold">{ac.count}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <Card className="border-green-200 bg-green-50/50">
        <CardContent className="pt-4 pb-4">
          <div className="flex items-center gap-2 mb-3">
            <Shield className="w-4 h-4 text-green-700" />
            <h3 className="text-sm font-semibold text-green-900">Blockchain Verification</h3>
          </div>
          <div className="flex gap-2">
            <Input placeholder="Enter Result ID to verify" value={searchId}
              onChange={e => setSearchId(e.target.value)} className="max-w-xs" />
            <Button onClick={handleVerify} className="bg-green-700 hover:bg-green-800 gap-1">
              <Search className="w-4 h-4" /> Verify
            </Button>
          {verifyData && (
            <div className="mt-4 p-4 rounded-lg bg-white border border-green-200">
              <div className="flex items-center gap-2 mb-3">
                {verifyData.chain_valid ? (
                  <><CheckCircle className="w-5 h-5 text-green-600" /><span className="font-medium text-green-800">Chain Integrity: VALID</span></>
                ) : (
                  <><XCircle className="w-5 h-5 text-red-600" /><span className="font-medium text-red-800">Chain Integrity: INVALID</span></>
                )}
              </div>
              {verifyData.dual_ledger && (
                <div className="grid grid-cols-2 gap-3 text-sm mb-3">
                  <div>
                    <p className="text-zinc-500">TigerBeetle</p>
                    <p className="font-mono text-xs">{verifyData.dual_ledger.tigerbeetle_status} | {verifyData.dual_ledger.tigerbeetle_transfer_id}</p>
                  </div>
                    <p className="text-zinc-500">Hyperledger</p>
                    <p className="font-mono text-xs">{verifyData.dual_ledger.hyperledger_status} | {verifyData.dual_ledger.hyperledger_tx_id?.slice(0, 20)}...</p>
                </div>
              )}
              <div className="space-y-1">
                {verifyData.audit_entries.map((e, i) => (
                  <div key={e.id} className="flex items-center gap-2 text-xs">
                    <div className="w-5 h-5 rounded-full bg-green-100 flex items-center justify-center text-green-700 font-bold">{i + 1}</div>
                    <Badge className={actionColors[e.action] || 'bg-zinc-100'}>{e.action}</Badge>
                    <span className="text-zinc-400">{e.timestamp}</span>
                    <Link2 className="w-3 h-3 text-zinc-400" />
                    <span className="font-mono text-zinc-400 truncate max-w-32">{e.block_hash?.slice(0, 16)}...</span>
                ))}
            </div>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-semibold flex items-center gap-2">
            <FileText className="w-4 h-4" /> Audit Trail
            <Badge variant="outline">{total} entries</Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Block #</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Entity</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Timestamp</TableHead>
                <TableHead>Block Hash</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map(e => (
                <TableRow key={e.id}>
                  <TableCell className="font-mono text-xs">{e.id}</TableCell>
                  <TableCell><Badge className={`text-xs ${actionColors[e.action] || 'bg-zinc-100'}`}>{e.action}</Badge></TableCell>
                  <TableCell className="text-sm">{e.entity_type} #{e.entity_id}</TableCell>
                  <TableCell className="text-sm">{e.full_name || e.username || '-'}</TableCell>
                  <TableCell className="text-xs text-zinc-500">{e.timestamp}</TableCell>
                  <TableCell className="font-mono text-xs text-zinc-400 max-w-24 truncate">{e.block_hash?.slice(0, 16)}...</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
    </div>
  );
