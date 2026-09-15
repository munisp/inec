/**
 * Server-time synchronization (R5-115).
 *
 * Device wall-clocks can be hours skewed; any client-side timestamp used for
 * ordering/conflict decisions must be corrected against server time. The API
 * layer feeds every response's `Date` header here; queue code stamps items
 * with `correctedNowIso()` so a slow phone does not silently lose data.
 *
 * Pure module — no Expo imports (jest-testable).
 */

/** Maximum accepted offset: refuse to "correct" by more than 24h (a broken
 *  Date header must not send timestamps to epoch/2099). */
export const MAX_SANE_OFFSET_MS = 24 * 60 * 60 * 1000;

/**
 * Compute server−client offset in ms from an HTTP Date header.
 * Returns null when the header is missing/unparsable or the offset is
 * beyond the sanity bound (caller keeps the previous offset).
 */
export function computeClockOffsetMs(
  serverDateHeader: string | null | undefined,
  clientNowMs: number,
): number | null {
  if (!serverDateHeader) return null;
  const serverMs = Date.parse(serverDateHeader);
  if (!Number.isFinite(serverMs)) return null;
  const offset = serverMs - clientNowMs;
  if (Math.abs(offset) > MAX_SANE_OFFSET_MS) return null;
  return offset;
}

/** Apply an offset to a client timestamp (ms). */
export function applyClockOffset(clientNowMs: number, offsetMs: number): number {
  return clientNowMs + offsetMs;
}

// ── Stateful singleton fed by the API layer ──

let currentOffsetMs = 0;
let lastSampleAt = 0;

/** Called by the API layer with each response's Date header. */
export function observeServerDate(serverDateHeader: string | null | undefined): void {
  const offset = computeClockOffsetMs(serverDateHeader, Date.now());
  if (offset === null) return;
  currentOffsetMs = offset;
  lastSampleAt = Date.now();
}

/** Server-corrected current time (ms). Falls back to device time when no
 *  sample has been observed yet. */
export function correctedNowMs(): number {
  return applyClockOffset(Date.now(), currentOffsetMs);
}

/** Server-corrected current time as an ISO string (for queue timestamps). */
export function correctedNowIso(): string {
  return new Date(correctedNowMs()).toISOString();
}

/** Exposed for tests/diagnostics. */
export function getClockOffsetMs(): number {
  return currentOffsetMs;
}

export function getLastClockSampleAt(): number {
  return lastSampleAt;
}

/** Test hook: reset the observed offset. */
export function resetClockSync(): void {
  currentOffsetMs = 0;
  lastSampleAt = 0;
}
