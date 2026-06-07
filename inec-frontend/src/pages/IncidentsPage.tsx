import { useEffect, useState } from 'react';
import { logger } from '@/lib/utils';
import { api } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { AlertTriangle, Plus, Activity, Search, CheckCircle, XCircle } from 'lucide-react';

interface Incident {
  id: number; election_id: number; polling_unit_code: string; incident_type: string;
  description: string; severity: string; status: string; reported_at: string;
  reporter_name: string;
}

const severityColors: Record<string, string> = {
  low: 'bg-blue-100 text-blue-800', medium: 'bg-amber-100 text-amber-800',
  high: 'bg-orange-100 text-orange-800', critical: 'bg-red-100 text-red-800',
};
const statusColors: Record<string, string> = {
  reported: 'bg-amber-100 text-amber-800', investigating: 'bg-blue-100 text-blue-800',
  resolved: 'bg-green-100 text-green-800', dismissed: 'bg-zinc-100 text-zinc-600',
};

export default function IncidentsPage() {
  const [incidents, setIncidents]= useState<Incident[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState({ polling_unit_code: '', incident_type: 'equipment_malfunction', description: '', severity: 'medium' });
  const [search, setSearch] = useState('');
  const [filterStatus, setFilterStatus] = useState('all');
  const [filterSeverity, setFilterSeverity] = useState('all');

  useEffect(() => { loadIncidents(); }, []);

  async function loadIncidents() {
    setLoading(true);
    try { setIncidents(await api.getIncidents(1)); } catch (e) { logger.error(e); }
    finally { setLoading(false); }
  }

  async function handleCreate() {
    try {
      await api.createIncident({ election_id: 1, ...form });
      setShowCreate(false);
      setForm({ polling_unit_code: '', incident_type: 'equipment_malfunction', description: '', severity: 'medium' });
      loadIncidents();
    } catch (e) { logger.error(e); }
  }

  async function handleUpdateStatus(id: number, status: string) {
    try {
      await api.updateIncident(id, status);
      loadIncidents();
    } catch (e) { logger.error(e); }
  }

  const filtered = incidents.filter(inc => {
    if (filterStatus !== 'all' && inc.status !== filterStatus) return false;
    if (filterSeverity !== 'all' && inc.severity !== filterSeverity) return false;
    if (search) {
      const s = search.toLowerCase();
      return inc.description?.toLowerCase().includes(s) ||
        inc.incident_type?.toLowerCase().includes(s) ||
        inc.polling_unit_code?.toLowerCase().includes(s) ||
        inc.reporter_name?.toLowerCase().includes(s);
    }
    return true;
  });

  if (loading) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="relative">
            <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
            <Input placeholder="Search incidents..." value={search} onChange={e => setSearch(e.target.value)} className="pl-8 w-48" />
          </div>
          <Select value={filterStatus} onValueChange={setFilterStatus}>
            <SelectTrigger className="w-36"><SelectValue placeholder="All Status" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Status</SelectItem>
              <SelectItem value="reported">Reported</SelectItem>
              <SelectItem value="investigating">Investigating</SelectItem>
              <SelectItem value="resolved">Resolved</SelectItem>
              <SelectItem value="dismissed">Dismissed</SelectItem>
            </SelectContent>
          </Select>
          <Select value={filterSeverity} onValueChange={setFilterSeverity}>
            <SelectTrigger className="w-32"><SelectValue placeholder="All Severity" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Severity</SelectItem>
              <SelectItem value="low">Low</SelectItem>
              <SelectItem value="medium">Medium</SelectItem>
              <SelectItem value="high">High</SelectItem>
              <SelectItem value="critical">Critical</SelectItem>
            </SelectContent>
          </Select>
          <Badge variant="outline">{filtered.length} of {incidents.length}</Badge>
        </div>
        <Dialog open={showCreate} onOpenChange={setShowCreate}>
          <DialogTrigger asChild>
            <Button className="bg-red-600 hover:bg-red-700 gap-1"><Plus className="w-4 h-4" /> Report Incident</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>Report Incident</DialogTitle></DialogHeader>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Polling Unit Code (optional)</Label>
                <Input placeholder="e.g. LA-001-W001-PU001" value={form.polling_unit_code}
                  onChange={e => setForm(p => ({ ...p, polling_unit_code: e.target.value }))} />
              </div>
              <div className="space-y-2">
                <Label>Incident Type</Label>
                <Select value={form.incident_type} onValueChange={v => setForm(p => ({ ...p, incident_type: v }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="equipment_malfunction">Equipment Malfunction</SelectItem>
                    <SelectItem value="voter_intimidation">Voter Intimidation</SelectItem>
                    <SelectItem value="ballot_tampering">Ballot Tampering</SelectItem>
                    <SelectItem value="violence">Violence</SelectItem>
                    <SelectItem value="network_issue">Network Issue</SelectItem>
                    <SelectItem value="other">Other</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Severity</Label>
                <Select value={form.severity} onValueChange={v => setForm(p => ({ ...p, severity: v }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="low">Low</SelectItem>
                    <SelectItem value="medium">Medium</SelectItem>
                    <SelectItem value="high">High</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Description</Label>
                <Input placeholder="Describe the incident" value={form.description}
                  onChange={e => setForm(p => ({ ...p, description: e.target.value }))} />
              </div>
              <Button onClick={handleCreate} className="w-full bg-red-600 hover:bg-red-700">Submit Report</Button>
            </div>
          </DialogContent>
        </Dialog>
      </div>

      <div className="space-y-3">
        {filtered.map(inc => (
          <Card key={inc.id} className="hover:shadow-md transition-shadow">
            <CardContent className="pt-4 pb-4">
              <div className="flex items-start justify-between">
                <div className="flex gap-3">
                  <div className="w-10 h-10 rounded-lg bg-red-100 flex items-center justify-center shrink-0">
                    <AlertTriangle className="w-5 h-5 text-red-600" />
                  </div>
                  <div>
                    <p className="font-medium text-sm">{inc.incident_type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())}</p>
                    <p className="text-sm text-zinc-600 mt-0.5">{inc.description}</p>
                    <div className="flex items-center gap-3 mt-2 text-xs text-zinc-400">
                      {inc.polling_unit_code && <span>PU: {inc.polling_unit_code}</span>}
                      <span>Reported by: {inc.reporter_name}</span>
                      <span>{inc.reported_at}</span>
                    </div>
                    {inc.status !== 'resolved' && inc.status !== 'dismissed' && (
                      <div className="flex gap-2 mt-2">
                        {inc.status === 'reported' && (
                          <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={() => handleUpdateStatus(inc.id, 'investigating')}>
                            Investigate
                          </Button>
                        )}
                        <Button variant="outline" size="sm" className="h-7 text-xs text-green-700 gap-1" onClick={() => handleUpdateStatus(inc.id, 'resolved')}>
                          <CheckCircle className="w-3 h-3" /> Resolve
                        </Button>
                        <Button variant="outline" size="sm" className="h-7 text-xs text-zinc-500 gap-1" onClick={() => handleUpdateStatus(inc.id, 'dismissed')}>
                          <XCircle className="w-3 h-3" /> Dismiss
                        </Button>
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex gap-1.5 shrink-0">
                  <Badge className={severityColors[inc.severity]}>{inc.severity}</Badge>
                  <Badge className={statusColors[inc.status]}>{inc.status}</Badge>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
        {filtered.length === 0 && (
          <Card><CardContent className="py-12 text-center text-zinc-500">No incidents match your filters</CardContent></Card>
        )}
      </div>
    </div>
  );
}
