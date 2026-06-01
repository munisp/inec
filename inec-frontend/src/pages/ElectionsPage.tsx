import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Landmark, Calendar, Users, Activity } from 'lucide-react';

interface Election {
  id: number; title: string; election_type: string; election_date: string;
  status: string; description: string; total_registered_voters: number;
}

function formatNumber(n: number) { return new Intl.NumberFormat().format(n); }

const statusColors: Record<string, string> = {
  upcoming: 'bg-blue-100 text-blue-800',
  active: 'bg-green-100 text-green-800',
  completed: 'bg-zinc-100 text-zinc-600',
  cancelled: 'bg-red-100 text-red-800',
};

export default function ElectionsPage() {
  const [elections, setElections] = useState<Election[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getElections().then(setElections).finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;

  return (
    <div className="space-y-4">
      <div className="grid gap-4">
        {elections.map(e => (
          <Card key={e.id} className="hover:shadow-md transition-shadow">
            <CardContent className="pt-4 pb-4">
              <div className="flex items-start justify-between">
                <div className="flex gap-4">
                  <div className="w-12 h-12 rounded-xl bg-green-100 flex items-center justify-center shrink-0">
                    <Landmark className="w-6 h-6 text-green-700" />
                  </div>
                  <div>
                    <h3 className="font-semibold text-zinc-900">{e.title}</h3>
                    <p className="text-sm text-zinc-500 mt-0.5">{e.description}</p>
                    <div className="flex items-center gap-4 mt-2 text-sm text-zinc-500">
                      <span className="flex items-center gap-1"><Calendar className="w-3.5 h-3.5" /> {e.election_date}</span>
                      <span className="flex items-center gap-1"><Users className="w-3.5 h-3.5" /> {formatNumber(e.total_registered_voters)} registered</span>
                      <Badge variant="outline" className="text-xs capitalize">{e.election_type.replace('_', ' ')}</Badge>
                    </div>
                  </div>
                </div>
                <Badge className={statusColors[e.status] || 'bg-zinc-100'}>{e.status}</Badge>
              </div>
            </CardContent>
          </Card>
        ))}
        {elections.length === 0 && (
          <Card><CardContent className="py-12 text-center text-zinc-500">No elections found</CardContent></Card>
        )}
      </div>
    </div>
  );
}
