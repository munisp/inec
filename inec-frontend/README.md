# INEC Election Platform — Web Frontend

React 19 + TypeScript + Vite SPA for the INEC blockchain-based election
results platform. Serves staff (admin, collation/presiding officers) and
read-only observer workflows behind a single hash-routed app.

## Setup

```bash
npm ci
npm run dev        # vite dev server (default :5173)
```

Copy `.env.example` to `.env` for local overrides — `.env` /
`.env.production` are gitignored and must never be committed.

## Environment variables

| Variable | Purpose | Default |
| --- | --- | --- |
| `VITE_API_URL` | Base URL of the main election API (inec-go-backend). Empty = same-origin relative paths through the proxy. | `''` |
| `VITE_GEOLIBRE_URL` | Base URL of the GeoLibre GIS viewer embedded by the GeoSpatial page. | `http://localhost:8090` |
| `VITE_GOTV_WS_HOST` | Override host for the GOTV live-operations WebSocket (defaults to `window.location.host`). | unset |

## Proxy topology

The app calls the backend with **relative URLs**; `vite.config.ts` maps them
(shared by `vite dev` and `vite preview`):

- nearly all first-level paths (`/elections`, `/results`, `/geo`, …) →
  `http://localhost:8088` (main Go backend), and
- `/gotv` → `http://localhost:8103` (gotv-svc, `ws: true` for the
  live-operations socket).

In production an edge proxy (APISIX/Caddy) must reproduce this mapping and
translate the `inec_token` httpOnly cookie into a Bearer header for the GOTV
SSE/WS endpoints (gotv-svc authenticates Bearer tokens only).

## Auth model

- Login POSTs to `/auth/login`; the backend sets an httpOnly session cookie.
- `localStorage.auth_token` holds the JWT only as a documented fallback for
  non-browser clients / cross-origin dev (`Authorization: Bearer …` header).
- A 401 from any API call dispatches `inec-session-expired`; the auth context
  clears state and routes to `#/login?returnTo=<page>` — no full-page reload.
- GOTV pages additionally require an explicit party tenancy selection
  (`GOTVPartySelector`, persisted as `gotv_party_code`); no party is ever
  defaulted.
- Pages declare required roles in `src/lib/page-roles.ts`; the nav filters
  hidden pages and the router renders a friendly 403. The backend remains the
  authoritative access-control layer.

## Build & test

```bash
npm run build      # production bundle in dist/
npx vitest run     # unit tests
npx tsc --noEmit   # typecheck
```

## Service worker

`public/sw.js` provides offline queueing for observer/results write requests
only. Queued requests are persisted without the `Authorization` header and
replay with the ambient cookie session.
