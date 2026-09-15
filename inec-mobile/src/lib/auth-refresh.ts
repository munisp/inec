/**
 * Pure token-refresh decision logic (no Expo imports — jest-testable).
 *
 * Backend contract (monolith POST /auth/refresh):
 *   request:  { refresh_token }
 *   200:      { access_token, refresh_token?, token_type, expires_in }
 *   401:      refresh token invalid/expired/revoked
 *
 * The client must tolerate BOTH refresh semantics (R5-108; W3 is rotating
 * the server contract concurrently):
 *   - OLD: response may omit `refresh_token` → the existing refresh token
 *     stays valid and is reused.
 *   - NEW (rotation): response carries a fresh `refresh_token` → it
 *     REPLACES the stored one (the old one may be revoked server-side).
 */

export interface RefreshTokens {
  accessToken: string;
  refreshToken: string;
}

export type RefreshFailure = 'http_error' | 'invalid_body';

export type RefreshParseResult =
  | { ok: true; tokens: RefreshTokens }
  | { ok: false; reason: RefreshFailure };

/**
 * Interpret a /auth/refresh response.
 *
 * @param status          HTTP status code.
 * @param body            Parsed JSON body (may be null/undefined on failure).
 * @param currentRefresh  The refresh token currently held by the client —
 *                        retained when the server does not rotate.
 */
export function parseRefreshResponse(
  status: number,
  body: unknown,
  currentRefresh: string,
): RefreshParseResult {
  if (status !== 200 || body == null || typeof body !== 'object') {
    return { ok: false, reason: 'http_error' };
  }
  const b = body as Record<string, unknown>;
  const access = b['access_token'];
  if (typeof access !== 'string' || access.length === 0) {
    return { ok: false, reason: 'invalid_body' };
  }
  // Rotation semantics: prefer the server's new refresh token; fall back to
  // the existing one when the server does not rotate (old semantics).
  const rotated = b['refresh_token'];
  const refreshToken =
    typeof rotated === 'string' && rotated.length > 0 ? rotated : currentRefresh;
  return { ok: true, tokens: { accessToken: access, refreshToken } };
}

/**
 * Whether a failed refresh means the session is unrecoverable and the user
 * must re-authenticate with credentials. Only an explicit rejection (401)
 * is terminal; network errors / 5xx are transient and must NOT log the
 * officer out mid–election-day.
 */
export function isTerminalRefreshFailure(status: number | null): boolean {
  return status === 401 || status === 400;
}
