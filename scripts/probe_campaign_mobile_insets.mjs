import { chromium } from 'playwright';

const baseUrl = 'https://campaign.inec.servers.upi.dev';
const routes = [
  '/', '/stakeholders', '/endorsements', '/timeline', '/registration', '/polling-units',
  '/volunteers', '/press-release', '/social-media', '/legal-compliance',
  '/opposition-research', '/war-room', '/results', '/manifesto', '/petition',
  '/diaspora', '/post-election', '/candidate-website', '/media-monitoring',
  '/debate-coach', '/fundraising', '/budget', '/profile', '/team', '/dashboard',
];
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
const page = await context.newPage();
await page.goto(`${baseUrl}/login`, { waitUntil: 'commit', timeout: 60_000 });
await page.waitForLoadState('domcontentloaded', { timeout: 20_000 }).catch(() => undefined);
await page.locator('#username').fill(process.env.CAMPAIGN_USERNAME ?? '');
await page.locator('#password').fill(process.env.CAMPAIGN_PASSWORD ?? '');
await page.getByRole('button', { name: /sign in/i }).click();
await page.waitForURL(`${baseUrl}/`);
const results = [];
for (const route of routes) {
  await page.goto(`${baseUrl}${route}`, { waitUntil: 'commit', timeout: 60_000 });
  await page.waitForLoadState('domcontentloaded', { timeout: 20_000 }).catch(() => undefined);
  await page.waitForTimeout(850);
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await page.waitForTimeout(150);
  results.push(await page.evaluate((currentRoute) => {
    const nav = document.querySelector('nav[aria-label="Campaign navigation"]');
    const navRect = nav?.getBoundingClientRect();
    const candidates = Array.from(document.querySelectorAll('main button:not([disabled]), main a[href], main input:not([disabled]), main select:not([disabled]), main textarea:not([disabled])'));
    const last = candidates
      .map((element) => ({ element, rect: element.getBoundingClientRect() }))
      .filter(({ rect }) => rect.width > 0 && rect.height > 0)
      .sort((a, b) => b.rect.bottom - a.rect.bottom)[0];
    const viewportBottom = window.innerHeight;
    return {
      route: currentRoute,
      documentHeight: document.documentElement.scrollHeight,
      viewportHeight: window.innerHeight,
      navigationHeight: navRect?.height ?? 0,
      navigationVisible: Boolean(navRect && navRect.height > 0),
      lastControl: last ? { text: last.element.textContent?.trim().slice(0, 100) || last.element.getAttribute('aria-label') || last.element.getAttribute('placeholder') || '', top: last.rect.top, bottom: last.rect.bottom } : null,
      overlapsNavigation: Boolean(navRect && last && last.rect.bottom > navRect.top && last.rect.top < navRect.bottom),
      belowSafeViewport: Boolean(navRect && last && last.rect.bottom > navRect.top),
      bodyHorizontalOverflow: document.documentElement.scrollWidth > window.innerWidth + 1,
      viewportBottom,
    };
  }, route));
}
console.log(JSON.stringify(results, null, 2));
await browser.close();
