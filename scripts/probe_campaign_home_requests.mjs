import { chromium } from 'playwright';

const baseUrl = 'https://campaign.inec.servers.upi.dev';
const browser = await chromium.launch({
  headless: true,
  executablePath: '/usr/bin/chromium',
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
});
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const responses = [];
page.on('response', async (response) => {
  if (response.status() >= 400) {
    let snippet = '';
    try { snippet = (await response.text()).slice(0, 600); } catch {}
    const requestCookie = response.request().headers()['cookie'] ?? '';
    responses.push({ status: response.status(), url: response.url(), hasSessionCookie: requestCookie.includes('app_session_id='), snippet });
  }
});
page.on('console', (message) => {
  if (message.type() === 'error') console.error('[console]', message.text());
});
await page.goto(`${baseUrl}/login`, { waitUntil: 'commit', timeout: 60_000 });
await page.waitForLoadState('domcontentloaded', { timeout: 20_000 }).catch(() => undefined);
await page.locator('#username').fill(process.env.CAMPAIGN_USERNAME ?? '');
await page.locator('#password').fill(process.env.CAMPAIGN_PASSWORD ?? '');
const loginResponsePromise = page.waitForResponse(response => response.url().endsWith('/api/login') && response.request().method() === 'POST');
await page.getByRole('button', { name: /sign in/i }).click();
const loginResponse = await loginResponsePromise;
const loginSetCookie = loginResponse.headers()['set-cookie'] ?? null;
await page.waitForURL(`${baseUrl}/`);
await page.waitForTimeout(2500);
const cookies = await page.context().cookies(baseUrl);
console.log(JSON.stringify({ url: page.url(), loginStatus: loginResponse.status(), loginSetCookie, cookies: cookies.map(({ name, domain, path, secure, sameSite, httpOnly }) => ({ name, domain, path, secure, sameSite, httpOnly })), responses }, null, 2));
await browser.close();
