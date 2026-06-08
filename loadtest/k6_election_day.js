// K6 Distributed Load Test — INEC Election Day Simulation
// Usage: k6 run --vus 500 --duration 10m loadtest/k6_election_day.js
// For distributed: k6 run --execution-segment "0:1/2" ... (split across nodes)

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Counter, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const resultSubmissions = new Counter('result_submissions');
const collationRequests = new Counter('collation_requests');
const biometricVerifications = new Counter('biometric_verifications');
const responseTime = new Trend('response_time_ms');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8088';
const STATES = ['FC','LA','KN','RV','OG','AN','EN','OY','KD','BO','AD','BA','BE','CR','DE','EB','ED','EK','GO','IM','JI','KB','KE','KO','KW','NA','NI','ON','OS','OT','PL','SO','TA','YO','ZA','AB','AK'];

export const options = {
  scenarios: {
    // Scenario 1: Result submissions from 48K+ polling units
    result_submission: {
      executor: 'ramping-vus',
      startVUs: 50,
      stages: [
        { duration: '2m', target: 200 },  // Ramp up (early results)
        { duration: '5m', target: 500 },  // Peak (all PUs reporting)
        { duration: '2m', target: 100 },  // Wind down
        { duration: '1m', target: 0 },    // Drain
      ],
      exec: 'submitResults',
    },
    // Scenario 2: Collation queries from officials + public
    collation_queries: {
      executor: 'constant-vus',
      vus: 100,
      duration: '10m',
      exec: 'queryCollation',
    },
    // Scenario 3: Real-time tracking (officials + observers)
    tracking_updates: {
      executor: 'constant-vus',
      vus: 50,
      duration: '10m',
      exec: 'trackingUpdate',
    },
    // Scenario 4: Health checks (monitoring systems)
    health_monitoring: {
      executor: 'constant-arrival-rate',
      rate: 10,
      timeUnit: '1s',
      duration: '10m',
      preAllocatedVUs: 10,
      exec: 'healthCheck',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<2000'],
    errors: ['rate<0.01'],
    http_req_failed: ['rate<0.01'],
  },
};

function getAuthToken() {
  const loginRes = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
    username: 'admin',
    password: 'admin123',
  }), { headers: { 'Content-Type': 'application/json' } });
  if (loginRes.status === 200) {
    const body = JSON.parse(loginRes.body);
    return body.access_token || body.token || '';
  }
  return '';
}

let authToken = '';

export function setup() {
  authToken = getAuthToken();
  return { token: authToken };
}

export function submitResults(data) {
  const token = data.token;
  const state = STATES[Math.floor(Math.random() * STATES.length)];
  const puCode = `${state}/${String(Math.floor(Math.random() * 44) + 1).padStart(2, '0')}/${String(Math.floor(Math.random() * 774) + 1).padStart(3, '0')}/${String(Math.floor(Math.random() * 9999) + 1).padStart(4, '0')}`;

  group('result_submission', () => {
    const payload = JSON.stringify({
      election_id: 1,
      polling_unit_code: puCode,
      accredited_voters: Math.floor(Math.random() * 500) + 100,
      total_votes_cast: Math.floor(Math.random() * 400) + 100,
      rejected_ballots: Math.floor(Math.random() * 10),
      party_scores: [
        { party_code: 'APC', votes: Math.floor(Math.random() * 200) },
        { party_code: 'PDP', votes: Math.floor(Math.random() * 150) },
        { party_code: 'LP', votes: Math.floor(Math.random() * 100) },
        { party_code: 'NNPP', votes: Math.floor(Math.random() * 50) },
      ],
    });

    const headers = {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
      'X-Idempotency-Key': `${puCode}-${Date.now()}`,
    };

    const res = http.post(`${BASE_URL}/results`, payload, { headers, tags: { name: 'POST /results' } });
    responseTime.add(res.timings.duration);
    resultSubmissions.add(1);

    const success = check(res, {
      'result submitted (200/201/409)': (r) => [200, 201, 409].includes(r.status),
      'response time < 1s': (r) => r.timings.duration < 1000,
    });
    errorRate.add(!success);
  });

  sleep(Math.random() * 2 + 0.5);
}

