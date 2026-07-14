import { create } from 'zustand';
import { api } from '@/lib/api';

interface IntegrationsState {
  middlewareStatus: any;
  lakehouseAnomalies: any[];
  workflowStatuses: Record<string, any>;
  isLoading: boolean;
  error: string | null;
  
  fetchMiddlewareStatus: () => Promise<void>;
  fetchLakehouseAnomalies: (electionId: number) => Promise<void>;
  startElectionWorkflow: (workflowName: string, payload: any) => Promise<string>;
  checkWorkflowStatus: (workflowId: string) => Promise<void>;
}

export const useIntegrationsStore = create<IntegrationsState>((set) => ({
  middlewareStatus: null,
  lakehouseAnomalies: [],
  workflowStatuses: {},
  isLoading: false,
  error: null,

  fetchMiddlewareStatus: async () => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.getMiddlewareStatus();
      set({ middlewareStatus: data, isLoading: false });
    } catch (error: any) {
      set({ error: error.message, isLoading: false });
    }
  },

  fetchLakehouseAnomalies: async (electionId: number) => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.getLakehouseAnomalies(electionId);
      set({ lakehouseAnomalies: data.anomalies || [], isLoading: false });
    } catch (error: any) {
      set({ error: error.message, isLoading: false });
    }
  },

  startElectionWorkflow: async (workflowName: string, payload: any) => {
    set({ isLoading: true, error: null });
    try {
      const data = await api.startWorkflow(workflowName, payload);
      set((state) => ({ 
        workflowStatuses: { ...state.workflowStatuses, [data.workflow_id]: 'STARTED' },
        isLoading: false 
      }));
      return data.workflow_id;
    } catch (error: any) {
      set({ error: error.message, isLoading: false });
      throw error;
    }
  },

  checkWorkflowStatus: async (workflowId: string) => {
    try {
      const data = await api.getWorkflowStatus(workflowId);
      set((state) => ({
        workflowStatuses: { ...state.workflowStatuses, [workflowId]: data.status }
      }));
    } catch (error: any) {
      console.error('Failed to check workflow status:', error);
    }
  }
}));
