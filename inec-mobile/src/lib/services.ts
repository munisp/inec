/**
 * Typed service client layer for the INEC mobile app.
 *
 * All requests route through the API gateway. The mobile app uses
 * Bearer token auth (stored in SecureStore) rather than httpOnly cookies.
 *
 * Service mapping mirrors the backend microservice architecture:
 *   auth-svc        → /auth/*
 *   election-svc    → /elections/*, /results/*
 *   biometric-svc   → /biometric/*
 *   geo-svc         → /geo/*
 *   bvas-svc        → /bvas/*
 *   compliance-svc  → /compliance/*
 */

import { api, API_URL, getToken } from './api';

// --- Auth ---

export const authService = {
  login: (username: string, password: string, totpCode?: string) =>
    api('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp_code: totpCode }),
    }),

  getMe: () => api('/auth/me'),

  setupMFA: () => api('/auth/mfa/setup', { method: 'POST' }),

  verifyMFA: (code: string) =>
    api('/auth/mfa/verify', { method: 'POST', body: JSON.stringify({ code }) }),
};

// --- Elections ---

export interface Election {
  id: number;
  title: string;
  election_type: string;
  status: string;
  election_date: string;
}

export const electionService = {
  list: (status?: string) =>
    api<Election[]>(`/elections${status ? `?status=${status}` : ''}`),

  get: (id: number) => api<Election>(`/elections/${id}`),

  submitResult: (data: Record<string, unknown>) =>
    api('/results/submit', { method: 'POST', body: JSON.stringify(data) }),

  getDashboardStats: (electionId: number) =>
    api(`/dashboard/stats?election_id=${electionId}`),
};

// --- Biometric ---

export const biometricService = {
  verify: (voterId: string, template: string) =>
    api('/biometric/verify', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, template }),
    }),

  enroll: (voterId: string, template: string) =>
    api('/biometric/enroll', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, template }),
    }),
};

// --- Geo ---

export const geoService = {
  getStates: () => api('/geo/states'),

  getLgas: (stateCode?: string) =>
    api(`/geo/lgas${stateCode ? `?state_code=${stateCode}` : ''}`),

  getPollingUnits: (params?: Record<string, string>) =>
    api(`/geo/polling-units?${new URLSearchParams(params)}`),

  validateGeofence: (lat: number, lng: number, puId: string) =>
    api('/geo/validate-geofence', {
      method: 'POST',
      body: JSON.stringify({ latitude: lat, longitude: lng, polling_unit_id: puId }),
    }),
};

// --- BVAS ---

export const bvasService = {
  getSummary: (electionId: number) =>
    api(`/bvas/summary?election_id=${electionId}`),

  getDevices: (params?: Record<string, string>) =>
    api(`/bvas/devices?${new URLSearchParams(params)}`),

  accredit: (deviceId: string, voterId: string) =>
    api('/bvas/accredit', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, voter_id: voterId }),
    }),
};

// --- Compliance ---

export const complianceService = {
  recordConsent: (voterId: string, purpose: string) =>
    api('/compliance/consent', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, purpose }),
    }),
};

// --- Gateway ---

export const gatewayService = {
  health: () => api('/health'),
  services: () => api('/services'),
};
