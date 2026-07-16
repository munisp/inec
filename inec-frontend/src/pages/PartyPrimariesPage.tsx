import { useEffect, useState, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Users, Vote, Shield, Clock, AlertTriangle, CheckCircle,
  RefreshCw, BarChart3, Lock, Globe, UserCheck, XCircle,
} from 'lucide-react';
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, CartesianGrid, Legend,
} from 'recharts';

const API = import.meta.env.VITE_API_URL || '';
const COLORS = ['#2563eb', '#dc2626', '#16a34a', '#ca8a04', '#7c3aed', '#0891b2'];

// ═══════════════════════════════════════════════════════════════════════════
// TYPES
// ═══════════════════════════════════════════════════════════════════════════

interface Aspirant {
  aspirant_id: string;
  full_name: string;
  position: string;
  state_of_origin: string;
  screening_status: string;
  status: string;
  manifesto_url: string;
  deposit_amount: number;
  deposit_paid: boolean;
}

interface Delegate {
  delegate_id: string;
  full_name: string;
  delegate_type: string;
  state_code: string;
  accreditation_status: string;
  has_voted: boolean;
  is_remote: boolean;
}

interface VotingRound {
  round_id: string;
  round_number: number;
  status: string;
  voting_method: string;
  total_votes: number;
  total_eligible: number;
  started_at: string;
  ended_at: string;
}

interface ConventionDashboard {
  total_delegates: number;
  accredited: number;
  checked_in: number;
  turnout_pct: number;
  quorum_met: boolean;
  current_round: number;
  aspirants_cleared: number;
  state_breakdown: { state: string; count: number; accredited: number }[];
}

interface TallyResult {
  aspirant_id: string;
  full_name: string;
  votes: number;
  percentage: number;
  rank: number;
}

interface CryptoAudit {
  total_keys: number;
  total_shuffles: number;
  total_ballots: number;
  remote_ballots: number;
  decoy_ballots: number;
  verified_decryptions: number;
}

// ═══════════════════════════════════════════════════════════════════════════
// MAIN PAGE COMPONENT
// ═══════════════════════════════════════════════════════════════════════════

