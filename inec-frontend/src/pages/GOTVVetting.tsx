import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  ShieldCheck, UserCheck, GraduationCap, XCircle, AlertTriangle,
  ChevronRight, RefreshCw,
} from 'lucide-react';

const VETTING_STATUS_COLORS: Record<string, string> = {
  pending: 'bg-yellow-100 text-yellow-800',
  nin_verified: 'bg-blue-100 text-blue-800',
  nin_failed: 'bg-red-100 text-red-800',
  trained: 'bg-indigo-100 text-indigo-800',
  approved: 'bg-green-100 text-green-800',
  rejected: 'bg-red-100 text-red-800',
  suspended: 'bg-orange-100 text-orange-800',
};

const PIPELINE_STEPS = [
  { key: 'pending', label: 'Pending', icon: AlertTriangle, color: 'text-yellow-600' },
  { key: 'nin_verified', label: 'NIN Verified', icon: ShieldCheck, color: 'text-blue-600' },
  { key: 'trained', label: 'Trained', icon: GraduationCap, color: 'text-indigo-600' },
  { key: 'approved', label: 'Approved', icon: UserCheck, color: 'text-green-600' },
];

interface Volunteer {
  volunteer_id: string;
  full_name: string;
  phone: string;
  role: string;
  vetting_status: string;
  nin_verified: boolean;
  training_completed: boolean;
  background_cleared: boolean;
  assigned_state: string;
  assigned_lga: string;
  created_at: string;
}

interface VettingCounts {
  pending: number;
  nin_verified: number;
  nin_failed: number;
  trained: number;
  approved: number;
  rejected: number;
  suspended: number;
}

