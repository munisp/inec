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
import { AlertTriangle, Plus, Activity } from 'lucide-react';

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
export default function IncidentsPage() {
  const [incidents, setIncidents]= useState<Incident[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState({ polling_unit_code: '', incident_type: 'equipment_malfunction', description: '', severity: 'medium' });
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
      loadIncidents();
    } catch (e) { logger.error(e); }
  if (loading) return <div className="flex items-center justify-center h-64"><Activity className="w-6 h-6 animate-spin text-green-700" /></div>;
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Badge variant="outline">{incidents.length} incidents</Badge>
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
                <Label>Severity</Label>
                <Select value={form.severity} onValueChange={v => setForm(p => ({ ...p, severity: v }))}>
                    <SelectItem value="low">Low</SelectItem>
                    <SelectItem value="medium">Medium</SelectItem>
                    <SelectItem value="high">High</SelectItem>
                    <SelectItem value="critical">Critical</SelectItem>
                <Label>Description</Label>
                <Input placeholder="Describe the incident" value={form.description}
                  onChange={e => setForm(p => ({ ...p, description: e.target.value }))} />
              <Button onClick={handleCreate} className="w-full bg-red-600 hover:bg-red-700">Submit Report</Button>
            </div>
          </DialogContent>
        </Dialog>
      </div>
      <div className="space-y-3">
        {incidents.map(inc => (
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
                </div>
                <div className="flex gap-1.5">
                  <Badge className={severityColors[inc.severity]}>{inc.severity}</Badge>
                  <Badge className={statusColors[inc.status]}>{inc.status}</Badge>
            </CardContent>
          </Card>
        ))}
        {incidents.length === 0 && (
          <Card><CardContent className="py-12 text-center text-zinc-500">No incidents reported</CardContent></Card>
        )}
    </div>
  );
