import {
  mapItemOutcomes, nextRetryState, shouldAttempt, type SyncItemOutcome,
} from '../outbox';

describe('mapItemOutcomes (W1 per-item outcome protocol)', () => {
  it('maps each backend status onto the right outbox action', () => {
    const items: SyncItemOutcome[] = [
      { index: 0, status: 'queued', job_id: 11, idempotency_key: 'k0' },
      { index: 1, status: 'duplicate', job_id: 12, idempotency_key: 'k1' },
      { index: 2, status: 'rejected', idempotency_key: 'k2', error: 'timestamp too old' },
      { index: 3, status: 'conflict', idempotency_key: 'k3', error: 'server has newer' },
    ];
    expect(mapItemOutcomes(items, 4)).toEqual([
      { index: 0, action: 'mark_synced' },
      { index: 1, action: 'mark_duplicate' },
      { index: 2, action: 'mark_failed', error: 'timestamp too old' },
      { index: 3, action: 'mark_conflict', error: 'server has newer' },
    ]);
  });

  it('treats unknown statuses as failures with a default error', () => {
    expect(mapItemOutcomes([{ index: 0, status: 'bogus' }], 1)).toEqual([
      { index: 0, action: 'mark_failed', error: 'rejected' },
    ]);
  });

  it('ignores out-of-range, non-integer and duplicate indices (untrusted server)', () => {
    const items: SyncItemOutcome[] = [
      { index: -1, status: 'queued' },
      { index: 5, status: 'queued' },      // out of range for batchSize 2
      { index: 0.5, status: 'queued' },    // non-integer
      { index: 0, status: 'queued' },
      { index: 0, status: 'rejected' },    // duplicate index — first wins
    ];
    expect(mapItemOutcomes(items, 2)).toEqual([{ index: 0, action: 'mark_synced' }]);
  });

  it('returns no actions for missing/empty outcome arrays', () => {
    expect(mapItemOutcomes(undefined, 3)).toEqual([]);
    expect(mapItemOutcomes(null, 3)).toEqual([]);
    expect(mapItemOutcomes([], 3)).toEqual([]);
  });

  it('never fabricates outcomes for indices the server did not report', () => {
    const actions = mapItemOutcomes([{ index: 1, status: 'queued' }], 3);
    expect(actions).toEqual([{ index: 1, action: 'mark_synced' }]);
    // index 0 and 2 stay pending — the caller must not mark them.
  });
});

describe('retry state (skip-and-continue, R5-007)', () => {
  it('increments attempts and exhausts at the cap', () => {
    expect(nextRetryState(0, 3)).toEqual({ attempts: 1, exhausted: false });
    expect(nextRetryState(2, 3)).toEqual({ attempts: 3, exhausted: true });
  });

  it('clamps negative attempt counts', () => {
    expect(nextRetryState(-4, 10).attempts).toBe(1);
  });

  it('shouldAttempt keeps failed items eligible until the cap, then parks them', () => {
    expect(shouldAttempt(0)).toBe(true);
    expect(shouldAttempt(9)).toBe(true);
    expect(shouldAttempt(10)).toBe(false);
    expect(shouldAttempt(null)).toBe(true);
    expect(shouldAttempt(undefined)).toBe(true);
  });
});