export default function PartyPrimariesPage() {
  const [activeTab, setActiveTab] = useState<string>('dashboard');
  const [dashboard, setDashboard] = useState<ConventionDashboard | null>(null);
  const [aspirants, setAspirants] = useState<Aspirant[]>([]);
  const [delegates, setDelegates] = useState<Delegate[]>([]);
  const [rounds, setRounds] = useState<VotingRound[]>([]);
  const [tallyResults, _setTallyResults] = useState<TallyResult[]>([]);
  const [cryptoAudit, setCryptoAudit] = useState<CryptoAudit | null>(null);
  const [_loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [electionId, setElectionId] = useState<string>('1');

  const headers = { 'Content-Type': 'application/json', 'X-GOTV-Party-Code': 'APC' };

  const fetchData = useCallback(async (endpoint: string) => {
    const res = await fetch(`${API}${endpoint}`, { headers });
    if (!res.ok) throw new Error(`${res.status}`);
    return res.json();
  }, []);

  const loadDashboard = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchData(`/gotv/primaries/elections/${electionId}/dashboard`);
      setDashboard(data);
    } catch (e) {
      setError('Failed to load dashboard');
    } finally {
      setLoading(false);
    }
  }, [electionId, fetchData]);

  const loadAspirants = useCallback(async () => {
    try {
      const data = await fetchData(`/gotv/primaries/aspirants?election_id=${electionId}`);
      setAspirants(data.aspirants || []);
    } catch {
      setAspirants([]);
    }
  }, [electionId, fetchData]);

  const loadDelegates = useCallback(async () => {
    try {
      const data = await fetchData(`/gotv/primaries/delegates?election_id=${electionId}`);
      setDelegates(data.delegates || []);
    } catch {
      setDelegates([]);
    }
  }, [electionId, fetchData]);

  const loadRounds = useCallback(async () => {
    try {
      const data = await fetchData(`/gotv/primaries/elections/${electionId}/rounds`);
      setRounds(data.rounds || []);
    } catch {
      setRounds([]);
    }
  }, [electionId, fetchData]);

  const loadCryptoAudit = useCallback(async () => {
    try {
      const data = await fetchData(`/gotv/primaries/elections/${electionId}/crypto/audit`);
      setCryptoAudit(data);
    } catch {
      setCryptoAudit(null);
    }
  }, [electionId, fetchData]);

  useEffect(() => {
    loadDashboard();
    loadAspirants();
    loadDelegates();
    loadRounds();
    loadCryptoAudit();
  }, [electionId]);

  const tabs = [
    { id: 'dashboard', label: 'Convention Dashboard', icon: BarChart3 },
    { id: 'aspirants', label: 'Aspirant Portal', icon: Users },
    { id: 'delegates', label: 'Delegate Management', icon: UserCheck },
    { id: 'voting', label: 'Voting Rounds', icon: Vote },
    { id: 'remote', label: 'Remote Voting', icon: Globe },
    { id: 'crypto', label: 'E2E Verification', icon: Shield },
  ];

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Party Primaries & Convention</h1>
          <p className="text-muted-foreground">
            Phase 1: Convention Management | Phase 2: Remote E2E Voting
          </p>
        </div>
        <div className="flex gap-2">
          <Input
            placeholder="Election ID"
            value={electionId}
            onChange={(e) => setElectionId(e.target.value)}
            className="w-32"
          />
          <Button onClick={loadDashboard} variant="outline" size="sm">
            <RefreshCw className="w-4 h-4 mr-1" /> Refresh
          </Button>
        </div>
      </div>

      {/* Tab Navigation */}
      <div className="flex gap-2 border-b pb-2 overflow-x-auto">
        {tabs.map((tab) => (
          <Button
            key={tab.id}
            variant={activeTab === tab.id ? 'default' : 'ghost'}
            size="sm"
            onClick={() => setActiveTab(tab.id)}
          >
            <tab.icon className="w-4 h-4 mr-1" />
            {tab.label}
          </Button>
        ))}
      </div>

      {error && (
        <Card className="border-red-200 bg-red-50">
          <CardContent className="pt-4">
            <div className="flex items-center gap-2 text-red-600">
              <AlertTriangle className="w-4 h-4" />
              {error}
            </div>
          </CardContent>
        </Card>
      )}

      {/* DASHBOARD TAB */}
      {activeTab === 'dashboard' && dashboard && (
        <DashboardTab dashboard={dashboard} />
      )}

      {/* ASPIRANTS TAB */}
      {activeTab === 'aspirants' && (
        <AspirantsTab aspirants={aspirants} onRefresh={loadAspirants} />
      )}

      {/* DELEGATES TAB */}
      {activeTab === 'delegates' && (
        <DelegatesTab delegates={delegates} onRefresh={loadDelegates} />
      )}

      {/* VOTING TAB */}
      {activeTab === 'voting' && (
        <VotingTab rounds={rounds} tallyResults={tallyResults} onRefresh={loadRounds} />
      )}

      {/* REMOTE VOTING TAB */}
      {activeTab === 'remote' && <RemoteVotingTab />}

      {/* CRYPTO AUDIT TAB */}
      {activeTab === 'crypto' && (
        <CryptoAuditTab cryptoAudit={cryptoAudit} onRefresh={loadCryptoAudit} />
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// SUB-COMPONENTS
// ═══════════════════════════════════════════════════════════════════════════

function DashboardTab({ dashboard }: { dashboard: ConventionDashboard }) {
  const stats = [
    { label: 'Total Delegates', value: dashboard.total_delegates, icon: Users },
    { label: 'Accredited', value: dashboard.accredited, icon: UserCheck },
    { label: 'Checked In', value: dashboard.checked_in, icon: CheckCircle },
    {
      label: 'Turnout',
      value: `${dashboard.turnout_pct?.toFixed(1) || 0}%`,
      icon: BarChart3,
    },
  ];

  return (
    <div className="space-y-6">
      {/* Quick Stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {stats.map((s) => (
          <Card key={s.label}>
            <CardContent className="pt-4 flex items-center gap-3">
              <s.icon className="w-8 h-8 text-blue-600" />
              <div>
                <p className="text-2xl font-bold">{s.value}</p>
                <p className="text-xs text-muted-foreground">{s.label}</p>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Quorum Status */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="w-5 h-5" />
            Quorum Status
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4">
            <Badge variant={dashboard.quorum_met ? 'default' : 'destructive'}>
              {dashboard.quorum_met ? 'QUORUM MET' : 'QUORUM NOT MET'}
            </Badge>
            <span className="text-sm text-muted-foreground">
              Current Round: {dashboard.current_round || 'None'}
            </span>
            <span className="text-sm text-muted-foreground">
              Cleared Aspirants: {dashboard.aspirants_cleared || 0}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* State Breakdown */}
      {dashboard.state_breakdown && dashboard.state_breakdown.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Delegate Distribution by State</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={dashboard.state_breakdown}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="state" fontSize={10} />
                <YAxis />
                <Tooltip />
                <Legend />
                <Bar dataKey="count" fill="#2563eb" name="Total" />
                <Bar dataKey="accredited" fill="#16a34a" name="Accredited" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function AspirantsTab({ aspirants, onRefresh }: { aspirants: Aspirant[]; onRefresh: () => void }) {
  const statusColor: Record<string, string> = {
    cleared: 'bg-green-100 text-green-800',
    disqualified: 'bg-red-100 text-red-800',
    pending: 'bg-yellow-100 text-yellow-800',
    withdrawn: 'bg-gray-100 text-gray-800',
  };

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-lg font-semibold">Aspirant Portal ({aspirants.length})</h2>
        <Button onClick={onRefresh} variant="outline" size="sm">
          <RefreshCw className="w-4 h-4 mr-1" /> Refresh
        </Button>
      </div>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {aspirants.map((a) => (
          <Card key={a.aspirant_id}>
            <CardHeader className="pb-2">
              <CardTitle className="text-base flex justify-between">
                {a.full_name}
                <Badge className={statusColor[a.screening_status] || ''}>
                  {a.screening_status}
                </Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-sm space-y-1">
                <p><strong>Position:</strong> {a.position}</p>
                <p><strong>State:</strong> {a.state_of_origin}</p>
                <p>
                  <strong>Deposit:</strong>{' '}
                  {a.deposit_paid ? (
                    <Badge variant="default">Paid</Badge>
                  ) : (
                    <Badge variant="destructive">Unpaid</Badge>
                  )}
                </p>
              </div>
            </CardContent>
          </Card>
        ))}
        {aspirants.length === 0 && (
          <p className="text-muted-foreground col-span-3 text-center py-8">
            No aspirants found for this election.
          </p>
        )}
      </div>
    </div>
  );
}

function DelegatesTab({ delegates, onRefresh }: { delegates: Delegate[]; onRefresh: () => void }) {
  const accredited = delegates.filter((d) => d.accreditation_status === 'accredited').length;
  const voted = delegates.filter((d) => d.has_voted).length;
  const remote = delegates.filter((d) => d.is_remote).length;

  const typeBreakdown = delegates.reduce<Record<string, number>>((acc, d) => {
    acc[d.delegate_type] = (acc[d.delegate_type] || 0) + 1;
    return acc;
  }, {});
  const typePieData = Object.entries(typeBreakdown).map(([name, value]) => ({ name, value }));

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-lg font-semibold">Delegate Management ({delegates.length})</h2>
        <Button onClick={onRefresh} variant="outline" size="sm">
          <RefreshCw className="w-4 h-4 mr-1" /> Refresh
        </Button>
      </div>

      <div className="grid grid-cols-4 gap-4">
        <Card><CardContent className="pt-4 text-center">
          <p className="text-2xl font-bold">{delegates.length}</p><p className="text-xs">Total</p>
        </CardContent></Card>
        <Card><CardContent className="pt-4 text-center">
          <p className="text-2xl font-bold text-green-600">{accredited}</p><p className="text-xs">Accredited</p>
        </CardContent></Card>
        <Card><CardContent className="pt-4 text-center">
          <p className="text-2xl font-bold text-blue-600">{voted}</p><p className="text-xs">Voted</p>
        </CardContent></Card>
        <Card><CardContent className="pt-4 text-center">
          <p className="text-2xl font-bold text-purple-600">{remote}</p><p className="text-xs">Remote</p>
        </CardContent></Card>
      </div>

      {typePieData.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Delegate Types</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <PieChart>
                <Pie data={typePieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={80} label>
                  {typePieData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
                </Pie>
                <Tooltip />
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="pt-4">
          <div className="overflow-auto max-h-96">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-2">Name</th>
                  <th className="text-left p-2">Type</th>
                  <th className="text-left p-2">State</th>
                  <th className="text-left p-2">Status</th>
                  <th className="text-left p-2">Voted</th>
                </tr>
              </thead>
              <tbody>
                {delegates.slice(0, 50).map((d) => (
                  <tr key={d.delegate_id} className="border-b hover:bg-muted/50">
                    <td className="p-2">{d.full_name}</td>
                    <td className="p-2">{d.delegate_type}</td>
                    <td className="p-2">{d.state_code}</td>
                    <td className="p-2">
                      <Badge variant={d.accreditation_status === 'accredited' ? 'default' : 'secondary'}>
                        {d.accreditation_status}
                      </Badge>
                    </td>
                    <td className="p-2">
                      {d.has_voted ? <CheckCircle className="w-4 h-4 text-green-600" /> : <XCircle className="w-4 h-4 text-gray-400" />}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function VotingTab({ rounds, tallyResults: _tallyResults, onRefresh }: {
  rounds: VotingRound[];
  tallyResults: TallyResult[];
  onRefresh: () => void;
}) {
  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-lg font-semibold">Voting Rounds ({rounds.length})</h2>
        <Button onClick={onRefresh} variant="outline" size="sm">
          <RefreshCw className="w-4 h-4 mr-1" /> Refresh
        </Button>
      </div>

      {rounds.map((round) => (
        <Card key={round.round_id}>
          <CardHeader className="pb-2">
            <CardTitle className="text-base flex justify-between">
              Round {round.round_number}
              <Badge variant={round.status === 'certified' ? 'default' : round.status === 'open' ? 'secondary' : 'outline'}>
                {round.status}
              </Badge>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-4 gap-4 text-sm">
              <div><strong>Method:</strong> {round.voting_method}</div>
              <div><strong>Votes:</strong> {round.total_votes}</div>
              <div><strong>Eligible:</strong> {round.total_eligible}</div>
              <div>
                <strong>Turnout:</strong>{' '}
                {round.total_eligible > 0
                  ? ((round.total_votes / round.total_eligible) * 100).toFixed(1)
                  : 0}%
              </div>
            </div>
            {round.started_at && (
              <p className="text-xs text-muted-foreground mt-2">
                <Clock className="w-3 h-3 inline mr-1" />
                Started: {new Date(round.started_at).toLocaleString()}
              </p>
            )}
          </CardContent>
        </Card>
      ))}

      {rounds.length === 0 && (
        <p className="text-muted-foreground text-center py-8">No voting rounds found.</p>
      )}
    </div>
  );
}

function RemoteVotingTab() {
  const [_deviceId, _setDeviceId] = useState('');
  const [verifyCode, setVerifyCode] = useState('');
  const [verifyResult, setVerifyResult] = useState<any>(null);

  const verifyBallot = async () => {
    try {
      const res = await fetch(
        `${API}/gotv/primaries/remote/verify?confirmation_code=${verifyCode}`,
      );
      const data = await res.json();
      setVerifyResult(data);
    } catch {
      setVerifyResult({ error: 'Verification failed' });
    }
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Globe className="w-5 h-5" />
            Remote Voting Portal
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            E2E verifiable remote electronic voting with ElectionGuard-style encryption,
            mix-net ballot anonymization, and coercion resistance.
          </p>

          <div className="grid md:grid-cols-3 gap-4">
            <Card className="border-blue-200">
              <CardContent className="pt-4">
                <Lock className="w-6 h-6 text-blue-600 mb-2" />
                <h3 className="font-semibold">Encrypted Ballot</h3>
                <p className="text-xs text-muted-foreground">
                  Homomorphic encryption ensures votes are tallied without decrypting individual ballots.
                </p>
              </CardContent>
            </Card>
            <Card className="border-green-200">
              <CardContent className="pt-4">
                <Shield className="w-6 h-6 text-green-600 mb-2" />
                <h3 className="font-semibold">Mix-Net Shuffle</h3>
                <p className="text-xs text-muted-foreground">
                  Ballots are shuffled and re-encrypted so no one can link a vote to a voter.
                </p>
              </CardContent>
            </Card>
            <Card className="border-purple-200">
              <CardContent className="pt-4">
                <AlertTriangle className="w-6 h-6 text-purple-600 mb-2" />
                <h3 className="font-semibold">Coercion Resistance</h3>
                <p className="text-xs text-muted-foreground">
                  Decoy ballots allow voting under duress — fake votes are silently discarded.
                </p>
              </CardContent>
            </Card>
          </div>
        </CardContent>
      </Card>

      {/* Ballot Verification */}
      <Card>
        <CardHeader>
          <CardTitle>Verify Your Ballot</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex gap-2">
            <Input
              placeholder="Enter confirmation code"
              value={verifyCode}
              onChange={(e) => setVerifyCode(e.target.value)}
            />
            <Button onClick={verifyBallot}>Verify</Button>
          </div>
          {verifyResult && (
            <pre className="bg-muted p-4 rounded text-xs overflow-auto">
              {JSON.stringify(verifyResult, null, 2)}
            </pre>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function CryptoAuditTab({ cryptoAudit, onRefresh }: {
  cryptoAudit: CryptoAudit | null;
  onRefresh: () => void;
}) {
  if (!cryptoAudit) {
    return <p className="text-muted-foreground text-center py-8">No cryptographic data available.</p>;
  }

  const data = [
    { name: 'Crypto Keys', value: cryptoAudit.total_keys },
    { name: 'Shuffles', value: cryptoAudit.total_shuffles },
    { name: 'Total Ballots', value: cryptoAudit.total_ballots },
    { name: 'Remote Ballots', value: cryptoAudit.remote_ballots },
    { name: 'Decoy Ballots', value: cryptoAudit.decoy_ballots },
    { name: 'Verified Decryptions', value: cryptoAudit.verified_decryptions },
  ];

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-lg font-semibold">E2E Verification Audit Trail</h2>
        <Button onClick={onRefresh} variant="outline" size="sm">
          <RefreshCw className="w-4 h-4 mr-1" /> Refresh
        </Button>
      </div>

      <div className="grid grid-cols-3 gap-4">
        {data.map((d) => (
          <Card key={d.name}>
            <CardContent className="pt-4 text-center">
              <p className="text-2xl font-bold">{d.value}</p>
              <p className="text-xs text-muted-foreground">{d.name}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Cryptographic Pipeline</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between gap-2 text-sm">
            {['Key Generation', 'Ballot Encryption', 'Mix-Net Shuffle', 'Threshold Decrypt', 'Tally Verify'].map(
              (step, i) => (
                <div key={step} className="flex items-center gap-2">
                  <div className="w-8 h-8 rounded-full bg-blue-600 text-white flex items-center justify-center text-xs font-bold">
                    {i + 1}
                  </div>
                  <span>{step}</span>
                  {i < 4 && <span className="text-muted-foreground">→</span>}
                </div>
              ),
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
