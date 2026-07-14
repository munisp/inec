# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: stakeholder_workflows.spec.ts >> Stakeholder Workflows >> Citizen can view public API and TV dashboard
- Location: tests/stakeholder_workflows.spec.ts:72:7

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: locator('text=Public API Access')
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 5000ms
  - waiting for locator('text=Public API Access')

```

# Test source

```ts
  1  | import { test, expect } from '@playwright/test';
  2  | 
  3  | test.describe('Stakeholder Workflows', () => {
  4  |   test.beforeEach(async ({ page }) => {
  5  |     // Setup mock API
  6  |     await page.route('**/api/auth/login', async route => {
  7  |       const { username } = JSON.parse(route.request().postData() || '{}');
  8  |       let role = 'citizen';
  9  |       if (username === 'admin') role = 'admin';
  10 |       if (username === 'observer1') role = 'observer';
  11 |       if (username === 'po_lagos_01') role = 'presiding_officer';
  12 |       if (username === 'ro_lagos') role = 'returning_officer';
  13 |       
  14 |       await route.fulfill({
  15 |         status: 200,
  16 |         json: { token: `mock_token_${role}`, user: { id: 1, username, role, full_name: 'Test User' } }
  17 |       });
  18 |     });
  19 |     
  20 |     await page.route('**/api/elections', async route => {
  21 |       await route.fulfill({
  22 |         status: 200,
  23 |         json: [{ id: 1, title: 'Presidential Election 2027', status: 'active', election_type: 'presidential' }]
  24 |       });
  25 |     });
  26 |   });
  27 | 
  28 |   test('Admin can view dashboard and command center', async ({ page }) => {
  29 |     await page.goto('/login');
  30 |     await page.fill('input[name="username"]', 'admin');
  31 |     await page.fill('input[name="password"]', 'password');
  32 |     await page.click('button[type="submit"]');
  33 |     
  34 |     await expect(page).toHaveURL('/dashboard');
  35 |     await page.click('text=Command Center');
  36 |     await expect(page).toHaveURL('/command-center');
  37 |   });
  38 | 
  39 |   test('Presiding Officer can submit results', async ({ page }) => {
  40 |     await page.goto('/login');
  41 |     await page.fill('input[name="username"]', 'po_lagos_01');
  42 |     await page.fill('input[name="password"]', 'password');
  43 |     await page.click('button[type="submit"]');
  44 |     
  45 |     await expect(page).toHaveURL('/dashboard');
  46 |     await page.click('text=Results');
  47 |     await expect(page).toHaveURL('/results');
  48 |   });
  49 | 
  50 |   test('Observer can report incidents', async ({ page }) => {
  51 |     await page.goto('/login');
  52 |     await page.fill('input[name="username"]', 'observer1');
  53 |     await page.fill('input[name="password"]', 'password');
  54 |     await page.click('button[type="submit"]');
  55 |     
  56 |     await expect(page).toHaveURL('/dashboard');
  57 |     await page.click('text=Incidents');
  58 |     await expect(page).toHaveURL('/incidents');
  59 |   });
  60 | 
  61 |   test('Returning Officer can view collation', async ({ page }) => {
  62 |     await page.goto('/login');
  63 |     await page.fill('input[name="username"]', 'ro_lagos');
  64 |     await page.fill('input[name="password"]', 'password');
  65 |     await page.click('button[type="submit"]');
  66 |     
  67 |     await expect(page).toHaveURL('/dashboard');
  68 |     await page.click('text=Collation');
  69 |     await expect(page).toHaveURL('/collation');
  70 |   });
  71 | 
  72 |   test('Citizen can view public API and TV dashboard', async ({ page }) => {
  73 |     await page.goto('/public-api');
> 74 |     await expect(page.locator('text=Public API Access')).toBeVisible();
     |                                                          ^ Error: expect(locator).toBeVisible() failed
  75 |     
  76 |     await page.goto('/tv-dashboard');
  77 |     await expect(page.locator('text=INEC Election Results')).toBeVisible();
  78 |   });
  79 | });
  80 | 
```