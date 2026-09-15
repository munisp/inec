/**
 * INEC Platform — Election Day Load Test
 * Simulates realistic traffic from 48K polling units with concurrent operations:
 * - Health monitoring (continuous)
 * - Read-heavy dashboard/results queries (80% of traffic)
 * - Write submissions from polling units (15% of traffic)
 * - Auth operations (5% of traffic)
 * - Geospatial queries (parallel)
 * - Compliance endpoints (audit)
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Counter, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('error_rate');
const resultSubmissions = new Counter('result_submissions');
const authAttempts = new Counter('auth_attempts');
const geoQueries = new Counter('geo_queries');
const complianceChecks = new Counter('compliance_checks');
const dashboardLatency = new Trend('dashboard_latency');
const submissionLatency = new Trend('submission_latency');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8088';

export const options = {
  scenarios: {
    // Scenario 1: Health monitoring (SRE team — constant)
    health_monitoring: {
      executor: 'constant-arrival-rate',
      rate: 30,
      timeUnit: '1s',
      duration: '3m',
      preAllocatedVUs: 10,
      maxVUs: 20,
      exec: 'healthCheck',
    },

    // Scenario 2: Dashboard viewers (election HQ, media — ramp up)
    dashboard_viewers: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '30s', target: 50 },   // Morning ramp
        { duration: '1m', target: 150 },   // Peak viewing
        { duration: '30s', target: 100 },  // Sustained
        { duration: '30s', target: 200 },  // Results announcement spike
        { duration: '30s', target: 50 },   // Tail off
      ],
      exec: 'dashboardViewer',
    },

    // Scenario 3: Result submissions (from polling units — gradual)
    result_submissions: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '30s', target: 30 },   // First PUs reporting
        { duration: '1m', target: 80 },    // Peak submission
        { duration: '1m', target: 100 },   // Steady state
        { duration: '30s', target: 20 },   // Winding down
      ],
      exec: 'submitResult',
    },

    // Scenario 4: Auth stress (login/logout cycles)
    auth_stress: {
      executor: 'ramping-arrival-rate',
      startRate: 5,
      timeUnit: '1s',
      stages: [
        { duration: '1m', target: 20 },
        { duration: '1m', target: 50 },
        { duration: '1m', target: 10 },
      ],
      preAllocatedVUs: 30,
      maxVUs: 100,
      exec: 'authCycle',
    },

    // Scenario 5: Geospatial queries (map users — concurrent)
    geo_queries: {
      executor: 'ramping-vus',
      startVUs: 2,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '1m', target: 40 },
        { duration: '1m', target: 30 },
        { duration: '30s', target: 10 },
      ],
      exec: 'geoQuery',
    },

    // Scenario 6: Architecture/compliance audit (admin — low rate)
    compliance_audit: {
      executor: 'constant-vus',
      vus: 3,
      duration: '3m',
      exec: 'complianceCheck',
    },
  },

  thresholds: {
    'http_req_duration': ['p(95)<1000', 'p(99)<2000'],
    'http_req_duration{scenario:health_monitoring}': ['p(99)<500'],
    'http_req_duration{scenario:dashboard_viewers}': ['p(95)<800'],
    'http_req_duration{scenario:result_submissions}': ['p(95)<1500'],
    'error_rate': ['rate<0.05'],
    'dashboard_latency': ['p(95)<800'],
    'submission_latency': ['p(95)<1500'],
  },
};

// Setup: authenticate once, then discover a REAL active election and REAL
// polling-unit codes — POST /results/submit 400s on fabricated ids/codes
// (R5-090: this test previously POSTed a made-up payload to the nonexistent
// POST /results, so every run "passed" on 404/405).
export function setup() {
  const loginRes = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
    username: __ENV.LOADTEST_USERNAME || 'admin',
    password: __ENV.LOADTEST_PASSWORD || 'admin123',
  }), { headers: { 'Content-Type': 'application/json' } });

  const token = loginRes.json('access_token') || '';
  if (!token) {
    throw new Error('login failed — cannot exercise authenticated write path');
  }
  const authHeaders = { headers: { 'Authorization': `Bearer ${token}` } };
  const elRes = http.get(`${BASE_URL}/elections`, authHeaders);
  let electionId = 0;
  if (elRes.status === 200) {
    const body = JSON.parse(elRes.body);
    const list = Array.isArray(body) ? body : (body.elections || body.data || []);
    const active = list.find((e) => e.status === 'active') || list[0];
    if (active) electionId = active.id;
  }
  if (!electionId) {
    throw new Error('no election found — seed an election before running the load test');
  }
  const puRes = http.get(`${BASE_URL}/geo/polling-units?limit=1000`);
  let puCodes = [];
  if (puRes.status === 200) {
    const body = JSON.parse(puRes.body);
    const list = Array.isArray(body) ? body : (body.polling_units || body.data || []);
    puCodes = list.map((pu) => pu.code).filter(Boolean);
  }
  if (puCodes.length === 0) {
    throw new Error('no polling units found — seed geography before running the load test');
  }
  return { token, electionId, puCodes };
}

// --- Scenario Executors ---

export function healthCheck() {
  const responses = http.batch([
    ['GET', `${BASE_URL}/healthz`],
    ['GET', `${BASE_URL}/readiness`],
    ['GET', `${BASE_URL}/metrics`],
  ]);

  for (const res of responses) {
    check(res, {
      'health status 200': (r) => r.status === 200,
    });
    errorRate.add(res.status !== 200);
  }
}

export function dashboardViewer(data) {
  const headers = {
    'Content-Type': 'application/json',
    'Cookie': `inec_token=${data.token}`,
  };

  const endpoints = [
    '/dashboard/stats',
    '/elections',
    // real route (there is no /collation/national):
    '/inec/collation?level=national',
    '/architecture/health',
    '/architecture/circuit-breakers',
  ];

  const endpoint = endpoints[Math.floor(Math.random() * endpoints.length)];
  const start = Date.now();
  const res = http.get(`${BASE_URL}${endpoint}`, { headers });
  dashboardLatency.add(Date.now() - start);

  check(res, {
    'dashboard ok': (r) => [200, 429].includes(r.status),
  });
  errorRate.add(![200, 429].includes(res.status));

  sleep(Math.random() * 2 + 0.5);
}

export function submitResult(data) {
  const puCode = data.puCodes[Math.floor(Math.random() * data.puCodes.length)];
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${data.token}`,
    'Cookie': `inec_token=${data.token}`,
    'X-Idempotency-Key': `k6-${__VU}-${__ITER}-${Date.now()}`,
  };

  // Real write path + real payload shape (handleSubmitResult):
  // election_id, polling_unit_code, party_scores[], accredited_voters,
  // rejected_votes. No polling_unit_id/party_id/votes fields exist.
  const payload = JSON.stringify({
    election_id: data.electionId,
    polling_unit_code: puCode,
    accredited_voters: Math.floor(Math.random() * 500) + 100,
    rejected_votes: Math.floor(Math.random() * 10),
    party_scores: [
      { party_code: 'APC', votes: Math.floor(Math.random() * 200) + 1 },
      { party_code: 'PDP', votes: Math.floor(Math.random() * 150) + 1 },
      { party_code: 'LP', votes: Math.floor(Math.random() * 100) + 1 },
    ],
  });

  const start = Date.now();
  const res = http.post(`${BASE_URL}/results/submit`, payload, { headers });
  submissionLatency.add(Date.now() - start);
  resultSubmissions.add(1);

  const ok = (r) =>
    [200, 201, 429].includes(r.status) ||
    (r.status === 400 && r.body && r.body.includes('already submitted'));
  check(res, { 'submission ok': ok });
  errorRate.add(!ok(res));

  sleep(Math.random() * 3 + 1);
}

export function authCycle() {
  authAttempts.add(1);

  const loginRes = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
    username: 'admin',
    password: 'admin123',
  }), { headers: { 'Content-Type': 'application/json' } });

  check(loginRes, {
    'auth ok': (r) => [200, 429].includes(r.status),
  });
  errorRate.add(![200, 429].includes(loginRes.status));

  if (loginRes.status === 200) {
    const token = loginRes.json('access_token') || '';
    sleep(0.5);

    const meRes = http.get(`${BASE_URL}/auth/me`, {
      headers: { 'Cookie': `inec_token=${token}` },
    });
    check(meRes, {
      'me ok': (r) => [200, 429].includes(r.status),
    });
  }
}

export function geoQuery(data) {
  geoQueries.add(1);
  const headers = {
    'Content-Type': 'application/json',
    'Cookie': `inec_token=${data.token}`,
  };

  const endpoints = [
    '/geo/nearby-pus?lat=9.0579&lng=7.4951&radius=5000',
    '/geo/landmarks',
    '/geo/tracking/officials?active_minutes=60',
    '/geo/crowd/density',
    '/geo/geofence/zones',
  ];

  const endpoint = endpoints[Math.floor(Math.random() * endpoints.length)];
  const res = http.get(`${BASE_URL}${endpoint}`, { headers });

  check(res, {
    'geo ok': (r) => [200, 429].includes(r.status),
  });
  errorRate.add(![200, 429].includes(res.status));

  sleep(Math.random() * 2 + 1);
}

export function complianceCheck(data) {
  complianceChecks.add(1);
  const headers = {
    'Content-Type': 'application/json',
    'Cookie': `inec_token=${data.token}`,
  };

  const endpoints = [
    '/compliance/dashboard',
    '/compliance/processing-register',
    '/compliance/breaches',
    '/admin/data-retention',
  ];

  const endpoint = endpoints[Math.floor(Math.random() * endpoints.length)];
  const res = http.get(`${BASE_URL}${endpoint}`, { headers });

  check(res, {
    'compliance ok': (r) => [200, 429].includes(r.status),
  });
  errorRate.add(![200, 429].includes(res.status));

  sleep(Math.random() * 5 + 2);
}

export function handleSummary(data) {
  const summary = {
    timestamp: new Date().toISOString(),
    duration_seconds: data.state.testRunDurationMs / 1000,
    scenarios: Object.keys(options.scenarios).length,
    total_requests: data.metrics.http_reqs ? data.metrics.http_reqs.values.count : 0,
    avg_rps: data.metrics.http_reqs ? data.metrics.http_reqs.values.rate : 0,
    p95_latency_ms: data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(95)'] : 0,
    p99_latency_ms: data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(99)'] : 0,
    error_rate: data.metrics.error_rate ? data.metrics.error_rate.values.rate : 0,
    thresholds_passed: !data.root_group || Object.values(data.root_group.checks || {}).every(c => c.fails === 0),
  };

  console.log('=== ELECTION DAY LOAD TEST SUMMARY ===');
  console.log(JSON.stringify(summary, null, 2));

  return {};
}
