# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: comprehensive_stakeholder.spec.ts >> Comprehensive Stakeholder Workflows >> Role: admin >> should access allowed features and workflows
- Location: tests/comprehensive_stakeholder.spec.ts:57:11

# Error details

```
Test timeout of 60000ms exceeded.
```

```
Error: page.waitForSelector: Test timeout of 60000ms exceeded.
Call log:
  - waiting for locator('input[name="username"]') to be visible

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
      |              ^ Error: page.waitForSelector: Test timeout of 60000ms exceeded.
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