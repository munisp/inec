---
name: testing-inec-frontend
description: Test the INEC Election Platform frontend end-to-end. Use when verifying UI pages, map integrations, or new frontend features.
---

# Testing INEC Frontend

## Prerequisites

- Node.js 18+ installed
- Frontend dependencies installed: `cd inec-frontend && npm install`
- Dev server running: `npm run dev` (serves on localhost:5173)

## Auth Bypass

The app uses JWT auth via localStorage. To bypass login for testing:

```javascript
// Run in browser console after navigating to localhost:5173
localStorage.setItem('token', 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIiwiZXhwIjo5OTk5OTk5OTk5LCJ1c2VybmFtZSI6ImFkbWluIiwicm9sZSI6ImFkbWluIn0.fake');
localStorage.setItem('user', JSON.stringify({id:1,username:'admin',full_name:'Admin User',role:'admin'}));
location.reload();
```

The JWT has `exp: 9999999999` (year 2286) so it never expires during testing. The `isTokenExpired` function in `src/lib/auth.tsx` checks `payload.exp * 1000 < Date.now()`.

## Sidebar Navigation

The sidebar has 44+ nav items organized by sections. To find items near the bottom (like GeoLibre GIS under "GeoSpatial"), you need to scroll the nav element multiple times. The nav is the element with `aria-label="Main navigation"`.

Alternatively, inspect the DOM for the target button's `devinid` and click directly — this is faster than scrolling.

## Known Issues

### MapLibre CSS Height Bug (GeoLibre GIS only)
The GeoLibre GIS page's Live Map tab map container might collapse to 0px height on initial load. This was fixed in PR #4 — check if tiles render immediately before applying the workaround.

**Workaround (if still needed):** Force a resize via JS console:
```javascript
document.querySelector('.maplibregl-map').style.height = '700px';
window.dispatchEvent(new Event('resize'));
```

The main Map View page (devinid=2) does NOT have this bug — it renders reliably.

### No Backend Data
When testing without the Go backend, all API calls fail gracefully — data providers return empty FeatureCollections. Stats show "0", layers have no data points, and spatial analysis shows "No polling unit data loaded" error. This is expected behavior.

## Test Approach

### What to Test
1. **Page navigation** — sidebar nav items load correct pages without crash
2. **Tab switching** — multi-tab pages (like GeoLibre GIS) switch between tabs cleanly
3. **Map rendering** — MapLibre canvas renders with tiles from public CDNs (CARTO, OpenFreeMap)
4. **Controls** — dropdowns, toggles, buttons respond to clicks and update state
5. **Graceful degradation** — pages show meaningful empty states when no backend data
6. **Error handling** — actions with no data show proper error messages, not crashes

### Map Tile Providers
- **OpenFreeMap** — default basemap, might have availability issues
- **CARTO Positron/Dark/Voyager** — reliable fallback basemaps
- If OpenFreeMap tiles don't load, switch to CARTO to verify map rendering works

### GeoLibre Viewer Tab
The iframe loads `https://viewer.geolibre.app` — this is an external site. It should load the full GeoLibre application with its own map, layers, and menu system. If the external site is down, the iframe will be blank but the toolbar controls should still render.

## Devin Secrets Needed
None — testing uses fake JWT tokens and public tile CDNs.

## Regression Scan Script

Use Playwright via CDP to rapidly test all 44 pages. Find Chrome's CDP port with `ss -tlnp | grep chrome`, then:

```javascript
const pw = require('/home/ubuntu/repos/inec/inec-frontend/node_modules/playwright');
const browser = await pw.chromium.connectOverCDP('http://127.0.0.1:<CDP_PORT>');
const page = browser.contexts()[0].pages()[0];

// Click each nav button via evaluate (avoids locator timeouts)
const clicked = await page.evaluate((label) => {
  const buttons = document.querySelectorAll('nav button');
  for (const btn of buttons) {
    if (btn.textContent.trim() === label) {
      btn.scrollIntoView(); btn.click(); return true;
    }
  }
  return false;
}, label);

// Check for ErrorBoundary
const hasError = await page.evaluate(() =>
  document.body.innerText.includes('Something went wrong'));
```

Use `evaluate` + `textContent.trim()` to click nav buttons — Playwright locators time out on CDP connections.

## Cache Busting Verification

To verify cache busting works after a build:
1. `CACHE_VERSION` in `dist/sw.js` should be a 13-digit numeric timestamp (not `__BUILD_TIMESTAMP__`)
2. `dist/index.html` should contain `no-cache, no-store, must-revalidate` meta header
3. Two consecutive builds should produce different `CACHE_VERSION` values
4. `STATIC_ASSETS` should be `[]` (index.html NOT pre-cached)
5. SW only registers in production mode — cannot test runtime cache behavior on dev server

## Tips
- Take screenshots at key moments for the test report
- Maximize browser window: `sudo apt-get install -y wmctrl 2>/dev/null; wmctrl -r :ACTIVE: -b add,maximized_vert,maximized_horz`
- When testing map pages, wait 10-15 seconds after navigation for API fallback + tile loading (no backend = API timeout before demo data loads)
- Use 127.0.0.1 instead of localhost for Playwright/Chrome CDP connections
- Check canvas dimensions via JS console: `document.querySelectorAll('canvas').forEach((c,i) => console.log('Canvas '+i+': '+c.clientWidth+'x'+c.clientHeight))`

## Key devinid Mappings
- Dashboard: 1, Map View: 2, Elections: 3, Results: 4, Collation: 5
- TV Dashboard: 41, Compliance Report: 42, ML Dashboard: 43, GeoLibre GIS: 44
