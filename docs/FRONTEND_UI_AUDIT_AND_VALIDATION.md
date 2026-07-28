# Frontend UI Audit and Validation

## Scope

This audit covered the authenticated administrator experience of the deployed INEC platform at `https://inec.servers.upi.dev/` and the campaign dashboard at `https://campaign.inec.servers.upi.dev/`. It inventoried all registered routes, exercised navigation, tabs, searchable controls, and selected forms without creating or modifying production records.

| Application | Route/page coverage | Primary concern |
|---|---:|---|
| INEC platform | 49 registered authenticated routes | Long left navigation, API failure recovery, optional-service errors, mobile PWA behavior |
| Campaign dashboard | 27 registered routes | Post-login session continuity, PWA safe-area layout, optional analytics and map configuration |

## Verified Findings and Repairs

| Finding | Repair | Validation evidence |
|---|---|---|
| A lower INEC sidebar destination could remain off-screen after direct navigation. | The shared layout now owns a stable viewport-height shell, scroll-contained navigation region, active-route auto-reveal, section labels, and a labelled mobile menu. | Desktop test opens `#/geolibre-map` with `GeoLibre GIS` visible after a controlled sidebar scroll. Mobile test opens, scrolls, and activates the lower destination. |
| The mobile INEC navigation control lacked a meaningful accessible name. | Added an explicit `Open navigation menu` label and accessible navigation sheet semantics. | Browser test locates the trigger by accessible name and completes the lower-route journey. |
| Campaign PWA pages could be obscured by the fixed bottom navigation. | Added one route-aware shell that reserves the navigation height plus safe-area inset for authenticated campaign routes. | 390×844 preview test measured 64px shell bottom padding for a 62px navigation bar with no horizontal overflow. |
| Campaign login could succeed but protected queries had no usable session when browser cookie return was blocked. | The local-login response now exposes a short-lived tab-scoped token mirror; the existing bearer fallback uses it only when the cookie is absent. | Campaign type check, build, and authentication regression passed; the browser test used the rendered PWA shell. |
| Campaign emitted a request using an unresolved analytics URL placeholder. | Replaced static placeholder injection with an optional, configuration-validated analytics loader. | Preview test found no unresolved analytics request. |
| Campaign map could request a provider URL with an undefined key. | Added strict optional map configuration validation and a clear in-app unavailable state. | Preview test found no undefined/null map-key request. |
| Command Center requested an SSE path that fell through to SPA HTML. | Standardized the client stream route to the API gateway and added `command-center` to the frontend Nginx proxy route set. | Source scan and production build passed. |
| Observer monitoring used a hard-coded localhost API and raw controls that failed silently. | Replaced the stale transport, gated optional streaming behind health checks, and added visible mutation errors plus keyboard-accessible tabs. | No `localhost:8088` observer client reference remains; production build and tests passed. |
| Production status called a removed generic ledger endpoint. | Replaced it with the actual operational-settlement health boundary and removed residual generic ledger client methods. | Source scan found no retired status/transfer/journal frontend path. |
| Anomaly, Public API, SMS/USSD, and Workflow pages hid optional-service outages or generated unnecessary background calls. | Added controlled availability states, explicit retry pathways, and deferred tab-specific requests where appropriate. | Each page compiles and is covered by the all-route browser inventory; the UI no longer presents missing data as successful information. |
| Blockchain page could throw on an empty or malformed gateway response. | Hardened shared API response parsing to return a controlled application error rather than uncaught JSON parsing. | Production build and existing regression suite passed. |

## Validation Matrix

| Check | Result |
|---|---|
| INEC frontend production build | Passed |
| INEC frontend Vitest suite | Passed: 65 tests across 4 files |
| Campaign TypeScript check | Passed |
| Campaign production build | Passed |
| Campaign authentication regression | Passed |
| Local INEC desktop and mobile navigation browser test | Passed |
| Local campaign PWA/mobile browser test | Passed |
| Source scan for legacy observer localhost, unresolved analytics placeholders, invalid map keys, and retired production ledger calls | Passed |
| Working-tree whitespace validation | Passed |

## Remaining Operational Prerequisites

The fixes intentionally do not fabricate data from offline external services. A page presents a clear unavailable state until its approved backend dependency is restored. Production operations should keep the anomaly inference service, SMS/USSD provider, public API administration service, and EMS workflow service configured and monitored. These conditions are operational availability requirements, not frontend fallback defects.

## Reproducible Audit Assets

The `scripts/` directory includes non-destructive deployed-page audit and local browser validation scripts. They are intended for future release checks and do not submit forms, mutate production data, or expose authentication tokens in output.
