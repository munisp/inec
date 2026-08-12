import { randomBytes } from "crypto";

// SECURITY: the session JWT (HS256) is signed and verified with JWT_SECRET.
// An empty or short secret makes sessions trivially forgeable, so we fail
// fast at boot unless this is an explicit development/test runtime.
const MIN_JWT_SECRET_LENGTH = 32;

function resolveCookieSecret(): string {
  const secret = process.env.JWT_SECRET;
  if (secret && secret.length >= MIN_JWT_SECRET_LENGTH) {
    return secret;
  }

  const runtime = (process.env.NODE_ENV ?? "").toLowerCase();
  if (runtime === "development" || runtime === "test") {
    const generated = randomBytes(32).toString("hex");
    console.warn(
      "\n[SECURITY] **************************************************************\n" +
        "[SECURITY] JWT_SECRET is unset or shorter than 32 characters.\n" +
        "[SECURITY] Generated an EPHEMERAL random secret for this development/test\n" +
        "[SECURITY] process only — all sessions are invalidated on every restart.\n" +
        "[SECURITY] Set JWT_SECRET (>= 32 random chars) for any real deployment.\n" +
        "[SECURITY] **************************************************************\n"
    );
    return generated;
  }

  throw new Error(
    `[SECURITY] JWT_SECRET must be set and at least ${MIN_JWT_SECRET_LENGTH} characters long. ` +
      "Refusing to boot: an empty/short secret makes session tokens forgeable. " +
      'Generate one with e.g. `openssl rand -hex 32`, or set NODE_ENV to "development"/"test" for local work.'
  );
}

// SECURITY: without a database URL the app boots but every dashboard is
// silently empty. In production that is a misconfiguration, not a runtime
// state — fail fast at boot so deploys surface it immediately.
function resolveDatabaseUrl(): string {
  const url = process.env.POSTGRES_URL || process.env.DATABASE_URL || "";
  if (!url && process.env.NODE_ENV === "production") {
    throw new Error(
      "[ENV] POSTGRES_URL or DATABASE_URL must be set when NODE_ENV=production. " +
        "Refusing to boot: without a database the app would serve silently empty dashboards."
    );
  }
  return url;
}

export const ENV = {
  appId: process.env.VITE_APP_ID ?? "",
  cookieSecret: resolveCookieSecret(),
  databaseUrl: resolveDatabaseUrl(),
  oAuthServerUrl: process.env.OAUTH_SERVER_URL ?? "",
  ownerOpenId: process.env.OWNER_OPEN_ID ?? "",
  isProduction: process.env.NODE_ENV === "production",
  forgeApiUrl: process.env.BUILT_IN_FORGE_API_URL ?? "",
  forgeApiKey: process.env.BUILT_IN_FORGE_API_KEY ?? "",
};
