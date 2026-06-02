import * as SecureStore from 'expo-secure-store';

export const API_URL = process.env.EXPO_PUBLIC_API_URL || 'http://10.0.2.2:8088';

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync('auth_token');
}

export async function setToken(token: string): Promise<void> {
  await SecureStore.setItemAsync('auth_token', token);
}

export async function clearToken(): Promise<void> {
  await SecureStore.deleteItemAsync('auth_token');
}

export async function api<T = unknown>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = await getToken();
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = headers['Content-Type'] || 'application/json';
  }

  const res = await fetch(`${API_URL}${path}`, { ...options, headers });

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`${res.status}: ${text}`);
  }

  const contentType = res.headers.get('content-type');
  if (contentType?.includes('application/json')) {
    return res.json() as Promise<T>;
  }
  return res.text() as unknown as T;
}

export interface LoginResponse {
  access_token: string;
  token_type: string;
  user: { id: number; username: string; role: string; full_name: string; staff_id: string };
}

export async function login(username: string, password: string): Promise<LoginResponse> {
  const data = await api<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
  await setToken(data.access_token);
  return data;
}

export interface ObserverStats {
  total_observers: number;
  active_check_ins: number;
  reports_today: number;
  active_alert_rules: number;
  active_sse_streams: number;
}

export interface ObserverReport {
  id: number;
  observer_id: number;
  polling_unit_code: string;
  election_id: number;
  report_type: string;
  photo_url: string;
  description: string;
  status: string;
  created_at: string;
}

export interface AlertRule {
  id: string;
  user_id: number;
  party_code: string;
  state_code: string;
  lga_code: string;
  alert_type: string;
  party?: string;
  state?: string;
  threshold?: number;
  is_active: number;
  created_at: string;
}

export interface PartyDashboard {
  party_code: string;
  total_votes: number;
  polling_units_with_results: number;
  total_polling_units: number;
  coverage_pct: number;
  observer_reports: number;
  state_breakdown: Array<{ state: string; votes: number }>;
  recent_results: Array<{ polling_unit: string; votes: number; time: string }>;
}

export interface CheckInResponse {
  message: string;
  checked_in: boolean;
  within_geofence: boolean;
  distance_m: number;
}

export const observerApi = {
  stats: () => api<ObserverStats>('/observer/stats'),

  reports: () => api<ObserverReport[]>('/observer/reports'),

  submitReport: (form: FormData) =>
    api<{ report_id: number; photo_url: string; status: string }>('/observer/reports', {
      method: 'POST',
      body: form,
    }),

  checkIn: (pollingUnitCode: string, latitude: number, longitude: number) =>
    api<CheckInResponse>('/observer/check-in', {
      method: 'POST',
      body: JSON.stringify({ polling_unit_code: pollingUnitCode, latitude, longitude }),
    }),

  alerts: () => api<AlertRule[]>('/observer/alerts'),

  createAlert: (rule: { alert_type: string; party?: string; state?: string; threshold?: number; party_code?: string; state_code?: string; lga_code?: string }) =>
    api<{ rule_id: number; message: string }>('/observer/alerts', {
      method: 'POST',
      body: JSON.stringify(rule),
    }),

  deleteAlert: (id: string | number) =>
    api<{ message: string }>(`/observer/alerts/${id}`, { method: 'DELETE' }),

  partyDashboard: (party: string) =>
    api<PartyDashboard>(`/observer/party-dashboard?party=${party}`),

  submitVideo: (form: FormData) =>
    api<{ video_url: string; status: string; analysis?: VideoAnalysis }>('/observer/video', {
      method: 'POST',
      body: form,
    }),
};

// ── KYC & Liveness API ──

export interface KYCResult {
  user_id: number;
  status: 'verified' | 'pending_review' | 'rejected' | 'requires_liveness' | 'not_started';
  identity_match_score: number;
  document_verified: boolean;
  face_match_score: number;
  liveness_passed: boolean;
  risk_score: number;
  checks_performed: string[];
  flags: string[];
  verification_timestamp: string;
}

export interface LivenessResult {
  user_id: number;
  passed: boolean;
  confidence: number;
  method: string;
  anti_spoofing_score: number;
  checks: Array<{ name: string; passed: boolean; value?: number; note?: string }>;
  timestamp: string;
}

