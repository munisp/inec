/**
 * Pure election-selection logic (no Expo/RN imports — jest-testable).
 * Used by useResolvedElection (election.tsx).
 */
import type { Election } from './api-types';

/** Latest ACTIVE election by date; falls back to the most recent election. */
export function pickLatestActiveElection(elections: Election[]): Election | null {
  if (!elections.length) return null;
  const active = elections.filter((e) => (e.status ?? '').toLowerCase() === 'active');
  const pool = active.length > 0 ? active : elections;
  const sorted = [...pool].sort((a, b) => Date.parse(b.date ?? '') - Date.parse(a.date ?? ''));
  return sorted[0] ?? null;
}
