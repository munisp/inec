import { useEffect, useState, useRef } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  AlertTriangle, Activity, Users, Car, Megaphone, MapPin, TrendingUp,
  RefreshCw, Wifi, WifiOff,
} from 'lucide-react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts';

interface WarRoomData {
  timestamp: string;
  ops: {
    active_campaigns: number;
    active_volunteers: number;
    pending_rides: number;
    dispatches_last_hour: number;
    pledges_today: number;
  };
  alerts: Alert[];
  coverage: CoverageRegion[];
}

interface Alert {
  level: string;
  message: string;
  ward_code: string;
  metric: string;
}

interface CoverageRegion {
  state_code: string;
  volunteers: number;
  contacts: number;
  pledges: number;
  coverage_pct: number;
}

export default function GOTVWarRoom() {
  const [data, setData] = useState<WarRoomData | null>(null);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const eventSourceRef = useRef<EventSource | null>(null);
  const headers = { Authorization: `Bearer ${localStorage.getItem('auth_token')}`, 'X-Party-ID': localStorage.getItem('gotv_party_id') || '1' };

  const loadData = () => {
    setLoading(true);
    fetch('/gotv/warroom/summary', { headers })
      .then(r => r.json())
      .then(d => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  };

  useEffect(() => {
    loadData();
    // SSE connection for real-time updates
    try {
      const es = new EventSource(`/gotv/warroom/stream?party_id=${localStorage.getItem('gotv_party_id') || '1'}`);
      es.onopen = () => setConnected(true);
      es.onmessage = (e) => {
        try {
          const update = JSON.parse(e.data);
          setData(prev => prev ? { ...prev, ...update } : update);
        } catch { /* ignore parse errors */ }
      };
      es.onerror = () => setConnected(false);
      eventSourceRef.current = es;
    } catch { /* SSE not supported */ }

    return () => { eventSourceRef.current?.close(); };
  }, []);

  if (loading) return <div className="text-center py-12 text-muted-foreground">Loading War Room...</div>;

  const ops = data?.ops || { active_campaigns: 0, active_volunteers: 0, pending_rides: 0, dispatches_last_hour: 0, pledges_today: 0 };
  const alerts = data?.alerts || [];
  const coverage = data?.coverage || [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Activity className="h-5 w-5 text-red-500" /> Election Day War Room
        </h2>
        <div className="flex items-center gap-2">
          <Badge className={connected ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}>
            {connected ? <><Wifi className="h-3 w-3 mr-1" /> Live</> : <><WifiOff className="h-3 w-3 mr-1" /> Offline</>}
          </Badge>
          <Button size="sm" variant="outline" onClick={loadData}>
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-5 gap-3">
        {[
          { label: 'Active Campaigns', value: ops.active_campaigns, icon: Megaphone, color: 'text-blue-500' },
          { label: 'Volunteers Online', value: ops.active_volunteers, icon: Users, color: 'text-green-500' },
          { label: 'Pending Rides', value: ops.pending_rides, icon: Car, color: 'text-orange-500' },
          { label: 'Dispatches/hr', value: ops.dispatches_last_hour, icon: TrendingUp, color: 'text-purple-500' },
          { label: 'Pledges Today', value: ops.pledges_today, icon: MapPin, color: 'text-emerald-500' },
        ].map(kpi => (
          <Card key={kpi.label}>
            <CardContent className="pt-4 pb-3">
              <div className="flex items-center justify-between">
                <kpi.icon className={`h-5 w-5 ${kpi.color}`} />
                <span className="text-2xl font-bold">{kpi.value}</span>
              </div>
              <div className="text-xs text-muted-foreground mt-1">{kpi.label}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Alerts */}
      {alerts.length > 0 && (
        <Card className="border-red-200">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-red-500" /> Active Alerts ({alerts.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {alerts.map((a, i) => (
                <div key={i} className={`flex items-center gap-3 p-2 rounded text-sm ${
                  a.level === 'critical' ? 'bg-red-50 text-red-800' :
                  a.level === 'warning' ? 'bg-yellow-50 text-yellow-800' : 'bg-blue-50 text-blue-800'
                }`}>
                  <AlertTriangle className="h-4 w-4 shrink-0" />
                  <span className="flex-1">{a.message}</span>
                  <Badge variant="secondary" className="text-xs">{a.ward_code}</Badge>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Coverage by State */}
      {coverage.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">State Coverage</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={coverage.slice(0, 15)}>
                <XAxis dataKey="state_code" tick={{ fontSize: 10 }} />
                <YAxis />
                <Tooltip />
                <Bar dataKey="volunteers" fill="#3b82f6" name="Volunteers" />
                <Bar dataKey="pledges" fill="#10b981" name="Pledges" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
