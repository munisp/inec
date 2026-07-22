import { test, expect } from '@playwright/test';

const ROLES = [
  'admin',
  'presiding_officer',
  'collation_officer',
  'observer',
  'returning_officer',
  'ward_collation_officer',
  'lga_collation_officer',
  'state_collation_officer',
  'public'
];

// Helper to login as a specific role
async function loginAsRole(page: any, role: string) {
  await page.goto('/#/login', { timeout: 30000 });
  
  // Wait for login form
  await page.waitForSelector('input[name="username"]');
  
  // Fill credentials (using seed data patterns)
  await page.fill('input[name="username"]', `${role}_user`);
  await page.fill('input[name="password"]', 'password123');
  
  // Submit
  await page.click('button[type="submit"]');
  
  // Wait for dashboard
  await page.waitForURL('**/#/dashboard', { timeout: 30000 });
}

// Helper to mock API responses
async function mockApis(page: any, role: string) {
  const user = { id: 1, username: `${role}_user`, role, full_name: `Test ${role}` };
  await page.route('**/auth/login', async (route) => {
    await route.fulfill({ json: { access_token: 'mock-jwt-token', token_type: 'bearer', user } });
  });
  // The app validates persisted sessions after a full page navigation. Keep the
  // mock contract consistent with the login response rather than bypassing it.
  await page.route('**/auth/me', async (route) => {
    await route.fulfill({ json: user });
  });

  await page.route('**/elections*', async (route) => {
    const json: any[] = [];
    await route.fulfill({ json });
  });

  await page.route('**/results*', async (route) => {
    const json = { results: [], total: 0 };
    await route.fulfill({ json });
  });
}

test.describe('Comprehensive Stakeholder Workflows', () => {
  for (const role of ROLES) {
    test.describe(`Role: ${role}`, () => {
      test('should access allowed features and workflows', async ({ page }) => {
        await mockApis(page, role);
        await loginAsRole(page, role);

        // Verify role in header
        await expect(page.locator('header')).toContainText(role.replace(/_/g, ' '));

        // Navigate to Stakeholder Workflow Center
        await page.goto('/#/stakeholder-workflows');
        await expect(page.getByRole('heading', { name: 'Stakeholder Workflow Center' })).toBeVisible();

        // Check if Available Workflows section exists
        await expect(page.locator('h2', { hasText: 'Available Workflows' })).toBeVisible();

        // For public, they shouldn't have active workflows
        if (role === 'public') {
          await expect(page.locator('text=No workflows available for your role.')).toBeVisible();
          return;
        }

        // Start the first available workflow
        const startButtons = page.locator('button:has-text("Start Workflow")');
        if (await startButtons.count() > 0) {
          await startButtons.first().click();
          
          // Verify Active Workflow section appears
          await expect(page.locator('h2', { hasText: 'Active Workflow:' })).toBeVisible();
          
          // Complete all steps
          let completeButtons = page.locator('button:has-text("Complete")');
          while (await completeButtons.count() > 0) {
            await completeButtons.first().click();
            // Wait for UI update
            await page.waitForTimeout(100);
            completeButtons = page.locator('button:has-text("Complete")');
          }
          
          // Finalize workflow
          await expect(page.locator('text=All steps completed!')).toBeVisible();
          await page.click('button:has-text("Finalize Workflow")');
          
          // Verify it moved to Completed Workflows
          await expect(page.locator('h2', { hasText: 'Completed Workflows' })).toBeVisible();
        }
      });
    });
  }
});
