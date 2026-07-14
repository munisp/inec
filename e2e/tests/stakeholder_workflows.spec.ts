import { test, expect } from '@playwright/test';

test.describe('Stakeholder Workflows', () => {
  test.beforeEach(async ({ page }) => {
    // Setup mock API
    await page.route('**/api/auth/login', async route => {
      const { username } = JSON.parse(route.request().postData() || '{}');
      let role = 'citizen';
      if (username === 'admin') role = 'admin';
      if (username === 'observer1') role = 'observer';
      if (username === 'po_lagos_01') role = 'presiding_officer';
      if (username === 'ro_lagos') role = 'returning_officer';
      
      await route.fulfill({
        status: 200,
        json: { token: `mock_token_${role}`, user: { id: 1, username, role, full_name: 'Test User' } }
      });
    });
    
    await page.route('**/api/elections', async route => {
      await route.fulfill({
        status: 200,
        json: [{ id: 1, title: 'Presidential Election 2027', status: 'active', election_type: 'presidential' }]
      });
    });
  });

  test('Admin can view dashboard and command center', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'password');
    await page.click('button[type="submit"]');
    
    await expect(page).toHaveURL('/dashboard');
    await page.click('text=Command Center');
    await expect(page).toHaveURL('/command-center');
  });

  test('Presiding Officer can submit results', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'po_lagos_01');
    await page.fill('input[name="password"]', 'password');
    await page.click('button[type="submit"]');
    
    await expect(page).toHaveURL('/dashboard');
    await page.click('text=Results');
    await expect(page).toHaveURL('/results');
  });

  test('Observer can report incidents', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'observer1');
    await page.fill('input[name="password"]', 'password');
    await page.click('button[type="submit"]');
    
    await expect(page).toHaveURL('/dashboard');
    await page.click('text=Incidents');
    await expect(page).toHaveURL('/incidents');
  });

  test('Returning Officer can view collation', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'ro_lagos');
    await page.fill('input[name="password"]', 'password');
    await page.click('button[type="submit"]');
    
    await expect(page).toHaveURL('/dashboard');
    await page.click('text=Collation');
    await expect(page).toHaveURL('/collation');
  });

  test('Citizen can view public API and TV dashboard', async ({ page }) => {
    await page.goto('/public-api');
    await expect(page.locator('text=Public API Access')).toBeVisible();
    
    await page.goto('/tv-dashboard');
    await expect(page.locator('text=INEC Election Results')).toBeVisible();
  });
});
