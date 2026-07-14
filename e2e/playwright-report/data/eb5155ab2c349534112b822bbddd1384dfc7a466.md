# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: stakeholder_workflows.spec.ts >> Stakeholder Workflows >> Observer can report incidents
- Location: tests/stakeholder_workflows.spec.ts:50:7

# Error details

```
Test timeout of 60000ms exceeded.
```

```
Error: page.fill: Test timeout of 60000ms exceeded.
Call log:
  - waiting for locator('input[name="username"]')

```

# Page snapshot

```yaml
- generic [ref=e4]:
  - generic [ref=e5]:
    - img [ref=e7]
    - heading "INEC Election Platform" [level=1] [ref=e10]
    - paragraph [ref=e11]: Blockchain-Based Election Results System v4.0
  - generic [ref=e12]:
    - generic [ref=e13]:
      - generic [ref=e14]: Sign In
      - generic [ref=e15]: Access the election management platform
    - generic [ref=e16]:
      - generic [ref=e17]:
        - generic [ref=e18]:
          - text: Username
          - textbox "Username" [ref=e19]:
            - /placeholder: Enter username
        - generic [ref=e20]:
          - text: Password
          - textbox "Password" [ref=e21]:
            - /placeholder: Enter password
        - button "Sign In" [ref=e22] [cursor=pointer]
      - generic [ref=e23]:
        - paragraph [ref=e24]: "Quick access (demo accounts):"
        - generic [ref=e25]:
          - button "Administrator Full system access" [ref=e26] [cursor=pointer]:
            - img [ref=e28]
            - generic [ref=e30]:
              - paragraph [ref=e31]: Administrator
              - paragraph [ref=e32]: Full system access
          - button "Presiding Officer Upload & manage results" [ref=e33] [cursor=pointer]:
            - img [ref=e35]
            - generic [ref=e38]:
              - paragraph [ref=e39]: Presiding Officer
              - paragraph [ref=e40]: Upload & manage results
          - button "Election Observer View & verify results" [ref=e41] [cursor=pointer]:
            - img [ref=e43]
            - generic [ref=e46]:
              - paragraph [ref=e47]: Election Observer
              - paragraph [ref=e48]: View & verify results
  - generic [ref=e49]:
    - paragraph [ref=e50]: Independent National Electoral Commission
    - paragraph [ref=e51]: Federal Republic of Nigeria
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
> 52 |     await page.fill('input[name="username"]', 'observer1');
     |                ^ Error: page.fill: Test timeout of 60000ms exceeded.
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
  74 |     await expect(page.locator('text=Public API Access')).toBeVisible();
  75 |     
  76 |     await page.goto('/tv-dashboard');
  77 |     await expect(page.locator('text=INEC Election Results')).toBeVisible();
  78 |   });
  79 | });
  80 | 
```