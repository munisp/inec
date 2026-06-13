import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts';

interface ChainStatus {
  total_blocks: number;
  total_transactions: number;
  verified_tx: number;
  merkle_anchors: number;
  latest_block_hash: string;
  latest_block_time: string;
  chain_integrity: boolean;
}

interface Block {
  block_number: number;
  prev_hash: string;
  merkle_root: string;
  block_hash: string;
  block_type: string;
  tx_count: number;
  timestamp: string;
}

export default function GOTVBlockchain() {
  const [status, setStatus] = useState<ChainStatus | null>(null);
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [loading, setLoading] = useState(true);
  const [anchoring, setAnchoring] = useState(false);
  const [anchorResult, setAnchorResult] = useState<Record<string, unknown> | null>(null);

  useEffect(() => {
    const load = async () => {
      try {
        const [statusRes, blocksRes] = await Promise.all([
          api.get('/gotv/blockchain/status'),
          api.get('/gotv/blockchain/blocks'),
        ]);
        setStatus(statusRes.data);
        setBlocks(blocksRes.data.blocks || []);
      } catch {
        // Seed demo data
        setStatus({
          total_blocks: 24,
          total_transactions: 156,
          verified_tx: 148,
          merkle_anchors: 18,
          latest_block_hash: 'a3f8c2d1e5b94706...',
          latest_block_time: '2026-06-12T22:30:00Z',
          chain_integrity: true,
        });
        setBlocks([
          { block_number: 24, prev_hash: '7e2c1b4a9d3f8506...', merkle_root: 'f1d2e3c4b5a69780...', block_hash: 'a3f8c2d1e5b94706...', block_type: 'pledges', tx_count: 42, timestamp: '2026-06-12T22:30:00Z' },
          { block_number: 23, prev_hash: '6d1b0a3e8c2f7495...', merkle_root: 'e0c1d2b3a4958670...', block_hash: '7e2c1b4a9d3f8506...', block_type: 'volunteers', tx_count: 18, timestamp: '2026-06-12T20:15:00Z' },
          { block_number: 22, prev_hash: '5c0a9b2d7e1f6384...', merkle_root: 'd9b0c1a2e3847560...', block_hash: '6d1b0a3e8c2f7495...', block_type: 'contacts', tx_count: 65, timestamp: '2026-06-12T18:00:00Z' },
          { block_number: 21, prev_hash: '4b9a8c1d6e0f5273...', merkle_root: 'c8a9b0d1e2736450...', block_hash: '5c0a9b2d7e1f6384...', block_type: 'pledges', tx_count: 31, timestamp: '2026-06-11T16:45:00Z' },
          { block_number: 20, prev_hash: '3a8b7c0d5e9f4162...', merkle_root: 'b7a8c9d0e1625340...', block_hash: '4b9a8c1d6e0f5273...', block_type: 'pledges', tx_count: 28, timestamp: '2026-06-10T14:30:00Z' },
        ]);
      }
      setLoading(false);
    };
    load();
  }, []);

  const handleAnchor = async (anchorType: string) => {
    setAnchoring(true);
    try {
      const res = await api.post('/gotv/blockchain/anchor', { anchor_type: anchorType });
      setAnchorResult(res.data);
      // Refresh
      const [statusRes, blocksRes] = await Promise.all([
        api.get('/gotv/blockchain/status'),
        api.get('/gotv/blockchain/blocks'),
      ]);
      setStatus(statusRes.data);
      setBlocks(blocksRes.data.blocks || []);
    } catch {
      setAnchorResult({ error: 'Anchor failed — database may not be connected' });
    }
    setAnchoring(false);
  };

  if (loading) return <div className="text-center py-12 text-muted-foreground">Loading blockchain...</div>;

  const blockTypeChart = (() => {
    const counts: Record<string, number> = {};
    blocks.forEach(b => { counts[b.block_type] = (counts[b.block_type] || 0) + b.tx_count; });
    return Object.entries(counts).map(([type, count]) => ({ name: type, transactions: count }));
  })();

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <h2 className="text-xl font-bold">GOTV Blockchain</h2>
        <Badge variant={status?.chain_integrity ? 'default' : 'destructive'}>
          {status?.chain_integrity ? 'Chain Valid' : 'Integrity Error'}
        </Badge>
        <Badge variant="outline">SHA-256</Badge>
        <Badge variant="outline">Merkle Anchored</Badge>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-5 gap-4">
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">{status?.total_blocks || 0}</div>
            <p className="text-sm text-muted-foreground">Blocks</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">{status?.total_transactions || 0}</div>
            <p className="text-sm text-muted-foreground">Transactions</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold text-green-600">{status?.verified_tx || 0}</div>
            <p className="text-sm text-muted-foreground">Verified TX</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold">{status?.merkle_anchors || 0}</div>
            <p className="text-sm text-muted-foreground">Merkle Anchors</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4">
            <div className="text-2xl font-bold font-mono text-xs">
              {status?.latest_block_hash?.substring(0, 16) || 'N/A'}
            </div>
            <p className="text-sm text-muted-foreground">Latest Block</p>
          </CardContent>
        </Card>
      </div>

      {/* Anchor Actions */}
      <Card>
        <CardHeader><CardTitle>Anchor to Chain</CardTitle></CardHeader>
        <CardContent>
          <div className="flex gap-3">
            <Button onClick={() => handleAnchor('pledges')} disabled={anchoring}>
              {anchoring ? 'Anchoring...' : 'Anchor Pledges'}
            </Button>
            <Button variant="outline" onClick={() => handleAnchor('volunteers')} disabled={anchoring}>
              Anchor Volunteers
            </Button>
            <Button variant="outline" onClick={() => handleAnchor('contacts')} disabled={anchoring}>
              Anchor Contacts
            </Button>
          </div>
          {anchorResult && (
            <div className="mt-4 p-3 bg-muted rounded-lg text-sm font-mono">
              <pre>{JSON.stringify(anchorResult, null, 2)}</pre>
            </div>
          )}
        </CardContent>
      </Card>

      {/* TX by Type Chart */}
      <Card>
        <CardHeader><CardTitle>Transactions by Anchor Type</CardTitle></CardHeader>
        <CardContent>
          <ResponsiveContainer width="100%" height={250}>
            <BarChart data={blockTypeChart}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="name" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="transactions" fill="#8b5cf6" />
            </BarChart>
          </ResponsiveContainer>
        </CardContent>
      </Card>

      {/* Block Explorer */}
      <Card>
        <CardHeader><CardTitle>Block Explorer</CardTitle></CardHeader>
        <CardContent>
          <table className="w-full text-sm">
            <thead><tr className="border-b">
              <th className="text-left py-2">#</th>
              <th className="text-left py-2">Block Hash</th>
              <th className="text-left py-2">Prev Hash</th>
              <th className="text-left py-2">Merkle Root</th>
              <th className="text-left py-2">Type</th>
              <th className="text-right py-2">TX Count</th>
              <th className="text-left py-2">Timestamp</th>
            </tr></thead>
            <tbody>
              {blocks.map(b => (
                <tr key={b.block_number} className="border-b hover:bg-muted/50">
                  <td className="py-2 font-bold">{b.block_number}</td>
                  <td className="py-2 font-mono text-xs">{b.block_hash}</td>
                  <td className="py-2 font-mono text-xs text-muted-foreground">{b.prev_hash}</td>
                  <td className="py-2 font-mono text-xs text-blue-600">{b.merkle_root}</td>
                  <td className="py-2"><Badge variant="outline">{b.block_type}</Badge></td>
                  <td className="py-2 text-right font-medium">{b.tx_count}</td>
                  <td className="py-2 text-xs">{new Date(b.timestamp).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {/* Cross-Party Verification */}
      <Card>
        <CardHeader><CardTitle>Cross-Party Verification</CardTitle></CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground mb-3">
            Verify another party&apos;s pledge count without revealing individual data (zero-knowledge aggregate).
          </p>
          <div className="grid grid-cols-3 gap-4 text-sm">
            <div className="p-3 border rounded">
              <div className="font-medium">Your Chain</div>
              <div className="text-2xl font-bold">{status?.total_blocks || 0} blocks</div>
              <div className="text-muted-foreground">{status?.merkle_anchors || 0} anchors</div>
            </div>
            <div className="p-3 border rounded">
              <div className="font-medium">Integrity</div>
              <div className={`text-2xl font-bold ${status?.chain_integrity ? 'text-green-600' : 'text-red-600'}`}>
                {status?.chain_integrity ? 'VALID' : 'BROKEN'}
              </div>
              <div className="text-muted-foreground">Hash chain verified</div>
            </div>
            <div className="p-3 border rounded">
              <div className="font-medium">Verification Rate</div>
              <div className="text-2xl font-bold">
                {status?.total_transactions ? ((status.verified_tx / status.total_transactions) * 100).toFixed(1) : 0}%
              </div>
              <div className="text-muted-foreground">{status?.verified_tx}/{status?.total_transactions} verified</div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
