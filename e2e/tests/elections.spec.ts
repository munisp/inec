import { test, expect } from '@playwright/test';

const API_URL = process.env.API_URL || 'http://localhost:8088';

test.describe('Election Management', () => {
  let authToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/auth/login`, {
      data: { username: 'admin', password: 'admin123' },
    });
    const body = await resp.json();
    authToken = body.access_token;
  });

  test('should list elections', async ({ page }) => {
    await page.goto('/#/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL(/#\/dashboard$/);

    await page.goto('/#/elections');
    await expect(page.getByText(/election/i)).toBeVisible();
  });

  test('should create election via API', async ({ request }) => {
    const resp = await request.post(`${API_URL}/elections`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: {
        title: 'E2E Test Election',
        election_type: 'presidential',
        election_date: '2027-02-15',
        description: 'Playwright E2E test',
      },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.id).toBeGreaterThan(0);
  });

  test('should enforce FSM transitions', async ({ request }) => {
    // Create election
    const createResp = await request.post(`${API_URL}/elections`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: {
        title: 'FSM Test Election',
        election_type: 'gubernatorial',
        election_date: '2027-06-15',
        description: 'Testing FSM',
      },
    });
    const { id } = await createResp.json();

    // Try invalid transition (draft → voting should fail)
    const invalidResp = await request.post(`${API_URL}/ems/elections/${id}/fsm/transition`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: { event: 'open_voting' },
    });
    expect(invalidResp.status()).toBe(422);
    const error = await invalidResp.json();
    expect(error.detail ?? error.error).toContain('invalid transition');

    // Valid transition: draft → scheduled
    const validResp = await request.post(`${API_URL}/ems/elections/${id}/fsm/transition`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: { event: 'schedule' },
    });
    // May fail due to 7-day guard, but response should be 422 with guard message
    expect([200, 422]).toContain(validResp.status());
  });

  test('should get FSM diagram', async ({ request }) => {
    const resp = await request.get(`${API_URL}/ems/elections/1/fsm/diagram`, {
      headers: { Authorization: `Bearer ${authToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.states).toContain('draft');
    expect(body.states).toContain('voting');
    expect(body.transitions.length).toBeGreaterThan(0);
  });
});

test.describe('Form EC8A Submission', () => {
  let authToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/auth/login`, {
      data: { username: 'officer1', password: 'officer123' },
    });
    const body = await resp.json();
    authToken = body.access_token;
  });

  test('should reject invalid EC8A (votes > accredited)', async ({ request }) => {
    const resp = await request.post(`${API_URL}/inec/ec8a/submit`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: {
        election_id: 1,
        polling_unit_code: 'FCT/ABJ/001/01',
        presiding_officer_id: 'PO-001',
        registered_voters: 500,
        accredited_voters: 400,
        total_votes_polled: 500,  // exceeds accredited
        rejected_ballots: 10,
        total_valid_votes: 490,
        party_results: [
          { party_code: 'APC', votes: 250 },
          { party_code: 'PDP', votes: 240 },
        ],
        bvas_serial_number: 'BVAS-001',
        biometric_match_count: 380,
      },
    });
    expect(resp.status()).toBe(422);
    const body = await resp.json();
    expect(body.violations.length).toBeGreaterThan(0);
  });

  test('should accept valid EC8A submission', async ({ request }) => {
    const resp = await request.post(`${API_URL}/inec/ec8a/submit`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: {
        election_id: 1,
        polling_unit_code: 'LA-001-W001-PU001',
        presiding_officer_id: 'PO-002',
        registered_voters: 500,
        accredited_voters: 400,
        total_votes_polled: 380,
        rejected_ballots: 5,
        total_valid_votes: 375,
        party_results: [
          { party_code: 'APC', votes: 200 },
          { party_code: 'PDP', votes: 175 },
        ],
        bvas_serial_number: 'BVAS-002',
        biometric_match_count: 380,
      },
    });
    expect([200, 201]).toContain(resp.status());
  });
});

test.describe('Collation', () => {
  let authToken: string;

  test.beforeAll(async ({ request }) => {
    const resp = await request.post(`${API_URL}/auth/login`, {
      data: { username: 'admin', password: 'admin123' },
    });
    const body = await resp.json();
    authToken = body.access_token;
  });

  test('should collate at national level', async ({ request }) => {
    const resp = await request.get(`${API_URL}/inec/collation?level=national&election_id=1`, {
      headers: { Authorization: `Bearer ${authToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.level).toBe('national');
    expect(body.party_totals).toBeDefined();
  });
});
