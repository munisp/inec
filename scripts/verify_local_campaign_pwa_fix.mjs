import { chromium } from 'playwright';

const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
const page = await context.newPage();
const requests = [];
page.on('request', (request) => requests.push(request.url()));
await page.route('**/api/trpc/**', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ result: { data: { json: null } } }]) }));

await page.goto('http://127.0.0.1:4174/', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(700);
const privateRoute = await page.evaluate(() => {
  const nav = document.querySelector('nav[aria-label="Campaign navigation"]');
  const navRect = nav?.getBoundingClientRect();
  const shell = nav?.previousElementSibling;
  const shellPaddingBottom = shell ? Number.parseFloat(getComputedStyle(shell).paddingBottom) : 0;
  return {
    navigationVisible: Boolean(navRect && navRect.height > 0),
    navigationHeight: navRect?.height ?? 0,
    shellPaddingBottom,
    documentOverflow: document.documentElement.scrollWidth > window.innerWidth + 1,
  };
});
if (!privateRoute.navigationVisible || privateRoute.navigationHeight < 50 || privateRoute.shellPaddingBottom < privateRoute.navigationHeight - 2 || privateRoute.documentOverflow) {
  throw new Error(`Campaign mobile shell validation failed: ${JSON.stringify(privateRoute)}`);
}

await page.goto('http://127.0.0.1:4174/login', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(350);
const publicRoute = await page.evaluate(() => {
  const nav = document.querySelector('nav[aria-label="Campaign navigation"]');
  const navRect = nav?.getBoundingClientRect();
  return { navigationVisible: Boolean(navRect && navRect.height > 0) };
});
if (publicRoute.navigationVisible) throw new Error('Campaign navigation should not obscure the public login route.');

await page.goto('http://127.0.0.1:4174/polling-units', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(700);
const unresolvedAnalyticsRequest = requests.some((url) => url.includes('%VITE_ANALYTICS_ENDPOINT%'));
const undefinedMapKeyRequest = requests.some((url) => /maps\/api\/js\?.*key=(undefined|null)/.test(url));
if (unresolvedAnalyticsRequest || undefinedMapKeyRequest) {
  throw new Error(`Invalid optional-service requests detected: ${JSON.stringify({ unresolvedAnalyticsRequest, undefinedMapKeyRequest })}`);
}

console.log(JSON.stringify({ privateRoute, publicRoute, unresolvedAnalyticsRequest, undefinedMapKeyRequest }, null, 2));
await browser.close();
