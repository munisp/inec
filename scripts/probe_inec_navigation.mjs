import { chromium } from 'playwright';

const baseUrl = 'https://inec.servers.upi.dev';
const browser = await chromium.launch({
  headless: true,
  executablePath: '/usr/bin/chromium',
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
});
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage();
await page.goto(`${baseUrl}/#/login`, { waitUntil: 'domcontentloaded' });
await page.locator('#username').fill(process.env.INEC_USERNAME ?? '');
await page.locator('#password').fill(process.env.INEC_PASSWORD ?? '');
await page.getByRole('button', { name: 'Sign In', exact: true }).click();
await page.waitForURL(/#\/dashboard/);

async function inspectNavigation(label) {
  let lastError;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.waitForLoadState('domcontentloaded', { timeout: 5_000 }).catch(() => undefined);
      await page.waitForTimeout(250);
      return await page.evaluate((name) => {
        const nav = Array.from(document.querySelectorAll('aside nav, [role="dialog"] nav')).find((candidate) => {
          const rect = candidate.getBoundingClientRect();
          return rect.width > 0 && rect.height > 0;
        }) ?? null;
        const active = Array.from(document.querySelectorAll('[aria-current="page"]')).find((candidate) => {
          const rect = candidate.getBoundingClientRect();
          return rect.width > 0 && rect.height > 0;
        }) ?? null;
        const main = document.querySelector('main');
        const navRect = nav?.getBoundingClientRect();
        const activeRect = active?.getBoundingClientRect();
        return {
          label: name,
          url: location.href,
          nav: nav ? {
            clientHeight: nav.clientHeight,
            scrollHeight: nav.scrollHeight,
            scrollTop: nav.scrollTop,
            overflowY: getComputedStyle(nav).overflowY,
            rect: { top: navRect?.top, bottom: navRect?.bottom, height: navRect?.height },
          } : null,
          active: active ? {
            text: active.textContent?.trim(),
            rect: { top: activeRect?.top, bottom: activeRect?.bottom },
            visibleWithinNav: Boolean(navRect && activeRect && activeRect.top >= navRect.top && activeRect.bottom <= navRect.bottom),
          } : null,
          main: main ? { scrollHeight: main.scrollHeight, clientHeight: main.clientHeight, scrollTop: main.scrollTop } : null,
          documentScrollTop: document.documentElement.scrollTop,
        };
      }, label);
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
  }
  return { label, error: lastError, url: page.url() };
}

const results = [];
results.push(await inspectNavigation('desktop-dashboard'));
await page.goto(`${baseUrl}/#/geolibre-map`, { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(900);
results.push(await inspectNavigation('desktop-lower-route-before-scroll'));
await page.evaluate(() => {
  const nav = document.querySelector('aside nav');
  if (nav) nav.scrollTop = nav.scrollHeight;
});
results.push(await inspectNavigation('desktop-lower-route-after-scroll'));

await page.setViewportSize({ width: 390, height: 844 });
await page.goto(`${baseUrl}/#/dashboard`, { waitUntil: 'domcontentloaded' });
await page.locator('header[role="banner"]').first().getByRole('button').first().click();
await page.waitForTimeout(250);
results.push(await inspectNavigation('mobile-dashboard-sheet-open'));
await page.evaluate(() => {
  const nav = Array.from(document.querySelectorAll('[role="dialog"] nav')).find((candidate) => {
    const rect = candidate.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  });
  if (nav) nav.scrollTop = nav.scrollHeight;
});
results.push(await inspectNavigation('mobile-sheet-after-scroll'));
const mobileDialog = page.getByRole('dialog');
await mobileDialog.getByRole('button', { name: /navigate to geolibre gis/i }).click();
await page.waitForTimeout(600);
results.push(await inspectNavigation('mobile-lower-route-after-tap'));

console.log(JSON.stringify(results, null, 2));
await browser.close();
