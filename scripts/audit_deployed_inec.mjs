import { chromium } from 'playwright';
import fs from 'node:fs/promises';
import path from 'node:path';

const baseUrl = process.env.INEC_BASE_URL ?? 'https://inec.servers.upi.dev/';
const username = process.env.INEC_USERNAME;
const password = process.env.INEC_PASSWORD;
if (!username || !password) {
  throw new Error('INEC_USERNAME and INEC_PASSWORD must be set');
}

const routes = [
  'dashboard', 'map', 'elections', 'results', 'collation', 'polling-units',
  'audit', 'incidents', 'middleware', 'bvas', 'anomaly-detection',
  'sms-verification', 'public-api', 'voter-registration', 'workflow-engine',
  'bvas-sync', 'portal-integration', 'data-validation', 'admin-console',
  'biometric', 'blockchain', 'training', 'stakeholders', 'ai-monitoring',
  'production', 'observer-monitoring', 'dispute-resolution', 'kyc-verification',
  'scale-health', 'geofencing', 'webhooks', 'user-management',
  'duplicate-detection', 'document-ai', 'evidence-journey', 'export-center',
  'command-center', 'citizen-portal', 'mfa', 'predictive-analytics',
  'tv-dashboard', 'compliance-report', 'integrity-score', 'ml-dashboard',
  'geolibre-map', 'gotv-portal', 'party-primaries', 'enrollment-kiosk',
  'stakeholder-workflows',
];

const outputDir = '/tmp/inec-deployed-audit';
await fs.mkdir(outputDir, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.CHROMIUM_PATH || '/usr/bin/chromium',
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
});
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const globalConsole = [];
page.on('console', (message) => {
  if (message.type() === 'error') globalConsole.push({ type: 'console', text: message.text(), url: page.url() });
});
page.on('pageerror', (error) => globalConsole.push({ type: 'pageerror', text: error.message, url: page.url() }));

await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
await page.locator('#username').fill(username);
await page.locator('#password').fill(password);
await page.getByRole('button', { name: 'Sign In', exact: true }).click();
await page.waitForURL(/#\/dashboard/, { timeout: 15_000 });
await page.waitForTimeout(1_250);

const results = [];
for (const route of routes) {
  const beforeErrorCount = globalConsole.length;
  const routeUrl = `${baseUrl.replace(/\/$/, '')}/#/${route}`;
  let navigationError = null;
  try {
    await page.goto(routeUrl, { waitUntil: 'domcontentloaded', timeout: 20_000 });
    await page.waitForTimeout(1_750);
  } catch (error) {
    navigationError = error instanceof Error ? error.message : String(error);
  }

  const inspection = await page.evaluate(() => {
    const main = document.querySelector('main');
    const nav = document.querySelector('aside nav');
    const text = document.body.innerText;
    const horizontalOverflow = document.documentElement.scrollWidth > window.innerWidth + 1;
    const focusable = Array.from(document.querySelectorAll('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])'))
      .filter((element) => !(element instanceof HTMLButtonElement && element.disabled)).length;
    const buttons = Array.from(document.querySelectorAll('button'));
    const searchInputs = Array.from(document.querySelectorAll('input')).filter((input) => /search|filter/i.test(input.placeholder || input.getAttribute('aria-label') || ''));
    const tabs = document.querySelectorAll('[role="tab"]').length;
    const activeNav = document.querySelector('[aria-current="page"]')?.textContent?.trim() || null;
    const navMetrics = nav ? {
      clientHeight: nav.clientHeight,
      scrollHeight: nav.scrollHeight,
      scrollTop: nav.scrollTop,
      overflowY: getComputedStyle(nav).overflowY,
    } : null;
    return {
      title: document.title,
      pageTextStart: text.slice(0, 500),
      hasNotFound: /page not found|\b404\b/i.test(text),
      hasErrorBoundary: /something went wrong|unexpected application error|error boundary/i.test(text),
      hasVisibleLoadingSkeleton: /loading/i.test(main?.innerText || '') && (main?.querySelectorAll('[class*="animate-pulse"]').length || 0) > 0,
      horizontalOverflow,
      focusable,
      buttonCount: buttons.length,
      disabledButtonCount: buttons.filter((button) => button.disabled).length,
      searchInputCount: searchInputs.length,
      tabs,
      activeNav,
      navMetrics,
      hasMain: Boolean(main),
      mainLandmarkCount: document.querySelectorAll('main').length,
    };
  });
  const routeErrors = globalConsole.slice(beforeErrorCount);
  const result = { route, url: page.url(), navigationError, routeErrors, ...inspection };
  results.push(result);
  if (navigationError || inspection.hasNotFound || inspection.hasErrorBoundary || inspection.horizontalOverflow || routeErrors.length) {
    await page.screenshot({ path: path.join(outputDir, `${route.replace(/[^a-z0-9-]/gi, '_')}.png`), fullPage: false });
  }
}

const sidebarBefore = await page.evaluate(() => {
  const nav = document.querySelector('aside nav');
  if (!nav) return null;
  return { scrollTop: nav.scrollTop, scrollHeight: nav.scrollHeight, clientHeight: nav.clientHeight };
});
await page.evaluate(() => {
  const nav = document.querySelector('aside nav');
  if (nav) nav.scrollTop = nav.scrollHeight;
});
const sidebarAfter = await page.evaluate(() => {
  const nav = document.querySelector('aside nav');
  if (!nav) return null;
  return {
    scrollTop: nav.scrollTop,
    scrollHeight: nav.scrollHeight,
    clientHeight: nav.clientHeight,
    footerVisible: Boolean(Array.from(document.querySelectorAll('aside *')).find((element) => element.textContent?.includes('System Administrator'))),
  };
});

await browser.close();
const report = {
  generatedAt: new Date().toISOString(),
  baseUrl,
  routeCount: routes.length,
  sidebarBefore,
  sidebarAfter,
  globalConsole,
  results,
};
await fs.writeFile(path.join(outputDir, 'report.json'), JSON.stringify(report, null, 2));
console.log(JSON.stringify({ output: path.join(outputDir, 'report.json'), routeCount: routes.length, issueCount: results.filter((result) => result.navigationError || result.hasNotFound || result.hasErrorBoundary || result.horizontalOverflow || result.routeErrors.length).length }, null, 2));
