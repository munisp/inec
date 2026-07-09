// k6 election-day load test for the INEC platform.
//
// Models a realistic election-day traffic mix: heavy dashboard/result reads with
// a smaller share of authenticated result submissions. Ramps to a high virtual
// user count and asserts SLOs via thresholds so the test FAILS (non-zero exit)
// when latency or error budgets are breached — suitable for CI gating.
//
// Usage:
//   k6 run -e BASE_URL=http://localhost:8099 election_load_test.js
//   k6 run -e BASE_URL=https://api.inec.gov.ng -e SUBMIT_TOKEN=$JWT election_load_test.js
//
// Scale up with:  k6 run --vus 2000 --duration 10m ...

import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8099';
const SUBMIT_TOKEN = __ENV.SUBMIT_TOKEN || '';

const errorRate = new Rate('errors');
const readLatency = new Trend('read_latency', true);
const writeLatency = new Trend('write_latency', true);

export const options = {
  scenarios: {
    // 1. Sustained read load (voters/observers refreshing dashboards).
    reads: {
      executor: 'ramping-vus',
      exec: 'readScenario',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 200 },
        { duration: '3m', target: 800 },
        { duration: '5m', target: 800 },
        { duration: '1m', target: 0 },
      ],
    },
    // 2. Result submissions from polling units (write path), lower rate.
    submissions: {
      executor: 'constant-arrival-rate',
      exec: 'submitScenario',
      rate: 50, // 50 submissions/sec
      timeUnit: '1s',
      duration: '10m',
      preAllocatedVUs: 100,
      maxVUs: 400,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],          // < 1% request failures
    errors: ['rate<0.01'],
    read_latency: ['p(95)<800'],             // 95% of reads under 800ms
    write_latency: ['p(95)<1500'],           // 95% of writes under 1.5s
    http_req_duration: ['p(99)<3000'],
  },
};

const READ_ENDPOINTS = ['/health', '/dashboard', '/results', '/elections', '/polling-units'];

export function readScenario() {
  group('reads', () => {
    const path = READ_ENDPOINTS[Math.floor(Math.random() * READ_ENDPOINTS.length)];
    const res = http.get(`${BASE_URL}${path}`);
    readLatency.add(res.timings.duration);
    const ok = check(res, { 'read status 200': (r) => r.status === 200 });
    errorRate.add(!ok);
  });
  sleep(Math.random() * 2);
}

export function submitScenario() {
  if (!SUBMIT_TOKEN) {
    // No token supplied: exercise the auth-rejection path instead so the write
    // scenario still generates representative load without forging results.
    const res = http.post(`${BASE_URL}/results/submit`, JSON.stringify(samplePayload()), {
      headers: { 'Content-Type': 'application/json' },
    });
    writeLatency.add(res.timings.duration);
    check(res, { 'unauth rejected (401/403)': (r) => r.status === 401 || r.status === 403 });
    return;
  }
  const res = http.post(`${BASE_URL}/results/submit`, JSON.stringify(samplePayload()), {
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${SUBMIT_TOKEN}` },
  });
  writeLatency.add(res.timings.duration);
  const ok = check(res, { 'submit accepted': (r) => r.status === 200 || r.status === 201 });
  errorRate.add(!ok);
}

function samplePayload() {
  const accredited = 400 + Math.floor(Math.random() * 600);
  const total = Math.floor(accredited * (0.4 + Math.random() * 0.5));
  return {
    polling_unit_code: `PU-${Math.floor(Math.random() * 900000) + 100000}`,
    election_id: 1,
    accredited_voters: accredited,
    total_votes: total,
    party_votes: { APC: Math.floor(total * 0.5), PDP: Math.floor(total * 0.3), LP: Math.floor(total * 0.2) },
  };
}
