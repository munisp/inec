import { create } from 'zustand';

interface MiddlewareComponent {
  connected: boolean;
  mode: string;
  latency?: number;
}

interface MiddlewareState {
  components: Record<string, MiddlewareComponent>;
  status: 'loading' | 'healthy' | 'degraded' | 'error';
  lastChecked: string | null;
  fetchStatus: () => Promise<void>;
}

const API_URL = import.meta.env.VITE_API_URL || '';

export const useMiddlewareStore = create<MiddlewareState>((set) => ({
  components: {},
  status: 'loading',
  lastChecked: null,
  fetchStatus: async () => {
    try {
      const res = await fetch(`${API_URL}/healthz`);
      const data = await res.json();
      const mw = data.checks?.middleware || {};
      const allConnected = Object.values(mw).every(
        (c) => (c as MiddlewareComponent).connected
      );
      set({
        components: mw,
        status: allConnected ? 'healthy' : 'degraded',
        lastChecked: new Date().toISOString(),
      });
    } catch {
      set({ status: 'error', lastChecked: new Date().toISOString() });
    }
  },
}));
