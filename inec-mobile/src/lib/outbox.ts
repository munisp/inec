/**
 * Pure outbox/sync-decision logic for the offline queue.
 *
 * This module has NO React Native / Expo imports so it is unit-testable
 * under plain jest (node environment). The SQLite-backed queue in
 * `src/lib/offline.ts` consumes these functions.
 *
 * Per-item outcome protocol (backend contract, R5-007 / W1):
 *   POST /ingestion/offline-sync →
 *   { items: [{ index, status, job_id?, idempotency_key?, error? }] }
 *   status ∈ queued | duplicate | rejected | conflict
 */

export interface SyncItemOutcome {
  index: number;
  status: string;
  job_id?: number | string;
  idempotency_key?: string;
  error?: string;
}

export type OutboxActionType = 'mark_synced' | 'mark_duplicate' | 'mark_failed' | 'mark_conflict';

export interface OutboxAction {
  /** Index into the submitted batch (matches SyncItemOutcome.index). */
  index: number;
  action: OutboxActionType;
  error?: string;
}

/**
 * Map the backend per-item outcome array onto outbox actions.
 *
 * - `queued`    → item accepted for processing; safe to mark synced.
 * - `duplicate` → server already has this item (idempotent replay); safe to
 *                 mark synced, but reported distinctly so callers can count
 *                 genuine duplicates instead of treating them as fresh syncs.
 * - `conflict`  → server-side newer version exists; the local item must NOT
 *                 be silently dropped — it is parked for review (conflict
 *                 log) rather than deleted (R5-109/R5-115).
 * - `rejected`/unknown → keep in the outbox with retry state and the error.
 *
 * Outcomes referencing an out-of-range index are ignored (never trust the
 * server to be well-formed), and indices with no outcome are left untouched
 * (the caller keeps them pending for the next drain).
 */
export function mapItemOutcomes(
  items: readonly SyncItemOutcome[] | null | undefined,
  batchSize: number,
): OutboxAction[] {
  if (!Array.isArray(items) || batchSize <= 0) return [];
  const actions: OutboxAction[] = [];
  const seen = new Set<number>();
  for (const item of items) {
    if (!item || typeof item.index !== 'number') continue;
    const index = item.index;
    if (!Number.isInteger(index) || index < 0 || index >= batchSize) continue;
    if (seen.has(index)) continue; // first outcome wins — never double-apply
    seen.add(index);
    switch (item.status) {
      case 'queued':
        actions.push({ index, action: 'mark_synced' });
        break;
      case 'duplicate':
        actions.push({ index, action: 'mark_duplicate' });
        break;
      case 'conflict':
        actions.push({ index, action: 'mark_conflict', error: item.error });
        break;
      case 'rejected':
      default:
        actions.push({ index, action: 'mark_failed', error: item.error ?? 'rejected' });
        break;
    }
  }
  return actions;
}

export interface RetryState {
  attempts: number;
  /** True once the item has exhausted retries and needs manual attention. */
  exhausted: boolean;
}

/**
 * Retry-state transition for a failed outbox item (skip-and-continue).
 * A failed item never blocks the rest of the queue (R5-007); it accrues
 * attempts and is retried on the next drain until `maxAttempts`, after
 * which it stays in the queue flagged `exhausted` for manual review instead
 * of being silently discarded.
 */
export function nextRetryState(currentAttempts: number, maxAttempts = 10): RetryState {
  const attempts = Math.max(0, currentAttempts) + 1;
  return { attempts, exhausted: attempts >= maxAttempts };
}

/**
 * Whether an outbox item should be attempted in this drain pass.
 * Exhausted items are skipped (but retained) so one poisoned item can never
 * wedge the queue head-of-line.
 */
export function shouldAttempt(attempts: number | null | undefined, maxAttempts = 10): boolean {
  return (attempts ?? 0) < maxAttempts;
}
