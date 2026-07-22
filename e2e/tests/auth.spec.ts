import { test, expect } from '@playwright/test';

const API_URL = process.env.API_URL || 'http://localhost:8088';

test.describe('Authentication Flows', () => {
  test('should show login page when unauthenticated', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('form')).toBeVisible();
    await expect(page.getByPlaceholder(/username/i)).toBeVisible();
    await expect(page.getByPlaceholder(/password/i)).toBeVisible();
  });

  test('should login with valid credentials', async ({ page }) => {
    await page.goto('/#/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'admin123');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/#\/dashboard$/);
  });

  test('should reject invalid credentials', async ({ page }) => {
    await page.goto('/#/login');
    await page.fill('#username', 'invalid');
    await page.fill('#password', 'wrong');
    await page.click('button[type="submit"]');
    await expect(page.getByText(/invalid|unauthorized|failed/i)).toBeVisible();
  });

  test('should redirect to login after logout', async ({ page }) => {
    // Login first
    await page.goto('/#/login');
    await page.fill('#username', 'admin');
    await page.fill('#password', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL(/#\/dashboard$/);

    // Logout
    const logoutBtn = page.getByRole('button', { name: /logout|sign out/i });
    if (await logoutBtn.isVisible()) {
      await logoutBtn.click();
      await expect(page).toHaveURL(/#\/login$/);
    }
  });

  test('should block admin self-registration', async ({ request }) => {
    const resp = await request.post(`${API_URL}/auth/register`, {
      // Password passes policy so the 403 is specifically the role lock.
      data: { username: 'hacker_admin', password: 'Test1234x', full_name: 'Hacker Admin', role: 'admin' },
    });
    expect(resp.status()).toBe(403);
  });
});

test.describe('CSRF Protection', () => {
  test('should reject POST without auth token', async ({ request }) => {
    const resp = await request.post(`${API_URL}/elections`, {
      data: { title: 'Test' },
    });
    expect([401, 403]).toContain(resp.status());
  });
});

test.describe('WAF Protection', () => {
  test('should block SQL injection in request body', async ({ request }) => {
    const resp = await request.post(`${API_URL}/auth/login`, {
      data: { username: "admin' OR 1=1--", password: 'test' },
    });
    expect(resp.status()).toBe(403);
  });

  test('should block XSS in query parameters', async ({ request }) => {
    const resp = await request.get(`${API_URL}/elections?search=<script>alert(1)</script>`);
    expect(resp.status()).toBe(403);
  });
});
