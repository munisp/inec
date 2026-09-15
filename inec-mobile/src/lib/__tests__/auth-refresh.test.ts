import { parseRefreshResponse, isTerminalRefreshFailure } from '../auth-refresh';

describe('parseRefreshResponse (R5-108)', () => {
  it('NEW rotation semantics: rotated refresh_token replaces the stored one', () => {
    const r = parseRefreshResponse(
      200,
      { access_token: 'new-access', refresh_token: 'new-refresh', token_type: 'bearer' },
      'old-refresh',
    );
    expect(r).toEqual({ ok: true, tokens: { accessToken: 'new-access', refreshToken: 'new-refresh' } });
  });

  it('OLD semantics: no refresh_token in the body keeps the existing one', () => {
    const r = parseRefreshResponse(200, { access_token: 'new-access' }, 'old-refresh');
    expect(r).toEqual({ ok: true, tokens: { accessToken: 'new-access', refreshToken: 'old-refresh' } });
  });

  it('empty-string refresh_token is treated as absent (keep existing)', () => {
    const r = parseRefreshResponse(200, { access_token: 'a', refresh_token: '' }, 'keep-me');
    expect(r).toEqual({ ok: true, tokens: { accessToken: 'a', refreshToken: 'keep-me' } });
  });

  it('rejects non-200 responses', () => {
    expect(parseRefreshResponse(401, { error: 'expired' }, 'x')).toEqual({ ok: false, reason: 'http_error' });
    expect(parseRefreshResponse(500, null, 'x')).toEqual({ ok: false, reason: 'http_error' });
  });

  it('rejects a 200 without an access_token (contract violation)', () => {
    expect(parseRefreshResponse(200, { token_type: 'bearer' }, 'x')).toEqual({ ok: false, reason: 'invalid_body' });
    expect(parseRefreshResponse(200, 'not-an-object', 'x')).toEqual({ ok: false, reason: 'http_error' });
  });
});

describe('isTerminalRefreshFailure (R5-108)', () => {
  it('only explicit rejections are terminal — transient failures keep the session', () => {
    expect(isTerminalRefreshFailure(401)).toBe(true);
    expect(isTerminalRefreshFailure(400)).toBe(true);
    expect(isTerminalRefreshFailure(403)).toBe(false);
    expect(isTerminalRefreshFailure(500)).toBe(false);
    expect(isTerminalRefreshFailure(null)).toBe(false); // network error
  });
});
