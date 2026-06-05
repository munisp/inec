import { useEffect, useState } from 'react';
import { logger } from '@/lib/utils';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, Legend
} from 'recharts';
import {
  Vote, CheckCircle, Clock, Database,
  TrendingUp, Activity
} from 'lucide-react';

interface DashboardData {
  election: { title: string; status: string; election_date: string };
  total_polling_units: number;
  results_received: number;
  completion_percentage: number;
  status_breakdown: Record<string, number>;
  vote_totals: { valid: number; rejected: number; cast: number; accredited: number };
  party_scores: Array<{ party_code: string; party_name: string; color: string; abbreviation: string; total_votes: number }>;
  state_results: Array<{ code: string; name: string; geo_zone: string; results_count: number; total_votes: number }>;
  zone_results: Array<{ geo_zone: string; total_votes: number; results_count: number }>;
  dual_ledger: { tigerbeetle_posted: number; hyperledger_confirmed: number; total_results: number; reconciliation_variance: number };
}

interface FeedItem {
  id: number;
  polling_unit_code: string;
  status: string;
  total_votes_cast: number;
  tigerbeetle_status: string;
  hyperledger_status: string;
  submitted_at: string;
  pu_name: string;
  ward_name: string;
  lga_name: string;
  state_name: string;
  state_code: string;
}

function formatNumber(n: number) {
  return new Intl.NumberFormat().format(n);
}

