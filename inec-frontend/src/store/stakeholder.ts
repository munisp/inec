import { create } from 'zustand';
import { api } from '../lib/api';

// ─── Stakeholder Role Definitions ───────────────────────────────────────────
export type StakeholderRole =
  | 'admin'
  | 'presiding_officer'
  | 'collation_officer'
  | 'observer'
  | 'returning_officer'
  | 'ward_collation_officer'
  | 'lga_collation_officer'
  | 'state_collation_officer'
  | 'public';

// Feature access matrix: which roles can access which features
export const ROLE_FEATURE_MATRIX: Record<StakeholderRole, string[]> = {
  admin: [
    'elections.create', 'elections.update', 'elections.delete', 'elections.view',
    'users.create', 'users.update', 'users.delete', 'users.view', 'users.promote',
    'results.submit', 'results.validate', 'results.finalize', 'results.view', 'results.dispute',
    'incidents.create', 'incidents.update', 'incidents.view',
    'bvas.register', 'bvas.update', 'bvas.view', 'bvas.accreditation',
    'audit.view', 'audit.verify',
    'command_center.view', 'command_center.live',
    'geo.view', 'geo.tracking', 'geo.submission',
    'biometric.view', 'biometric.quality_check',
    'compliance.view', 'compliance.report',
    'dispute.create', 'dispute.resolve', 'dispute.view',
    'training.create', 'training.view',
    'ai.integrity_score', 'ai.heatmap',
    'data.classification', 'data.erasure',
    'mfa.setup', 'mfa.verify',
    'batch.users', 'batch.status',
    'export.pdf', 'export.csv',
    'observer.photo_verify',
    'blockchain.view',
    'predictive.analytics',
    'middleware.view',
    'webhook.manage',
  ],
  presiding_officer: [
    'elections.view',
    'results.submit', 'results.view',
    'incidents.create', 'incidents.view',
    'bvas.view', 'bvas.accreditation',
    'audit.view',
    'geo.view', 'geo.tracking', 'geo.submission',
    'biometric.view', 'biometric.quality_check',
    'mfa.setup', 'mfa.verify',
    'export.pdf',
    'offline.conflict_resolve',
  ],
  collation_officer: [
    'elections.view',
    'results.submit', 'results.validate', 'results.view',
    'incidents.create', 'incidents.view',
    'bvas.view',
    'audit.view',
    'geo.view',
    'mfa.setup', 'mfa.verify',
    'export.pdf',
  ],
  returning_officer: [
    'elections.view',
    'results.validate', 'results.finalize', 'results.view',
    'incidents.view',
    'audit.view',
    'geo.view',
    'compliance.view',
    'export.pdf',
  ],
  ward_collation_officer: [
    'elections.view',
    'results.submit', 'results.view',
    'incidents.create', 'incidents.view',
    'geo.view',
    'export.pdf',
  ],
  lga_collation_officer: [
    'elections.view',
    'results.submit', 'results.validate', 'results.view',
    'incidents.view',
    'geo.view',
    'export.pdf',
  ],
  state_collation_officer: [
    'elections.view',
    'results.validate', 'results.view',
    'incidents.view',
    'geo.view',
    'compliance.view',
    'export.pdf',
  ],
  observer: [
    'elections.view',
    'results.view',
    'incidents.create', 'incidents.view',
    'audit.view',
    'geo.view',
    'observer.photo_verify',
    'biometric.view',
  ],
  public: [
    'elections.view',
    'results.view',
    'geo.view',
  ],
};

// ─── Stakeholder Workflow Store ──────────────────────────────────────────────
interface WorkflowStep {
  id: string;
  name: string;
  status: 'pending' | 'in_progress' | 'completed' | 'failed';
  timestamp?: string;
  data?: Record<string, unknown>;
}

interface StakeholderWorkflow {
  role: StakeholderRole;
  currentWorkflow: string | null;
  steps: WorkflowStep[];
  completedWorkflows: string[];
  pendingActions: string[];
}

interface StakeholderStore extends StakeholderWorkflow {
  // Workflow actions
  startWorkflow: (workflowName: string) => void;
  completeStep: (stepId: string, data?: Record<string, unknown>) => void;
  failStep: (stepId: string, error: string) => void;
  completeWorkflow: () => void;
  resetWorkflow: () => void;
  
  // Permission checks
  canAccess: (feature: string, role: StakeholderRole) => boolean;
  
  // Workflow templates
  getWorkflowSteps: (workflowName: string, role: StakeholderRole) => WorkflowStep[];
  
  // API actions
  submitResult: (data: Record<string, unknown>) => Promise<unknown>;
  reportIncident: (data: Record<string, unknown>) => Promise<unknown>;
  accreditVoter: (data: Record<string, unknown>) => Promise<unknown>;
  validateResult: (id: number) => Promise<unknown>;
  finalizeResult: (id: number) => Promise<unknown>;
  resolveDispute: (id: number, resolution: string) => Promise<unknown>;
}