export function queryCollation(data) {
  const token = data.token;
  const state = STATES[Math.floor(Math.random() * STATES.length)];

  group('collation_query', () => {
    // National results
    const natRes = http.get(`${BASE_URL}/collation/national?election_id=1`, {
      headers: { 'Authorization': `Bearer ${token}` },
      tags: { name: 'GET /collation/national' },
    });
    responseTime.add(natRes.timings.duration);
    collationRequests.add(1);
    check(natRes, { 'national collation 200': (r) => r.status === 200 });

    // State results
    const stateRes = http.get(`${BASE_URL}/collation/state?election_id=1&state_code=${state}`, {
      headers: { 'Authorization': `Bearer ${token}` },
      tags: { name: 'GET /collation/state' },
    });
    responseTime.add(stateRes.timings.duration);
    collationRequests.add(1);
    check(stateRes, { 'state collation 200': (r) => r.status === 200 });
  });

  sleep(Math.random() * 3 + 1);
}

export function trackingUpdate(data) {
  const token = data.token;
  const state = STATES[Math.floor(Math.random() * STATES.length)];
  const lat = 6.0 + Math.random() * 7;
  const lng = 3.0 + Math.random() * 12;

  group('tracking', () => {
    const payload = JSON.stringify({
      officer_id: `OFF-${Math.floor(Math.random() * 50000)}`,
      latitude: lat,
      longitude: lng,
      accuracy: Math.random() * 10 + 5,
      battery_pct: Math.floor(Math.random() * 100),
      activity: ['voting', 'collating', 'transit', 'idle'][Math.floor(Math.random() * 4)],
    });

    const res = http.post(`${BASE_URL}/geo/tracking/update`, payload, {
      headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
      tags: { name: 'POST /geo/tracking/update' },
    });
    responseTime.add(res.timings.duration);
    check(res, { 'tracking update 200': (r) => r.status === 200 });

    // Query nearby officials
    const nearby = http.get(`${BASE_URL}/geo/tracking/officials?active_minutes=30`, {
      headers: { 'Authorization': `Bearer ${token}` },
      tags: { name: 'GET /geo/tracking/officials' },
    });
    check(nearby, { 'tracking query 200': (r) => r.status === 200 });
  });

  sleep(Math.random() * 5 + 2);
}

export function healthCheck() {
  group('health', () => {
    const res = http.get(`${BASE_URL}/healthz`, { tags: { name: 'GET /healthz' } });
    check(res, { 'healthz 200': (r) => r.status === 200 });

    const ready = http.get(`${BASE_URL}/readiness`, { tags: { name: 'GET /readiness' } });
    check(ready, { 'readiness 200': (r) => r.status === 200 });
  });

  sleep(0.5);
}

export function handleSummary(data) {
  const summary = {
    total_requests: data.metrics.http_reqs.values.count,
    avg_response_ms: data.metrics.http_req_duration.values.avg.toFixed(2),
    p95_response_ms: data.metrics.http_req_duration.values['p(95)'].toFixed(2),
    p99_response_ms: data.metrics.http_req_duration.values['p(99)'].toFixed(2),
    error_rate: (data.metrics.http_req_failed.values.rate * 100).toFixed(2) + '%',
    results_submitted: data.metrics.result_submissions ? data.metrics.result_submissions.values.count : 0,
    collation_queries: data.metrics.collation_requests ? data.metrics.collation_requests.values.count : 0,
  };

  return {
    stdout: JSON.stringify(summary, null, 2),
    'loadtest/results.json': JSON.stringify(data, null, 2),
  };
}