export default function DashboardPage() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [feed, setFeed] = useState<FeedItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        const [stats, liveFeed] = await Promise.all([
          api.getDashboardStats(1),
          api.getLiveFeed(1, 15)
        ]);
        setData(stats);
        setFeed(liveFeed);
      } catch (e) {
        logger.error(e);
      } finally {
        setLoading(false);
      }
    }
    load();
    const interval = setInterval(load, 30000);
    return () => clearInterval(interval);
  }, []);

  if (loading || !data) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;

  const topParties = data.party_scores.slice(0, 6);
  const pieData= topParties.map(p => ({ name: p.abbreviation, value: p.total_votes, color: p.color }));

  const statusData = [
    { name: 'Finalized', value: data.status_breakdown.finalized, color: '#16a34a' },
    { name: 'Validated', value: data.status_breakdown.validated, color: '#2563eb' },
    { name: 'Pending', value: data.status_breakdown.pending, color: '#f59e0b' },
    { name: 'Disputed', value: data.status_breakdown.disputed, color: '#dc2626' },
  ].filter(s => s.value > 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-bold text-zinc-900">{data.election.title}</h2>
          <p className="text-sm text-zinc-500">Election Date: {data.election.election_date} | Status: <Badge variant="outline" className="ml-1 capitalize">{data.election.status}</Badge></p>
        </div>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardContent className="pt-4 pb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-blue-100 flex items-center justify-center">
                <Vote className="w-5 h-5 text-blue-700" />
              </div>
              <div>
                <p className="text-xs text-zinc-500">Results Received</p>
                <p className="text-xl font-bold">{formatNumber(data.results_received)}</p>
                <p className="text-xs text-zinc-400">of {formatNumber(data.total_polling_units)} PUs</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 pb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-green-100 flex items-center justify-center">
                <CheckCircle className="w-5 h-5 text-green-700" />
              </div>
              <div>
                <p className="text-xs text-zinc-500">Total Votes Cast</p>
                <p className="text-xl font-bold">{formatNumber(data.vote_totals.cast)}</p>
                <p className="text-xs text-zinc-400">{formatNumber(data.vote_totals.rejected)} rejected</p>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 pb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-amber-100 flex items-center justify-center">
                <Clock className="w-5 h-5 text-amber-700" />
              </div>
              <div>
                <p className="text-xs text-zinc-500">Completion</p>
                <p className="text-xl font-bold">{data.completion_percentage}%</p>
                <Progress value={data.completion_percentage} className="h-1.5 mt-1" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 pb-4">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-purple-100 flex items-center justify-center">
                <TrendingUp className="w-5 h-5 text-purple-700" />
              </div>
              <div>
                <p className="text-xs text-zinc-500">Finalized</p>
                <p className="text-xl font-bold">{formatNumber(data.status_breakdown.finalized)}</p>
                <p className="text-xs text-zinc-400">{data.status_breakdown.pending} pending</p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card className="border-green-200 bg-green-50/50">
        <CardContent className="pt-4 pb-4">
          <div className="flex items-center gap-2 mb-3">
            <Database className="w-4 h-4 text-green-700" />
            <h3 className="text-sm font-semibold text-green-900">Dual-Ledger Reconciliation Status</h3>
          </div>
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div>
              <p className="text-xs text-green-700">TigerBeetle (Operational)</p>
              <p className="text-lg font-bold text-green-900">{formatNumber(data.dual_ledger.tigerbeetle_posted)} POSTED</p>
            </div>
            <div>
              <p className="text-xs text-green-700">Hyperledger (Official)</p>
              <p className="text-lg font-bold text-green-900">{formatNumber(data.dual_ledger.hyperledger_confirmed)} CONFIRMED</p>
            </div>
            <div>
              <p className="text-xs text-green-700">Variance</p>
              <p className="text-lg font-bold text-green-900">{data.dual_ledger.reconciliation_variance}%</p>
            </div>
            <div>
              <p className="text-xs text-green-700">Reconciliation</p>
              <Badge className={data.dual_ledger.reconciliation_variance < 0.5 ? 'bg-green-700' : 'bg-red-600'}>
                {data.dual_ledger.reconciliation_variance < 0.5 ? 'PASS' : 'ALERT'}
              </Badge>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">Party Results</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={topParties} layout="vertical">
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis type="number" tickFormatter={v => formatNumber(v)} />
                <YAxis type="category" dataKey="abbreviation" width={50} />
                <Tooltip formatter={(v: number) => formatNumber(v)} />
                <Bar dataKey="total_votes" name="Votes">
                  {topParties.map((p, i) => (
                    <Cell key={i} fill={p.color} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">Vote Distribution</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <PieChart>
                <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={100} label={({ name, percent }) => `${name} ${(percent * 100).toFixed(1)}%`}>
                  {pieData.map((p, i) => (
                    <Cell key={i} fill={p.color} />
                  ))}
                </Pie>
                <Tooltip formatter={(v: number) => formatNumber(v)} />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      <div className="grid lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">Result Status Breakdown</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <PieChart>
                <Pie data={statusData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={60} outerRadius={90} label>
                  {statusData.map((s, i) => (
                    <Cell key={i} fill={s.color} />
                  ))}
                </Pie>
                <Tooltip />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">Geo-Political Zone Results</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={data.zone_results}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="geo_zone" tick={{ fontSize: 10 }} />
                <YAxis tickFormatter={v => `${(v/1000).toFixed(0)}k`} />
                <Tooltip formatter={(v: number) => formatNumber(v)} />
                <Bar dataKey="total_votes" fill="#16a34a" name="Votes" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-semibold">Live Results Feed</CardTitle>
            <Badge variant="outline" className="text-green-700 border-green-200">
              <Activity className="w-3 h-3 mr-1" /> Live
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            {feed.map((item) => (
              <div key={item.id} className="flex items-center justify-between p-2.5 rounded-lg border border-zinc-100 hover:bg-zinc-50">
                <div className="flex items-center gap-3">
                  <div className={`w-2 h-2 rounded-full ${
                    item.status === 'finalized' ? 'bg-green-500' :
                    item.status === 'validated' ? 'bg-blue-500' :
                    item.status === 'pending' ? 'bg-amber-500' : 'bg-red-500'
                  }`} />
                  <div>
                    <p className="text-sm font-medium">{item.pu_name}</p>
                    <p className="text-xs text-zinc-500">{item.ward_name}, {item.lga_name}, {item.state_name}</p>
                  </div>
                </div>
                <div className="text-right">
                  <p className="text-sm font-semibold">{formatNumber(item.total_votes_cast)} votes</p>
                  <div className="flex gap-1 justify-end">
                    <Badge variant="outline" className="text-xs px-1.5 py-0">
                      TB: {item.tigerbeetle_status}
                    </Badge>
                    <Badge variant="outline" className="text-xs px-1.5 py-0">
                      HL: {item.hyperledger_status}
                    </Badge>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
