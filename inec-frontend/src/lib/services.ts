/**
 * Typed service client layer for the INEC microservice architecture.
 *
 * In monolith mode, all requests go through a single API gateway.
 * In distributed mode, each service can be reached independently.
 *
 * Service mapping:
 *   auth-svc:8090        → /auth/*
 *   election-svc:8091    → /elections/*, /results/*, /dashboard/*
 *   biometric-svc:8092   → /biometric/*
 *   geo-svc:8093         → /geo/*
 *   compliance-svc:8094  → /compliance/*
 *   ingestion-svc:8095   → /ingestion/*
 *   bvas-svc:8096        → /bvas/*
 *   inference-engine:8097 → /inference/*
 *   lakehouse:8098       → /analytics/*
 *   document-ai:8099     → /documents/*
 *   fluvio-stream:8100   → /stream/*
 */

const GATEWAY_URL = import.meta.env.VITE_API_URL ?? '';

export interface ServiceConfig {
  gatewayUrl: string;
  distributed: boolean;
  serviceUrls?: Record<string, string>;
}

const defaultConfig: ServiceConfig = {
  gatewayUrl: GATEWAY_URL,
  distributed: false,
};

function serviceUrl(service: string, path: string, config = defaultConfig): string {
  if (config.distributed && config.serviceUrls?.[service]) {
    return `${config.serviceUrls[service]}${path}`;
  }
  return `${config.gatewayUrl}${path}`;
}

async function serviceRequest<T = unknown>(
  service: string,
  path: string,
  options: RequestInit = {},
  config = defaultConfig,
): Promise<T> {
  const url = serviceUrl(service, path, config);
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };
  const res = await fetch(url, { ...options, headers, credentials: 'include' });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ detail: res.statusText }));
    throw new Error(err.detail || err.error || `${service}: ${res.status}`);
  }
  return res.json();
}

// --- Auth Service ---

export const authService = {
  login: (username: string, password: string, totpCode?: string) =>
    serviceRequest('auth-svc', '/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp_code: totpCode }),
    }),

  getMe: () => serviceRequest('auth-svc', '/auth/me'),

  setupMFA: () => serviceRequest('auth-svc', '/auth/mfa/setup', { method: 'POST' }),

  verifyMFA: (code: string) =>
    serviceRequest('auth-svc', '/auth/mfa/verify', {
      method: 'POST',
      body: JSON.stringify({ code }),
    }),
};

// --- Election Service ---

export interface Election {
  id: number;
  title: string;
  election_type: string;
  status: string;
  election_date: string;
}

export const electionService = {
  list: (status?: string) =>
    serviceRequest<Election[]>('election-svc', `/elections${status ? `?status=${status}` : ''}`),

  get: (id: number) => serviceRequest<Election>('election-svc', `/elections/${id}`),

  create: (data: Record<string, string>) =>
    serviceRequest('election-svc', '/elections', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  submitResult: (data: Record<string, unknown>) =>
    serviceRequest('election-svc', '/results/submit', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  getDashboardStats: (electionId: number) =>
    serviceRequest('election-svc', `/dashboard/stats?election_id=${electionId}`),
};

// --- Biometric Service ---

export const biometricService = {
  verify: (voterId: string, template: string) =>
    serviceRequest('biometric-svc', '/biometric/verify', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, template }),
    }),

  enroll: (voterId: string, template: string) =>
    serviceRequest('biometric-svc', '/biometric/enroll', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, template }),
    }),
};

// --- Geo Service ---

export const geoService = {
  getStates: () => serviceRequest('geo-svc', '/geo/states'),
  getLgas: (stateCode?: string) =>
    serviceRequest('geo-svc', `/geo/lgas${stateCode ? `?state_code=${stateCode}` : ''}`),
  getWards: (lgaCode?: string) =>
    serviceRequest('geo-svc', `/geo/wards${lgaCode ? `?lga_code=${lgaCode}` : ''}`),
  getPollingUnits: (params?: Record<string, string>) =>
    serviceRequest('geo-svc', `/geo/polling-units?${new URLSearchParams(params)}`),
  validateGeofence: (lat: number, lng: number, puId: string) =>
    serviceRequest('geo-svc', '/geo/validate-geofence', {
      method: 'POST',
      body: JSON.stringify({ latitude: lat, longitude: lng, polling_unit_id: puId }),
    }),
};

// --- Compliance Service ---

export const complianceService = {
  getProcessingRegister: () =>
    serviceRequest('compliance-svc', '/compliance/ndpr/processing-register'),
  getDashboard: () => serviceRequest('compliance-svc', '/compliance/ndpr/dashboard'),
  recordConsent: (voterId: string, purpose: string) =>
    serviceRequest('compliance-svc', '/compliance/consent', {
      method: 'POST',
      body: JSON.stringify({ voter_id: voterId, purpose }),
    }),
  submitDSR: (requestType: string, voterId: string) =>
    serviceRequest('compliance-svc', '/compliance/dsr', {
      method: 'POST',
      body: JSON.stringify({ request_type: requestType, voter_id: voterId }),
    }),
};

// --- BVAS Service ---

export const bvasService = {
  getSummary: (electionId: number) =>
    serviceRequest('bvas-svc', `/bvas/summary?election_id=${electionId}`),
  getDevices: (params?: Record<string, string>) =>
    serviceRequest('bvas-svc', `/bvas/devices?${new URLSearchParams(params)}`),
  accredit: (deviceId: string, voterId: string) =>
    serviceRequest('bvas-svc', '/bvas/accredit', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, voter_id: voterId }),
    }),
};

// --- Ingestion Service ---

export const ingestionService = {
  getStats: () => serviceRequest('ingestion-svc', '/ingestion/stats'),
  getJobs: (status?: string) =>
    serviceRequest('ingestion-svc', `/ingestion/jobs${status ? `?status=${status}` : ''}`),
  submit: (data: Record<string, unknown>) =>
    serviceRequest('ingestion-svc', '/ingestion/submit', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
};

// --- Inference Engine (Rust) ---

export const inferenceService = {
  predict: (model: string, input: unknown) =>
    serviceRequest('inference-engine', `/models/${model}/predict`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  listModels: () => serviceRequest('inference-engine', '/models'),
  getAnomalies: (electionId: number) =>
    serviceRequest('inference-engine', `/ai/anomalies?election_id=${electionId}`),
};

// --- Lakehouse Analytics (Python) ---

export const analyticsService = {
  query: (queryName: string) =>
    serviceRequest('lakehouse-analytics', `/analytics/${queryName}`),
  detectAnomalies: (electionId: number) =>
    serviceRequest('lakehouse-analytics', '/analytics/anomalies', {
      method: 'POST',
      body: JSON.stringify({ election_id: electionId }),
    }),
  spatialAnalysis: (type: string, params: Record<string, unknown>) =>
    serviceRequest('lakehouse-analytics', '/spatial/analyze', {
      method: 'POST',
      body: JSON.stringify({ analysis_type: type, ...params }),
    }),
};

// --- Document AI (Python) ---

export const documentAIService = {
  extract: (imageData: string, formType: string) =>
    serviceRequest('document-ai', '/extract', {
      method: 'POST',
      body: JSON.stringify({ image: imageData, type: formType }),
    }),
  verify: (docId: string) =>
    serviceRequest('document-ai', `/verify/${docId}`),
};

// --- Gateway ---

export const gatewayService = {
  health: () => serviceRequest('gateway', '/health'),
  services: () => serviceRequest('gateway', '/services'),
  architecture: () => serviceRequest('gateway', '/architecture'),
};