export default function GOTVVetting() {
  const [volunteers, setVolunteers] = useState<Volunteer[]>([]);
  const [counts, setCounts] = useState<VettingCounts | null>(null);
  const [statusFilter, setStatusFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [ninInput, setNinInput] = useState<Record<string, string>>({});
  const [rejectReason, setRejectReason] = useState('');
  const [showRejectModal, setShowRejectModal] = useState<string | null>(null);

  const loadPipeline = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.getGOTVVettingPipeline(statusFilter || undefined) as {
        volunteers: Volunteer[];
        counts: VettingCounts;
      };
      setVolunteers(data.volunteers || []);
      setCounts(data.counts || null);
    } catch { /* empty */ }
    setLoading(false);
  }, [statusFilter]);

  useEffect(() => { loadPipeline(); }, [loadPipeline]);

  const handleVerifyNIN = async (id: string) => {
    const nin = ninInput[id];
    if (!nin) return;
    setActionLoading(id);
    try {
      await api.verifyGOTVVolunteerNIN(id, nin, 'pass');
      loadPipeline();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleCompleteTraining = async (id: string) => {
    setActionLoading(id);
    try {
      await api.completeGOTVVolunteerTraining(id);
      loadPipeline();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleApprove = async (id: string) => {
    setActionLoading(id);
    try {
      await api.approveGOTVVolunteer(id);
      loadPipeline();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleReject = async (id: string) => {
    setActionLoading(id);
    try {
      await api.rejectGOTVVolunteer(id, rejectReason);
      setShowRejectModal(null);
      setRejectReason('');
      loadPipeline();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleSuspend = async (id: string) => {
    setActionLoading(id);
    try {
      await api.suspendGOTVVolunteer(id, 'Suspended by coordinator');
      loadPipeline();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const getNextAction = (v: Volunteer) => {
    switch (v.vetting_status) {
      case 'pending':
        return (
          <div className="flex items-center gap-2">
            <Input
              placeholder="NIN number"
              className="w-36 h-8 text-xs"
              value={ninInput[v.volunteer_id] || ''}
              onChange={e => setNinInput(prev => ({ ...prev, [v.volunteer_id]: e.target.value }))}
            />
            <Button size="sm" variant="default" className="h-8"
              onClick={() => handleVerifyNIN(v.volunteer_id)}
              disabled={actionLoading === v.volunteer_id || !ninInput[v.volunteer_id]}>
              <ShieldCheck className="h-3 w-3 mr-1" /> Verify NIN
            </Button>
          </div>
        );
      case 'nin_verified':
        return (
          <div className="flex gap-2">
            <Button size="sm" className="h-8" onClick={() => handleCompleteTraining(v.volunteer_id)}
              disabled={actionLoading === v.volunteer_id}>
              <GraduationCap className="h-3 w-3 mr-1" /> Mark Trained
            </Button>
            <Button size="sm" className="h-8" variant="default" onClick={() => handleApprove(v.volunteer_id)}
              disabled={actionLoading === v.volunteer_id}>
              <UserCheck className="h-3 w-3 mr-1" /> Approve
            </Button>
          </div>
        );
      case 'trained':
        return (
          <Button size="sm" className="h-8 bg-green-600 hover:bg-green-700" onClick={() => handleApprove(v.volunteer_id)}
            disabled={actionLoading === v.volunteer_id}>
            <UserCheck className="h-3 w-3 mr-1" /> Approve
          </Button>
        );
      case 'approved':
        return (
          <Button size="sm" variant="destructive" className="h-8" onClick={() => handleSuspend(v.volunteer_id)}
            disabled={actionLoading === v.volunteer_id}>
            <XCircle className="h-3 w-3 mr-1" /> Suspend
          </Button>
        );
      default:
        return <Badge className={VETTING_STATUS_COLORS[v.vetting_status]}>{v.vetting_status}</Badge>;
    }
  };

  return (
    <div className="space-y-6">
      {/* Pipeline Summary */}
      <div className="grid grid-cols-4 gap-4">
        {PIPELINE_STEPS.map(step => (
          <Card key={step.key} className={`cursor-pointer transition-all ${statusFilter === step.key ? 'ring-2 ring-primary' : ''}`}
            onClick={() => setStatusFilter(statusFilter === step.key ? '' : step.key)}>
            <CardContent className="pt-4 flex items-center gap-3">
              <step.icon className={`h-8 w-8 ${step.color}`} />
              <div>
                <div className="text-2xl font-bold">{counts ? (counts as Record<string, number>)[step.key] || 0 : '—'}</div>
                <div className="text-sm text-muted-foreground">{step.label}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Rejected / Suspended counts */}
      {counts && (counts.rejected > 0 || counts.suspended > 0 || counts.nin_failed > 0) && (
        <div className="flex gap-4">
          {counts.nin_failed > 0 && (
            <Badge className="bg-red-100 text-red-800 cursor-pointer" onClick={() => setStatusFilter('nin_failed')}>
              NIN Failed: {counts.nin_failed}
            </Badge>
          )}
          {counts.rejected > 0 && (
            <Badge className="bg-red-100 text-red-800 cursor-pointer" onClick={() => setStatusFilter('rejected')}>
              Rejected: {counts.rejected}
            </Badge>
          )}
          {counts.suspended > 0 && (
            <Badge className="bg-orange-100 text-orange-800 cursor-pointer" onClick={() => setStatusFilter('suspended')}>
              Suspended: {counts.suspended}
            </Badge>
          )}
        </div>
      )}

      {/* Vetting Pipeline Progress */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-lg">Volunteer Vetting Pipeline</CardTitle>
          <Button size="sm" variant="outline" onClick={loadPipeline}>
            <RefreshCw className="h-4 w-4 mr-1" /> Refresh
          </Button>
        </CardHeader>
        <CardContent>
          {/* Progress bar */}
          {counts && (
            <div className="flex h-3 rounded-full overflow-hidden mb-6 bg-gray-100">
              {counts.approved > 0 && <div className="bg-green-500 transition-all" style={{ width: `${(counts.approved / (Object.values(counts).reduce((a, b) => a + b, 0) || 1)) * 100}%` }} />}
              {counts.trained > 0 && <div className="bg-indigo-500 transition-all" style={{ width: `${(counts.trained / (Object.values(counts).reduce((a, b) => a + b, 0) || 1)) * 100}%` }} />}
              {counts.nin_verified > 0 && <div className="bg-blue-500 transition-all" style={{ width: `${(counts.nin_verified / (Object.values(counts).reduce((a, b) => a + b, 0) || 1)) * 100}%` }} />}
              {counts.pending > 0 && <div className="bg-yellow-400 transition-all" style={{ width: `${(counts.pending / (Object.values(counts).reduce((a, b) => a + b, 0) || 1)) * 100}%` }} />}
            </div>
          )}

          {loading ? (
            <div className="text-center py-8 text-muted-foreground">Loading vetting data...</div>
          ) : (
            <div className="rounded-md border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="p-3 text-left">Name</th>
                    <th className="p-3 text-left">Phone</th>
                    <th className="p-3 text-left">Role</th>
                    <th className="p-3 text-left">Status</th>
                    <th className="p-3 text-left">NIN</th>
                    <th className="p-3 text-left">Training</th>
                    <th className="p-3 text-left">Location</th>
                    <th className="p-3 text-left">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {volunteers.map(v => (
                    <tr key={v.volunteer_id} className="border-b hover:bg-muted/30">
                      <td className="p-3 font-medium">{v.full_name}</td>
                      <td className="p-3 font-mono text-xs">{v.phone}</td>
                      <td className="p-3"><Badge variant="outline">{v.role}</Badge></td>
                      <td className="p-3">
                        <Badge className={VETTING_STATUS_COLORS[v.vetting_status] || ''}>
                          {v.vetting_status}
                        </Badge>
                      </td>
                      <td className="p-3">{v.nin_verified ? '✓' : '—'}</td>
                      <td className="p-3">{v.training_completed ? '✓' : '—'}</td>
                      <td className="p-3 text-xs">{v.assigned_state || '—'}{v.assigned_lga ? ` / ${v.assigned_lga}` : ''}</td>
                      <td className="p-3">{getNextAction(v)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {volunteers.length === 0 && (
                <div className="text-center py-8 text-muted-foreground">
                  No volunteers in {statusFilter || 'pipeline'}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Reject Modal */}
      {showRejectModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <Card className="w-96">
            <CardHeader><CardTitle>Reject Volunteer</CardTitle></CardHeader>
            <CardContent className="space-y-4">
              <Input placeholder="Reason for rejection" value={rejectReason}
                onChange={e => setRejectReason(e.target.value)} />
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => setShowRejectModal(null)}>Cancel</Button>
                <Button variant="destructive" onClick={() => handleReject(showRejectModal)}>Reject</Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
