import { useEffect, useState } from 'react';
import { logger } from '@/lib/utils';
import { api } from '@/lib/api';
import { useAuth } from '@/lib/auth';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Activity, Plus, Eye, CheckCircle, Shield, AlertTriangle } from 'lucide-react';

interface Party { code: string; name: string; abbreviation: string; color: string; }
interface State { code: string; name: string; }
interface ResultItem {
  id: number; polling_unit_code: string; status: string; total_valid_votes: number;
  rejected_votes: number; total_votes_cast: number; accredited_voters: number;
  tigerbeetle_status: string; hyperledger_status: string; submitted_at: string;
  pu_name: string; ward_name: string; lga_name: string; state_name: string;
  party_scores: Array<{ party_code: string; party_name: string; color: string; votes: number }>;
}

function formatNumber(n: number) { return new Intl.NumberFormat().format(n); }

export default function ResultsPage() {
  const { user } = useAuth();
  const [results, setResults] = useState<ResultItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [parties, setParties] = useState<Party[]>([]);
  const [states, setStates] = useState<State[]>([]);
  const [filterState, setFilterState] = useState('all');
  const [filterStatus, setFilterStatus] = useState('all');
  const [selectedResult, setSelectedResult] = useState<ResultItem | null>(null);
  const [showUpload, setShowUpload] = useState(false);
  const [uploadData, setUploadData] = useState({ polling_unit_code: '', accredited_voters: '', rejected_votes: '0', scores: {} as Record<string, string> });
  const [submitMsg, setSubmitMsg] = useState('');
  const canUpload = user?.role === 'admin' || user?.role === 'presiding_officer';
  const canManage = user?.role === 'admin' || user?.role === 'collation_officer';

  useEffect(() => {
    Promise.all([api.getParties(), api.getStates()]).then(([p, s]) => { setParties(p); setStates(s); });
  }, []);

  useEffect(() => { loadResults(); }, [filterState, filterStatus]);

  async function loadResults() {
    setLoading(true);
    try {
      const params: Record<string, string> = {};
      if (filterState !== 'all') params.state_code = filterState;
      if (filterStatus !== 'all') params.status = filterStatus;
      const res = await api.getResults(1, params);
      setResults(res.results);
      setTotal(res.total);
    } catch (e) { logger.error(e); }
    finally { setLoading(false); }
  }

  async function handleSubmit() {
    try {
      const party_scores = Object.entries(uploadData.scores).filter(([, v]) => v).map(([code, votes]) => ({ party_code: code, votes: parseInt(votes) }));
      await api.submitResult({
        election_id: 1, polling_unit_code: uploadData.polling_unit_code,
        party_scores, accredited_voters: parseInt(uploadData.accredited_voters),
        rejected_votes: parseInt(uploadData.rejected_votes || '0')
      });
      setSubmitMsg('Result submitted successfully!');
      setShowUpload(false);
      loadResults();
    } catch (e: unknown) { setSubmitMsg(e instanceof Error ? e.message : 'Submission failed'); }
  }

  async function handleAction(id: number, action: 'validate' | 'finalize' | 'dispute') {
    try {
      if (action === 'validate') await api.validateResult(id);
      else if (action === 'finalize') await api.finalizeResult(id);
      else await api.disputeResult(id);
      loadResults();
    } catch (e) { logger.error(e); }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Select value={filterState} onValueChange={setFilterState}>
            <SelectTrigger className="w-40"><SelectValue placeholder="All States" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All States</SelectItem>
              {states.map(s => <SelectItem key={s.code} value={s.code}>{s.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={filterStatus} onValueChange={setFilterStatus}>
            <SelectTrigger className="w-36"><SelectValue placeholder="All Status" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Status</SelectItem>
              <SelectItem value="pending">Pending</SelectItem>
              <SelectItem value="validated">Validated</SelectItem>
              <SelectItem value="finalized">Finalized</SelectItem>
              <SelectItem value="disputed">Disputed</SelectItem>
            </SelectContent>
          </Select>
          <Badge variant="outline">{total} results</Badge>
        </div>
        {canUpload && (
          <Dialog open={showUpload} onOpenChange={setShowUpload}>
            <DialogTrigger asChild>
              <Button className="bg-green-700 hover:bg-green-800 gap-1"><Plus className="w-4 h-4" /> Upload Result</Button>
            </DialogTrigger>
            <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
              <DialogHeader><DialogTitle>Submit Polling Unit Result</DialogTitle></DialogHeader>
              <div className="space-y-4">
                {submitMsg && <div className="p-2 text-sm rounded bg-blue-50 text-blue-800">{submitMsg}</div>}
                <div className="space-y-2">
                  <Label>Polling Unit Code</Label>
                  <Input placeholder="e.g. LA-001-W001-PU001" value={uploadData.polling_unit_code}
                    onChange={e => setUploadData(p => ({ ...p, polling_unit_code: e.target.value }))} />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-2">
                    <Label>Accredited Voters</Label>
                    <Input type="number" value={uploadData.accredited_voters}
                      onChange={e => setUploadData(p => ({ ...p, accredited_voters: e.target.value }))} />
                  </div>
                  <div className="space-y-2">
                    <Label>Rejected Votes</Label>
                    <Input type="number" value={uploadData.rejected_votes}
                      onChange={e => setUploadData(p => ({ ...p, rejected_votes: e.target.value }))} />
                  </div>
                </div>
                <div className="space-y-2">
                  <Label>Party Votes</Label>
                  {parties.map(p => (
                    <div key={p.code} className="flex items-center gap-2">
                      <div className="w-3 h-3 rounded-full" style={{ backgroundColor: p.color }} />
                      <span className="text-sm w-16">{p.abbreviation}</span>
                      <Input type="number" placeholder="0" className="flex-1"
                        value={uploadData.scores[p.code] || ''}
                        onChange={e => setUploadData(prev => ({ ...prev, scores: { ...prev.scores, [p.code]: e.target.value } }))} />
                    </div>
                  ))}
                </div>
                <Button onClick={handleSubmit} className="w-full bg-green-700 hover:bg-green-800">Submit Result</Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
      </div>

      <Card>
        <CardContent className="overflow-x-auto p-0">
          {loading ? (
            <div className="flex items-center justify-center h-32"><Activity className="w-5 h-5 animate-spin text-green-700" /></div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Polling Unit</TableHead>
                  <TableHead>Location</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Valid Votes</TableHead>
                  <TableHead className="text-right">Total Cast</TableHead>
                  <TableHead>Ledger</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {results.map(r => (
                  <TableRow key={r.id}>
                    <TableCell>
                      <p className="text-sm font-medium">{r.pu_name}</p>
                      <p className="text-xs text-zinc-400">{r.polling_unit_code}</p>
                    </TableCell>
                    <TableCell>
                      <p className="text-sm">{r.lga_name}</p>
                      <p className="text-xs text-zinc-400">{r.state_name}</p>
                    </TableCell>
                    <TableCell>
                      <Badge className={`text-xs ${
                        r.status === 'finalized' ? 'bg-green-100 text-green-800' :
                        r.status === 'validated' ? 'bg-blue-100 text-blue-800' :
                        r.status === 'pending' ? 'bg-amber-100 text-amber-800' :
                        'bg-red-100 text-red-800'
                      }`}>{r.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right font-medium">{formatNumber(r.total_valid_votes)}</TableCell>
                    <TableCell className="text-right">{formatNumber(r.total_votes_cast)}</TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Badge variant="outline" className="text-xs px-1">TB:{r.tigerbeetle_status}</Badge>
                        <Badge variant="outline" className="text-xs px-1">HL:{r.hyperledger_status}</Badge>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Dialog>
                          <DialogTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => setSelectedResult(r)}>
                              <Eye className="w-3.5 h-3.5" />
                            </Button>
                          </DialogTrigger>
                          <DialogContent>
                            <DialogHeader><DialogTitle>Result Details</DialogTitle></DialogHeader>
                            {selectedResult && selectedResult.id === r.id && (
                              <div className="space-y-3">
                                <div className="grid grid-cols-2 gap-2 text-sm">
                                  <div><span className="text-zinc-500">PU:</span> {r.polling_unit_code}</div>
                                  <div><span className="text-zinc-500">Status:</span> {r.status}</div>
                                  <div><span className="text-zinc-500">Accredited:</span> {formatNumber(r.accredited_voters)}</div>
                                  <div><span className="text-zinc-500">Total Cast:</span> {formatNumber(r.total_votes_cast)}</div>
                                  <div><span className="text-zinc-500">Valid:</span> {formatNumber(r.total_valid_votes)}</div>
                                  <div><span className="text-zinc-500">Rejected:</span> {formatNumber(r.rejected_votes)}</div>
                                  <div><span className="text-zinc-500">TB ID:</span> <span className="text-xs font-mono">{r.tigerbeetle_status}</span></div>
                                  <div><span className="text-zinc-500">HL:</span> <span className="text-xs font-mono">{r.hyperledger_status}</span></div>
                                </div>
                                <div className="space-y-1">
                                  {r.party_scores?.map(ps => (
                                    <div key={ps.party_code} className="flex items-center justify-between text-sm">
                                      <div className="flex items-center gap-2">
                                        <div className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: ps.color }} />
                                        <span>{ps.party_name}</span>
                                      </div>
                                      <span className="font-medium">{formatNumber(ps.votes)}</span>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            )}
                          </DialogContent>
                        </Dialog>
                        {canManage && r.status === 'pending' && (
                          <Button variant="ghost" size="icon" className="h-7 w-7 text-blue-600" onClick={() => handleAction(r.id, 'validate')}>
                            <CheckCircle className="w-3.5 h-3.5" />
                          </Button>
                        )}
                        {canManage && (r.status === 'pending' || r.status === 'validated') && (
                          <Button variant="ghost" size="icon" className="h-7 w-7 text-green-600" onClick={() => handleAction(r.id, 'finalize')}>
                            <Shield className="w-3.5 h-3.5" />
                          </Button>
                        )}
                        {(user?.role === 'admin' || user?.role === 'observer') && r.status !== 'disputed' && r.status !== 'voided' && (
                          <Button variant="ghost" size="icon" className="h-7 w-7 text-red-600" onClick={() => handleAction(r.id, 'dispute')}>
                            <AlertTriangle className="w-3.5 h-3.5" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
