import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import {
  ClipboardList, Play, CheckCircle, XCircle, Zap, Plus, RefreshCw,
} from 'lucide-react';

const TASK_STATUS_COLORS: Record<string, string> = {
  unassigned: 'bg-gray-100 text-gray-800',
  assigned: 'bg-blue-100 text-blue-800',
  in_progress: 'bg-yellow-100 text-yellow-800',
  completed: 'bg-green-100 text-green-800',
  cancelled: 'bg-red-100 text-red-800',
  blocked: 'bg-orange-100 text-orange-800',
};

const TASK_TYPE_ICONS: Record<string, string> = {
  door_knock: '🚪',
  phone_call: '📞',
  ride_duty: '🚗',
  event_setup: '🎪',
  data_collection: '📋',
  voter_registration: '📝',
  materials_distribution: '📦',
  monitoring: '👁️',
};

const PRIORITY_COLORS: Record<number, string> = {
  1: 'bg-gray-100 text-gray-600',
  2: 'bg-blue-100 text-blue-600',
  3: 'bg-yellow-100 text-yellow-600',
  4: 'bg-orange-100 text-orange-600',
  5: 'bg-red-100 text-red-600',
};

interface Task {
  task_id: string;
  task_type: string;
  title: string;
  description: string;
  status: string;
  volunteer_id: string;
  volunteer_name: string;
  ward_code: string;
  state_code: string;
  lga_code: string;
  target_count: number;
  completed_count: number;
  priority: number;
  due_date?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
}

