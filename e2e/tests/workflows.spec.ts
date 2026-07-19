import { test, expect } from '@playwright/test';

const API_URL = process.env.API_URL || 'http://localhost:8088';

// EMS election-workflow lifecycle: create -> list -> inspect phases -> advance.
// Contracts verified against inec-go-backend/ems.go.
test.describe('EMS Workflows', () => {
  let adminToken: string;
  let observerToken: string;
  let electionId: number;
  let workflowId: number;

  test.beforeAll(async ({ request }) => {
    const adminResp = await request.post(`${API_URL}/auth/login`, {
      data: { username: 'admin', password: 'admin123' },
    });
    adminToken = (await adminResp.json()).access_token;

    const obsResp = await request.post(`${API_URL}/auth/login`, {
      data: { username: 'observer', password: 'observer123' },
    });
    observerToken = (await obsResp.json()).access_token;

    const elResp = await request.post(`${API_URL}/elections`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      data: {
        title: 'Workflow E2E Election',
        election_type: 'gubernatorial',
        election_date: '2027-11-20',
        description: 'E2E workflow lifecycle',
      },
    });
    expect(elResp.status()).toBe(200);
    electionId = (await elResp.json()).id;
  });

  test('observer cannot create a workflow (403)', async ({ request }) => {
    const resp = await request.post(`${API_URL}/ems/workflows`, {
      headers: { Authorization: `Bearer ${observerToken}` },
      data: { election_id: electionId, workflow_type: 'full_election' },
    });
    expect(resp.status()).toBe(403);
  });

  test('admin creates a workflow with 7 seeded phases (201)', async ({ request }) => {
    const resp = await request.post(`${API_URL}/ems/workflows`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      data: { election_id: electionId, workflow_type: 'full_election' },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.id).toBeGreaterThan(0);
    workflowId = body.id;
  });

  test('workflow appears in the election workflow list', async ({ request }) => {
    const resp = await request.get(`${API_URL}/ems/workflows?election_id=${electionId}`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(Array.isArray(body)).toBe(true);
    expect(body.some((w: any) => w.id === workflowId)).toBe(true);
  });

  test('workflow detail exposes all 7 phases starting at planning', async ({ request }) => {
    const resp = await request.get(`${API_URL}/ems/workflows/${workflowId}`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.current_phase).toBe('planning');
    expect(body.status).toBe('active');
    expect(body.phases.length).toBe(7);
    expect(body.phases.map((p: any) => p.phase)).toEqual([
      'planning', 'registration', 'accreditation', 'voting',
      'collation', 'declaration', 'certification',
    ]);
  });

  test('advance moves planning -> registration', async ({ request }) => {
    const resp = await request.post(`${API_URL}/ems/workflows/${workflowId}/advance`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body.previous_phase).toBe('planning');
    expect(body.current_phase).toBe('registration');
  });

  test('unknown workflow returns 404', async ({ request }) => {
    const resp = await request.get(`${API_URL}/ems/workflows/999999999`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    expect(resp.status()).toBe(404);
  });
});
