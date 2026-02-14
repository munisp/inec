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

  getMiddlewareStatus: () => request('/middleware/status'),
  getMiddlewareHealth: () => request('/middleware/health'),
  getKafkaTopics: () => request('/middleware/kafka/topics'),
  getTemporalWorkflows: () => request('/middleware/temporal/workflows'),
  getTigerBeetleAccounts: () => request('/middleware/tigerbeetle/accounts'),
  getAPISIXRoutes: () => request('/middleware/apisix/routes'),
  getRedisStats: () => request('/middleware/redis/stats'),
  getFluvioTopics: () => request('/middleware/fluvio/topics'),
  getLakehouseTables: () => request('/middleware/lakehouse/tables'),
  getLakehouseAnalytics: (electionId: number, type: string) =>
    request(`/middleware/lakehouse/analytics/${electionId}/${type}`),

  getBVASSummary: (electionId: number) => request(`/bvas/summary?election_id=${electionId}`),
  getBVASDevices: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/bvas/devices?${q}`);
  },
  getBVASReconciliation: (electionId: number, flaggedOnly?: boolean) =>
    request(`/bvas/reconciliation?election_id=${electionId}${flaggedOnly ? '&flagged_only=true' : ''}`),
  getBVASAccreditationFeed: (electionId: number, limit?: number) =>
    request(`/bvas/accreditation/feed?election_id=${electionId}&limit=${limit || 50}`),
  getBVASAccreditationTimeline: (electionId: number, interval?: string) =>
    request(`/bvas/accreditation/timeline?election_id=${electionId}&interval=${interval || 'hour'}`),
  getIngestionStats: () => request('/ingestion/stats'),
  getIngestionJobs: (status?: string) =>
    request(`/ingestion/jobs${status ? `?status=${status}` : ''}`),
  getDeadLetterQueue: () => request('/ingestion/dead-letter'),

  smsVerify: (phone: string, pollingUnitCode: string) =>
    request('/sms/verify', { method: 'POST', body: JSON.stringify({ phone, polling_unit_code: pollingUnitCode }) }),
  ussdGateway: (sessionId: string, phoneNumber: string, text: string) =>
    request('/ussd/gateway', { method: 'POST', body: JSON.stringify({ sessionId, phoneNumber, text }) }),
  getSMSStats: () => request('/sms/stats'),

  getAIAnomalies: (electionId: number, severity?: string) =>
    request(`/ai/anomalies?election_id=${electionId}${severity ? `&severity=${severity}` : ''}`),
  getAIBenford: (electionId: number) =>
    request(`/ai/benford?election_id=${electionId}`),
  getAIIntegrity: (electionId: number) =>
    request(`/ai/integrity?election_id=${electionId}`),
  getAIMethods: () => request('/ai/methods'),

  getPublicAPIDocs: () => request('/api/v1/docs'),
  generateAPIKey: (name: string, owner: string) =>
    request('/api/v1/keys', { method: 'POST', body: JSON.stringify({ name, owner }) }),
  getAPIKeys: () => request('/api/v1/keys'),
  getAPIUsage: () => request('/api/v1/usage'),

  getEMSVoters: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/voters?${q}`);
  },
  getEMSVoterStats: (stateCode?: string) =>
    request(`/ems/voters/stats${stateCode ? `?state_code=${stateCode}` : ''}`),
  getEMSRegistrationCenters: (stateCode?: string) =>
    request(`/ems/registration-centers${stateCode ? `?state_code=${stateCode}` : ''}`),

  getEMSWorkflows: (electionId?: number) =>
    request(`/ems/workflows${electionId ? `?election_id=${electionId}` : ''}`),
  getEMSWorkflow: (id: number) => request(`/ems/workflows/${id}`),
  advanceEMSWorkflow: (id: number) =>
    request(`/ems/workflows/${id}/advance`, { method: 'POST' }),

  getEMSSyncStats: (deviceId?: string) =>
    request(`/ems/sync/stats${deviceId ? `?device_id=${deviceId}` : ''}`),
  getEMSSyncQueue: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/sync/queue?${q}`);
  },
  resolveEMSSyncConflict: (id: number, resolution: string) =>
    request(`/ems/sync/conflicts/${id}/resolve`, { method: 'POST', body: JSON.stringify({ resolution }) }),

  getEMSPortalStatus: () => request('/ems/portals/status'),
  getEMSPortal: (id: number) => request(`/ems/portals/${id}`),
  syncEMSPortal: (id: number) =>
    request(`/ems/portals/${id}/sync`, { method: 'POST' }),
  getEMSPortalSyncLog: () => request('/ems/portals/sync-log'),

  getEMSValidationRules: (entityType?: string) =>
    request(`/ems/validation/rules${entityType ? `?entity_type=${entityType}` : ''}`),
  getEMSValidationStats: () => request('/ems/validation/stats'),
  getEMSValidationHistory: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/validation/history?${q}`);
  },

  getEMSLifecycle: (electionId: number) => request(`/ems/elections/${electionId}/lifecycle`),
  getEMSStaff: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/staff?${q}`);
  },
  getEMSMaterials: (params?: Record<string, string>) => {
    const q = new URLSearchParams(params);
    return request(`/ems/materials?${q}`);
  },
  getEMSMaterialStats: (electionId?: number) =>
    request(`/ems/materials/stats${electionId ? `?election_id=${electionId}` : ''}`),
  getEMSDashboard: (electionId: number) => request(`/ems/dashboard?election_id=${electionId}`),
};