export interface VideoAnalysis {
  duration_seconds: number;
  frame_count: number;
  fps: number;
  resolution: { width: number; height: number };
  key_frames_extracted: number;
  anomalies_detected: Array<{ frame: number; timestamp_sec: number; type: string; description?: string }>;
  ballot_counting_events: Array<{ frame: number; timestamp_sec: number; type: string }>;
  integrity_score: number;
  analysis_summary: string;
}

export interface DocumentAnalysis {
  report_id: number;
  ocr: {
    serial_number: string | null;
    polling_unit_code: string | null;
    party_results: Array<{ party_code: string; votes: number; confidence: number }>;
    total_valid_votes: number | null;
    confidence_score: number;
    extraction_warnings: string[];
  };
  vlm: {
    is_valid_ec8a: boolean;
    tampering_detected: boolean;
    tampering_confidence: number;
    tampering_indicators: string[];
    document_quality: string;
    completeness_score: number;
    analysis_summary: string;
  };
  combined_confidence: number;
  requires_manual_review: boolean;
}

export const kycApi = {
  verify: (form: FormData) =>
    api<KYCResult>('/kyc/verify', { method: 'POST', body: form }),

  liveness: (form: FormData) =>
    api<LivenessResult>('/kyc/liveness', { method: 'POST', body: form }),

  status: (userId: number) =>
    api<KYCResult>(`/kyc/status?user_id=${userId}`),
};

export const documentAIApi = {
  analyze: (reportId: number) =>
    api<DocumentAnalysis>(`/document-ai/analyze?report_id=${reportId}`, { method: 'POST' }),

  status: (reportId: number) =>
    api<{ report_id: number; status: string; ocr_confidence?: number; tampering_detected?: boolean }>(`/document-ai/status?report_id=${reportId}`),
};

// ── Disputes API ──

export interface Dispute {
  id: number;
  election_id: number;
  polling_unit_code: string;
  filed_by: string;
  party: string;
  category: string;
  description: string;
  evidence: string[];
  status: string;
  assigned_to: string;
  resolution: string;
  resolved_by: string;
  filed_at: string;
  resolved_at: string;
  priority: string;
}

export interface DisputeStats {
  total: number;
  by_status: Record<string, number>;
  by_priority: Record<string, number>;
  categories: string[];
}

export const disputeApi = {
  list: () => api<Dispute[]>('/disputes'),
  stats: () => api<DisputeStats>('/disputes/stats'),
  file: (data: { election_id: number; polling_unit_code: string; category: string; description: string; party?: string }) =>
    api<Dispute>('/disputes', { method: 'POST', body: JSON.stringify(data) }),
  updateStatus: (id: number, status: string, resolution?: string) =>
    api<Dispute>(`/disputes/${id}/status`, {
      method: 'PUT',
      body: JSON.stringify({ status, resolution }),
    }),
};

// ── Elections & Results API ──

export interface Election {
  id: number;
  name: string;
  type: string;
  date: string;
  status: string;
  total_polling_units: number;
  results_submitted: number;
  registered_voters: number;
}

export interface Result {
  id: number;
  election_id: number;
  polling_unit_code: string;
  polling_unit_name?: string;
  state?: string;
  lga?: string;
  total_valid_votes: number;
  rejected_votes: number;
  total_votes_cast: number;
  status: string;
  submitted_at: string;
  party_scores?: Array<{ party_code: string; votes: number }>;
}

export interface CollationSummary {
  level: string;
  area_code: string;
  area_name: string;
  total_votes: number;
  total_valid: number;
  total_rejected: number;
  party_totals: Array<{ party_code: string; total_votes: number }>;
  reporting_pct: number;
}

export const electionApi = {
  list: () => api<Election[]>('/elections'),
  results: (electionId: number) => api<Result[]>(`/results?election_id=${electionId}`),
  collation: (electionId: number, level: string) =>
    api<CollationSummary[]>(`/collation?election_id=${electionId}&level=${level}`),
  dashboardStats: (electionId: number) =>
    api<{ total_votes: number; results_count: number; polling_units: number; rejection_rate: number }>(`/dashboard/stats?election_id=${electionId}`),
};

// ── Scale Health API ──

export const scaleApi = {
  health: () => api<Record<string, unknown>>('/scale/health'),
  middlewareModes: () => api<Array<{ Name: string; IsReal: boolean; Connection: string }>>('/middleware/modes'),
};

