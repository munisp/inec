# INEC Mobile (Expo / React Native)

Observer + GOTV canvasser mobile app (expo-router, Expo SDK 56).

## Setup

```bash
npm ci
npm start          # expo start
```

`npm ci` is canonical — there is no pnpm lockfile.

## Environment variables

Set via `eas.json` build-profile `env` blocks (EXPO_PUBLIC_* are inlined at
build time):

| Variable | Purpose |
| --- | --- |
| `EXPO_PUBLIC_API_URL` | Main election API base URL (inec-go-backend). |
| `EXPO_PUBLIC_GOTV_API_URL` | GOTV service base URL. **Required** in non-dev builds — `lib/gotv-auth.ts` throws at startup if unset, rather than silently hitting localhost. |

The preview/production profiles currently contain
`TODO-REPLACE-WITH-REAL-*.invalid` placeholders that must be replaced with the
real staging/production hosts before cutting a build.

## EAS build profiles

- `development` — dev-client build, localhost backends.
- `preview` — internal APK, staging backends.
- `production` — app-bundle, production backends.

The `submit` section was removed: it contained placeholder credentials
(`ascAppId` 1234567890, a missing `google-services.json`, and the
`inecnigeria.org` placeholder domain). Re-add it with real values when store
submission is configured.

## Dev-client note (encryption)

Field-level protection of offline data is **obfuscation, not encryption**:
Expo Go / the managed build has no SQLCipher, so `src/lib/crypto.ts` performs
keyed-XOR obfuscation with a CSPRNG-generated key (expo-crypto) and the
offline contact cache stores masked names only. Real at-rest encryption
requires a dev-client build with SQLCipher (see `lib/storage.ts` header).

## Checks

```bash
npx tsc --noEmit   # typecheck (canonical gate)
npm test           # placeholder — no unit tests configured yet
```
