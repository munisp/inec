# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: comprehensive_stakeholder.spec.ts >> Comprehensive Stakeholder Workflows >> Role: returning_officer >> should access allowed features and workflows
- Location: tests/comprehensive_stakeholder.spec.ts:57:11

# Error details

```
Error: page.waitForSelector: Test ended.
Call log:
  - waiting for locator('input[name="username"]') to be visible

```

# Test source

```ts
  1   | import { test, expect } from '@playwright/test';
  2   | 
  3   | const ROLES = [
  4   |   'admin',
  5   |   'presiding_officer',
  6   |   'collation_officer',
  7   |   'observer',
  8   |   'returning_officer',
  9   |   'ward_collation_officer',
  10  |   'lga_collation_officer',
  11  |   'state_collation_officer',
  12  |   'public'
  13  | ];
  14  | 
  15  | // Helper to login as a specific role
  16  | async function loginAsRole(page: any, role: string) {
  17  |   await page.goto('/login', { timeout: 30000 });
  18  |   
  19  |   // Wait for login form
> 20  |   await page.waitForSelector('input[name="username"]');
      |              ^ Error: page.waitForSelector: Test ended.
  21  |   
  22  |   // Fill credentials (using seed data patterns)
  23  |   await page.fill('input[name="username"]', `${role}_user`);
  24  |   await page.fill('input[name="password"]', 'password123');
  25  |   
  26  |   // Submit
  27  |   await page.click('button[type="submit"]');
  28  |   
  29  |   // Wait for dashboard
  30  |   await page.waitForURL('/dashboard', { timeout: 30000 });
  31  | }
  32  | 
  33  | // Helper to mock API responses
  34  | async function mockApis(page: any, role: string) {
  35  |   await page.route('**/api/v1/auth/login', async (route) => {
  36  |     const json = {
  37  |       token: 'mock-jwt-token',
  38  |       user: { id: 1, username: `${role}_user`, role, full_name: `Test ${role}` }
  39  |     };
  40  |     await route.fulfill({ json });
  41  |   });
  42  | 
  43  |   await page.route('**/api/v1/elections*', async (route) => {
  44  |     const json = { elections: [], total: 0 };
  45  |     await route.fulfill({ json });
  46  |   });
  47  | 
  48  |   await page.route('**/api/v1/results*', async (route) => {
  49  |     const json = { results: [], total: 0 };
  50  |     await route.fulfill({ json });
  51  |   });
  52  | }
  53  | 
  54  | test.describe('Comprehensive Stakeholder Workflows', () => {
  55  |   for (const role of ROLES) {
  56  |     test.describe(`Role: ${role}`, () => {
  57  |       test('should access allowed features and workflows', async ({ page }) => {
  58  |         await mockApis(page, role);
  59  |         await loginAsRole(page, role);
  60  | 
  61  |         // Verify role in header
  62  |         await expect(page.locator('header')).toContainText(role.replace(/_/g, ' '));
  63  | 
  64  |         // Navigate to Stakeholder Workflow Center
  65  |         await page.goto('/stakeholder-workflows');
  66  |         await expect(page.locator('h1')).toContainText('Stakeholder Workflow Center');
  67  | 
  68  |         // Check if Available Workflows section exists
  69  |         await expect(page.locator('h2', { hasText: 'Available Workflows' })).toBeVisible();
  70  | 
  71  |         // For public, they shouldn't have active workflows
  72  |         if (role === 'public') {
  73  |           await expect(page.locator('text=No workflows available for your role.')).toBeVisible();
  74  |           return;
  75  |         }
  76  | 
  77  |         // Start the first available workflow
  78  |         const startButtons = page.locator('button:has-text("Start Workflow")');
  79  |         if (await startButtons.count() > 0) {
  80  |           await startButtons.first().click();
  81  |           
  82  |           // Verify Active Workflow section appears
  83  |           await expect(page.locator('h2', { hasText: 'Active Workflow:' })).toBeVisible();
  84  |           
  85  |           // Complete all steps
  86  |           let completeButtons = page.locator('button:has-text("Complete")');
  87  |           while (await completeButtons.count() > 0) {
  88  |             await completeButtons.first().click();
  89  |             // Wait for UI update
  90  |             await page.waitForTimeout(100);
  91  |             completeButtons = page.locator('button:has-text("Complete")');
  92  |           }
  93  |           
  94  |           // Finalize workflow
  95  |           await expect(page.locator('text=All steps completed!')).toBeVisible();
  96  |           await page.click('button:has-text("Finalize Workflow")');
  97  |           
  98  |           // Verify it moved to Completed Workflows
  99  |           await expect(page.locator('h2', { hasText: 'Completed Workflows' })).toBeVisible();
  100 |         }
  101 |       });
  102 |     });
  103 |   }
  104 | });
  105 | 
```