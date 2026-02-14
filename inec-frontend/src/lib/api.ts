const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8000';

function getToken(): string | null {
  return localStorage.getItem('token');
}

async function request(path: string, options: RequestInit = {}) {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const res = await fetch(`${API_URL}${path}`, { ...options, headers });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ detail: res.statusText }));
    throw new Error(err.detail || 'Request failed');
  }
  return res.json();
}

export const api = {
  login: (username: string, password: string) =>
    request('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  register: (data: { username: string; password: string; full_name: string; role?: string }) =>
    request('/auth/register', { method: 'POST', body: JSON.stringify(data) }),
  getMe: () => request('/auth/me'),

  getElections: (status?: string) =>
    request(`/elections${status ? `?status=${status}` : ''}`),
  getElection: (id: number) => request(`/elections/${id}`),
  getElectionStats: (id: number) => request(`/elections/${id}/stats`),
  createElection: (data: Record<string, string>) =>
    request('/elections', { method: 'POST', body: JSON.stringify(data) }),
  updateElection: (id: number, data: Record<string, string>) =>
    request(`/elections/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),

  getDashboardStats: (electionId: number) =>
    request(`/dashboard/stats?election_id=${electionId}`),
  getLiveFeed: (electionId: number, limit = 20) =>
    request(`/dashboard/live-feed?election_id=${electionId}&limit=${limit}`),
  getCollation: (electionId: number, level: string, parentCode?: string) =>
    request(`/dashboard/collation?election_id=${electionId}&level=${level}${parentCode ? `&parent_code=${parentCode}` : ''}`),

  getResults: (electionId: number, params?: Record<string, string>) => {
    const q = new URLSearchParams({ election_id: String(electionId), ...params });
    return request(`/results?${q}`);
  },
  getResult: (id: number) => request(`/results/${id}`),
  submitResult: (data: Record<string, unknown>) =>
    request('/results/submit', { method: 'POST', body: JSON.stringify(data) }),
  validateResult: (id: number) =>
    request(`/results/${id}/validate`, { method: 'POST' }),
  finalizeResult: (id: number) =>
    request(`/results/${id}/finalize`, { method: 'POST' }),
  disputeResult: (id: number) =>
    request(`/results/${id}/dispute`, { method: 'POST' }),

  getStates: () => request('/geo/states'),
  getLgas: (stateCode?: string) =>
    request(`/geo/lgas${stateCode ? `?state_code=${stateCode}` : ''}`),
  getWards: (lgaCode?: string) =>
    request(`/geo/wards${lgaCode ? `?lga_code=${lgaCode}` : ''}`),
  getPollingUnits: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/geo/polling-units?${q}`);
  },

  getParties: () => request('/parties'),

  getAuditTrail: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/audit/trail?${q}`);
  },
  verifyResult: (id: number) => request(`/audit/verify/${id}`),
  getAuditStats: () => request('/audit/stats'),

  getIncidents: (electionId: number) =>
    request(`/incidents?election_id=${electionId}`),
  createIncident: (data: Record<string, unknown>) =>
    request('/incidents', { method: 'POST', body: JSON.stringify(data) }),

  getMapData: (electionId: number, stateCode?: string) =>
    request(`/geo/map-data?election_id=${electionId}${stateCode ? `&state_code=${stateCode}` : ''}`),
};
