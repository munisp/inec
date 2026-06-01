import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const loginDuration = new Trend('login_duration');
const resultSubmitDuration = new Trend('result_submit_duration');
const healthCheckDuration = new Trend('health_check_duration');

// Test configuration — ramp up to simulate election day traffic
export const options = {
  stages: [
    { duration: '30s', target: 10 },   // Warm up
    { duration: '1m', target: 50 },    // Ramp to normal load
    { duration: '2m', target: 200 },   // Peak election day traffic
    { duration: '1m', target: 500 },   // Stress test
    { duration: '30s', target: 0 },    // Cool down
  ],
  thresholds: {
    http_req_duration: ['p(95)<2000', 'p(99)<5000'],
    errors: ['rate<0.05'],             // Less than 5% error rate
    login_duration: ['p(95)<1000'],
    result_submit_duration: ['p(95)<2000'],
    health_check_duration: ['p(99)<500'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:8088';

// Shared state
let authToken = '';

export function setup() {
  // Login once to get auth token
  const loginRes = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
    username: 'admin',
    password: 'admin123',
  }), { headers: { 'Content-Type': 'application/json' } });

  check(loginRes, { 'login succeeded': (r) => r.status === 200 });

  const body = JSON.parse(loginRes.body);
  return { token: body.access_token };
}

export default function(data) {
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${data.token}`,
  };

  group('Health Check', () => {
    const start = Date.now();
    const res = http.get(`${BASE_URL}/healthz`);
    healthCheckDuration.add(Date.now() - start);
    const success = check(res, {
      'health check 200': (r) => r.status === 200,
      'all middleware connected': (r) => {
        const body = JSON.parse(r.body);
        return body.status === 'healthy';
      },
    });
    errorRate.add(!success);
  });

  sleep(0.5);

  group('Authentication Flow', () => {
    const start = Date.now();
    const res = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
      username: 'admin',
      password: 'admin123',
    }), { headers: { 'Content-Type': 'application/json' } });
    loginDuration.add(Date.now() - start);
    const success = check(res, { 'login 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('Dashboard Stats', () => {
    const res = http.get(`${BASE_URL}/dashboard/stats`, { headers });
    const success = check(res, { 'dashboard 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('Election Results List', () => {
    const res = http.get(`${BASE_URL}/results?page=1&per_page=20`, { headers });
    const success = check(res, { 'results 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('Collation Data', () => {
    const res = http.get(`${BASE_URL}/dashboard/collation`, { headers });
    const success = check(res, { 'collation 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('EC8A Result Submission', () => {
    const puCode = `PU-${String(__VU).padStart(3, '0')}-${String(__ITER % 100).padStart(3, '0')}`;
    const start = Date.now();
    const res = http.post(`${BASE_URL}/domain/ec8a/submit`, JSON.stringify({
      election_id: 1,
      polling_unit_code: puCode,
      presiding_officer_id: 'PO-001',
      registered_voters: 500,
      accredited_voters: 400,
      total_votes_polled: 380,
      rejected_ballots: 5,
      total_valid_votes: 375,
      party_results: [
        { party_code: 'APC', votes: 150 },
        { party_code: 'PDP', votes: 120 },
        { party_code: 'LP', votes: 80 },
        { party_code: 'NNPP', votes: 25 },
      ],
      bvas_serial_number: `BVAS-${__VU}`,
      biometric_match_count: 395,
    }), { headers });
    resultSubmitDuration.add(Date.now() - start);
    const success = check(res, {
      'EC8A submit accepted': (r) => r.status === 200 || r.status === 422,
    });
    errorRate.add(!success);
  });

  sleep(0.5);

  group('Middleware Status', () => {
    const res = http.get(`${BASE_URL}/middleware/`, { headers });
    const success = check(res, { 'middleware 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('Geo States', () => {
    const res = http.get(`${BASE_URL}/geo/states`);
    const success = check(res, {
      'geo states 200': (r) => r.status === 200,
      'has states': (r) => JSON.parse(r.body).length > 0,
    });
    errorRate.add(!success);
  });

  sleep(0.3);

  group('BVAS Summary', () => {
    const res = http.get(`${BASE_URL}/bvas/summary`, { headers });
    const success = check(res, { 'bvas 200': (r) => r.status === 200 });
    errorRate.add(!success);
  });

  sleep(1);
}

export function teardown(data) {
  // Logout
  http.post(`${BASE_URL}/auth/logout`, null, {
    headers: { 'Authorization': `Bearer ${data.token}` },
  });
}
