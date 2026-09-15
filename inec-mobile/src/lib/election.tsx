/**
 * Shared election resolver for election-scoped mobile screens.
 *
 * Rules:
 *  - The election id is NEVER hardcoded. It resolves from an explicit user
 *    selection persisted to AsyncStorage, otherwise the latest ACTIVE election
 *    fetched from /elections.
 *  - Screens must gate rendering on a non-null electionId — while unresolved
 *    (loading or fetch failure) no election-scoped request may fire.
 */
import { useCallback, useEffect, useState } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { electionApi, type Election } from './api';
import { pickLatestActiveElection } from './election-picker';

export { pickLatestActiveElection };

const SELECTED_ELECTION_KEY = 'inec_selected_election_id';

export interface ResolvedElection {
  electionId: number | null;
  election: Election | null;
  elections: Election[];
  loading: boolean;
  error: string | null;
  /** Explicitly choose an election (persisted across launches). */
  selectElection: (id: number | null) => Promise<void>;
  reload: () => void;
}

export function useResolvedElection(): ResolvedElection {
  const [elections, setElections] = useState<Election[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      let persisted: number | null = null;
      try {
        const raw = await AsyncStorage.getItem(SELECTED_ELECTION_KEY);
        const id = raw ? Number(raw) : NaN;
        persisted = Number.isFinite(id) && id > 0 ? id : null;
      } catch { /* AsyncStorage unavailable — continue without persistence */ }
      try {
        const list = await electionApi.list();
        if (cancelled) return;
        const elections = Array.isArray(list) ? list : [];
        setElections(elections);
        const persistedValid =
          persisted !== null && elections.some((e) => e.id === persisted) ? persisted : null;
        setSelectedId(persistedValid ?? pickLatestActiveElection(elections)?.id ?? null);
      } catch (e) {
        if (cancelled) return;
        setElections([]);
        // Election list failed: trust the persisted choice rather than nothing.
        setSelectedId(persisted);
        setError(e instanceof Error ? e.message : 'elections-unavailable');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [reloadKey]);

  const selectElection = useCallback(async (id: number | null) => {
    setSelectedId(id);
    try {
      if (id && id > 0) await AsyncStorage.setItem(SELECTED_ELECTION_KEY, String(id));
      else await AsyncStorage.removeItem(SELECTED_ELECTION_KEY);
    } catch { /* persistence best-effort */ }
  }, []);

  const reload = useCallback(() => setReloadKey((k) => k + 1), []);

  return {
    electionId: selectedId,
    election: elections.find((e) => e.id === selectedId) ?? null,
    elections,
    loading,
    error,
    selectElection,
    reload,
  };
}
