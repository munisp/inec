import { test, expect } from '@playwright/test';

const API_URL = process.env.API_URL || 'http://localhost:8088';

test.describe('Observer Monitoring', () => {
  let observerToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/login`, {
      data: { username: 'observer', password: 'observer123' },
    });
    const body = await resp.json();
    observerToken = body.access_token;
  });

  test('should check-in with geofence validation', async ({ request }) => {
    const resp = await request.post(`${API_URL}/observer/check-in`, {
      headers: { Authorization: `Bearer ${observerToken}` },
      data: {
        polling_unit_code: 'FCT/ABJ/001/01',
        latitude: 9.0579,
        longitude: 7.4951,
        device_id: 'TEST-DEVICE-001',
      },
    });
    // Either accepted (within 500m) or rejected (outside geofence)
    expect([200, 403]).toContain(resp.status());
  });

  test('should stream SSE events', async ({ request }) => {
    const resp = await request.get(`${API_URL}/observer/stream?party=APC`);
    expect(resp.status()).toBe(200);
    expect(resp.headers()['content-type']).toContain('text/event-stream');
  });

  test('should get party dashboard', async ({ request }) => {
    const resp = await request.get(`${API_URL}/observer/party-dashboard?party=APC`, {
      headers: { Authorization: `Bearer ${observerToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.party).toBe('APC');
  });

  test('should create alert rule', async ({ request }) => {
    const resp = await request.post(`${API_URL}/observer/alerts`, {
      headers: { Authorization: `Bearer ${observerToken}` },
      data: {
        type: 'result_submitted',
        filter_state: 'FCT',
        filter_party: 'APC',
      },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.id).toBeDefined();
  });

  test('should upload observer report', async ({ request }) => {
    // Create a simple test image buffer (1x1 white PNG)
    const pngHeader = Buffer.from([
      0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
      0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
      0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
    ]);

    const resp = await request.post(`${API_URL}/observer/reports`, {
      headers: { Authorization: `Bearer ${observerToken}` },
      multipart: {
        polling_unit_code: 'FCT/ABJ/001/01',
        description: 'E2E test report',
        photo: { name: 'test.png', mimeType: 'image/png', buffer: pngHeader },
      },
    });
    expect([200, 201]).toContain(resp.status());
  });
});

test.describe('GPS Spoofing Detection', () => {
  let officerToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/login`, {
      data: { username: 'officer', password: 'officer123' },
    });
    const body = await resp.json();
    officerToken = body.access_token;
  });

  test('should detect teleportation (impossible velocity)', async ({ request }) => {
    // First position
    await request.post(`${API_URL}/geo/spoof-check`, {
      headers: { Authorization: `Bearer ${officerToken}` },
      data: {
        device_id: 'SPOOF-TEST-001',
        lat: 9.0579,
        lng: 7.4951,
        accuracy: 5.0,
        timestamp: new Date(Date.now() - 5000).toISOString(),
      },
    });

    // Second position 1000km away 5 seconds later
    const resp = await request.post(`${API_URL}/geo/spoof-check`, {
      headers: { Authorization: `Bearer ${officerToken}` },
      data: {
        device_id: 'SPOOF-TEST-001',
        lat: 6.5244,
        lng: 3.3792,
        accuracy: 5.0,
        timestamp: new Date().toISOString(),
        meta: { is_mock_provider: false },
      },
    });
    expect(resp.status()).toBe(403);
    const body = await resp.json();
    expect(body.spoofing_analysis.is_spoofed).toBe(true);
  });

  test('should detect mock provider', async ({ request }) => {
    const resp = await request.post(`${API_URL}/geo/spoof-check`, {
      headers: { Authorization: `Bearer ${officerToken}` },
      data: {
        device_id: 'MOCK-TEST-001',
        lat: 9.0579,
        lng: 7.4951,
        accuracy: 0,
        meta: { is_mock_provider: true },
      },
    });
    expect(resp.status()).toBe(403);
    const body = await resp.json();
    expect(body.spoofing_analysis.mock_provider).toBe(true);
  });
});

test.describe('Webhook Subscriptions', () => {
  let adminToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/login`, {
      data: { username: 'admin', password: 'admin123' },
    });
    const body = await resp.json();
    adminToken = body.access_token;
  });

  test('should create webhook subscription', async ({ request }) => {
    const resp = await request.post(`${API_URL}/api/v1/webhooks`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      data: {
        url: 'https://example.com/webhook',
        events: ['result.submitted', 'election.finalized'],
        secret: 'test-secret-123',
      },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.id).toBeGreaterThan(0);
    expect(body.active).toBe(true);
  });

  test('should list webhooks', async ({ request }) => {
    const resp = await request.get(`${API_URL}/api/v1/webhooks`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.webhooks.length).toBeGreaterThan(0);
  });
});

test.describe('Dashboard SSE', () => {
  test('should stream dashboard updates', async ({ request }) => {
    const resp = await request.get(`${API_URL}/dashboard/stream`);
    expect(resp.status()).toBe(200);
    expect(resp.headers()['content-type']).toContain('text/event-stream');
  });
});
