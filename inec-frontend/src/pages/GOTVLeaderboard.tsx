import { useEffect, useState } from 'react';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Trophy, Medal, Star } from 'lucide-react';

interface LeaderboardEntry {
  volunteer_id: string;
  full_name: string;
  role: string;
  score: number;
  rank: number;
  badge: string;
  doors_knocked: number;
  calls_made: number;
  rides_given: number;
}

const BADGE_ICONS: Record<string, { icon: typeof Trophy; color: string; label: string }> = {
  champion: { icon: Trophy, color: 'text-yellow-500', label: 'Champion' },
  top_performer: { icon: Medal, color: 'text-blue-500', label: 'Top Performer' },
  all_star: { icon: Star, color: 'text-purple-500', label: 'All Star' },
};

export default function GOTVLeaderboard() {
  const [entries, setEntries] = useState<LeaderboardEntry[]>([]);
  const [period, setPeriod] = useState<'daily' | 'weekly' | 'monthly' | 'all'>('weekly');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    fetch(`/gotv/leaderboard?period=${period}&limit=50`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}`, 'X-Party-ID': localStorage.getItem('gotv_party_id') || '1' },
    })
      .then(r => r.json())
      .then(data => setEntries(data.entries || []))
      .catch(() => setEntries([]))
      .finally(() => setLoading(false));
  }, [period]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Trophy className="h-5 w-5 text-yellow-500" /> Volunteer Leaderboard
        </h2>
        <div className="flex gap-1">
          {(['daily', 'weekly', 'monthly', 'all'] as const).map(p => (
            <Button
              key={p}
              size="sm"
              variant={period === p ? 'default' : 'outline'}
              onClick={() => setPeriod(p)}
            >
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </Button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="text-center py-8 text-muted-foreground">Loading leaderboard...</div>
      ) : entries.length === 0 ? (
        <Card><CardContent className="py-8 text-center text-muted-foreground">No volunteer activity for this period</CardContent></Card>
      ) : (
        <div className="space-y-2">
          {entries.map(e => {
            const badgeInfo = BADGE_ICONS[e.badge];
            return (
              <Card key={e.volunteer_id} className={e.rank <= 3 ? 'border-yellow-200 bg-yellow-50/50' : ''}>
                <CardContent className="flex items-center gap-4 py-3">
                  <div className="text-2xl font-bold w-10 text-center text-muted-foreground">
                    {e.rank <= 3 ? (
                      <span className={e.rank === 1 ? 'text-yellow-500' : e.rank === 2 ? 'text-gray-400' : 'text-amber-600'}>
                        #{e.rank}
                      </span>
                    ) : (
                      `#${e.rank}`
                    )}
                  </div>
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{e.full_name}</span>
                      <Badge variant="secondary" className="text-xs">{e.role}</Badge>
                      {badgeInfo && (
                        <Badge className="bg-yellow-100 text-yellow-800 text-xs flex items-center gap-1">
                          <badgeInfo.icon className={`h-3 w-3 ${badgeInfo.color}`} />
                          {badgeInfo.label}
                        </Badge>
                      )}
                    </div>
                    <div className="flex gap-4 text-xs text-muted-foreground mt-1">
                      <span>🚪 {e.doors_knocked} doors</span>
                      <span>📞 {e.calls_made} calls</span>
                      <span>🚗 {e.rides_given} rides</span>
                    </div>
                  </div>
                  <div className="text-right">
                    <div className="text-xl font-bold">{e.score}</div>
                    <div className="text-xs text-muted-foreground">points</div>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}
