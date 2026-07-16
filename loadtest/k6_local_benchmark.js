// K6 Capacity Test — INEC Platform (No rate limiter stress)
// Clean test measuring raw throughput/latency under load.

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Counter, Trend } from 'k6/metrics';

const errorRate = new Rate('errors');
const responseTime = new Trend('response_time_ms');
const resultSubmissions = new Counter('result_submissions');
const readOps = new Counter('read_operations');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8088';

export const options = {
  scenarios: {
    health_monitoring: {
      executor: 'constant-arrival-rate',
      rate: 20,
      timeUnit: '1s',
      duration: '90s',
      preAllocatedVUs: 5,
      exec: 'healthCheck',
    },
    read_queries: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '30s', target: 50 },
        { duration: '30s', target: 100 },
        { duration: '30s', target: 50 },
      ],
      exec: 'readQueries',
    },
    write_submissions: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '30s', target: 30 },
        { duration: '30s', target: 60 },
        { duration: '30s', target: 30 },
      ],
      exec: 'writeSubmissions',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<2000'],
    errors: ['rate<0.05'],
    http_req_failed: ['rate<0.05'],
    'http_req_duration{name:healthz}': ['p(99)<100'],
    'http_req_duration{name:dashboard}': ['p(95)<500'],
    'http_req_duration{name:submit_result}': ['p(95)<1000'],
  },
};

const STATES = ['FC','LA','KN','RV','OG','AN','EN','OY','KD','BO','AD','BA','BE','CR','DE','EB','ED','EK','GO','IM','JI','KB','KE','KO','KW','NA','NI','ON','OS','OT','PL','SO','TA','YO','ZA','AB','AK'];

export function setup() {
  const res = http.post(`${BASE_URL}/auth/login`, JSON.stringify({
    username: 'admin', password: 'admin123',
  }), { headers: { 'Content-Type': 'application/json' } });
  if (res.status !== 200) {
    console.error(`Login failed: ${res.status} ${res.body}`);
    return { token: '' };
  }
  const body = JSON.parse(res.body);
  console.log('Setup: token obtained successfully');
  return { token: body.access_token };
}

function authHeaders(data) {
  return { 'Authorization': `Bearer ${data.token}` };
}

export function healthCheck() {
  group('health_monitoring', () => {
    let res = http.get(`${BASE_URL}/healthz`, { tags: { name: 'healthz' } });
    const ok = check(res, { 'healthz 200': (r) => r.status === 200 });
    if (!ok) errorRate.add(1);
    responseTime.add(res.timings.duration);

    res = http.get(`${BASE_URL}/metrics`, { tags: { name: 'metrics' } });
    check(res, { 'metrics 200': (r) => r.status === 200 });
    responseTime.add(res.timings.duration);
  });
}

export function readQueries(data) {
  const headers = authHeaders(data);

  group('read_queries', () => {
    let res = http.get(`${BASE_URL}/dashboard/stats`, { headers, tags: { name: 'dashboard' } });
    let ok = check(res, { 'dashboard 200': (r) => r.status === 200 });
    if (!ok) errorRate.add(1);
    responseTime.add(res.timings.duration);
    readOps.add(1);

    res = http.get(`${BASE_URL}/elections`, { headers, tags: { name: 'elections' } });
    ok = check(res, { 'elections 200': (r) => r.status === 200 });
    if (!ok) errorRate.add(1);
    responseTime.add(res.timings.duration);
    readOps.add(1);

    res = http.get(`${BASE_URL}/results`, { headers, tags: { name: 'results' } });
    ok = check(res, { 'results ok': (r) => [200, 429].includes(r.status) });
    if (!ok) errorRate.add(1);
    responseTime.add(res.timings.duration);
    readOps.add(1);

    res = http.get(`${BASE_URL}/architecture/health`, { headers, tags: { name: 'arch_health' } });
    ok = check(res, { 'arch health 200': (r) => r.status === 200 });
    if (!ok) errorRate.add(1);
    responseTime.add(res.timings.duration);
    readOps.add(1);

    sleep(0.3);
  });
}

export function writeSubmissions(data) {
  const headers = authHeaders(data);
  const state = STATES[Math.floor(Math.random() * STATES.length)];
  const puCode = `${state}/${String(Math.floor(Math.random() * 44) + 1).padStart(2, '0')}/${String(Math.floor(Math.random() * 774) + 1).padStart(3, '0')}/${String(Math.floor(Math.random() * 9999) + 1).padStart(4, '0')}`;

  group('write_submissions', () => {
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

    const res = http.post(`${BASE_URL}/results`, payload, {
      headers: Object.assign({}, headers, {
        'Content-Type': 'application/json',
        'X-Idempotency-Key': `${puCode}-${Date.now()}-${Math.random()}`,
      }),
      tags: { name: 'submit_result' },
    });
    responseTime.add(res.timings.duration);
    const ok = check(res, { 'result submitted': (r) => [200, 201, 409, 429].includes(r.status) });
    if (!ok) errorRate.add(1);
    resultSubmissions.add(1);

    sleep(0.1);
  });
}
