import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { AuthoritativeDataUnavailable } from '@/components/AuthoritativeDataUnavailable';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell,
} from 'recharts';

const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6'];

interface Account {
  id: string;
  account_type: string;
  balance_kobo: number;
  balance_naira: number;
  pending_kobo: number;
  currency: string;
}

interface Transfer {
  id: string;
  debit: string;
  credit: string;
  amount_kobo: number;
  amount_naira: number;
  code: number;
  status: string;
  description: string;
  created_at: string;
  posted_at?: string;
}

interface Reconciliation {
  party_id: number;
  account_count: number;
  transfer_count: number;
  posted: number;
  pending: number;
  voided: number;
  total_posted_ngn: number;
  balanced: boolean;
  variance: number;
}

const CODE_LABELS: Record<number, string> = {
  100: 'Campaign Spend',
  200: 'Ride Cost',
  300: 'Volunteer Reimb.',
  400: 'Materials',
  500: 'Event Cost',
  600: 'SMS Cost',
  700: 'Phone Bank',
};

export default function GOTVLedger() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [reconciliation, setReconciliation] = useState<Reconciliation | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [acctRes, txRes, reconRes] = await Promise.all([
        api.get('/gotv/ledger/accounts'),
        api.get('/gotv/ledger/history?limit=50'),
        api.get('/gotv/ledger/reconcile'),
      ]);
      setAccounts(acctRes.data.accounts || []);
      setTransfers(txRes.data.transfers || []);
      setReconciliation(reconRes.data);
    } catch (err) {
      // Never fabricate accounts, transfers, or a reconciliation result on failure.
      setAccounts([]);
      setTransfers([]);
      setReconciliation(null);
      setLoadError(err instanceof Error ? err.message : 'ledger-source-unavailable');
    }
    setLoading(false);
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (loading) return <div className="text-center py-12 text-muted-foreground">Loading ledger...</div>;

  if (loadError || !reconciliation) {
    return (
      <AuthoritativeDataUnavailable
        title="Ledger data is unavailable"
        description="The TigerBeetle ledger service did not return verified account, transfer, and reconciliation records. No simulated balances, transfers, or balance status are shown."
        error={loadError || 'ledger-source-unavailable'}
        onRetry={load}
      />
    );
  }

  const accountChart = accounts.map(a => ({
    name: a.account_type.charAt(0).toUpperCase() + a.account_type.slice(1),
    balance: a.balance_naira,
    pending: a.pending_kobo / 100,
  }));

  const statusData = reconciliation ? [
    { name: 'Posted', value: reconciliation.posted },
    { name: 'Pending', value: reconciliation.pending },
    { name: 'Voided', value: reconciliation.voided },
  ] : [];

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <h2 className="text-xl font-bold">TigerBeetle Ledger</h2>
        <Badge variant={reconciliation?.balanced ? 'default' : 'destructive'}>
          {reconciliation?.balanced ? 'Balanced' : 'Imbalanced'}
        </Badge>
        <Badge variant="outline">Double-Entry</Badge>
        <Badge variant="outline">ACID</Badge>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-4 gap-4">
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">₦{((reconciliation?.total_posted_ngn || 0)).toLocaleString()}</div>
            <p className="text-sm text-muted-foreground">Total Posted</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">{reconciliation?.transfer_count || 0}</div>
            <p className="text-sm text-muted-foreground">Total Transfers</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">{accounts.length}</div>
            <p className="text-sm text-muted-foreground">Accounts</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold text-green-600">₦{(reconciliation?.variance || 0).toLocaleString()}</div>
            <p className="text-sm text-muted-foreground">Variance</p>
          </CardContent>
        </Card>
      </div>

      {/* Charts */}
      <div className="grid grid-cols-2 gap-6">
        <Card>
          <CardHeader><CardTitle>Account Balances (₦)</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={accountChart}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="name" />
                <YAxis />
                <Tooltip formatter={(v: number) => `₦${v.toLocaleString()}`} />
                <Bar dataKey="balance" fill="#3b82f6" name="Posted" />
                <Bar dataKey="pending" fill="#f59e0b" name="Pending" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Transfer Status</CardTitle></CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <PieChart>
                <Pie data={statusData} cx="50%" cy="50%" innerRadius={60} outerRadius={100} dataKey="value" label={({ name, value }) => `${name}: ${value}`}>
                  {statusData.map((_, i) => <Cell key={i} fill={COLORS[i]} />)}
                </Pie>
                <Tooltip />
              </PieChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Accounts Table */}
      <Card>
        <CardHeader><CardTitle>Ledger Accounts</CardTitle></CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead><tr className="border-b">
              <th className="text-left py-2">Account ID</th>
              <th className="text-left py-2">Type</th>
              <th className="text-right py-2">Balance (₦)</th>
              <th className="text-right py-2">Pending (₦)</th>
              <th className="text-left py-2">Currency</th>
            </tr></thead>
            <tbody>
              {accounts.map(a => (
                <tr key={a.id} className="border-b hover:bg-muted/50">
                  <td className="py-2 font-mono text-xs">{a.id}</td>
                  <td className="py-2"><Badge variant="outline">{a.account_type}</Badge></td>
                  <td className="py-2 text-right font-medium">₦{a.balance_naira.toLocaleString()}</td>
                  <td className="py-2 text-right text-muted-foreground">₦{(a.pending_kobo / 100).toLocaleString()}</td>
                  <td className="py-2">{a.currency}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {/* Recent Transfers */}
      <Card>
        <CardHeader><CardTitle>Recent Transfers</CardTitle></CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead><tr className="border-b">
              <th className="text-left py-2">TX ID</th>
              <th className="text-left py-2">Type</th>
              <th className="text-left py-2">Debit → Credit</th>
              <th className="text-right py-2">Amount</th>
              <th className="text-left py-2">Status</th>
              <th className="text-left py-2">Description</th>
              <th className="text-left py-2">Date</th>
            </tr></thead>
            <tbody>
              {transfers.map(tx => (
                <tr key={tx.id} className="border-b hover:bg-muted/50">
                  <td className="py-2 font-mono text-xs">{tx.id}</td>
                  <td className="py-2"><Badge variant="outline">{CODE_LABELS[tx.code] || `Code ${tx.code}`}</Badge></td>
                  <td className="py-2 text-xs">{tx.debit} → {tx.credit}</td>
                  <td className="py-2 text-right font-medium">₦{tx.amount_naira.toLocaleString()}</td>
                  <td className="py-2">
                    <Badge variant={tx.status === 'POSTED' ? 'default' : tx.status === 'VOIDED' ? 'destructive' : 'secondary'}>
                      {tx.status}
                    </Badge>
                  </td>
                  <td className="py-2 text-muted-foreground text-xs">{tx.description}</td>
                  <td className="py-2 text-xs">{new Date(tx.created_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
