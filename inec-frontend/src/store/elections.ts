import { create } from 'zustand';

interface Election {
  id: number;
  title: string;
  election_type: string;
  election_date: string;
  status: string;
}

interface ElectionsState {
  elections: Election[];
  selectedElection: Election | null;
  loading: boolean;
  error: string | null;
  fetchElections: (token: string) => Promise<void>;
  selectElection: (election: Election | null) => void;
}

const API_URL = import.meta.env.VITE_API_URL || '';

export const useElectionsStore = create<ElectionsState>((set) => ({
  elections: [],
  selectedElection: null,
  loading: false,
  error: null,
  fetchElections: async (token: string) => {
    set({ loading: true, error: null });
    try {
      const res = await fetch(`${API_URL}/elections`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      set({ elections: Array.isArray(data) ? data : [], loading: false });
    } catch (err) {
      set({ error: (err as Error).message, loading: false });
    }
  },
  selectElection: (election) => set({ selectedElection: election }),
}));
