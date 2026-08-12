/**
 * gotv-session.tsx — shared tenancy/identity resolution for GOTV and election-scoped pages.
 *
 * Rules enforced here:
 *  - The GOTV party code is NEVER defaulted. It comes from an explicit user
 *    selection persisted to localStorage ('gotv_party_code'). Pages must require
 *    a selection before loading party-scoped data.
 *  - A real Bearer token (stored by the auth flow as 'auth_token') is attached
 *    whenever available; X-GOTV-Party-Code is supplementary tenancy info only.
 *  - Election scoping resolves through the elections store: an explicit user
 *    selection (persisted to 'inec_selected_election_id'), otherwise the latest
 *    ACTIVE election from the API — never a silent hardcoded id.
 */
import { useEffect, useState } from 'react';
import { useElectionsStore } from '@/store/elections';

export const GOTV_PARTY_CODE_KEY = 'gotv_party_code';
export const SELECTED_ELECTION_ID_KEY = 'inec_selected_election_id';

const PARTY_CHANGED_EVENT = 'gotv-party-changed';
const ELECTION_CHANGED_EVENT = 'inec-election-changed';

/** Registered parties offered by the GOTV party selector (no default is applied). */
export const GOTV_PARTY_OPTIONS: { code: string; name: string }[] = [
  { code: 'APC', name: 'All Progressives Congress' },
  { code: 'PDP', name: 'Peoples Democratic Party' },
  { code: 'LP', name: 'Labour Party' },
  { code: 'NNPP', name: 'New Nigeria Peoples Party' },
  { code: 'ADC', name: 'African Democratic Congress' },
];

/** Real session token, or null when the session is cookie-only / absent. */
export function getAuthToken(): string | null {
  const token = localStorage.getItem('auth_token');
  // auth.tsx stores the literal marker 'httponly-cookie' for cookie-only sessions.
  return token && token !== 'httponly-cookie' ? token : null;
}

/** Authorization header for the current session, when a real token exists. */
export function getSessionAuthHeaders(): Record<string, string> {
  const token = getAuthToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** Explicitly selected GOTV party code, or null when none has been chosen. */
export function getGOTVPartyCode(): string | null {
  const code = localStorage.getItem(GOTV_PARTY_CODE_KEY);
  return code && code.trim() ? code.trim().toUpperCase() : null;
}

export function setGOTVPartyCode(code: string | null): void {
  if (code && code.trim()) {
    localStorage.setItem(GOTV_PARTY_CODE_KEY, code.trim().toUpperCase());
  } else {
    localStorage.removeItem(GOTV_PARTY_CODE_KEY);
  }
  window.dispatchEvent(new Event(PARTY_CHANGED_EVENT));
}

/**
 * Headers for GOTV microservice calls: real Bearer token when available,
 * plus the selected party code as supplementary tenancy info (never defaulted).
 */
export function gotvAuthHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const headers: Record<string, string> = { ...getSessionAuthHeaders(), ...extra };
  const party = getGOTVPartyCode();
  if (party) headers['X-GOTV-Party-Code'] = party;
  return headers;
}

/** React hook: [selectedPartyCode, setSelectedPartyCode], synced across components. */
export function useGOTVParty(): [string | null, (code: string | null) => void] {
  const [party, setParty] = useState<string | null>(() => getGOTVPartyCode());
  useEffect(() => {
    const sync = () => setParty(getGOTVPartyCode());
    window.addEventListener(PARTY_CHANGED_EVENT, sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener(PARTY_CHANGED_EVENT, sync);
      window.removeEventListener('storage', sync);
    };
  }, []);
  return [party, setGOTVPartyCode];
}

/** Party selector for page headers. Requires an explicit choice — no default. */
export function GOTVPartySelector({ className = '' }: { className?: string }) {
  const [party] = useGOTVParty();
  return (
    <label className={`flex items-center gap-2 text-sm ${className}`}>
      <span className="text-muted-foreground">Party</span>
      <select
        aria-label="GOTV party"
        className="h-9 rounded-md border border-input bg-background px-2 text-sm"
        value={party ?? ''}
        onChange={(e) => setGOTVPartyCode(e.target.value || null)}
      >
        <option value="" disabled>
          Select party…
        </option>
        {GOTV_PARTY_OPTIONS.map((p) => (
          <option key={p.code} value={p.code}>
            {p.code} — {p.name}
          </option>
        ))}
      </select>
    </label>
  );
}

// ─── Election resolution ────────────────────────────────────────────────────

interface ElectionLike {
  id: number;
  election_date?: string;
  status?: string;
}

/** Latest active election by date; falls back to the most recent election overall. */
export function pickLatestActiveElection<T extends ElectionLike>(elections: T[]): T | null {
  if (!elections.length) return null;
  const active = elections.filter((e) => (e.status ?? '').toLowerCase() === 'active');
  const pool = active.length > 0 ? active : elections;
  const sorted = [...pool].sort(
    (a, b) => Date.parse(b.election_date ?? '') - Date.parse(a.election_date ?? ''),
  );
  return sorted[0] ?? null;
}

export function getPersistedElectionId(): number | null {
  const raw = localStorage.getItem(SELECTED_ELECTION_ID_KEY);
  const id = raw ? Number(raw) : NaN;
  return Number.isFinite(id) && id > 0 ? id : null;
}

export function setPersistedElectionId(id: number | null): void {
  if (id && id > 0) localStorage.setItem(SELECTED_ELECTION_ID_KEY, String(id));
  else localStorage.removeItem(SELECTED_ELECTION_ID_KEY);
  window.dispatchEvent(new Event(ELECTION_CHANGED_EVENT));
}

/**
 * Resolves the election an election-scoped page should use:
 * explicit selection (store/persisted) → latest active election from the API.
 * Returns null while nothing can be resolved — callers must not silently
 * substitute a hardcoded id.
 */
export function useResolvedElection() {
  const { elections, selectedElection, loading, error, fetchElections, selectElection } =
    useElectionsStore();
  const [persistedId, setPersistedId] = useState<number | null>(() => getPersistedElectionId());

  useEffect(() => {
    if (elections.length === 0 && !loading) {
      fetchElections().catch(() => undefined);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const sync = () => setPersistedId(getPersistedElectionId());
    window.addEventListener(ELECTION_CHANGED_EVENT, sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener(ELECTION_CHANGED_EVENT, sync);
      window.removeEventListener('storage', sync);
    };
  }, []);

  const persistedValid =
    persistedId !== null && elections.some((e) => e.id === persistedId) ? persistedId : null;
  const electionId =
    selectedElection?.id ??
    persistedValid ??
    pickLatestActiveElection(elections)?.id ??
    // Election list not loaded yet (or failed): trust the persisted choice rather than nothing.
    (elections.length === 0 ? persistedId : null);

  const choose = (id: number | null) => {
    setPersistedElectionId(id);
    selectElection(id !== null ? elections.find((e) => e.id === id) ?? null : null);
  };

  return { electionId, elections, loading, error, selectElection: choose };
}
