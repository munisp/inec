// Standalone GOTV mobile auth — separate from INEC portal Keycloak.
// Uses phone+OTP for party-scoped canvasser/volunteer authentication.
// Tokens stored in expo-secure-store (AES-256-GCM on-device).

import * as SecureStore from 'expo-secure-store';

const GOTV_API = process.env.EXPO_PUBLIC_GOTV_API_URL ?? 'http://localhost:8103';

const TOKEN_KEY = 'gotv_mobile_token';
const REFRESH_KEY = 'gotv_mobile_refresh';
const USER_KEY = 'gotv_mobile_user';

export interface GOTVUser {
  user_id: string;
  party_id: number;
  role: string;
  display_name: string;
  party_code: string;
}

export interface OTPResponse {
  session_id: string;
  expires_in: number;
  message: string;
}

export interface VerifyOTPResponse {
  token: string;
  refresh_token: string;
  expires_in: number;
  user_id: string;
  party_id: number;
  role: string;
  display_name: string;
}

// Request OTP for phone number + party code
export async function requestOTP(phone: string, partyCode: string, name?: string): Promise<OTPResponse> {
  const res = await fetch(`${GOTV_API}/gotv/mobile/auth/request-otp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ phone, party_code: partyCode, name: name ?? '' }),
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Request failed' }));
    throw new Error(err.error ?? `OTP request failed (${res.status})`);
  }

  return res.json();
}

// Verify OTP and get JWT token
export async function verifyOTP(phone: string, partyCode: string, otpCode: string): Promise<GOTVUser> {
  const res = await fetch(`${GOTV_API}/gotv/mobile/auth/verify-otp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ phone, party_code: partyCode, otp_code: otpCode }),
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Verification failed' }));
    throw new Error(err.error ?? `OTP verification failed (${res.status})`);
  }

  const data: VerifyOTPResponse = await res.json();

  // Store tokens securely
  await SecureStore.setItemAsync(TOKEN_KEY, data.token);
  await SecureStore.setItemAsync(REFRESH_KEY, data.refresh_token);

  const user: GOTVUser = {
    user_id: data.user_id,
    party_id: data.party_id,
    role: data.role,
    display_name: data.display_name,
    party_code: partyCode,
  };
  await SecureStore.setItemAsync(USER_KEY, JSON.stringify(user));

  return user;
}

// Get stored mobile JWT token
export async function getMobileToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

// Get stored user profile
export async function getMobileUser(): Promise<GOTVUser | null> {
  const raw = await SecureStore.getItemAsync(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as GOTVUser;
  } catch {
    return null;
  }
}

// Check if user is authenticated
export async function isAuthenticated(): Promise<boolean> {
  const token = await getMobileToken();
  return token !== null;
}

// Refresh token
export async function refreshToken(): Promise<boolean> {
  const refresh = await SecureStore.getItemAsync(REFRESH_KEY);
  if (!refresh) return false;

  try {
    const res = await fetch(`${GOTV_API}/gotv/mobile/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refresh }),
    });

    if (!res.ok) return false;

    const data = await res.json();
    await SecureStore.setItemAsync(TOKEN_KEY, data.token);
    await SecureStore.setItemAsync(REFRESH_KEY, data.refresh_token);
    return true;
  } catch {
    return false;
  }
}

// Authenticated fetch for mobile endpoints
export async function gotvFetch<T = unknown>(path: string, options: RequestInit = {}): Promise<T> {
  let token = await getMobileToken();
  if (!token) throw new Error('Not authenticated');

  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
    'Authorization': `Bearer ${token}`,
  };

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = headers['Content-Type'] ?? 'application/json';
  }

  let res = await fetch(`${GOTV_API}${path}`, { ...options, headers });

  // If 401, try refreshing token once
  if (res.status === 401) {
    const refreshed = await refreshToken();
    if (refreshed) {
      token = await getMobileToken();
      headers['Authorization'] = `Bearer ${token}`;
      res = await fetch(`${GOTV_API}${path}`, { ...options, headers });
    }
  }

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

// Logout — clear all stored auth data
export async function logout(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
  await SecureStore.deleteItemAsync(REFRESH_KEY);
  await SecureStore.deleteItemAsync(USER_KEY);
}
