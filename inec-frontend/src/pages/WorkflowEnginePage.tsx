import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { GitBranch, ChevronRight, Play, CheckCircle2, Clock, AlertCircle } from 'lucide-react';

const PHASES = ['planning','registration','accreditation','voting','collation','declaration','certification'];

const phaseIcon = (status: string) => {
  switch(status) {
    case 'completed': return <CheckCircle2 className="w-4 h-4 text-green-600" />;
    case 'in_progress': return <Play className="w-4 h-4 text-blue-600" />;
    case 'failed': return <AlertCircle className="w-4 h-4 text-red-600" />;
    default: return <Clock className="w-4 h-4 text-zinc-400" />;
  }
};

const phaseColor = (status: string) => {
  switch(status) {
    case 'completed': return 'bg-green-100 text-green-800 border-green-200';
    case 'in_progress': return 'bg-blue-100 text-blue-800 border-blue-200';
    case 'failed': return 'bg-red-100 text-red-800 border-red-200';
    default: return 'bg-zinc-50 text-zinc-500 border-zinc-200';
  }
};

export default function WorkflowEnginePage() {
  const [workflows, setWorkflows] = useState<any[]>([]);
  const [selected, setSelected] = useState<any>(null);
  const [advancing, setAdvancing] = useState(false);

  useEffect(() => { loadWorkflows(); }, []);

  const loadWorkflows = async () => {
    try { const d = await api.getEMSWorkflows(); setWorkflows(d || []); } catch {}
  };

  const loadWorkflow = async (id: number) => {
    try { const d = await api.getEMSWorkflow(id); setSelected(d); } catch {}
  };

  const advanceWorkflow = async (id: number) => {
    setAdvancing(true);
    try {
      await api.advanceEMSWorkflow(id);
      await loadWorkflow(id);
      await loadWorkflows();
    } catch {}
    setAdvancing(false);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-zinc-900">Workflow Engine</h1>
        <p className="text-sm text-zinc-500">End-to-end election pipeline: Registration → Accreditation → Voting → Collation → Declaration → Certification</p>
      </div>

      <div className="grid lg:grid-cols-3 gap-6">
        <div className="lg:col-span-1 space-y-3">
          <h3 className="text-sm font-medium text-zinc-700">Active Workflows</h3>
          {workflows.map((wf: any) => (
            <Card key={wf.id} className={`cursor-pointer transition-colors ${selected?.id === wf.id ? 'ring-2 ring-green-500' : 'hover:bg-zinc-50'}`}
              onClick={() => loadWorkflow(wf.id)}>
              <CardContent className="p-4">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <GitBranch className="w-4 h-4 text-green-600" />
                    <span className="font-medium text-sm">Workflow #{wf.id}</span>
                  </div>
                  <Badge className={wf.status === 'active' ? 'bg-green-100 text-green-800' : wf.status === 'completed' ? 'bg-blue-100 text-blue-800' : 'bg-zinc-100 text-zinc-800'}>
                    {wf.status}
                  </Badge>
                </div>
                <p className="text-xs text-zinc-500">{wf.election_title}</p>
                <div className="flex items-center gap-1 mt-2">
                  <span className="text-xs text-zinc-400">Phase:</span>
                  <Badge variant="outline" className="text-xs capitalize">{wf.current_phase}</Badge>
                </div>
              </CardContent>
            </Card>
          ))}
          {workflows.length === 0 && <p className="text-sm text-zinc-400">No workflows found</p>}
        </div>

        <div className="lg:col-span-2">
          {selected ? (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-sm">Workflow #{selected.id} — {selected.election_title}</CardTitle>
                  {selected.status === 'active' && (
                    <Button size="sm" onClick={() => advanceWorkflow(selected.id)} disabled={advancing}>
                      <ChevronRight className="w-4 h-4 mr-1" /> {advancing ? 'Advancing...' : 'Advance Phase'}
                    </Button>
                  )}
                </div>
              </CardHeader>
              <CardContent>
                <div className="mb-6">
                  <div className="flex items-center gap-1 mb-4">
                    {PHASES.map((ph, i) => {
                      const phaseData = (selected.phases || []).find((p: any) => p.phase === ph);
                      const status = phaseData?.status || 'pending';
                      const isCurrent = selected.current_phase === ph;
                      return (
                        <div key={ph} className="flex items-center flex-1">
                          <div className={`flex-1 h-2 rounded-full ${status === 'completed' ? 'bg-green-500' : status === 'in_progress' ? 'bg-blue-500' : 'bg-zinc-200'} ${isCurrent ? 'ring-2 ring-blue-300' : ''}`} />
                          {i < PHASES.length - 1 && <ChevronRight className="w-3 h-3 text-zinc-300 mx-0.5 shrink-0" />}
                        </div>
                      );
                    })}
                  </div>
                  <div className="flex justify-between text-xs text-zinc-500">
                    {PHASES.map(ph => <span key={ph} className="capitalize">{ph.slice(0,4)}</span>)}
                  </div>
                </div>

                <div className="space-y-3">
                  {(selected.phases || []).map((ph: any) => (
                    <div key={ph.phase} className={`flex items-center gap-3 p-3 rounded-lg border ${phaseColor(ph.status)}`}>
                      {phaseIcon(ph.status)}
                      <div className="flex-1">
                        <span className="font-medium text-sm capitalize">{ph.phase}</span>
                        {ph.started_at && <span className="text-xs ml-2 opacity-70">Started: {new Date(ph.started_at).toLocaleDateString()}</span>}
                        {ph.completed_at && <span className="text-xs ml-2 opacity-70">Done: {new Date(ph.completed_at).toLocaleDateString()}</span>}
                      </div>
                      <Badge variant="outline" className="text-xs capitalize">{ph.status}</Badge>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : (
            <div className="flex items-center justify-center h-64 text-zinc-400">
              <p>Select a workflow to view details</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
