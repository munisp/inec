import { useState } from 'react';
import { useAuthStore } from '../store/auth';
import { useStakeholderStore, ROLE_FEATURE_MATRIX, StakeholderRole } from '../store/stakeholder';

const WORKFLOW_NAMES: Record<string, Record<string, string>> = {
  admin: {
    'election_setup': 'Election Setup',
    'user_management': 'User Management',
  },
  presiding_officer: {
    'election_day': 'Election Day Operations',
    'incident_report': 'Incident Report',
  },
  collation_officer: {
    'ward_collation': 'Ward Collation',
    'lga_collation': 'LGA Collation',
  },
  returning_officer: {
    'state_collation': 'State Collation',
  },
  observer: {
    'monitoring': 'Polling Unit Monitoring',
    'incident_report': 'Incident Report',
  },
};

export default function StakeholderWorkflowPage() {
  const { user } = useAuthStore();
  const { getWorkflowSteps, startWorkflow, completeStep, completeWorkflow, resetWorkflow, completedWorkflows } = useStakeholderStore();
  const [selectedWorkflow, setSelectedWorkflow] = useState<string | null>(null);
  const [workflowSteps, setWorkflowSteps] = useState<{ id: string; name: string; status: string }[]>([]);

  const role = (user?.role || 'public') as StakeholderRole;
  const features = ROLE_FEATURE_MATRIX[role] || [];
  const workflows = WORKFLOW_NAMES[role] || {};

  const handleStartWorkflow = (workflowName: string) => {
    const steps = getWorkflowSteps(workflowName, role);
    setWorkflowSteps(steps);
    setSelectedWorkflow(workflowName);
    startWorkflow(workflowName);
  };

  const handleCompleteStep = (stepId: string) => {
    setWorkflowSteps((prev) =>
      prev.map((s) => (s.id === stepId ? { ...s, status: 'completed' } : s))
    );
    completeStep(stepId, { completedAt: new Date().toISOString() });
  };

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-2">Stakeholder Workflow Center</h1>
      <p className="text-gray-500 mb-6">Role: <span className="font-semibold text-blue-600">{role.replace(/_/g, ' ').toUpperCase()}</span></p>

      {/* Feature Access Matrix */}
      <div className="mb-8">
        <h2 className="text-lg font-semibold mb-3">Your Feature Access ({features.length} features)</h2>
        <div className="flex flex-wrap gap-2">
          {features.map((f) => (
            <span key={f} className="px-2 py-1 bg-green-100 text-green-800 rounded text-xs">{f}</span>
          ))}
        </div>
      </div>

      {/* Available Workflows */}
      <div className="mb-8">
        <h2 className="text-lg font-semibold mb-3">Available Workflows</h2>
        {Object.keys(workflows).length === 0 ? (
          <p className="text-gray-400">No workflows available for your role.</p>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {Object.entries(workflows).map(([key, name]) => (
              <div key={key} className="border rounded-lg p-4 hover:shadow-md transition-shadow">
                <h3 className="font-medium mb-2">{name}</h3>
                <p className="text-sm text-gray-500 mb-3">
                  {getWorkflowSteps(key, role).length} steps
                  {completedWorkflows.includes(key) && (
                    <span className="ml-2 text-green-600 font-semibold">✓ Completed</span>
                  )}
                </p>
                <button
                  onClick={() => handleStartWorkflow(key)}
                  className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm"
                >
                  {completedWorkflows.includes(key) ? 'Restart' : 'Start Workflow'}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Active Workflow */}
      {selectedWorkflow && workflowSteps.length > 0 && (
        <div className="border rounded-lg p-6 bg-gray-50">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-lg font-semibold">
              Active Workflow: {workflows[selectedWorkflow]}
            </h2>
            <button
              onClick={() => { resetWorkflow(); setSelectedWorkflow(null); setWorkflowSteps([]); }}
              className="text-sm text-gray-500 hover:text-red-500"
            >
              Cancel
            </button>
          </div>
          <div className="space-y-3">
            {workflowSteps.map((step, idx) => {
              const prevCompleted = idx === 0 || workflowSteps[idx - 1].status === 'completed';
              return (
                <div
                  key={step.id}
                  className={`flex items-center gap-3 p-3 rounded-lg border ${
                    step.status === 'completed' ? 'bg-green-50 border-green-200' :
                    step.status === 'in_progress' ? 'bg-blue-50 border-blue-200' :
                    'bg-white border-gray-200'
                  }`}
                >
                  <div className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold ${
                    step.status === 'completed' ? 'bg-green-500 text-white' :
                    step.status === 'in_progress' ? 'bg-blue-500 text-white' :
                    'bg-gray-200 text-gray-600'
                  }`}>
                    {step.status === 'completed' ? '✓' : idx + 1}
                  </div>
                  <span className="flex-1 font-medium">{step.name}</span>
                  {step.status !== 'completed' && prevCompleted && (
                    <button
                      onClick={() => handleCompleteStep(step.id)}
                      className="px-3 py-1 bg-blue-600 text-white rounded text-sm hover:bg-blue-700"
                    >
                      Complete
                    </button>
                  )}
                </div>
              );
            })}
          </div>
          {workflowSteps.every((s) => s.status === 'completed') && (
            <div className="mt-4 text-center">
              <p className="text-green-600 font-semibold mb-3">All steps completed!</p>
              <button
                onClick={() => { completeWorkflow(); setSelectedWorkflow(null); setWorkflowSteps([]); }}
                className="px-6 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700"
              >
                Finalize Workflow
              </button>
            </div>
          )}
        </div>
      )}

      {/* Completed Workflows */}
      {completedWorkflows.length > 0 && (
        <div className="mt-8">
          <h2 className="text-lg font-semibold mb-3">Completed Workflows</h2>
          <div className="flex flex-wrap gap-2">
            {completedWorkflows.map((w) => (
              <span key={w} className="px-3 py-1 bg-green-100 text-green-800 rounded-full text-sm">
                ✓ {workflows[w] || w}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