export default function GOTVTasks() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState('');
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  // Create form
  const [newTaskType, setNewTaskType] = useState('door_knock');
  const [newTitle, setNewTitle] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [newState, setNewState] = useState('');
  const [newWard, setNewWard] = useState('');
  const [newTarget, setNewTarget] = useState(50);
  const [newPriority, setNewPriority] = useState(3);
  const [newDueDate, setNewDueDate] = useState('');

  const loadTasks = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.getGOTVTasks(statusFilter || undefined) as { tasks: Task[] };
      setTasks(data.tasks || []);
    } catch { /* empty */ }
    setLoading(false);
  }, [statusFilter]);

  useEffect(() => { loadTasks(); }, [loadTasks]);

  const handleCreateTask = async () => {
    if (!newTitle) return;
    setActionLoading('create');
    try {
      await api.createGOTVTask({
        task_type: newTaskType, title: newTitle, description: newDesc,
        state_code: newState, ward_code: newWard, target_count: newTarget,
        priority: newPriority, due_date: newDueDate,
      });
      setShowCreateForm(false);
      setNewTitle(''); setNewDesc('');
      loadTasks();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleStartTask = async (id: string) => {
    setActionLoading(id);
    try {
      await api.updateGOTVTaskStatus(id, 'in_progress');
      loadTasks();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleCompleteTask = async (id: string, target: number) => {
    setActionLoading(id);
    try {
      await api.updateGOTVTaskStatus(id, 'completed', target);
      loadTasks();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const handleAutoAssign = async () => {
    setActionLoading('auto');
    try {
      const result = await api.autoAssignGOTVTasks() as { auto_assigned: number };
      alert(`Auto-assigned ${result.auto_assigned} tasks`);
      loadTasks();
    } catch { /* empty */ }
    setActionLoading(null);
  };

  const statusCounts = tasks.reduce((acc, t) => {
    acc[t.status] = (acc[t.status] || 0) + 1;
    return acc;
  }, {} as Record<string, number>);

  return (
    <div className="space-y-6">
      {/* Summary Cards */}
      <div className="grid grid-cols-5 gap-4">
        {[
          { key: '', label: 'All Tasks', count: tasks.length, icon: ClipboardList, color: 'text-gray-600' },
          { key: 'unassigned', label: 'Unassigned', count: statusCounts['unassigned'] || 0, icon: ClipboardList, color: 'text-gray-500' },
          { key: 'assigned', label: 'Assigned', count: statusCounts['assigned'] || 0, icon: ClipboardList, color: 'text-blue-600' },
          { key: 'in_progress', label: 'In Progress', count: statusCounts['in_progress'] || 0, icon: Play, color: 'text-yellow-600' },
          { key: 'completed', label: 'Completed', count: statusCounts['completed'] || 0, icon: CheckCircle, color: 'text-green-600' },
        ].map(s => (
          <Card key={s.key} className={`cursor-pointer transition-all ${statusFilter === s.key ? 'ring-2 ring-primary' : ''}`}
            onClick={() => setStatusFilter(s.key)}>
            <CardContent className="pt-4 flex items-center gap-3">
              <s.icon className={`h-6 w-6 ${s.color}`} />
              <div>
                <div className="text-xl font-bold">{s.count}</div>
                <div className="text-xs text-muted-foreground">{s.label}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Actions Bar */}
      <div className="flex items-center justify-between">
        <div className="flex gap-2">
          <Button size="sm" onClick={() => setShowCreateForm(true)}>
            <Plus className="h-4 w-4 mr-1" /> Create Task
          </Button>
          <Button size="sm" variant="outline" onClick={handleAutoAssign} disabled={actionLoading === 'auto'}>
            <Zap className="h-4 w-4 mr-1" /> Auto-Assign All
          </Button>
        </div>
        <Button size="sm" variant="outline" onClick={loadTasks}>
          <RefreshCw className="h-4 w-4 mr-1" /> Refresh
        </Button>
      </div>

      {/* Create Task Form */}
      {showCreateForm && (
        <Card>
          <CardHeader><CardTitle className="text-lg">Create New Task</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium">Task Type</label>
                <select className="w-full border rounded-md p-2 text-sm mt-1" value={newTaskType}
                  onChange={e => setNewTaskType(e.target.value)}>
                  <option value="door_knock">🚪 Door Knock</option>
                  <option value="phone_call">📞 Phone Call</option>
                  <option value="ride_duty">🚗 Ride Duty</option>
                  <option value="event_setup">🎪 Event Setup</option>
                  <option value="data_collection">📋 Data Collection</option>
                  <option value="voter_registration">📝 Voter Registration</option>
                  <option value="materials_distribution">📦 Materials Distribution</option>
                  <option value="monitoring">👁️ Monitoring</option>
                </select>
              </div>
              <div>
                <label className="text-sm font-medium">Title *</label>
                <Input className="mt-1" value={newTitle} onChange={e => setNewTitle(e.target.value)}
                  placeholder="e.g. Canvass Ikeja Ward 5" />
              </div>
            </div>
            <div>
              <label className="text-sm font-medium">Description</label>
              <Input value={newDesc} onChange={e => setNewDesc(e.target.value)}
                placeholder="Details about the task" />
            </div>
            <div className="grid grid-cols-4 gap-4">
              <div>
                <label className="text-sm font-medium">State</label>
                <Input className="mt-1" value={newState} onChange={e => setNewState(e.target.value)} placeholder="Lagos" />
              </div>
              <div>
                <label className="text-sm font-medium">Ward Code</label>
                <Input className="mt-1" value={newWard} onChange={e => setNewWard(e.target.value)} placeholder="LA-IK-W05" />
              </div>
              <div>
                <label className="text-sm font-medium">Target Count</label>
                <Input className="mt-1" type="number" value={newTarget} onChange={e => setNewTarget(Number(e.target.value))} />
              </div>
              <div>
                <label className="text-sm font-medium">Priority (1-5)</label>
                <select className="w-full border rounded-md p-2 text-sm mt-1" value={newPriority}
                  onChange={e => setNewPriority(Number(e.target.value))}>
                  <option value={1}>1 — Low</option>
                  <option value={2}>2 — Normal</option>
                  <option value={3}>3 — Medium</option>
                  <option value={4}>4 — High</option>
                  <option value={5}>5 — Critical</option>
                </select>
              </div>
            </div>
            <div>
              <label className="text-sm font-medium">Due Date</label>
              <Input className="mt-1" type="date" value={newDueDate} onChange={e => setNewDueDate(e.target.value)} />
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="outline" onClick={() => setShowCreateForm(false)}>Cancel</Button>
              <Button onClick={handleCreateTask} disabled={actionLoading === 'create' || !newTitle}>
                {actionLoading === 'create' ? 'Creating...' : 'Create Task'}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Task List */}
      {loading ? (
        <div className="text-center py-8 text-muted-foreground">Loading tasks...</div>
      ) : (
        <div className="space-y-3">
          {tasks.map(t => (
            <Card key={t.task_id} className="hover:shadow-md transition-shadow">
              <CardContent className="pt-4">
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-lg">{TASK_TYPE_ICONS[t.task_type] || '📋'}</span>
                      <h3 className="font-medium">{t.title}</h3>
                      <Badge className={TASK_STATUS_COLORS[t.status]}>{t.status}</Badge>
                      <Badge className={PRIORITY_COLORS[t.priority] || ''}>P{t.priority}</Badge>
                    </div>
                    {t.description && <p className="text-sm text-muted-foreground mb-2">{t.description}</p>}
                    <div className="flex items-center gap-4 text-xs text-muted-foreground">
                      {t.volunteer_name && <span>👤 {t.volunteer_name}</span>}
                      {t.state_code && <span>📍 {t.state_code}</span>}
                      {t.ward_code && <span>🗺️ {t.ward_code}</span>}
                      {t.due_date && <span>📅 {new Date(t.due_date).toLocaleDateString()}</span>}
                    </div>
                    {t.target_count > 1 && (
                      <div className="mt-2 flex items-center gap-2">
                        <Progress value={(t.completed_count / t.target_count) * 100} className="flex-1 h-2" />
                        <span className="text-xs font-mono">{t.completed_count}/{t.target_count}</span>
                      </div>
                    )}
                  </div>
                  <div className="flex gap-2 ml-4">
                    {t.status === 'assigned' && (
                      <Button size="sm" variant="outline" className="h-8" onClick={() => handleStartTask(t.task_id)}
                        disabled={actionLoading === t.task_id}>
                        <Play className="h-3 w-3 mr-1" /> Start
                      </Button>
                    )}
                    {(t.status === 'in_progress' || t.status === 'assigned') && (
                      <Button size="sm" className="h-8 bg-green-600 hover:bg-green-700"
                        onClick={() => handleCompleteTask(t.task_id, t.target_count)}
                        disabled={actionLoading === t.task_id}>
                        <CheckCircle className="h-3 w-3 mr-1" /> Complete
                      </Button>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
          {tasks.length === 0 && (
            <div className="text-center py-8 text-muted-foreground">
              No tasks found. Create one to get started.
            </div>
          )}
        </div>
      )}
    </div>
  );
}
