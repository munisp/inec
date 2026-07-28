import { chromium } from 'playwright';

const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
await context.addInitScript(() => {
  localStorage.setItem('user', JSON.stringify({ id: 1, username: 'admin', full_name: 'Administrator', role: 'admin' }));
  localStorage.setItem('auth_token', 'local-preview-token');
});
const page = await context.newPage();

await page.route('**/auth/me', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 1, username: 'admin', full_name: 'Administrator', role: 'admin' }) }));
await page.route('**/auth/refresh', (route) => route.fulfill({ status: 401, contentType: 'application/json', body: '{}' }));

async function visibleNavMetrics() {
  return page.evaluate(() => {
    const nav = Array.from(document.querySelectorAll('aside nav, [role="dialog"] nav')).find((candidate) => {
      const rect = candidate.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    });
    const active = Array.from(document.querySelectorAll('[aria-current="page"]')).find((candidate) => {
      const rect = candidate.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    });
    const navRect = nav?.getBoundingClientRect();
    const activeRect = active?.getBoundingClientRect();
    return {
      navHeight: nav?.clientHeight ?? 0,
      navScrollHeight: nav?.scrollHeight ?? 0,
      navScrollTop: nav?.scrollTop ?? 0,
      activeText: active?.textContent?.trim() ?? null,
      activeVisible: Boolean(navRect && activeRect && activeRect.top >= navRect.top && activeRect.bottom <= navRect.bottom),
      activeTop: activeRect?.top ?? null,
      activeBottom: activeRect?.bottom ?? null,
    };
  });
}

await page.goto('http://127.0.0.1:4173/#/geolibre-map', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(800);
const desktop = await visibleNavMetrics();
if (!desktop.activeVisible || desktop.activeText !== 'GeoLibre GIS') {
  throw new Error(`Desktop active-item reveal failed: ${JSON.stringify(desktop)}`);
}

await page.setViewportSize({ width: 390, height: 844 });
await page.goto('http://127.0.0.1:4173/#/dashboard', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(600);
const menu = page.getByRole('button', { name: 'Open navigation menu' });
if (await menu.count() !== 1) throw new Error('Accessible mobile navigation trigger is missing.');
await menu.click();
await page.waitForTimeout(300);
const mobileInitial = await visibleNavMetrics();
if (mobileInitial.navHeight < 300 || mobileInitial.navScrollHeight <= mobileInitial.navHeight) {
  throw new Error(`Mobile navigation is not a scrollable contained region: ${JSON.stringify(mobileInitial)}`);
}
await page.evaluate(() => {
  const nav = Array.from(document.querySelectorAll('[role="dialog"] nav')).find((candidate) => {
    const rect = candidate.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  });
  if (!nav) throw new Error('Visible mobile navigation not found.');
  nav.scrollTop = nav.scrollHeight;
});
const mobileScrolled = await visibleNavMetrics();
if (mobileScrolled.navScrollTop <= 0) throw new Error(`Mobile navigation did not scroll: ${JSON.stringify(mobileScrolled)}`);
await page.getByRole('dialog').getByRole('button', { name: 'Navigate to GeoLibre GIS' }).click();
await page.waitForTimeout(500);
if (!page.url().includes('#/geolibre-map')) throw new Error(`Lower mobile route did not activate: ${page.url()}`);

console.log(JSON.stringify({ desktop, mobileInitial, mobileScrolled, finalUrl: page.url() }, null, 2));
await browser.close();
