import * as SecureStore from 'expo-secure-store';

const API_URL = process.env.EXPO_PUBLIC_API_URL || 'http://10.0.2.2:8088';

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
  id: number;
  user_id: number;
  party_code: string;
  state_code: string;
  lga_code: string;
  alert_type: string;
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

  createAlert: (rule: { party_code: string; state_code: string; lga_code: string; alert_type: string }) =>
    api<{ rule_id: number; message: string }>('/observer/alerts', {
      method: 'POST',
      body: JSON.stringify(rule),
    }),

  deleteAlert: (id: number) =>
    api<{ message: string }>(`/observer/alerts/${id}`, { method: 'DELETE' }),

  partyDashboard: (party: string) =>
    api<PartyDashboard>(`/observer/party-dashboard?party=${party}`),
};

export { API_URL };
