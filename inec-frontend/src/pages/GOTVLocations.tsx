import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

import {
  MapPin, Users, Zap, RefreshCw,
  BarChart3,
} from 'lucide-react';
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell,
} from 'recharts';

const PIE_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6'];

interface LocationCapacity {
  state: string;
  lga: string;
  ward: string;
  total: number;
  canvassers: number;
  drivers: number;
  callers: number;
  coordinators: number;
  observers: number;
}

interface Volunteer {
  volunteer_id: string;
  full_name: string;
  role: string;
  is_active: boolean;
  vetting_status: string;
  assigned_state: string;
  assigned_lga: string;
  assigned_ward: string;
}

export default function GOTVLocations() {
  const [locations, setLocations] = useState<LocationCapacity[]>([]);
  const [volunteers, setVolunteers] = useState<Volunteer[]>([]);
  const [loading, setLoading] = useState(true);
  const [stateFilter, setStateFilter] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [selectedVolunteer, setSelectedVolunteer] = useState<string | null>(null);
  const [assignState, setAssignState] = useState('');
  const [assignLGA, setAssignLGA] = useState('');
  const [assignWard, setAssignWard] = useState('');
  const [assignPU, setAssignPU] = useState('');

  const loadData = useCallback(async () => {
    try {
      setLoading(true);
      const [capData, volData] = await Promise.all([
        api.getGOTVLocationCapacity(stateFilter || undefined) as Promise<{ locations: LocationCapacity[] }>,
        api.getGOTVVolunteers() as Promise<{ volunteers: Volunteer[] }>,
      ]);
      setLocations(capData.locations || []);
      setVolunteers(volData.volunteers || []);
    } catch { /* empty */ }
    setLoading(false);
  }, [stateFilter]);

  useEffect(() => { loadData(); }, [loadData]);

  const handleAssignLocation = async () => {
    if (!selectedVolunteer || !assignState) return;
    setActionLoading(selectedVolunteer);
    try {
      await api.assignGOTVVolunteerLocation(selectedVolunteer, {
        state: assignState, lga: assignLGA, ward: assignWard, polling_unit: assignPU,
      });
      setSelectedVolunteer(null);
      setAssignState(''); setAssignLGA(''); setAssignWard(''); setAssignPU('');
      loadData();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleAutoAssign = async () => {
    setActionLoading('auto');
    try {
      const result = await api.autoAssignGOTVLocations() as { auto_assigned: number; total_unassigned: number };
      alert(`Auto-assigned ${result.auto_assigned} of ${result.total_unassigned} unassigned volunteers`);
      loadData();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  // Aggregate by state for chart
  const stateAgg = locations.reduce((acc, l) => {
    if (!acc[l.state]) acc[l.state] = { state: l.state, total: 0, canvassers: 0, drivers: 0, callers: 0 };
    acc[l.state].total += l.total;
    acc[l.state].canvassers += l.canvassers;
    acc[l.state].drivers += l.drivers;
    acc[l.state].callers += l.callers;
    return acc;
  }, {} as Record<string, { state: string; total: number; canvassers: number; drivers: number; callers: number }>);
  const chartData = Object.values(stateAgg).sort((a, b) => b.total - a.total).slice(0, 15);

  const totalVolunteers = locations.reduce((a, l) => a + l.total, 0);
  const unassignedCount = volunteers.filter(v => !v.assigned_state).length;
  const roleBreakdown = [
    { name: 'Canvassers', value: locations.reduce((a, l) => a + l.canvassers, 0) },
    { name: 'Drivers', value: locations.reduce((a, l) => a + l.drivers, 0) },
    { name: 'Callers', value: locations.reduce((a, l) => a + l.callers, 0) },
    { name: 'Coordinators', value: locations.reduce((a, l) => a + l.coordinators, 0) },
    { name: 'Observers', value: locations.reduce((a, l) => a + l.observers, 0) },
  ].filter(r => r.value > 0);

  return (
    <div className="space-y-6">
      {/* Summary */}
      <div className="grid grid-cols-4 gap-4">
        <Card>
          <CardContent className="pt-4 flex items-center gap-3">
            <MapPin className="h-8 w-8 text-blue-600" />
            <div>
              <div className="text-2xl font-bold">{locations.length}</div>
              <div className="text-sm text-muted-foreground">Active Locations</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 flex items-center gap-3">
            <Users className="h-8 w-8 text-green-600" />
            <div>
              <div className="text-2xl font-bold">{totalVolunteers}</div>
              <div className="text-sm text-muted-foreground">Assigned Volunteers</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 flex items-center gap-3">
            <Users className="h-8 w-8 text-yellow-600" />
            <div>
              <div className="text-2xl font-bold">{unassignedCount}</div>
              <div className="text-sm text-muted-foreground">Unassigned</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 flex items-center gap-3">
            <BarChart3 className="h-8 w-8 text-purple-600" />
            <div>
              <div className="text-2xl font-bold">{Object.keys(stateAgg).length}</div>
              <div className="text-sm text-muted-foreground">States Covered</div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between">
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={handleAutoAssign} disabled={actionLoading === 'auto'}>
            <Zap className="h-4 w-4 mr-1" /> Auto-Assign Unassigned
          </Button>
          <Input placeholder="Filter by state..." className="w-48" value={stateFilter}
            onChange={e => setStateFilter(e.target.value)} />
        </div>
        <Button size="sm" variant="outline" onClick={loadData}>
          <RefreshCw className="h-4 w-4 mr-1" /> Refresh
        </Button>
      </div>

      {/* Charts */}
      <div className="grid grid-cols-3 gap-6">
        <Card className="col-span-2">
          <CardHeader><CardTitle className="text-lg">Volunteers by State</CardTitle></CardHeader>
          <CardContent>
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height={280}>
                <BarChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="state" tick={{ fontSize: 11 }} />
                  <YAxis />
                  <Tooltip />
                  <Bar dataKey="canvassers" stackId="a" fill="#3b82f6" name="Canvassers" />
                  <Bar dataKey="drivers" stackId="a" fill="#10b981" name="Drivers" />
                  <Bar dataKey="callers" stackId="a" fill="#f59e0b" name="Callers" />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="text-center py-12 text-muted-foreground">No location data yet</div>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle className="text-lg">Role Distribution</CardTitle></CardHeader>
          <CardContent>
            {roleBreakdown.length > 0 ? (
              <ResponsiveContainer width="100%" height={280}>
                <PieChart>
                  <Pie data={roleBreakdown} dataKey="value" nameKey="name" cx="50%" cy="50%"
                    outerRadius={90} label={({ name, value }) => `${name}: ${value}`}>
                    {roleBreakdown.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <div className="text-center py-12 text-muted-foreground">No role data</div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Assign Location Panel */}
      {selectedVolunteer && (
        <Card className="border-2 border-primary">
          <CardHeader>
            <CardTitle className="text-lg">
              Assign Location — {volunteers.find(v => v.volunteer_id === selectedVolunteer)?.full_name}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid grid-cols-4 gap-4">
              <div>
                <label className="text-sm font-medium">State *</label>
                <Input className="mt-1" value={assignState} onChange={e => setAssignState(e.target.value)} placeholder="Lagos" />
              </div>
              <div>
                <label className="text-sm font-medium">LGA</label>
                <Input className="mt-1" value={assignLGA} onChange={e => setAssignLGA(e.target.value)} placeholder="Ikeja" />
              </div>
              <div>
                <label className="text-sm font-medium">Ward</label>
                <Input className="mt-1" value={assignWard} onChange={e => setAssignWard(e.target.value)} placeholder="LA-IK-W05" />
              </div>
              <div>
                <label className="text-sm font-medium">Polling Unit</label>
                <Input className="mt-1" value={assignPU} onChange={e => setAssignPU(e.target.value)} placeholder="LA-PU-0024" />
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setSelectedVolunteer(null)}>Cancel</Button>
              <Button onClick={handleAssignLocation} disabled={!assignState || actionLoading === selectedVolunteer}>
                Assign Location
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Location Table */}
      <Card>
        <CardHeader><CardTitle className="text-lg">Location Capacity</CardTitle></CardHeader>
        <CardContent>
          {loading ? (
            <div className="text-center py-8 text-muted-foreground">Loading...</div>
          ) : (
            <div className="rounded-md border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="p-3 text-left">State</th>
                    <th className="p-3 text-left">LGA</th>
                    <th className="p-3 text-left">Ward</th>
                    <th className="p-3 text-right">Total</th>
                    <th className="p-3 text-right">Canvassers</th>
                    <th className="p-3 text-right">Drivers</th>
                    <th className="p-3 text-right">Callers</th>
                    <th className="p-3 text-right">Coordinators</th>
                    <th className="p-3 text-right">Observers</th>
                  </tr>
                </thead>
                <tbody>
                  {locations.map((l, i) => (
                    <tr key={i} className="border-b hover:bg-muted/30">
                      <td className="p-3 font-medium">{l.state}</td>
                      <td className="p-3">{l.lga || '—'}</td>
                      <td className="p-3 font-mono text-xs">{l.ward || '—'}</td>
                      <td className="p-3 text-right font-bold">{l.total}</td>
                      <td className="p-3 text-right">{l.canvassers || '—'}</td>
                      <td className="p-3 text-right">{l.drivers || '—'}</td>
                      <td className="p-3 text-right">{l.callers || '—'}</td>
                      <td className="p-3 text-right">{l.coordinators || '—'}</td>
                      <td className="p-3 text-right">{l.observers || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {locations.length === 0 && (
                <div className="text-center py-8 text-muted-foreground">No location assignments yet</div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Unassigned Volunteers */}
      {unassignedCount > 0 && (
        <Card>
          <CardHeader><CardTitle className="text-lg">Unassigned Volunteers ({unassignedCount})</CardTitle></CardHeader>
          <CardContent>
            <div className="grid md:grid-cols-3 lg:grid-cols-4 gap-3">
              {volunteers.filter(v => !v.assigned_state).slice(0, 20).map(v => (
                <div key={v.volunteer_id}
                  className={`p-3 rounded-lg border cursor-pointer transition-all hover:shadow-md ${selectedVolunteer === v.volunteer_id ? 'ring-2 ring-primary bg-primary/5' : ''}`}
                  onClick={() => setSelectedVolunteer(v.volunteer_id)}>
                  <div className="font-medium text-sm">{v.full_name}</div>
                  <div className="flex items-center gap-1 mt-1">
                    <Badge variant="outline" className="text-xs">{v.role}</Badge>
                    <Badge className={`text-xs ${v.vetting_status === 'approved' ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'}`}>
                      {v.vetting_status}
                    </Badge>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