// Workflow templates for each role
const WORKFLOW_TEMPLATES: Record<string, Record<string, WorkflowStep[]>> = {
  presiding_officer: {
    'election_day': [
      { id: 'open_polling', name: 'Open Polling Unit', status: 'pending' },
      { id: 'verify_bvas', name: 'Verify BVAS Device', status: 'pending' },
      { id: 'accreditation', name: 'Voter Accreditation', status: 'pending' },
      { id: 'voting', name: 'Voting Process', status: 'pending' },
      { id: 'close_polling', name: 'Close Polling Unit', status: 'pending' },
      { id: 'count_votes', name: 'Count Votes', status: 'pending' },
      { id: 'complete_ec8a', name: 'Complete EC8A Form', status: 'pending' },
      { id: 'submit_results', name: 'Submit Results', status: 'pending' },
      { id: 'upload_documents', name: 'Upload Supporting Documents', status: 'pending' },
    ],
    'incident_report': [
      { id: 'identify_incident', name: 'Identify Incident', status: 'pending' },
      { id: 'document_incident', name: 'Document Incident', status: 'pending' },
      { id: 'report_incident', name: 'Report to Supervisor', status: 'pending' },
      { id: 'resolve_incident', name: 'Resolve/Escalate Incident', status: 'pending' },
    ],
  },
  collation_officer: {
    'ward_collation': [
      { id: 'receive_results', name: 'Receive PU Results', status: 'pending' },
      { id: 'verify_results', name: 'Verify Results', status: 'pending' },
      { id: 'collate_results', name: 'Collate Ward Results', status: 'pending' },
      { id: 'complete_ec8b', name: 'Complete EC8B Form', status: 'pending' },
      { id: 'submit_collation', name: 'Submit Ward Collation', status: 'pending' },
    ],
    'lga_collation': [
      { id: 'receive_ward_results', name: 'Receive Ward Results', status: 'pending' },
      { id: 'verify_ward_results', name: 'Verify Ward Results', status: 'pending' },
      { id: 'collate_lga', name: 'Collate LGA Results', status: 'pending' },
      { id: 'complete_ec8c', name: 'Complete EC8C Form', status: 'pending' },
      { id: 'submit_lga_collation', name: 'Submit LGA Collation', status: 'pending' },
    ],
  },
  returning_officer: {
    'state_collation': [
      { id: 'receive_lga_results', name: 'Receive LGA Results', status: 'pending' },
      { id: 'verify_lga_results', name: 'Verify LGA Results', status: 'pending' },
      { id: 'collate_state', name: 'Collate State Results', status: 'pending' },
      { id: 'complete_ec8d', name: 'Complete EC8D Form', status: 'pending' },
      { id: 'declare_results', name: 'Declare Results', status: 'pending' },
      { id: 'sign_results', name: 'Sign Results Certificate', status: 'pending' },
    ],
  },
  observer: {
    'monitoring': [
      { id: 'check_in', name: 'Check-in at Polling Unit', status: 'pending' },
      { id: 'monitor_accreditation', name: 'Monitor Accreditation', status: 'pending' },
      { id: 'monitor_voting', name: 'Monitor Voting', status: 'pending' },
      { id: 'monitor_counting', name: 'Monitor Vote Counting', status: 'pending' },
      { id: 'submit_report', name: 'Submit Observer Report', status: 'pending' },
    ],
    'incident_report': [
      { id: 'identify_incident', name: 'Identify Incident', status: 'pending' },
      { id: 'document_incident', name: 'Document with Photo', status: 'pending' },
      { id: 'report_incident', name: 'Submit Incident Report', status: 'pending' },
    ],
  },
  admin: {
    'election_setup': [
      { id: 'create_election', name: 'Create Election', status: 'pending' },
      { id: 'configure_election', name: 'Configure Election Parameters', status: 'pending' },
      { id: 'assign_staff', name: 'Assign Staff', status: 'pending' },
      { id: 'setup_bvas', name: 'Setup BVAS Devices', status: 'pending' },
      { id: 'publish_election', name: 'Publish Election', status: 'pending' },
    ],
    'user_management': [
      { id: 'create_user', name: 'Create User Account', status: 'pending' },
      { id: 'assign_role', name: 'Assign Role', status: 'pending' },
      { id: 'assign_location', name: 'Assign Location', status: 'pending' },
      { id: 'send_credentials', name: 'Send Credentials', status: 'pending' },
    ],
  },
};

export const useStakeholderStore = create<StakeholderStore>((set) => ({
  role: 'public',
  currentWorkflow: null,
  steps: [],
  completedWorkflows: [],
  pendingActions: [],

  canAccess: (feature: string, role: StakeholderRole) => {
    const features = ROLE_FEATURE_MATRIX[role] || [];
    return features.includes(feature);
  },

  getWorkflowSteps: (workflowName: string, role: StakeholderRole) => {
    return WORKFLOW_TEMPLATES[role]?.[workflowName] || [];
  },

  startWorkflow: (workflowName: string) => {
    set({ currentWorkflow: workflowName });
  },

  completeStep: (stepId: string, data?: Record<string, unknown>) => {
    set((state) => ({
      steps: state.steps.map((s) =>
        s.id === stepId ? { ...s, status: 'completed', data, timestamp: new Date().toISOString() } : s
      ),
    }));
  },

  failStep: (stepId: string, error: string) => {
    set((state) => ({
      steps: state.steps.map((s) =>
        s.id === stepId ? { ...s, status: 'failed', data: { error }, timestamp: new Date().toISOString() } : s
      ),
    }));
  },

  completeWorkflow: () => {
    set((state) => ({
      completedWorkflows: [...state.completedWorkflows, state.currentWorkflow || ''],
      currentWorkflow: null,
      steps: [],
    }));
  },

  resetWorkflow: () => {
    set({ currentWorkflow: null, steps: [] });
  },

  submitResult: async (data: Record<string, unknown>) => {
    return api.submitResult(data);
  },

  reportIncident: async (data: Record<string, unknown>) => {
    return api.createIncident(data);
  },

  accreditVoter: async (data: Record<string, unknown>) => {
    return api.bvasAccreditation(data);
  },

  validateResult: async (id: number) => {
    return api.validateResult(id);
  },

  finalizeResult: async (id: number) => {
    return api.finalizeResult(id);
  },

  resolveDispute: async (id: number, resolution: string) => {
    return api.resolveDispute(id, resolution);
  },
}));
