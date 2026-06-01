import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Landmark, Calendar, Users, Activity, Plus, ArrowLeft, BarChart3, Edit2 } from 'lucide-react';

interface Election {
  id: number; title: string; election_type: string; election_date: string;
  status: string; description: string; total_registered_voters: number;
}

interface ElectionStats {
  total_results: number; total_valid_votes: number; total_rejected_votes: number;
  total_votes_cast: number; total_accredited: number; turnout_percent: number;
  completion_percent: number; party_totals: { party_code: string; party_name: string; total_votes: number }[];
}

function formatNumber(n: number) { return new Intl.NumberFormat().format(n); }

const statusColors: Record<string, string> = {
  upcoming: 'bg-blue-100 text-blue-800',
  active: 'bg-green-100 text-green-800',
  completed: 'bg-zinc-100 text-zinc-600',
  cancelled: 'bg-red-100 text-red-800',
};

type View = 'list' | 'create' | 'detail' | 'edit';

export default function ElectionsPage() {
  const [elections, setElections] = useState<Election[]>([]);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<View>('list');
  const [selected, setSelected] = useState<Election | null>(null);
  const [stats, setStats] = useState<ElectionStats | null>(null);
  const [filterStatus, setFilterStatus] = useState('');
  const [form, setForm] = useState({ title: '', description: '', election_type: 'presidential', election_date: '', status: 'upcoming' });
  const [saving, setSaving] = useState(false);

  const loadElections = () => {
    setLoading(true);
    api.getElections(filterStatus || undefined).then(setElections).finally(() => setLoading(false));
  };

  useEffect(() => { loadElections(); }, [filterStatus]);

  const openDetail = (e: Election) => {
    setSelected(e);
    setView('detail');
    api.getElectionStats(e.id).then(setStats).catch(() => setStats(null));
  };

  const openEdit = (e: Election) => {
    setSelected(e);
    setForm({ title: e.title, description: e.description, election_type: e.election_type, election_date: e.election_date, status: e.status });
    setView('edit');
  };

  const handleCreate = async () => {
    setSaving(true);
    try {
      await api.createElection(form);
      setView('list');
      loadElections();
    } catch { /* handled by api */ }
    setSaving(false);
  };

  const handleUpdate = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await api.updateElection(selected.id, form);
      setView('list');
      loadElections();
    } catch { /* handled by api */ }
    setSaving(false);
  };

  if (view === 'create' || view === 'edit') {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" onClick={() => setView('list')}>
          <ArrowLeft className="w-4 h-4 mr-1" /> Back to Elections
        </Button>
        <Card>
          <CardContent className="pt-6 space-y-4">
            <h2 className="text-lg font-semibold">{view === 'create' ? 'Create New Election' : 'Edit Election'}</h2>
            <div className="grid gap-3">
              <label className="text-sm font-medium">Title</label>
              <Input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="e.g., 2027 Presidential Election" />
              <label className="text-sm font-medium">Description</label>
              <Input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} placeholder="Brief description of this election" />
              <label className="text-sm font-medium">Type</label>
              <select className="border rounded p-2 text-sm" value={form.election_type} onChange={e => setForm({ ...form, election_type: e.target.value })}>
                <option value="presidential">Presidential</option>
                <option value="gubernatorial">Gubernatorial</option>
                <option value="senatorial">Senatorial</option>
                <option value="house_of_reps">House of Representatives</option>
                <option value="state_assembly">State Assembly</option>
                <option value="local_government">Local Government</option>
              </select>
              <label className="text-sm font-medium">Date</label>
              <Input type="date" value={form.election_date} onChange={e => setForm({ ...form, election_date: e.target.value })} />
              {view === 'edit' && (
                <>
                  <label className="text-sm font-medium">Status</label>
                  <select className="border rounded p-2 text-sm" value={form.status} onChange={e => setForm({ ...form, status: e.target.value })}>
                    <option value="upcoming">Upcoming</option>
                    <option value="active">Active</option>
                    <option value="completed">Completed</option>
                    <option value="cancelled">Cancelled</option>
                  </select>
                </>
              )}
            </div>
            <Button onClick={view === 'create' ? handleCreate : handleUpdate} disabled={saving || !form.title || !form.election_date}>
              {saving ? 'Saving...' : view === 'create' ? 'Create Election' : 'Save Changes'}
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (view === 'detail' && selected) {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <Button variant="ghost" size="sm" onClick={() => setView('list')}>
            <ArrowLeft className="w-4 h-4 mr-1" /> Back
          </Button>
          <Button variant="outline" size="sm" onClick={() => openEdit(selected)}>
            <Edit2 className="w-4 h-4 mr-1" /> Edit
          </Button>
        </div>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-4 mb-4">
              <div className="w-14 h-14 rounded-xl bg-green-100 flex items-center justify-center">
                <Landmark className="w-7 h-7 text-green-700" />
              </div>
              <div>
                <h2 className="text-xl font-bold">{selected.title}</h2>
                <p className="text-sm text-zinc-500">{selected.description}</p>
                <div className="flex items-center gap-3 mt-1">
                  <Badge className={statusColors[selected.status] || 'bg-zinc-100'}>{selected.status}</Badge>
                  <span className="text-xs text-zinc-400">{selected.election_date}</span>
                  <span className="text-xs text-zinc-400">{formatNumber(selected.total_registered_voters)} registered</span>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {stats && (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <Card><CardContent className="pt-4 text-center">
              <p className="text-2xl font-bold text-green-700">{formatNumber(stats.total_votes_cast)}</p>
              <p className="text-xs text-zinc-500">Total Votes Cast</p>
            </CardContent></Card>
            <Card><CardContent className="pt-4 text-center">
              <p className="text-2xl font-bold text-blue-700">{stats.turnout_percent?.toFixed(1)}%</p>
              <p className="text-xs text-zinc-500">Turnout</p>
            </CardContent></Card>
            <Card><CardContent className="pt-4 text-center">
              <p className="text-2xl font-bold text-purple-700">{stats.completion_percent?.toFixed(1)}%</p>
              <p className="text-xs text-zinc-500">Results Submitted</p>
            </CardContent></Card>
            <Card><CardContent className="pt-4 text-center">
              <p className="text-2xl font-bold text-red-600">{formatNumber(stats.total_rejected_votes)}</p>
              <p className="text-xs text-zinc-500">Rejected Votes</p>
            </CardContent></Card>
          </div>
        )}

        {stats?.party_totals && stats.party_totals.length > 0 && (
          <Card>
            <CardContent className="pt-4">
              <h3 className="font-semibold mb-3 flex items-center gap-2"><BarChart3 className="w-4 h-4" /> Party Results</h3>
              <div className="space-y-2">
                {stats.party_totals.map((pt) => {
                  const pct = stats.total_valid_votes > 0 ? (pt.total_votes / stats.total_valid_votes) * 100 : 0;
                  return (
                    <div key={pt.party_code} className="flex items-center gap-3">
                      <span className="w-12 text-xs font-mono font-bold">{pt.party_code}</span>
                      <div className="flex-1 bg-zinc-100 rounded-full h-5 overflow-hidden">
                        <div className="bg-green-600 h-full rounded-full transition-all" style={{ width: `${pct}%` }} />
                      </div>
                      <span className="text-sm font-medium w-24 text-right">{formatNumber(pt.total_votes)} ({pct.toFixed(1)}%)</span>
                    </div>
                  );
                })}
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    );
  }

  // List view
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <select className="border rounded px-2 py-1 text-sm" value={filterStatus} onChange={e => setFilterStatus(e.target.value)}>
            <option value="">All Status</option>
            <option value="upcoming">Upcoming</option>
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="cancelled">Cancelled</option>
          </select>
          <Badge variant="outline">{elections.length} elections</Badge>
        </div>
        <Button size="sm" onClick={() => { setForm({ title: '', description: '', election_type: 'presidential', election_date: '', status: 'upcoming' }); setView('create'); }}>
          <Plus className="w-4 h-4 mr-1" /> New Election
        </Button>
      </div>

      {loading && <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>}

      {!loading && (
        <div className="grid gap-4">
          {elections.map(e => (
            <Card key={e.id} className="hover:shadow-md transition-shadow cursor-pointer" onClick={() => openDetail(e)}>
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
      )}
    </div>
  );
}
