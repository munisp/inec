import { useEffect, useState } from 'react';
import { logger } from '@/lib/utils';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ChevronRight, ArrowLeft, Activity } from 'lucide-react';

interface CollationItem {
  code: string;
  name: string;
  geo_zone?: string;
  total_pus: number;
  reported_pus: number;
  total_valid_votes: number | null;
  rejected_votes: number | null;
  total_votes_cast: number | null;
  party_scores: Array<{ party_code: string; abbreviation: string; color: string; total_votes: number }>;
  registered_voters?: number;
  result_id?: number;
  status?: string;
  tigerbeetle_status?: string;
  hyperledger_status?: string;
  accredited_voters?: number;
}

type Level = 'state' | 'lga' | 'ward' | 'pu';

const LEVEL_LABELS: Record<Level, string> = { state: 'States', lga: 'LGAs', ward: 'Wards', pu: 'Polling Units' };
const NEXT_LEVEL: Record<Level, Level | null> = { state: 'lga', lga: 'ward', ward: 'pu', pu: null };

function formatNumber(n: number | null) {
  return n != null ? new Intl.NumberFormat().format(n) : '-';
}

export default function CollationPage() {
  const [level, setLevel] = useState<Level>('state');
  const [parentCode, setParentCode] = useState<string | undefined>();
  const [data, setData] = useState<CollationItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [breadcrumbs, setBreadcrumbs] = useState<Array<{ level: Level; code?: string; name: string }>>([
    { level: 'state', name: 'National' }
  ]);

  useEffect(() => {
    async function load() {
      setLoading(true);
      try {
        const res = await api.getCollation(1, level, parentCode);
        setData(res);
      } catch (e) {
        logger.error(e);
      } finally {
        setLoading(false);
      }
    }
    load();
  }, [level, parentCode]);

  const drillDown = (item: CollationItem) => {
    const next = NEXT_LEVEL[level];
    if (!next) return;
    setBreadcrumbs(prev => [...prev, { level: next, code: item.code, name: item.name }]);
    setParentCode(item.code);
    setLevel(next);
  };

  const goBack = (index: number) => {
    const bc = breadcrumbs[index];
    const newBc = breadcrumbs.slice(0, index + 1);
    setBreadcrumbs(newBc);
    setLevel(bc.level);
    setParentCode(bc.code);
  };

  const topParties = data.length > 0 && data[0].party_scores
    ? [...new Set(data.flatMap(d => d.party_scores.map(p => p.abbreviation)))].slice(0, 6)
    : [];

  if (loading) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 text-sm flex-wrap">
        {breadcrumbs.map((bc, i) => (
          <div key={i} className="flex items-center gap-1">
            {i > 0 && <ChevronRight className="w-3 h-3 text-zinc-400" />}
            <button
              onClick={() => goBack(i)}
              className={`px-2 py-1 rounded ${i === breadcrumbs.length - 1 ? 'bg-green-100 text-green-800 font-medium' : 'text-zinc-600 hover:text-zinc-900'}`}
            >
              {bc.name}
            </button>
          </div>
        ))}
      </div>

      {breadcrumbs.length > 1 && (
        <Button variant="ghost" size="sm" onClick={() => goBack(breadcrumbs.length - 2)} className="gap-1">
          <ArrowLeft className="w-4 h-4" /> Back
        </Button>
      )}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-semibold">
            {LEVEL_LABELS[level]} Level Collation
            <Badge variant="outline" className="ml-2">{data.length} {LEVEL_LABELS[level].toLowerCase()}</Badge>
          </CardTitle>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-48">{level === 'pu' ? 'Polling Unit' : 'Area'}</TableHead>
                {level === 'state' && <TableHead>Zone</TableHead>}
                <TableHead className="text-right">PUs</TableHead>
                <TableHead className="text-right">Reported</TableHead>
                <TableHead className="text-right">Valid Votes</TableHead>
                <TableHead className="text-right">Rejected</TableHead>
                {topParties.map(p => (
                  <TableHead key={p} className="text-right text-xs">{p}</TableHead>
                ))}
                {level === 'pu' && <TableHead>Status</TableHead>}
                {NEXT_LEVEL[level] && <TableHead></TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((item) => {
                const partyMap: Record<string, number> = {};
                item.party_scores?.forEach(p => { partyMap[p.abbreviation] = p.total_votes; });
                return (
                  <TableRow key={item.code} className="hover:bg-zinc-50">
                    <TableCell className="font-medium">{item.name}</TableCell>
                    {level === 'state' && <TableCell><Badge variant="outline" className="text-xs">{item.geo_zone}</Badge></TableCell>}
                    <TableCell className="text-right">{item.total_pus || '-'}</TableCell>
                    <TableCell className="text-right">
                      {item.reported_pus || 0}
                      {item.total_pus > 0 && (
                        <span className="text-xs text-zinc-400 ml-1">
                          ({Math.round((item.reported_pus || 0) / item.total_pus * 100)}%)
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-medium">{formatNumber(item.total_valid_votes)}</TableCell>
                    <TableCell className="text-right text-zinc-500">{formatNumber(item.rejected_votes)}</TableCell>
                    {topParties.map(p => (
                      <TableCell key={p} className="text-right text-sm">{formatNumber(partyMap[p] || 0)}</TableCell>
                    ))}
                    {level === 'pu' && (
                      <TableCell>
                        <Badge className={`text-xs ${
                          item.status === 'finalized' ? 'bg-green-100 text-green-800' :
                          item.status === 'validated' ? 'bg-blue-100 text-blue-800' :
                          item.status === 'pending' ? 'bg-amber-100 text-amber-800' :
                          !item.status ? 'bg-zinc-100 text-zinc-500' : 'bg-red-100 text-red-800'
                        }`}>
                          {item.status || 'No result'}
                        </Badge>
                      </TableCell>
                    )}
                    {NEXT_LEVEL[level] && (
                      <TableCell>
                        <Button variant="ghost" size="sm" onClick={() => drillDown(item)} className="gap-1 text-xs">
                          Drill down <ChevronRight className="w-3 h-3" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })}
              {data.length === 0 && (
                <TableRow><TableCell colSpan={10} className="text-center py-8 text-zinc-500">No data available</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
