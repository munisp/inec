import { chromium } from 'playwright';
import fs from 'node:fs/promises';
import path from 'node:path';

const baseUrl = process.env.CAMPAIGN_BASE_URL ?? 'https://campaign.inec.servers.upi.dev';
const username = process.env.CAMPAIGN_USERNAME;
const password = process.env.CAMPAIGN_PASSWORD;
const routes = [
  '/', '/join', '/stakeholders', '/endorsements', '/timeline', '/registration',
  '/polling-units', '/volunteers', '/press-release', '/social-media',
  '/legal-compliance', '/opposition-research', '/war-room', '/results',
  '/manifesto', '/petition', '/diaspora', '/post-election', '/candidate-website',
  '/media-monitoring', '/debate-coach', '/fundraising', '/budget', '/profile',
  '/team', '/dashboard', '/login',
];
const outputDir = '/tmp/campaign-deployed-audit';
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

if (username && password) {
  await page.goto(`${baseUrl}/login`, { waitUntil: 'domcontentloaded' });
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.getByRole('button', { name: /sign in/i }).click();
  await page.waitForURL(new RegExp(`${baseUrl.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/?$`), { timeout: 15_000 });
  await page.waitForTimeout(750);
}

const results = [];
for (const route of routes) {
  const beforeErrorCount = globalConsole.length;
  const url = `${baseUrl}${route}`;
  let navigationError = null;
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 20_000 });
    await page.waitForTimeout(1_500);
  } catch (error) {
    navigationError = error instanceof Error ? error.message : String(error);
  }
  let inspection;
  let inspectionError = null;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.waitForLoadState('domcontentloaded', { timeout: 5_000 }).catch(() => undefined);
      await page.waitForTimeout(500);
      inspection = await page.evaluate(() => {
        const bodyText = document.body.innerText;
        const main = document.querySelector('main');
        const buttons = Array.from(document.querySelectorAll('button'));
        const links = Array.from(document.querySelectorAll('a[href]'));
        const inputs = Array.from(document.querySelectorAll('input, textarea, select'));
        const horizontalOverflow = document.documentElement.scrollWidth > window.innerWidth + 1;
        return {
          title: document.title,
          hasNotFound: /page not found|\b404\b/i.test(bodyText),
          hasErrorBoundary: /something went wrong|unexpected application error|error boundary/i.test(bodyText),
          hasVisibleConfigurationError: /OPENAI_API_KEY is not configured|AI narrative failed|not configured/i.test(bodyText),
          horizontalOverflow,
          hasMain: Boolean(main),
          focusable: document.querySelectorAll('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])').length,
          buttonCount: buttons.length,
          disabledButtonCount: buttons.filter((button) => button.disabled).length,
          linkCount: links.length,
          formControlCount: inputs.length,
          searchInputCount: inputs.filter((input) => /search|filter/i.test(input.getAttribute('placeholder') || input.getAttribute('aria-label') || '')).length,
          tabs: document.querySelectorAll('[role="tab"]').length,
          pageTextStart: bodyText.slice(0, 500),
        };
      });
      break;
    } catch (error) {
      inspectionError = error instanceof Error ? error.message : String(error);
    }
  }
  inspection ??= {
    title: '', hasNotFound: false, hasErrorBoundary: false, hasVisibleConfigurationError: false,
    horizontalOverflow: false, hasMain: false, focusable: 0, buttonCount: 0,
    disabledButtonCount: 0, linkCount: 0, formControlCount: 0, searchInputCount: 0,
    tabs: 0, pageTextStart: '',
  };
  navigationError ??= inspectionError;
  const routeErrors = globalConsole.slice(beforeErrorCount);
  const result = { route, url: page.url(), navigationError, routeErrors, ...inspection };
  results.push(result);
  if (navigationError || inspection.hasNotFound || inspection.hasErrorBoundary || inspection.hasVisibleConfigurationError || inspection.horizontalOverflow || routeErrors.length) {
    await page.screenshot({ path: path.join(outputDir, `${route === '/' ? 'home' : route.slice(1).replace(/[^a-z0-9-]/gi, '_')}.png`), fullPage: false });
  }
}
await browser.close();
const report = { generatedAt: new Date().toISOString(), baseUrl, routeCount: routes.length, globalConsole, results };
await fs.writeFile(path.join(outputDir, 'report.json'), JSON.stringify(report, null, 2));
const issues = results.filter((item) => item.navigationError || item.hasNotFound || item.hasErrorBoundary || item.hasVisibleConfigurationError || item.horizontalOverflow || item.routeErrors.length);
console.log(JSON.stringify({ output: path.join(outputDir, 'report.json'), routeCount: routes.length, issueCount: issues.length, issues: issues.map(({ route, navigationError, hasNotFound, hasErrorBoundary, hasVisibleConfigurationError, horizontalOverflow, routeErrors }) => ({ route, navigationError, hasNotFound, hasErrorBoundary, hasVisibleConfigurationError, horizontalOverflow, errors: routeErrors.map((error) => error.text.split('\n')[0]) })) }, null, 2));
