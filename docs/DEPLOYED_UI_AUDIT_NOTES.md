# Deployed UI Audit Notes

## Targets inspected

| Application | URL | Authentication state used |
|---|---|---|
| INEC Election Platform | https://inec.servers.upi.dev/ | Administrator credentials supplied by the user; login reached `#/dashboard`. |
| INEC Campaign Intelligence | https://campaign.inec.servers.upi.dev/ | Administrator credentials supplied by the user; login reached `/`. |

## Browser observations

The INEC application exposes 49 static hash-routed administrator pages. A non-destructive Playwright sweep reached every static route without rendering a 404, error boundary, or horizontal document overflow. Its desktop sidebar reports `scrollHeight: 2360px`, `clientHeight: 726px`, and can scroll to `scrollTop: 1634px`; however, the fixed navigation needs a deterministic scroll-region treatment and active-item reveal behavior to avoid inconsistent interactions on different viewport sizes.

The deployed INEC application logged recoverable external-service errors on `anomaly-detection`, `sms-verification`, `public-api`, `voter-registration`, `workflow-engine`, `blockchain`, `production`, `observer-monitoring`, and `command-center`. The specific observed failures were HTTP 503 responses, an empty JSON response parsed by the Blockchain page, a Production-page 404, an observer-service connection refusal, and an EventSource endpoint returning HTML instead of `text/event-stream`.

The campaign application exposes 27 static routes. Before signing in, the campaign home visibly showed the message `AI narrative failed: OPENAI_API_KEY is not configured`; this should not be surfaced as a raw infrastructure error. After the local username/password login, `profile.get` still returned HTTP 401, so the campaign shell reached `/` but protected profile data did not load. The authenticated probe found the login response was followed by `GET /api/trpc/profile.get` returning `Please login (10001)`. The deployment also requested a literal `/%VITE_ANALYTICS_ENDPOINT%/umami`, causing HTTP 400, and the polling-unit page attempted to load a Google Maps proxy with `key=undefined`, generating a CORS failure.

## Source repositories mapped

| Application | Repository | Source root |
|---|---|---|
| INEC Election Platform | `munisp/inec` | `inec-frontend/` |
| INEC Campaign Intelligence | `munisp/inec` | `campaign-platform/` |
| User-selected auxiliary portal | `munisp/lanai` | `lanai-portal/` (does not match the deployed campaign application) |

## Additional reproduced navigation and gateway evidence

At a desktop viewport of 1440×900, the INEC sidebar navigation had `clientHeight: 726px` and `scrollHeight: 2360px`. Directly navigating to the lower `#/geolibre-map` route left its active `GeoLibre GIS` control at approximately `top: 2385px`, outside the navigation viewport until the user manually scrolled the sidebar to `scrollTop: 1634px`. The mobile sheet navigation has a contained scroll region and can reach the lower route, but the menu trigger did not have an accessible name before the repair.

The campaign login request completed with HTTP 200 and the browser retained a secure HttpOnly `app_session_id` cookie. The subsequent protected `GET /api/trpc/profile.get` request still carried no session-cookie header and returned HTTP 401 (`Please login (10001)`). This establishes a cookie-return failure at the deployed browser/request boundary, not invalid supplied credentials.

The main INEC Command Center SSE request was observed at `https://inec.servers.upi.dev/command-center/stream` and failed because it returned `text/html` instead of `text/event-stream`. Source inspection confirms the deployed frontend Nginx proxy list omitted `command-center`, causing the request to fall through to the SPA `index.html` handler. The edge Caddy configuration forwards `/api/*` through the API gateway, while the frontend Nginx container forwards configured bare backend prefixes.

The Production page’s aggregate request sequence also includes the removed legacy `/production/ledger/stats` path. The current backend instead exposes `/operational-settlements/health` as the native TigerBeetle/Mojaloop operational-boundary health API, so the frontend needs to replace its legacy production-ledger representation rather than retry the removed route.

The campaign mobile navigation is fixed at the bottom of the PWA. Source inventory shows that, before the shared shell repair, nearly every campaign page except the home/dashboard routes lacked bottom safe-area spacing; final controls could be obscured by the navigation bar on PWA-sized viewports.
